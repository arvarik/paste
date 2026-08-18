package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/arvarik/paste/internal/storage"
)

func TestBackupVerifyAndRestoreCommands(t *testing.T) {
	originalDataDir := storage.DataDir
	originalLimits := storage.GetStorageLimits()
	t.Cleanup(func() {
		storage.DataDir = originalDataDir
		storage.SetStorageLimits(originalLimits)
	})
	source := filepath.Join(t.TempDir(), "source")
	storage.DataDir = source
	id, err := storage.CreatePaste("CLI", "safe", "text")
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.tar")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"backup", "--data-dir", source, "--output", archive}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup exit = %d: %s", code, stderr.String())
	}

	destination := filepath.Join(t.TempDir(), "destination")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"restore", "--data-dir", destination, "--input", archive, "--force"}, &stdout, &stderr); code != 0 {
		t.Fatalf("restore exit = %d: %s", code, stderr.String())
	}
	storage.DataDir = destination
	paste, err := storage.GetPaste(id)
	if err != nil || paste.Content != "safe" {
		t.Fatalf("restored paste = %#v, %v", paste, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"verify", "--data-dir", destination}, &stdout, &stderr); code != 0 {
		t.Fatalf("verify exit = %d: %s", code, stderr.String())
	}
	var report storage.IntegrityReport
	if err := json.NewDecoder(&stdout).Decode(&report); err != nil || !report.Healthy || report.Items != 1 {
		t.Fatalf("verify report = %#v, %v", report, err)
	}
}

func TestBackupCommandUsesConfiguredBackupLimit(t *testing.T) {
	originalDataDir := storage.DataDir
	originalLimits := storage.GetStorageLimits()
	t.Cleanup(func() {
		storage.DataDir = originalDataDir
		storage.SetStorageLimits(originalLimits)
	})
	t.Setenv("PASTE_BACKUP_LIMIT", "1024")
	source := t.TempDir()
	archive := filepath.Join(t.TempDir(), "backup.tar")
	var stdout, stderr bytes.Buffer
	code := run([]string{"backup", "--data-dir", source, "--output", archive}, &stdout, &stderr)
	if code != 1 || !bytes.Contains(stderr.Bytes(), []byte("storage quota exceeded")) {
		t.Fatalf("backup exit = %d, stderr = %q", code, stderr.String())
	}
	if storage.GetStorageLimits().MaxBackupBytes != 1024 {
		t.Fatalf("configured backup limit = %d", storage.GetStorageLimits().MaxBackupBytes)
	}
}

func TestRestoreRequiresExplicitForce(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"restore", "--input", "backup.tar"}, &stdout, &stderr)
	if code != 1 || !bytes.Contains(stderr.Bytes(), []byte("--force")) {
		t.Fatalf("restore exit = %d, stderr = %q", code, stderr.String())
	}
}
