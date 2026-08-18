package storage

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
)

const backupManifestName = "backup-manifest.json"

const (
	defaultMaxBackupEntries = 100_000
	maxBackupManifestBytes  = 16 << 20
	maxBackupEntryBytes     = 64 << 20
	maxBackupPathBytes      = 512
	maxBackupPathDepth      = 4
)

var maximumBackupEntries = defaultMaxBackupEntries

var renameStoragePath = os.Rename

// BackupManifest describes every file in one portable backup.
type BackupManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	CreatedAt     time.Time         `json:"createdAt"`
	Files         map[string]string `json:"files"`
}

type backupFile struct {
	Relative string
	Path     string
	Size     int64
	Checksum string
}

// ExportBackup writes a verified tar archive to writer.
func ExportBackup(writer io.Writer) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		return err
	}
	if report := verifyStorageRoot(DataDir); !report.Healthy {
		return fmt.Errorf("%w: active storage failed integrity verification: %s", ErrCorrupt, report.Issues[0].Problem)
	}
	files, err := collectBackupFiles(DataDir)
	if err != nil {
		return err
	}
	manifest := BackupManifest{SchemaVersion: 1, CreatedAt: time.Now().UTC(), Files: make(map[string]string, len(files))}
	for _, file := range files {
		manifest.Files[file.Relative] = file.Checksum
	}
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestContent = append(manifestContent, '\n')
	total := tarRecordSize(int64(len(manifestContent))) + 2*tarBlockSize
	for _, file := range files {
		total += tarRecordSize(file.Size)
	}
	limit := GetStorageLimits().MaxBackupBytes
	if limit > 0 && total > limit {
		return fmt.Errorf("%w: backup exceeds %d bytes", ErrQuotaExceeded, limit)
	}

	tarWriter := tar.NewWriter(writer)
	if err := writeTarFile(tarWriter, backupManifestName, manifestContent, time.Now()); err != nil {
		_ = tarWriter.Close()
		return err
	}
	for _, file := range files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			_ = tarWriter.Close()
			return err
		}
		if checksumBytes(content) != file.Checksum {
			_ = tarWriter.Close()
			return fmt.Errorf("%w: file changed during backup: %s", ErrCorrupt, file.Relative)
		}
		if err := writeTarFile(tarWriter, file.Relative, content, time.Now()); err != nil {
			_ = tarWriter.Close()
			return err
		}
	}
	return tarWriter.Close()
}

const tarBlockSize int64 = 512

func tarRecordSize(contentSize int64) int64 {
	return tarBlockSize + ((contentSize+tarBlockSize-1)/tarBlockSize)*tarBlockSize
}

func collectBackupFiles(root string) ([]backupFile, error) {
	result := make([]backupFile, 0)
	for _, directory := range []string{itemsDirectoryName, revisionsDirectory} {
		base := filepath.Join(root, directory)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: backup contains a symbolic link", ErrCorrupt)
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("%w: backup contains a non-regular file", ErrCorrupt)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			result = append(result, backupFile{Relative: filepath.ToSlash(relative), Path: path, Size: info.Size(), Checksum: checksumBytes(content)})
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Relative < result[right].Relative })
	return result, nil
}

func writeTarFile(writer *tar.Writer, name string, content []byte, modified time.Time) error {
	header := &tar.Header{Name: name, Mode: int64(dataFileMode), Size: int64(len(content)), ModTime: modified}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(content)
	return err
}

// ImportBackup merges a verified backup. Existing items require overwrite=true.
func ImportBackup(reader io.Reader, overwrite bool) error {
	return applyBackup(reader, overwrite, false)
}

// RestoreBackup atomically replaces the canonical item and revision trees.
func RestoreBackup(reader io.Reader) error {
	return applyBackup(reader, true, true)
}

