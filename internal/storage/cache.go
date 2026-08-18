package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

// DataDir is the filesystem path where stored items live.
var DataDir = util.GetEnv("DATA_DIR", "./data")

// PasteCache is the in-memory paste index.
type PasteCache struct {
	sync.RWMutex
	Items map[string]models.CachedPaste
}

// GlobalCache is the process paste index.
var GlobalCache = &PasteCache{Items: make(map[string]models.CachedPaste)}

var (
	contentCacheMu   sync.Mutex
	contentCacheUsed int64
	searchIndexUsed  int64
)

func clearStorageCaches() {
	GlobalCache.Lock()
	GlobalDiffCache.Lock()
	contentCacheMu.Lock()
	GlobalCache.Items = make(map[string]models.CachedPaste)
	GlobalDiffCache.Items = make(map[string]models.CachedDiff)
	contentCacheUsed = 0
	searchIndexUsed = 0
	contentCacheMu.Unlock()
	GlobalDiffCache.Unlock()
	GlobalCache.Unlock()
	resetUsage()
}

func pasteContentSize(cached models.CachedPaste) int64 {
	return int64(len(cached.Content))
}

func diffContentSize(cached models.CachedDiff) int64 {
	return int64(len(cached.Base) + len(cached.Compare) + len(cached.BaseContent) + len(cached.CompareContent))
}

func storePasteCache(cached models.CachedPaste) {
	GlobalCache.Lock()
	contentCacheMu.Lock()
	if old, exists := GlobalCache.Items[cached.ID]; exists {
		contentCacheUsed -= pasteContentSize(old)
		searchIndexUsed -= int64(len(old.SearchText))
	}
	limit := GetStorageLimits().MaxCachedContentBytes
	if limit <= 0 || contentCacheUsed+pasteContentSize(cached) > limit {
		cached.Content = ""
	}
	contentCacheUsed += pasteContentSize(cached)
	indexLimit := GetStorageLimits().MaxSearchIndexBytes
	if indexLimit <= 0 || searchIndexUsed+int64(len(cached.SearchText)) > int64(indexLimit) {
		cached.SearchText = ""
	}
	searchIndexUsed += int64(len(cached.SearchText))
	GlobalCache.Items[cached.ID] = cached
	contentCacheMu.Unlock()
	GlobalCache.Unlock()
}

func removePasteCache(id string) {
	GlobalCache.Lock()
	contentCacheMu.Lock()
	if old, exists := GlobalCache.Items[id]; exists {
		contentCacheUsed -= pasteContentSize(old)
		searchIndexUsed -= int64(len(old.SearchText))
		delete(GlobalCache.Items, id)
	}
	contentCacheMu.Unlock()
	GlobalCache.Unlock()
}

func isExpired(metadata models.ItemMetadata, now time.Time) bool {
	return metadata.ExpiresAt != nil && !metadata.ExpiresAt.After(now)
}

func loadPaste(metadata models.ItemMetadata) (models.CachedPaste, error) {
	path := itemDataPath(metadata)
	content, err := readStorageFile(path, metadata.Size)
	if err != nil {
		return models.CachedPaste{}, err
	}
	if int64(len(content)) != metadata.Size || checksumBytes(content) != metadata.Checksum {
		return models.CachedPaste{}, fmt.Errorf("%w: paste %s checksum or size mismatch", ErrCorrupt, metadata.ID)
	}
	return cachedPasteFromData(metadata, content), nil
}

func cachedPasteFromData(metadata models.ItemMetadata, content []byte) models.CachedPaste {
	text := string(content)
	preview := util.GetPreview(text)
	searchParts := []string{metadata.Title, metadata.Language, strings.Join(metadata.Tags, " ")}
	if metadata.BurnAfterRead {
		preview = ""
	} else {
		searchParts = append(searchParts, text)
	}
	return models.CachedPaste{
		ID:             metadata.ID,
		Title:          metadata.Title,
		TitleLower:     strings.ToLower(metadata.Title),
		Content:        text,
		Language:       metadata.Language,
		LanguageLower:  strings.ToLower(metadata.Language),
		CreatedAt:      metadata.CreatedAt,
		UpdatedAt:      metadata.UpdatedAt,
		Preview:        preview,
		LineCount:      strings.Count(text, "\n") + 1,
		Tags:           append([]string(nil), metadata.Tags...),
		Favorite:       metadata.Favorite,
		ExpiresAt:      metadata.ExpiresAt,
		BurnAfterRead:  metadata.BurnAfterRead,
		Revision:       metadata.Revision,
		Size:           metadata.Size,
		EditSecretHash: metadata.EditSecretHash,
		Checksum:       metadata.Checksum,
		DataPath:       itemDataPath(metadata),
		SearchText:     buildSearchText(searchParts...),
	}
}

