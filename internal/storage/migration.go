package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

func ensureStorageLayout() error {
	if err := os.MkdirAll(DataDir, dataDirMode); err != nil {
		return err
	}
	if err := recoverStorageTransactions(); err != nil {
		return err
	}
	for _, path := range []string{
		itemRoot(models.ItemKindPaste),
		itemRoot(models.ItemKindDiff),
		filepath.Join(DataDir, revisionsDirectory, "pastes"),
		filepath.Join(DataDir, revisionsDirectory, "diffs"),
	} {
		if err := os.MkdirAll(path, dataDirMode); err != nil {
			return err
		}
	}
	return nil
}

// Initialize recovers interrupted mutations and verifies writable storage.
func Initialize() error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		return err
	}
	if err := cleanupTransactionArtifacts(); err != nil {
		return err
	}
	probe, err := os.CreateTemp(DataDir, ".write-check-*")
	if err != nil {
		return fmt.Errorf("storage directory is not writable: %w", err)
	}
	probePath := probe.Name()
	if err := probe.Chmod(dataFileMode); err != nil {
		_ = probe.Close()
		_ = os.Remove(probePath)
		return err
	}
	if _, err := probe.Write([]byte("ok\n")); err != nil {
		_ = probe.Close()
		_ = os.Remove(probePath)
		return fmt.Errorf("storage directory is not writable: %w", err)
	}
	if err := probe.Sync(); err != nil {
		_ = probe.Close()
		_ = os.Remove(probePath)
		return err
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return err
	}
	return removeFileDurably(probePath)
}

func migrateLegacyPastes() error {
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 || len(parts[0]) != 6 || !validStorageID(parts[0]) {
			continue
		}
		id := parts[0]
		if _, err := readMetadata(models.ItemKindPaste, id); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		legacyPath := filepath.Join(DataDir, entry.Name())
		content, err := os.ReadFile(legacyPath)
		if err != nil {
			return err
		}
		extension := filepath.Ext(entry.Name())
		title := strings.TrimSuffix(parts[1], extension)
		if title == "" {
			title = "Untitled"
		}
		createdAt := info.ModTime().UTC()
		metadata := models.ItemMetadata{
			SchemaVersion: metadataSchemaVersion,
			Kind:          models.ItemKindPaste,
			ID:            id,
			Title:         title,
			Language:      util.ExtToLang(extension),
			CreatedAt:     createdAt,
			UpdatedAt:     createdAt,
			Tags:          []string{},
			Revision:      1,
			Size:          int64(len(content)),
			Checksum:      checksumBytes(content),
			DataFile:      "content-r1" + extension,
		}
		if err := reconcileCommittedFile(itemDataPath(metadata), content, createFileExclusive(itemDataPath(metadata), content)); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			if err := verifyMigrationCollision(itemDataPath(metadata), content); err != nil {
				return err
			}
		}
		if err := writeMetadata(metadata); err != nil {
			return err
		}
		adjustUsage(0, metadata.Size, 1)
		if err := removeFileDurably(legacyPath); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyDiffs() error {
	legacyRoot := filepath.Join(DataDir, "diffs")
	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 || len(parts[0]) != 6 || !validStorageID(parts[0]) {
			continue
		}
		id := parts[0]
		if _, err := readMetadata(models.ItemKindDiff, id); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		legacyPath := filepath.Join(legacyRoot, entry.Name())
		content, err := os.ReadFile(legacyPath)
		if err != nil {
			return err
		}
		var data models.DiffData
		if err := json.Unmarshal(content, &data); err != nil {
			return fmt.Errorf("%w: legacy diff %s: %v", ErrCorrupt, entry.Name(), err)
		}
		createdAt := info.ModTime().UTC()
		title := strings.TrimSuffix(parts[1], ".json")
		if title == "" {
			title = "Untitled Diff"
		}
		metadata := models.ItemMetadata{
			SchemaVersion: metadataSchemaVersion,
			Kind:          models.ItemKindDiff,
			ID:            id,
			Title:         title,
			CreatedAt:     createdAt,
			UpdatedAt:     createdAt,
			Tags:          []string{},
			Revision:      1,
			Size:          int64(len(content)),
			Checksum:      checksumBytes(content),
			DataFile:      "content-r1.json",
		}
		if err := reconcileCommittedFile(itemDataPath(metadata), content, createFileExclusive(itemDataPath(metadata), content)); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			if err := verifyMigrationCollision(itemDataPath(metadata), content); err != nil {
				return err
			}
		}
		if err := writeMetadata(metadata); err != nil {
			return err
		}
		adjustUsage(0, metadata.Size, 1)
		if err := removeFileDurably(legacyPath); err != nil {
			return err
		}
	}
	return nil
}

func verifyMigrationCollision(path string, expected []byte) error {
	existing, err := readStorageFile(path, int64(len(expected)))
	if err != nil {
		return err
	}
	if len(existing) != len(expected) || checksumBytes(existing) != checksumBytes(expected) {
		return fmt.Errorf("%w: migration target %s contains different data", ErrCorrupt, filepath.Base(path))
	}
	return nil
}

func newMetadata(kind models.ItemKind, id, title, language, dataFile string, data []byte, options CreateOptions) (models.ItemMetadata, string, error) {
	secret := options.EditSecret
	if secret == "" {
		var err error
		secret, err = generateEditSecret()
		if err != nil {
			return models.ItemMetadata{}, "", err
		}
	}
	hash, err := hashEditSecret(secret)
	if err != nil {
		return models.ItemMetadata{}, "", err
	}
	now := time.Now().UTC()
	metadata := models.ItemMetadata{
		SchemaVersion:  metadataSchemaVersion,
		Kind:           kind,
		ID:             id,
		Title:          title,
		Language:       language,
		CreatedAt:      now,
		UpdatedAt:      now,
		EditSecretHash: hash,
		Tags:           canonicalTags(options.Tags),
		Favorite:       options.Favorite,
		ExpiresAt:      options.ExpiresAt,
		BurnAfterRead:  options.BurnAfterRead,
		Revision:       1,
		Size:           int64(len(data)),
		Checksum:       checksumBytes(data),
		DataFile:       dataFile,
	}
	return metadata, secret, nil
}