func applyBackup(reader io.Reader, overwrite, replace bool) error {
	mutationMu.Lock()
	if err := ensureStorageLayout(); err != nil {
		mutationMu.Unlock()
		return err
	}
	stage, err := os.MkdirTemp(DataDir, ".restore-*")
	if err != nil {
		mutationMu.Unlock()
		return err
	}
	defer os.RemoveAll(stage)
	err = extractBackup(reader, stage)
	if err == nil {
		for _, path := range []string{
			filepath.Join(stage, itemsDirectoryName, "pastes"),
			filepath.Join(stage, itemsDirectoryName, "diffs"),
			filepath.Join(stage, revisionsDirectory, "pastes"),
			filepath.Join(stage, revisionsDirectory, "diffs"),
		} {
			if mkdirErr := os.MkdirAll(path, dataDirMode); mkdirErr != nil {
				err = mkdirErr
				break
			}
		}
	}
	if err == nil {
		report := verifyStorageRoot(stage)
		if !report.Healthy {
			err = fmt.Errorf("%w: restored archive failed integrity verification: %s", ErrCorrupt, report.Issues[0].Problem)
		} else if replace {
			err = checkStorageRootQuotas(stage)
		}
	}
	if err == nil {
		if replace {
			err = replaceStorageTrees(stage)
		} else {
			err = mergeStorageTrees(stage, overwrite)
		}
	}
	mutationMu.Unlock()
	if err != nil {
		return err
	}
	resetUsage()
	LoadCacheFromDisk()
	LoadDiffCacheFromDisk()
	return nil
}

func extractBackup(reader io.Reader, stage string) error {
	limit := GetStorageLimits().MaxBackupBytes
	var limited *io.LimitedReader
	if limit > 0 {
		limited = &io.LimitedReader{R: reader, N: limit + 1}
		reader = limited
	}
	tarReader := tar.NewReader(reader)
	var manifest *BackupManifest
	seen := make(map[string]string)
	entryCount := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if backupLimitExceeded(limited) {
				return fmt.Errorf("%w: archive is too large", ErrQuotaExceeded)
			}
			return err
		}
		entryCount++
		if entryCount > maximumBackupEntries+1 {
			return fmt.Errorf("%w: archive contains too many entries", ErrQuotaExceeded)
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("%w: non-regular archive entry", ErrCorrupt)
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if name == "." || len(name) > maxBackupPathBytes || strings.HasPrefix(name, "../") || filepath.IsAbs(header.Name) {
			return fmt.Errorf("%w: unsafe archive path", ErrCorrupt)
		}
		if _, duplicate := seen[name]; duplicate || name == backupManifestName && manifest != nil {
			return fmt.Errorf("%w: duplicate archive entry", ErrCorrupt)
		}
		maximum := maximumBackupEntrySize(name)
		if header.Size < 0 || header.Size > maximum {
			return fmt.Errorf("%w: archive entry is too large", ErrQuotaExceeded)
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(content)) != header.Size {
			if backupLimitExceeded(limited) {
				return fmt.Errorf("%w: archive is too large", ErrQuotaExceeded)
			}
			return fmt.Errorf("%w: truncated archive entry", ErrCorrupt)
		}
		if name == backupManifestName {
			if entryCount != 1 {
				return fmt.Errorf("%w: backup manifest must be the first entry", ErrCorrupt)
			}
			var decoded BackupManifest
			if err := json.Unmarshal(content, &decoded); err != nil || decoded.SchemaVersion != 1 {
				return fmt.Errorf("%w: invalid backup manifest", ErrCorrupt)
			}
			if len(decoded.Files) > maximumBackupEntries {
				return fmt.Errorf("%w: backup manifest contains too many entries", ErrQuotaExceeded)
			}
			manifest = &decoded
			continue
		}
		if manifest == nil {
			return fmt.Errorf("%w: missing leading backup manifest", ErrCorrupt)
		}
		if !validBackupEntryPath(name) {
			return fmt.Errorf("%w: unsupported archive path", ErrCorrupt)
		}
		if _, declared := manifest.Files[name]; !declared {
			return fmt.Errorf("%w: undeclared archive entry", ErrCorrupt)
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if !strings.HasPrefix(target, stage+string(os.PathSeparator)) {
			return fmt.Errorf("%w: unsafe archive path", ErrCorrupt)
		}
		if err := reconcileCommittedFile(target, content, createFileExclusive(target, content)); err != nil {
			return err
		}
		seen[name] = checksumBytes(content)
	}
	if limited != nil {
		_, _ = io.Copy(io.Discard, limited)
		if backupLimitExceeded(limited) {
			return fmt.Errorf("%w: archive is too large", ErrQuotaExceeded)
		}
	}
	if manifest == nil {
		return fmt.Errorf("%w: missing backup manifest", ErrCorrupt)
	}
	if len(seen) != len(manifest.Files) {
		return fmt.Errorf("%w: backup file count mismatch", ErrCorrupt)
	}
	for name, checksum := range manifest.Files {
		if seen[name] != checksum {
			return fmt.Errorf("%w: checksum mismatch for %s", ErrCorrupt, name)
		}
	}
	return nil
}

func backupLimitExceeded(reader *io.LimitedReader) bool {
	return reader != nil && reader.N == 0
}