// LoadCacheFromDisk migrates legacy files and rebuilds the paste index.
func LoadCacheFromDisk() {
	start := time.Now()
	resetUsage()
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		log.Printf("[cache] create storage layout: %v", err)
		return
	}
	if err := migrateLegacyPastes(); err != nil {
		log.Printf("[cache] migrate legacy pastes: %v", err)
	}

	entries, err := os.ReadDir(itemRoot(models.ItemKindPaste))
	if err != nil {
		log.Printf("[cache] read paste root: %v", err)
		return
	}
	metadataItems := make([]models.ItemMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validStorageID(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		metadata, err := readMetadata(models.ItemKindPaste, entry.Name())
		if err != nil {
			log.Printf("[cache] read paste metadata %s: %v", entry.Name(), err)
			continue
		}
		if isExpired(metadata, time.Now()) {
			if err := deleteItemFiles(metadata.Kind, metadata.ID); err != nil {
				log.Printf("[cache] delete expired paste %s: %v", metadata.ID, err)
			}
			continue
		}
		metadataItems = append(metadataItems, metadata)
	}
	sort.Slice(metadataItems, func(left, right int) bool { return metadataItems[left].UpdatedAt.After(metadataItems[right].UpdatedAt) })
	items := make(map[string]models.CachedPaste, len(metadataItems))
	GlobalDiffCache.RLock()
	otherUsage := int64(0)
	otherIndexUsage := int64(0)
	for _, diff := range GlobalDiffCache.Items {
		otherUsage += diffContentSize(diff)
		otherIndexUsage += int64(len(diff.SearchText))
	}
	GlobalDiffCache.RUnlock()
	configuredCacheBytes := GetStorageLimits().MaxCachedContentBytes
	usedCacheBytes := otherUsage
	usedIndexBytes := otherIndexUsage
	configuredIndexBytes := GetStorageLimits().MaxSearchIndexBytes
	for _, metadata := range metadataItems {
		cached, err := loadPaste(metadata)
		if err != nil {
			log.Printf("[cache] read paste %s: %v", metadata.ID, err)
			continue
		}
		if configuredCacheBytes <= 0 || usedCacheBytes+pasteContentSize(cached) > configuredCacheBytes {
			cached.Content = ""
		} else {
			usedCacheBytes += pasteContentSize(cached)
		}
		if configuredIndexBytes <= 0 || usedIndexBytes+int64(len(cached.SearchText)) > int64(configuredIndexBytes) {
			cached.SearchText = ""
		} else {
			usedIndexBytes += int64(len(cached.SearchText))
		}
		items[metadata.ID] = cached
	}
	GlobalCache.Lock()
	GlobalCache.Items = items
	GlobalCache.Unlock()
	contentCacheMu.Lock()
	contentCacheUsed = usedCacheBytes
	searchIndexUsed = usedIndexBytes
	contentCacheMu.Unlock()
	log.Printf("[cache] loaded %d pastes in %v", len(items), time.Since(start))
}

// FindPasteFile returns the stable content path for a paste.
func FindPasteFile(id string) (string, error) {
	if !validStorageID(id) {
		return "", fs.ErrNotExist
	}
	GlobalCache.RLock()
	cached, ok := GlobalCache.Items[id]
	GlobalCache.RUnlock()
	if ok && cached.DataPath != "" {
		if info, err := os.Lstat(cached.DataPath); err == nil && info.Mode().IsRegular() {
			return cached.DataPath, nil
		}
	}
	metadata, err := readMetadata(models.ItemKindPaste, id)
	if err == nil {
		path := itemDataPath(metadata)
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() {
			return path, nil
		}
		return "", fs.ErrNotExist
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	// This fallback keeps legacy files readable before the startup migration runs.
	entries, readErr := os.ReadDir(DataDir)
	if readErr != nil {
		return "", readErr
	}
	prefix := id + "_"
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(DataDir, entry.Name()), nil
		}
	}
	return "", fs.ErrNotExist
}

