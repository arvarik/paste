package storage

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
)

func TestImportOverwriteReplacesCompleteItemHistory(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Import", "one", "text")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Import", "two", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"three", "four", "five"} {
		if err := UpdatePasteTrusted(id, "Import", content, "text", MetadataPatch{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ImportBackup(bytes.NewReader(archive.Bytes()), true); err != nil {
		t.Fatal(err)
	}
	paste, err := GetPaste(id)
	if err != nil || paste.Content != "two" || paste.Revision != 2 {
		t.Fatalf("imported paste = %#v, %v", paste, err)
	}
	revisions, err := ListRevisions(models.ItemKindPaste, id)
	if err != nil || len(revisions) != 1 || revisions[0].Revision != 1 {
		t.Fatalf("imported revisions = %#v, %v", revisions, err)
	}
	if report := VerifyIntegrity(); !report.Healthy {
		t.Fatalf("integrity = %#v", report)
	}
}

func TestRestoreBlocksCachedReadersDuringTreeSwap(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Restore", "backup", "text")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Restore", "current", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}

	originalRename := renameStoragePath
	installed := make(chan struct{})
	resume := make(chan struct{})
	blocked := false
	renameStoragePath = func(oldPath, newPath string) error {
		err := os.Rename(oldPath, newPath)
		if err == nil && !blocked && filepath.Base(oldPath) == itemsDirectoryName && newPath == filepath.Join(DataDir, itemsDirectoryName) && strings.Contains(oldPath, ".restore-") {
			blocked = true
			close(installed)
			<-resume
		}
		return err
	}
	t.Cleanup(func() { renameStoragePath = originalRename })

	restoreDone := make(chan error, 1)
	go func() { restoreDone <- RestoreBackup(bytes.NewReader(archive.Bytes())) }()
	<-installed
	readDone := make(chan string, 1)
	go func() {
		paste, readErr := GetPaste(id)
		if readErr != nil {
			readDone <- "error: " + readErr.Error()
			return
		}
		readDone <- paste.Content
	}()
	select {
	case result := <-readDone:
		t.Fatalf("read escaped the swap lock with %q", result)
	case <-time.After(25 * time.Millisecond):
	}
	close(resume)
	if err := <-restoreDone; err != nil {
		t.Fatal(err)
	}
	if result := <-readDone; result != "backup" {
		t.Fatalf("read after restore = %q", result)
	}
}

func TestRestoreAcceptsCommittedJournalRemoval(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Restore", "backup", "text")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Restore", "current", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	originalRemove := removeStoragePath
	originalSync := syncDirectory
	journalRemoved := false
	failed := false
	removeStoragePath = func(path string) error {
		err := originalRemove(path)
		if path == filepath.Join(DataDir, swapJournalName) && err == nil {
			journalRemoved = true
		}
		return err
	}
	syncDirectory = func(path string) error {
		if journalRemoved && !failed && path == DataDir {
			failed = true
			return errors.New("injected post-removal sync failure")
		}
		return originalSync(path)
	}
	t.Cleanup(func() {
		removeStoragePath = originalRemove
		syncDirectory = originalSync
	})
	if err := RestoreBackup(bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	paste, err := GetPaste(id)
	if err != nil || paste.Content != "backup" {
		t.Fatalf("restored paste = %#v, %v", paste, err)
	}
}

func TestDeleteRollsBackWhenDestinationDirectorySyncFails(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Delete", "safe", "text")
	if err != nil {
		t.Fatal(err)
	}
	originalSync := syncDirectory
	dataSyncs := 0
	syncDirectory = func(path string) error {
		if path == DataDir {
			dataSyncs++
			if dataSyncs == 2 {
				return errors.New("injected destination sync failure")
			}
		}
		return originalSync(path)
	}
	t.Cleanup(func() { syncDirectory = originalSync })
	if err := DeletePaste(id); err == nil {
		t.Fatal("DeletePaste() error = nil")
	}
	paste, err := GetPaste(id)
	if err != nil || paste.Content != "safe" {
		t.Fatalf("paste after rollback = %#v, %v", paste, err)
	}
}

func TestCommittedSyncErrorsReconcileFiles(t *testing.T) {
	t.Run("revision snapshot", func(t *testing.T) {
		setupStorageTest(t)
		id, err := CreatePaste("Sync", "one", "text")
		if err != nil {
			t.Fatal(err)
		}
		originalSync := syncDirectory
		failed := false
		syncDirectory = func(path string) error {
			if !failed && path == revisionRoot(models.ItemKindPaste, id) {
				failed = true
				return errors.New("injected revision sync failure")
			}
			return originalSync(path)
		}
		t.Cleanup(func() { syncDirectory = originalSync })
		if err := UpdatePasteTrusted(id, "Sync", "two", "text", MetadataPatch{}); err != nil {
			t.Fatal(err)
		}
		diskBytes, diskItems, err := storageUsage()
		if err != nil {
			t.Fatal(err)
		}
		usageMu.Lock()
		trackedBytes, trackedItems := usageBytes, usageItems
		usageMu.Unlock()
		if trackedBytes != diskBytes || trackedItems != diskItems {
			t.Fatalf("tracked usage = %d/%d, disk = %d/%d", trackedBytes, trackedItems, diskBytes, diskItems)
		}
	})

	t.Run("metadata replacement", func(t *testing.T) {
		setupStorageTest(t)
		id, err := CreatePaste("Sync", "one", "text")
		if err != nil {
			t.Fatal(err)
		}
		originalSync := syncDirectory
		itemSyncs := 0
		syncDirectory = func(path string) error {
			if path == itemDirectory(models.ItemKindPaste, id) {
				itemSyncs++
				if itemSyncs == 2 {
					return errors.New("injected metadata sync failure")
				}
			}
			return originalSync(path)
		}
		t.Cleanup(func() { syncDirectory = originalSync })
		if err := UpdatePasteTrusted(id, "Sync", "two", "text", MetadataPatch{}); err != nil {
			t.Fatal(err)
		}
		paste, err := GetPaste(id)
		if err != nil || paste.Content != "two" || paste.Revision != 2 {
			t.Fatalf("committed paste = %#v, %v", paste, err)
		}
		if report := VerifyIntegrity(); !report.Healthy {
			t.Fatalf("integrity = %#v", report)
		}
	})
}

func TestObsoleteContentRemovalFailureDoesNotBlockBackup(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Cleanup", "one", "text")
	if err != nil {
		t.Fatal(err)
	}
	originalRemove := removeStoragePath
	t.Cleanup(func() { removeStoragePath = originalRemove })
	removeStoragePath = func(path string) error {
		if strings.HasPrefix(filepath.Base(path), ".obsolete-") {
			return errors.New("injected obsolete removal failure")
		}
		return originalRemove(path)
	}
	if err := UpdatePasteTrusted(id, "Cleanup", "two", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	if report := VerifyIntegrity(); !report.Healthy {
		t.Fatalf("integrity = %#v", report)
	}
	var archive bytes.Buffer
	if err := ExportBackup(&archive); err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	removeStoragePath = originalRemove
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
}

func TestLazyMigrationUpdatesQuotaCounters(t *testing.T) {
	setupStorageTest(t)
	limits := GetStorageLimits()
	limits.MaxTotalBytes = 8
	limits.MaxItemBytes = 8
	SetStorageLimits(limits)
	if _, err := CreatePaste("Normal", "1234", "text"); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(DataDir, "abc123_Legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("5678"), dataFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPaste("abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePaste("Over", "x", "text"); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CreatePaste() error = %v, want quota error", err)
	}
}

func TestBurnItemsStayOutOfAllListAndSearchIndexes(t *testing.T) {
	setupStorageTest(t)
	if _, _, err := CreatePasteWithOptions("Burn paste", "needle", "text", CreateOptions{BurnAfterRead: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateDiffWithOptions("Burn diff", "text", "text", "needle", "other", CreateOptions{BurnAfterRead: true}); err != nil {
		t.Fatal(err)
	}
	if page, err := ListPastesPage("", 10); err != nil || len(page.Items) != 0 {
		t.Fatalf("paste list = %#v, %v", page, err)
	}
	if page, err := QueryPastesPage("needle", ItemFilter{}, "", 10); err != nil || len(page.Items) != 0 {
		t.Fatalf("paste search = %#v, %v", page, err)
	}
	if page, err := ListDiffsPage("", 10); err != nil || len(page.Items) != 0 {
		t.Fatalf("diff list = %#v, %v", page, err)
	}
	if page, err := QueryDiffsPage("needle", ItemFilter{}, "", 10); err != nil || len(page.Items) != 0 {
		t.Fatalf("diff search = %#v, %v", page, err)
	}
}

func TestRevisionReadRejectsFalseSize(t *testing.T) {
	setupStorageTest(t)
	id, err := CreatePaste("Revision", "one", "text")
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdatePasteTrusted(id, "Revision", "two", "text", MetadataPatch{}); err != nil {
		t.Fatal(err)
	}
	path := revisionPath(models.ItemKindPaste, id, 1)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document revisionDocument
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	document.Metadata.Size++
	content, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, dataFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := GetRevision(models.ItemKindPaste, id, 1); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("GetRevision() error = %v, want corruption", err)
	}
}

func TestBackupImportResourceLimits(t *testing.T) {
	t.Run("manifest size", func(t *testing.T) {
		setupStorageTest(t)
		archive := headerOnlyArchive(t, backupManifestName, maxBackupManifestBytes+1)
		if err := ImportBackup(bytes.NewReader(archive), false); !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("ImportBackup() error = %v, want quota error", err)
		}
	})

	t.Run("entry size", func(t *testing.T) {
		setupStorageTest(t)
		name := "items/pastes/abc123/content-r1.txt"
		manifest := BackupManifest{SchemaVersion: 1, Files: map[string]string{name: strings.Repeat("0", 64)}}
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		writeManifestForTest(t, writer, manifest)
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: dataFileMode, Size: maximumBackupEntrySize(name) + 1}); err != nil {
			t.Fatal(err)
		}
		if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("ImportBackup() error = %v, want quota error", err)
		}
	})

	t.Run("entry count", func(t *testing.T) {
		setupStorageTest(t)
		originalLimit := maximumBackupEntries
		maximumBackupEntries = 2
		t.Cleanup(func() { maximumBackupEntries = originalLimit })
		files := map[string]string{}
		for index := 0; index < 3; index++ {
			name := fmt.Sprintf("items/pastes/abc12%d/metadata.json", index)
			files[name] = checksumBytes(nil)
		}
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		writeManifestForTest(t, writer, BackupManifest{SchemaVersion: 1, Files: files})
		for name := range files {
			if err := writeTarFile(writer, name, nil, time.Time{}); err != nil {
				t.Fatal(err)
			}
		}
		_ = writer.Close()
		if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("ImportBackup() error = %v, want quota error", err)
		}
	})

	t.Run("path depth", func(t *testing.T) {
		setupStorageTest(t)
		name := "items/pastes/abc123/nested/content.txt"
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		writeManifestForTest(t, writer, BackupManifest{SchemaVersion: 1, Files: map[string]string{name: checksumBytes(nil)}})
		if err := writeTarFile(writer, name, nil, time.Time{}); err != nil {
			t.Fatal(err)
		}
		_ = writer.Close()
		if err := ImportBackup(bytes.NewReader(archive.Bytes()), false); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("ImportBackup() error = %v, want corruption", err)
		}
	})
}

func headerOnlyArchive(t *testing.T, name string, size int64) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: dataFileMode, Size: size}); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func writeManifestForTest(t *testing.T, writer *tar.Writer, manifest BackupManifest) {
	t.Helper()
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(writer, backupManifestName, content, time.Time{}); err != nil {
		t.Fatal(err)
	}
}