func maximumBackupEntrySize(name string) int64 {
	if name == backupManifestName {
		return maxBackupManifestBytes
	}
	if filepath.Base(name) == metadataFileName {
		return 1 << 20
	}
	configured := GetStorageLimits().MaxItemBytes
	if configured <= 0 || configured > maxBackupEntryBytes {
		configured = maxBackupEntryBytes
	}
	if strings.HasPrefix(name, revisionsDirectory+"/") {
		configured = configured*2 + 1<<20
		if configured > maxBackupEntryBytes {
			configured = maxBackupEntryBytes
		}
	}
	return configured
}

func validBackupEntryPath(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) != maxBackupPathDepth || (parts[0] != itemsDirectoryName && parts[0] != revisionsDirectory) {
		return false
	}
	if parts[1] != "pastes" && parts[1] != "diffs" || !validStorageID(parts[2]) {
		return false
	}
	return parts[3] != "" && parts[3] != "." && parts[3] != ".."
}

func mergeStorageTrees(stage string, overwrite bool) error {
	importedItems, err := backupItemIDs(stage)
	if err != nil {
		return err
	}
	if !overwrite {
		for kind, ids := range importedItems {
			for _, id := range ids {
				if _, err := os.Stat(itemDirectory(kind, id)); err == nil {
					return fs.ErrExist
				} else if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
		}
	}
	combined, err := os.MkdirTemp(DataDir, ".import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(combined)
	for _, path := range []string{
		filepath.Join(combined, itemsDirectoryName, "pastes"),
		filepath.Join(combined, itemsDirectoryName, "diffs"),
		filepath.Join(combined, revisionsDirectory, "pastes"),
		filepath.Join(combined, revisionsDirectory, "diffs"),
	} {
		if err := os.MkdirAll(path, dataDirMode); err != nil {
			return err
		}
	}
	if err := copyBackupTree(DataDir, combined); err != nil {
		return err
	}
	if overwrite {
		for kind, ids := range importedItems {
			for _, id := range ids {
				if err := os.RemoveAll(filepath.Join(combined, itemsDirectoryName, string(kind)+"s", id)); err != nil {
					return err
				}
				if err := os.RemoveAll(filepath.Join(combined, revisionsDirectory, string(kind)+"s", id)); err != nil {
					return err
				}
			}
		}
	}
	if err := copyBackupTree(stage, combined); err != nil {
		return err
	}
	if report := verifyStorageRoot(combined); !report.Healthy {
		return fmt.Errorf("%w: combined import failed integrity verification: %s", ErrCorrupt, report.Issues[0].Problem)
	}
	if err := checkStorageRootQuotas(combined); err != nil {
		return err
	}
	return replaceStorageTrees(combined)
}

func backupItemIDs(root string) (map[models.ItemKind][]string, error) {
	result := make(map[models.ItemKind][]string)
	for _, kind := range []models.ItemKind{models.ItemKindPaste, models.ItemKindDiff} {
		base := filepath.Join(root, itemsDirectoryName, string(kind)+"s")
		entries, err := os.ReadDir(base)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() && validStorageID(entry.Name()) {
				result[kind] = append(result[kind], entry.Name())
			}
		}
	}
	return result, nil
}

func copyBackupTree(sourceRoot, targetRoot string) error {
	files, err := collectBackupFiles(sourceRoot)
	if err != nil {
		return err
	}
	for _, file := range files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetRoot, filepath.FromSlash(file.Relative))
		if err := reconcileCommittedFile(target, content, replaceFileAtomically(target, content, time.Time{})); err != nil {
			return err
		}
	}
	return nil
}

func checkStorageRootQuotas(root string) error {
	configured := GetStorageLimits()
	var total int64
	var count int
	for _, kind := range []models.ItemKind{models.ItemKindPaste, models.ItemKindDiff} {
		base := filepath.Join(root, itemsDirectoryName, string(kind)+"s")
		entries, err := os.ReadDir(base)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			content, err := os.ReadFile(filepath.Join(base, entry.Name(), metadataFileName))
			if err != nil {
				return err
			}
			var metadata models.ItemMetadata
			if err := json.Unmarshal(content, &metadata); err != nil {
				return fmt.Errorf("%w: invalid metadata during quota check", ErrCorrupt)
			}
			if configured.MaxItemBytes > 0 && metadata.Size > configured.MaxItemBytes {
				return fmt.Errorf("%w: item %s exceeds %d bytes", ErrQuotaExceeded, metadata.ID, configured.MaxItemBytes)
			}
			total += metadata.Size
			count++
		}
	}
	revisionBase := filepath.Join(root, revisionsDirectory)
	err := filepath.WalkDir(revisionBase, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if configured.MaxItems > 0 && count > configured.MaxItems {
		return fmt.Errorf("%w: item count exceeds %d", ErrQuotaExceeded, configured.MaxItems)
	}
	if configured.MaxTotalBytes > 0 && total > configured.MaxTotalBytes {
		return fmt.Errorf("%w: stored content and revisions exceed %d bytes", ErrQuotaExceeded, configured.MaxTotalBytes)
	}
	return nil
}

