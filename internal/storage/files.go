package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	removeStoragePath = os.Remove
	syncDirectory     = syncDirectoryPath
)

type committedMutationError struct {
	err error
}

func (err *committedMutationError) Error() string { return err.err.Error() }
func (err *committedMutationError) Unwrap() error { return err.err }

func mutationCommitted(err error) bool {
	var committed *committedMutationError
	return errors.As(err, &committed)
}

func reconcileCommittedFile(path string, data []byte, err error) error {
	if err == nil || !mutationCommitted(err) {
		return err
	}
	stored, readErr := readStorageFile(path, int64(len(data)))
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	if !bytes.Equal(stored, data) {
		return errors.Join(err, fmt.Errorf("%w: committed file does not match", ErrCorrupt))
	}
	return nil
}

const (
	dataDirMode       = 0o700
	dataFileMode      = 0o600
	maxIDAttempts     = 32
	maxStoredFileSize = 4 << 20
)

var mutationMu sync.Mutex

func readStorageFile(path string, maximum int64) ([]byte, error) {
	rootPath, err := filepath.Abs(DataDir)
	if err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(rootPath, absolutePath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: storage path escapes the data directory", ErrCorrupt)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: storage path is not a regular file", ErrCorrupt)
	}
	if maximum >= 0 && info.Size() > maximum {
		return nil, fmt.Errorf("%w: storage file exceeds its expected size", ErrCorrupt)
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	limit := maximum
	if limit < 0 {
		limit = info.Size()
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if maximum >= 0 && int64(len(content)) > maximum {
		return nil, fmt.Errorf("%w: storage file exceeds its expected size", ErrCorrupt)
	}
	return content, nil
}

// createFileExclusive writes a new file without replacing an existing file.
func createFileExclusive(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dataDirMode); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, dataFileMode)
	if err != nil {
		return err
	}

	complete := false
	defer func() {
		if !complete {
			_ = removeStoragePath(path)
			_ = syncDirectory(filepath.Dir(path))
		}
	}()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	complete = true
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return &committedMutationError{err: err}
	}
	return nil
}

// replaceFileAtomically writes a complete temporary file before replacement.
func replaceFileAtomically(path string, data []byte, modifiedAt time.Time) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, dataDirMode); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".paste-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(dataFileMode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if !modifiedAt.IsZero() {
		if err := os.Chtimes(temporaryPath, time.Now(), modifiedAt); err != nil {
			_ = temporary.Close()
			return err
		}
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}

	if err := syncDirectory(directory); err != nil {
		return &committedMutationError{err: err}
	}
	return nil
}

// removeFileDurably removes a file and syncs its parent directory.
func removeFileDurably(path string) error {
	if err := removeStoragePath(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// syncDirectory commits directory-entry changes on supported filesystems.
func syncDirectoryPath(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}
