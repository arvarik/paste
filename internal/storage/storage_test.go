package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

// setupStorageTest resets the package storage state for one test.
func setupStorageTest(t *testing.T) {
	t.Helper()
	originalDataDir := DataDir
	originalLimits := GetStorageLimits()
	DataDir = t.TempDir()
	SetStorageLimits(StorageLimits{MaxTotalBytes: 1 << 30, MaxItemBytes: 4 << 20, MaxItems: 10_000, MaxSearchResults: 100, MaxSearchIndexBytes: 8 << 20, MaxCachedContentBytes: 16 << 20, MaxBackupBytes: 2 << 30})
	resetUsage()
	contentCacheMu.Lock()
	contentCacheUsed = 0
	searchIndexUsed = 0
	contentCacheMu.Unlock()

	GlobalCache.Lock()
	GlobalCache.Items = make(map[string]models.CachedPaste)
	GlobalCache.Unlock()
	GlobalDiffCache.Lock()
	GlobalDiffCache.Items = make(map[string]models.CachedDiff)
	GlobalDiffCache.Unlock()

	t.Cleanup(func() {
		DataDir = originalDataDir
		SetStorageLimits(originalLimits)
		resetUsage()
		GlobalCache.Lock()
		GlobalCache.Items = make(map[string]models.CachedPaste)
		GlobalCache.Unlock()
		GlobalDiffCache.Lock()
		GlobalDiffCache.Items = make(map[string]models.CachedDiff)
		GlobalDiffCache.Unlock()
		contentCacheMu.Lock()
		contentCacheUsed = 0
		searchIndexUsed = 0
		contentCacheMu.Unlock()
	})
}

func TestPasteLifecycle(t *testing.T) {
	setupStorageTest(t)

	id, err := CreatePaste("First", "one\ntwo", "go")
	if err != nil {
		t.Fatalf("CreatePaste() error = %v", err)
	}
	if !util.IsValidID(id) {
		t.Fatalf("CreatePaste() ID = %q, want a valid ID", id)
	}

	created, err := GetPaste(id)
	if err != nil {
		t.Fatalf("GetPaste() error = %v", err)
	}
	if created.Content != "one\ntwo" || created.LineCount != 2 {
		t.Fatalf("GetPaste() = %#v, want the created content", created)
	}
	originalCreatedAt := created.CreatedAt

	if err := UpdatePaste(id, "Renamed", "three", "text"); err != nil {
		t.Fatalf("UpdatePaste() error = %v", err)
	}
	updated, err := GetPaste(id)
	if err != nil {
		t.Fatalf("GetPaste() after update error = %v", err)
	}
	if updated.Title != "Renamed" || updated.Content != "three" || updated.Language != "text" {
		t.Fatalf("updated paste = %#v, want renamed text content", updated)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("updated CreatedAt = %v, want %v", updated.CreatedAt, originalCreatedAt)
	}
	GlobalCache.Lock()
	GlobalCache.Items = make(map[string]models.CachedPaste)
	GlobalCache.Unlock()
	LoadCacheFromDisk()
	reloaded, err := GetPaste(id)
	if err != nil {
		t.Fatalf("GetPaste() after reload error = %v", err)
	}
	if !reloaded.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("reloaded CreatedAt = %v, want %v", reloaded.CreatedAt, originalCreatedAt)
	}

	filePath, err := FindPasteFile(id)
	if err != nil {
		t.Fatalf("FindPasteFile() error = %v", err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if info.Mode().Perm() != dataFileMode {
		t.Fatalf("paste mode = %o, want %o", info.Mode().Perm(), dataFileMode)
	}

	if err := DeletePaste(id); err != nil {
		t.Fatalf("DeletePaste() error = %v", err)
	}
	if _, err := GetPaste(id); !os.IsNotExist(err) {
		t.Fatalf("GetPaste() after delete error = %v, want not exist", err)
	}
}

func TestPasteUpdateFailurePreservesOriginal(t *testing.T) {
	setupStorageTest(t)

	id, err := CreatePaste("Original", "safe content", "text")
	if err != nil {
		t.Fatalf("CreatePaste() error = %v", err)
	}
	originalPath, err := FindPasteFile(id)
	if err != nil {
		t.Fatalf("FindPasteFile() error = %v", err)
	}

	blockedPath := filepath.Join(itemDirectory(models.ItemKindPaste, id), "content-r2.txt")
	if err := os.Mkdir(blockedPath, dataDirMode); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	if err := UpdatePaste(id, "Blocked", "replacement", "text"); err == nil {
		t.Fatal("UpdatePaste() error = nil, want a replacement error")
	}

	content, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("original paste no longer exists: %v", err)
	}
	if string(content) != "safe content" {
		t.Fatalf("original content = %q, want safe content", content)
	}
	cached, err := GetPaste(id)
	if err != nil || cached.Content != "safe content" {
		t.Fatalf("cached paste = %#v, %v, want original content", cached, err)
	}
	if err := os.Remove(blockedPath); err != nil {
		t.Fatal(err)
	}
	if err := UpdatePaste(id, "Recovered", "replacement", "text"); err != nil {
		t.Fatalf("UpdatePaste() after failed snapshot error = %v", err)
	}
}