func replaceStorageTrees(stage string) error {
	stamp, err := generateStorageID()
	if err != nil {
		return err
	}
	names := []string{itemsDirectoryName, revisionsDirectory}
	journalPath := filepath.Join(DataDir, swapJournalName)
	if err := writeStorageJournal(swapJournalName, swapJournal{Stamp: stamp}); err != nil {
		return err
	}
	clearStorageCaches()
	backups := make(map[string]string, len(names))
	for _, name := range names {
		staged := filepath.Join(stage, name)
		if err := os.MkdirAll(staged, dataDirMode); err != nil {
			return err
		}
		current := filepath.Join(DataDir, name)
		backup := filepath.Join(DataDir, ".old-"+name+"-"+stamp)
		if err := renameStoragePath(current, backup); err != nil {
			return errors.Join(err, rollbackStorageTrees(backups, nil, journalPath))
		}
		backups[name] = backup
	}
	installed := make(map[string]bool, len(names))
	for _, name := range names {
		if err := renameStoragePath(filepath.Join(stage, name), filepath.Join(DataDir, name)); err != nil {
			return errors.Join(err, rollbackStorageTrees(backups, installed, journalPath))
		}
		installed[name] = true
	}
	if err := syncDirectory(DataDir); err != nil {
		return errors.Join(err, rollbackStorageTrees(backups, installed, journalPath))
	}
	if err := removeFileDurably(journalPath); err != nil {
		if _, statErr := os.Lstat(journalPath); !errors.Is(statErr, fs.ErrNotExist) {
			return errors.Join(err, rollbackStorageTrees(backups, installed, journalPath))
		}
	}
	for _, backup := range backups {
		_ = os.RemoveAll(backup)
	}
	_ = syncDirectory(DataDir)
	return nil
}

