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
)

const (
	swapJournalName    = ".storage-swap.json"
	deleteJournalName  = ".storage-delete.json"
	cleanupJournalName = ".storage-cleanup.json"
)

type swapJournal struct {
	Stamp string `json:"stamp"`
}

type deleteJournal struct {
	Kind  models.ItemKind `json:"kind"`
	ID    string          `json:"id"`
	Stamp string          `json:"stamp"`
}

type cleanupJournal struct {
	Kind        models.ItemKind `json:"kind"`
	ID          string          `json:"id"`
	OldDataFile string          `json:"oldDataFile"`
	NewDataFile string          `json:"newDataFile"`
	Stamp       string          `json:"stamp"`
}

func writeStorageJournal(name string, document any) error {
	content, err := json.Marshal(document)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	path := filepath.Join(DataDir, name)
	return reconcileCommittedFile(path, content, replaceFileAtomically(path, content, timeZero))
}

var timeZero = time.Time{}

func recoverStorageTransactions() error {
	if err := recoverStorageSwap(); err != nil {
		return err
	}
	if err := recoverStorageDelete(); err != nil {
		return err
	}
	return recoverContentCleanup()
}

func beginContentCleanup(current, next models.ItemMetadata) (cleanupJournal, error) {
	if current.DataFile == next.DataFile {
		return cleanupJournal{}, nil
	}
	stamp, err := generateStorageID()
	if err != nil {
		return cleanupJournal{}, err
	}
	journal := cleanupJournal{
		Kind: current.Kind, ID: current.ID, OldDataFile: current.DataFile,
		NewDataFile: next.DataFile, Stamp: stamp,
	}
	return journal, writeStorageJournal(cleanupJournalName, journal)
}

func finishContentCleanup(journal cleanupJournal) error {
	if journal.Stamp == "" {
		return nil
	}
	oldPath := filepath.Join(itemDirectory(journal.Kind, journal.ID), journal.OldDataFile)
	tombstone := contentCleanupTombstone(journal)
	if _, err := os.Lstat(oldPath); err == nil {
		if err := renameStoragePathDurably(oldPath, tombstone); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	journalPath := filepath.Join(DataDir, cleanupJournalName)
	if err := removeFileDurably(journalPath); err != nil {
		if _, statErr := os.Lstat(journalPath); !errors.Is(statErr, fs.ErrNotExist) {
			return err
		}
	}
	if err := removeStoragePath(tombstone); err == nil {
		_ = syncDirectory(DataDir)
	}
	return nil
}

func recoverContentCleanup() error {
	path := filepath.Join(DataDir, cleanupJournalName)
	content, err := readStorageFile(path, 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal cleanupJournal
	if err := json.Unmarshal(content, &journal); err != nil ||
		(journal.Kind != models.ItemKindPaste && journal.Kind != models.ItemKindDiff) ||
		!validStorageID(journal.ID) || !validStorageID(journal.Stamp) ||
		filepath.Base(journal.OldDataFile) != journal.OldDataFile || filepath.Base(journal.NewDataFile) != journal.NewDataFile {
		return fmt.Errorf("%w: invalid content cleanup journal", ErrCorrupt)
	}
	metadata, err := readMetadata(journal.Kind, journal.ID)
	if err != nil {
		return err
	}
	if metadata.DataFile == journal.NewDataFile {
		return finishContentCleanup(journal)
	}
	if metadata.DataFile != journal.OldDataFile {
		return fmt.Errorf("%w: content cleanup metadata mismatch", ErrCorrupt)
	}
	tombstone := contentCleanupTombstone(journal)
	oldPath := filepath.Join(itemDirectory(journal.Kind, journal.ID), journal.OldDataFile)
	if _, err := os.Lstat(tombstone); err == nil {
		if _, oldErr := os.Lstat(oldPath); oldErr == nil {
			return fmt.Errorf("%w: content cleanup rollback target exists", ErrCorrupt)
		} else if !errors.Is(oldErr, fs.ErrNotExist) {
			return oldErr
		}
		if err := renameStoragePathDurably(tombstone, oldPath); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	newPath := filepath.Join(itemDirectory(journal.Kind, journal.ID), journal.NewDataFile)
	if err := removeFileDurably(newPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeFileDurably(path)
}

func contentCleanupTombstone(journal cleanupJournal) string {
	return filepath.Join(DataDir, ".obsolete-"+string(journal.Kind)+"-"+journal.ID+"-"+journal.Stamp)
}

func recoverStorageSwap() error {
	path := filepath.Join(DataDir, swapJournalName)
	content, err := readStorageFile(path, 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal swapJournal
	if err := json.Unmarshal(content, &journal); err != nil || !validStorageID(journal.Stamp) {
		return fmt.Errorf("%w: invalid storage swap journal", ErrCorrupt)
	}
	for _, name := range []string{itemsDirectoryName, revisionsDirectory} {
		backup := filepath.Join(DataDir, ".old-"+name+"-"+journal.Stamp)
		if _, err := os.Lstat(backup); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		current := filepath.Join(DataDir, name)
		if err := os.RemoveAll(current); err != nil {
			return err
		}
		if err := os.Rename(backup, current); err != nil {
			return err
		}
	}
	if err := syncDirectory(DataDir); err != nil {
		return err
	}
	return removeFileDurably(path)
}

func recoverStorageDelete() error {
	path := filepath.Join(DataDir, deleteJournalName)
	content, err := readStorageFile(path, 1<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal deleteJournal
	if err := json.Unmarshal(content, &journal); err != nil ||
		(journal.Kind != models.ItemKindPaste && journal.Kind != models.ItemKindDiff) ||
		!validStorageID(journal.ID) || !validStorageID(journal.Stamp) {
		return fmt.Errorf("%w: invalid storage delete journal", ErrCorrupt)
	}
	itemTombstone, revisionTombstone := deletionTombstones(journal)
	for _, entry := range []struct {
		tombstone string
		current   string
	}{
		{itemTombstone, itemDirectory(journal.Kind, journal.ID)},
		{revisionTombstone, revisionRoot(journal.Kind, journal.ID)},
	} {
		if _, err := os.Lstat(entry.tombstone); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if _, err := os.Lstat(entry.current); err == nil {
			return fmt.Errorf("%w: delete recovery target already exists", ErrCorrupt)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := renameStoragePathDurably(entry.tombstone, entry.current); err != nil {
			return err
		}
	}
	return removeFileDurably(path)
}

func cleanupTransactionArtifacts() error {
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(name, ".old-items-") || strings.HasPrefix(name, ".old-revisions-") ||
			strings.HasPrefix(name, ".deleted-item-") || strings.HasPrefix(name, ".deleted-revisions-") ||
			strings.HasPrefix(name, ".restore-") || strings.HasPrefix(name, ".import-") {
			if err := os.RemoveAll(filepath.Join(DataDir, name)); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".obsolete-") {
			continue
		}
		if err := removeStoragePath(filepath.Join(DataDir, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(DataDir)
}

func deletionTombstones(journal deleteJournal) (string, string) {
	suffix := string(journal.Kind) + "-" + journal.ID + "-" + journal.Stamp
	return filepath.Join(DataDir, ".deleted-item-"+suffix), filepath.Join(DataDir, ".deleted-revisions-"+suffix)
}