func TestDiffLifecycleAndFailedUpdate(t *testing.T) {
	setupStorageTest(t)

	id, err := CreateDiff("First", "RAW", "RAW", "before", "after")
	if err != nil {
		t.Fatalf("CreateDiff() error = %v", err)
	}
	created, err := GetDiff(id)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	originalCreatedAt := created.CreatedAt

	if err := UpdateDiff(id, "Renamed", "RAW", "RAW", "left", "right"); err != nil {
		t.Fatalf("UpdateDiff() error = %v", err)
	}
	updated, err := GetDiff(id)
	if err != nil {
		t.Fatalf("GetDiff() after update error = %v", err)
	}
	if updated.Title != "Renamed" || updated.BaseContent != "left" || updated.CompareContent != "right" {
		t.Fatalf("updated diff = %#v, want renamed content", updated)
	}
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("updated CreatedAt = %v, want %v", updated.CreatedAt, originalCreatedAt)
	}
	GlobalDiffCache.Lock()
	GlobalDiffCache.Items = make(map[string]models.CachedDiff)
	GlobalDiffCache.Unlock()
	LoadDiffCacheFromDisk()
	reloaded, err := GetDiff(id)
	if err != nil {
		t.Fatalf("GetDiff() after reload error = %v", err)
	}
	if !reloaded.CreatedAt.Equal(originalCreatedAt) {
		t.Fatalf("reloaded CreatedAt = %v, want %v", reloaded.CreatedAt, originalCreatedAt)
	}

	originalPath, err := FindDiffFile(id)
	if err != nil {
		t.Fatalf("FindDiffFile() error = %v", err)
	}
	blockedPath := filepath.Join(itemDirectory(models.ItemKindDiff, id), "content-r3.json")
	if err := os.Mkdir(blockedPath, dataDirMode); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	if err := UpdateDiff(id, "Blocked", "RAW", "RAW", "bad", "write"); err == nil {
		t.Fatal("UpdateDiff() error = nil, want a replacement error")
	}
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("original diff no longer exists: %v", err)
	}
	preserved, err := GetDiff(id)
	if err != nil || preserved.BaseContent != "left" {
		t.Fatalf("cached diff = %#v, %v, want previous content", preserved, err)
	}
	if err := os.Remove(blockedPath); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDiff(id, "Recovered", "RAW", "RAW", "good", "write"); err != nil {
		t.Fatalf("UpdateDiff() after failed snapshot error = %v", err)
	}

	if err := DeleteDiff(id); err != nil {
		t.Fatalf("DeleteDiff() error = %v", err)
	}
	if _, err := GetDiff(id); !os.IsNotExist(err) {
		t.Fatalf("GetDiff() after delete error = %v, want not exist", err)
	}
}

func TestLoadCacheReplacesStaleEntries(t *testing.T) {
	setupStorageTest(t)

	GlobalCache.Lock()
	GlobalCache.Items["stale1"] = models.CachedPaste{ID: "stale1", CreatedAt: time.Now()}
	GlobalCache.Unlock()
	if err := os.WriteFile(filepath.Join(DataDir, "abc123_Valid.txt"), []byte("content"), dataFileMode); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.Symlink(filepath.Join(DataDir, "abc123_Valid.txt"), filepath.Join(DataDir, "def456_Link.txt")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	LoadCacheFromDisk()
	GlobalCache.RLock()
	defer GlobalCache.RUnlock()
	if _, ok := GlobalCache.Items["stale1"]; ok {
		t.Fatal("LoadCacheFromDisk() retained a stale cache entry")
	}
	if _, ok := GlobalCache.Items["abc123"]; !ok {
		t.Fatal("LoadCacheFromDisk() did not load the valid paste")
	}
	if _, ok := GlobalCache.Items["def456"]; ok {
		t.Fatal("LoadCacheFromDisk() followed a symbolic link")
	}
}