func rollbackStorageTrees(backups map[string]string, installed map[string]bool, journalPath string) error {
	var rollbackErrors []error
	for name, backup := range backups {
		current := filepath.Join(DataDir, name)
		if installed[name] {
			if err := os.RemoveAll(current); err != nil {
				rollbackErrors = append(rollbackErrors, err)
				continue
			}
		}
		if err := renameStoragePath(backup, current); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	if err := syncDirectory(DataDir); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	if len(rollbackErrors) == 0 {
		if err := removeFileDurably(journalPath); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	return errors.Join(rollbackErrors...)
}

// IntegrityIssue describes one corrupt or unsafe stored path.
type IntegrityIssue struct {
	Kind    models.ItemKind `json:"kind,omitempty"`
	ID      string          `json:"id,omitempty"`
	Path    string          `json:"path"`
	Problem string          `json:"problem"`
}

// IntegrityReport summarizes a complete canonical storage scan.
type IntegrityReport struct {
	Healthy   bool             `json:"healthy"`
	Items     int              `json:"items"`
	Revisions int              `json:"revisions"`
	Bytes     int64            `json:"bytes"`
	Issues    []IntegrityIssue `json:"issues"`
}

// VerifyIntegrity validates metadata, content, checksums, revisions, and paths.
func VerifyIntegrity() IntegrityReport {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return verifyStorageRoot(DataDir)
}

func verifyStorageRoot(root string) IntegrityReport {
	report := IntegrityReport{Healthy: true, Issues: []IntegrityIssue{}}
	addIssue := func(issue IntegrityIssue) { report.Healthy = false; report.Issues = append(report.Issues, issue) }
	activeItems := make(map[string]struct{})
	for _, kind := range []models.ItemKind{models.ItemKindPaste, models.ItemKindDiff} {
		base := filepath.Join(root, itemsDirectoryName, string(kind)+"s")
		entries, err := os.ReadDir(base)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				addIssue(IntegrityIssue{Kind: kind, Path: base, Problem: err.Error()})
			}
			continue
		}
		for _, entry := range entries {
			id := entry.Name()
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !validStorageID(id) {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: filepath.Join(base, id), Problem: "invalid item directory"})
				continue
			}
			metaPath := filepath.Join(base, id, metadataFileName)
			if info, err := os.Lstat(metaPath); err != nil || !info.Mode().IsRegular() {
				problem := "metadata is not a regular file"
				if err != nil {
					problem = err.Error()
				}
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: metaPath, Problem: problem})
				continue
			}
			content, err := os.ReadFile(metaPath)
			if err != nil {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: metaPath, Problem: err.Error()})
				continue
			}
			var metadata models.ItemMetadata
			if err := json.Unmarshal(content, &metadata); err != nil || metadata.ID != id || metadata.Kind != kind {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: metaPath, Problem: "invalid metadata"})
				continue
			}
			if err := normalizeMetadata(&metadata); err != nil {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: metaPath, Problem: err.Error()})
				continue
			}
			dataPath := filepath.Join(base, id, metadata.DataFile)
			if info, err := os.Lstat(dataPath); err != nil || !info.Mode().IsRegular() {
				problem := "content is not a regular file"
				if err != nil {
					problem = err.Error()
				}
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: dataPath, Problem: problem})
				continue
			}
			data, err := os.ReadFile(dataPath)
			if err != nil {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: dataPath, Problem: err.Error()})
				continue
			}
			if int64(len(data)) != metadata.Size || checksumBytes(data) != metadata.Checksum {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: dataPath, Problem: "size or checksum mismatch"})
				continue
			}
			itemEntries, err := os.ReadDir(filepath.Join(base, id))
			if err != nil {
				addIssue(IntegrityIssue{Kind: kind, ID: id, Path: filepath.Join(base, id), Problem: err.Error()})
				continue
			}
			for _, itemEntry := range itemEntries {
				if itemEntry.Name() != metadataFileName && itemEntry.Name() != metadata.DataFile {
					addIssue(IntegrityIssue{Kind: kind, ID: id, Path: filepath.Join(base, id, itemEntry.Name()), Problem: "unreferenced item file"})
				}
			}
			activeItems[string(kind)+"/"+id] = struct{}{}
			report.Items++
			report.Bytes += int64(len(data))
		}
	}
	revisionBase := filepath.Join(root, revisionsDirectory)
	_ = filepath.WalkDir(revisionBase, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			addIssue(IntegrityIssue{Path: path, Problem: err.Error()})
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			addIssue(IntegrityIssue{Path: path, Problem: "symbolic link"})
			return nil
		}
		relative, relErr := filepath.Rel(revisionBase, path)
		if relErr != nil {
			addIssue(IntegrityIssue{Path: path, Problem: relErr.Error()})
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if entry.IsDir() {
			if len(parts) == 2 && parts[0] != "." {
				kindName := strings.TrimSuffix(parts[0], "s")
				if _, exists := activeItems[kindName+"/"+parts[1]]; !exists {
					addIssue(IntegrityIssue{Path: path, Problem: "orphan revision directory"})
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() || len(parts) != 3 || filepath.Ext(path) != ".json" {
			addIssue(IntegrityIssue{Path: path, Problem: "invalid revision path"})
			return nil
		}
		var kind models.ItemKind
		switch parts[0] {
		case "pastes":
			kind = models.ItemKindPaste
		case "diffs":
			kind = models.ItemKindDiff
		default:
			addIssue(IntegrityIssue{Path: path, Problem: "invalid revision kind"})
			return nil
		}
		id := parts[1]
		revisionNumber, valid := revisionNumberFromName(parts[2])
		if !valid || !validStorageID(id) {
			addIssue(IntegrityIssue{Kind: kind, ID: id, Path: path, Problem: "invalid revision identity"})
			return nil
		}
		if _, exists := activeItems[string(kind)+"/"+id]; !exists {
			addIssue(IntegrityIssue{Kind: kind, ID: id, Path: path, Problem: "orphan revision"})
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			addIssue(IntegrityIssue{Path: path, Problem: err.Error()})
			return nil
		}
		var revision revisionDocument
		if err := json.Unmarshal(content, &revision); err != nil ||
			revision.Metadata.Kind != kind || revision.Metadata.ID != id || revision.Metadata.Revision != revisionNumber ||
			int64(len(revision.Data)) != revision.Metadata.Size || checksumBytes(revision.Data) != revision.Metadata.Checksum {
			addIssue(IntegrityIssue{Path: path, Problem: "invalid revision"})
			return nil
		}
		report.Revisions++
		report.Bytes += int64(len(revision.Data))
		return nil
	})
	return report
}