func deleteItemFiles(kind models.ItemKind, id string) error {
	if !validStorageID(id) {
		return fs.ErrNotExist
	}
	stamp, err := generateStorageID()
	if err != nil {
		return err
	}
	journal := deleteJournal{Kind: kind, ID: id, Stamp: stamp}
	if err := writeStorageJournal(deleteJournalName, journal); err != nil {
		return err
	}
	itemTombstone, revisionTombstone := deletionTombstones(journal)
	itemPath := itemDirectory(kind, id)
	if err := renameStoragePathDurably(itemPath, itemTombstone); err != nil {
		_ = removeFileDurably(filepath.Join(DataDir, deleteJournalName))
		return err
	}

	revisionPath := revisionRoot(kind, id)
	revisionMoved := false
	if _, statErr := os.Lstat(revisionPath); statErr == nil {
		if err := renameStoragePathDurably(revisionPath, revisionTombstone); err != nil {
			_ = renameStoragePathDurably(itemTombstone, itemPath)
			return err
		}
		revisionMoved = true
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		_ = renameStoragePathDurably(itemTombstone, itemPath)
		return statErr
	}
	journalPath := filepath.Join(DataDir, deleteJournalName)
	if err := removeFileDurably(journalPath); err != nil {
		if _, statErr := os.Lstat(journalPath); !errors.Is(statErr, fs.ErrNotExist) {
			if revisionMoved {
				_ = renameStoragePathDurably(revisionTombstone, revisionPath)
			}
			_ = renameStoragePathDurably(itemTombstone, itemPath)
			return err
		}
	}
	_ = os.RemoveAll(itemTombstone)
	_ = os.RemoveAll(revisionTombstone)
	_ = syncDirectory(DataDir)
	return nil
}

func renameStoragePathDurably(oldPath, newPath string) error {
	if err := renameStoragePath(oldPath, newPath); err != nil {
		return err
	}
	oldParent := filepath.Dir(oldPath)
	newParent := filepath.Dir(newPath)
	if err := syncDirectory(oldParent); err != nil {
		return errors.Join(err, rollbackRenamedStoragePath(newPath, oldPath))
	}
	if newParent != oldParent {
		if err := syncDirectory(newParent); err != nil {
			return errors.Join(err, rollbackRenamedStoragePath(newPath, oldPath))
		}
	}
	return nil
}

func rollbackRenamedStoragePath(currentPath, originalPath string) error {
	if err := renameStoragePath(currentPath, originalPath); err != nil {
		return err
	}
	var result []error
	if err := syncDirectory(filepath.Dir(currentPath)); err != nil {
		result = append(result, err)
	}
	if filepath.Dir(originalPath) != filepath.Dir(currentPath) {
		if err := syncDirectory(filepath.Dir(originalPath)); err != nil {
			result = append(result, err)
		}
	}
	return errors.Join(result...)
}

func deleteItemLocked(kind models.ItemKind, id string) error {
	metadata, err := readMetadata(kind, id)
	if err != nil {
		return err
	}
	revisionBytes, err := revisionStorageBytes(kind, id)
	if err != nil {
		return err
	}
	if err := deleteItemFiles(kind, id); err != nil {
		return err
	}
	if kind == models.ItemKindPaste {
		removePasteCache(id)
	} else {
		removeDiffCache(id)
	}
	adjustUsage(metadata.Size+revisionBytes, 0, -1)
	return nil
}

func revisionStorageBytes(kind models.ItemKind, id string) (int64, error) {
	var total int64
	err := filepath.WalkDir(revisionRoot(kind, id), func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symbolic link in revision storage", ErrCorrupt)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: non-regular revision file", ErrCorrupt)
		}
		total += info.Size()
		return nil
	})
	return total, err
}
