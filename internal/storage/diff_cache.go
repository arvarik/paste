package storage

import (
	"encoding/json"
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
)

// DiffCache is the in-memory diff index.
type DiffCache struct {
	sync.RWMutex
	Items map[string]models.CachedDiff
}

// GlobalDiffCache is the process diff index.
var GlobalDiffCache = &DiffCache{Items: make(map[string]models.CachedDiff)}

func storeDiffCache(cached models.CachedDiff) {
	GlobalDiffCache.Lock()
	contentCacheMu.Lock()
	if old, exists := GlobalDiffCache.Items[cached.ID]; exists {
		contentCacheUsed -= diffContentSize(old)
		searchIndexUsed -= int64(len(old.SearchText))
	}
	limit := GetStorageLimits().MaxCachedContentBytes
	if limit <= 0 || contentCacheUsed+diffContentSize(cached) > limit {
		cached.Base = ""
		cached.Compare = ""
		cached.BaseContent = ""
		cached.CompareContent = ""
	}
	contentCacheUsed += diffContentSize(cached)
	indexLimit := GetStorageLimits().MaxSearchIndexBytes
	if indexLimit <= 0 || searchIndexUsed+int64(len(cached.SearchText)) > int64(indexLimit) {
		cached.SearchText = ""
	}
	searchIndexUsed += int64(len(cached.SearchText))
	GlobalDiffCache.Items[cached.ID] = cached
	contentCacheMu.Unlock()
	GlobalDiffCache.Unlock()
}

func removeDiffCache(id string) {
	GlobalDiffCache.Lock()
	contentCacheMu.Lock()
	if old, exists := GlobalDiffCache.Items[id]; exists {
		contentCacheUsed -= diffContentSize(old)
		searchIndexUsed -= int64(len(old.SearchText))
		delete(GlobalDiffCache.Items, id)
	}
	contentCacheMu.Unlock()
	GlobalDiffCache.Unlock()
}

func loadDiff(metadata models.ItemMetadata) (models.CachedDiff, error) {
	path := itemDataPath(metadata)
	content, err := readStorageFile(path, metadata.Size)
	if err != nil {
		return models.CachedDiff{}, err
	}
	if int64(len(content)) != metadata.Size || checksumBytes(content) != metadata.Checksum {
		return models.CachedDiff{}, fmt.Errorf("%w: diff %s checksum or size mismatch", ErrCorrupt, metadata.ID)
	}
	var data models.DiffData
	if err := json.Unmarshal(content, &data); err != nil {
		return models.CachedDiff{}, fmt.Errorf("%w: decode diff %s: %v", ErrCorrupt, metadata.ID, err)
	}
	return cachedDiffFromData(metadata, data), nil
}

func cachedDiffFromData(metadata models.ItemMetadata, data models.DiffData) models.CachedDiff {
	searchParts := []string{metadata.Title, strings.Join(metadata.Tags, " ")}
	if !metadata.BurnAfterRead {
		searchParts = append(searchParts, data.Base, data.Compare, data.BaseContent, data.CompareContent)
	}
	return models.CachedDiff{
		ID:             metadata.ID,
		Title:          metadata.Title,
		TitleLower:     strings.ToLower(metadata.Title),
		Base:           data.Base,
		Compare:        data.Compare,
		BaseContent:    data.BaseContent,
		CompareContent: data.CompareContent,
		CreatedAt:      metadata.CreatedAt,
		UpdatedAt:      metadata.UpdatedAt,
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

// LoadDiffCacheFromDisk migrates legacy files and rebuilds the diff index.
func LoadDiffCacheFromDisk() {
	start := time.Now()
	resetUsage()
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		log.Printf("[cache] create storage layout: %v", err)
		return
	}
	if err := migrateLegacyDiffs(); err != nil {
		log.Printf("[cache] migrate legacy diffs: %v", err)
	}
	entries, err := os.ReadDir(itemRoot(models.ItemKindDiff))
	if err != nil {
		log.Printf("[cache] read diff root: %v", err)
		return
	}
	metadataItems := make([]models.ItemMetadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validStorageID(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		metadata, err := readMetadata(models.ItemKindDiff, entry.Name())
		if err != nil {
			log.Printf("[cache] read diff metadata %s: %v", entry.Name(), err)
			continue
		}
		if isExpired(metadata, time.Now()) {
			if err := deleteItemFiles(metadata.Kind, metadata.ID); err != nil {
				log.Printf("[cache] delete expired diff %s: %v", metadata.ID, err)
			}
			continue
		}
		metadataItems = append(metadataItems, metadata)
	}
	sort.Slice(metadataItems, func(left, right int) bool { return metadataItems[left].UpdatedAt.After(metadataItems[right].UpdatedAt) })
	items := make(map[string]models.CachedDiff, len(metadataItems))
	GlobalCache.RLock()
	usedCacheBytes := int64(0)
	usedIndexBytes := int64(0)
	for _, paste := range GlobalCache.Items {
		usedCacheBytes += pasteContentSize(paste)
		usedIndexBytes += int64(len(paste.SearchText))
	}
	GlobalCache.RUnlock()
	configuredCacheBytes := GetStorageLimits().MaxCachedContentBytes
	configuredIndexBytes := GetStorageLimits().MaxSearchIndexBytes
	for _, metadata := range metadataItems {
		cached, err := loadDiff(metadata)
		if err != nil {
			log.Printf("[cache] read diff %s: %v", metadata.ID, err)
			continue
		}
		if configuredCacheBytes <= 0 || usedCacheBytes+diffContentSize(cached) > configuredCacheBytes {
			cached.Base = ""
			cached.Compare = ""
			cached.BaseContent = ""
			cached.CompareContent = ""
		} else {
			usedCacheBytes += diffContentSize(cached)
		}
		if configuredIndexBytes <= 0 || usedIndexBytes+int64(len(cached.SearchText)) > int64(configuredIndexBytes) {
			cached.SearchText = ""
		} else {
			usedIndexBytes += int64(len(cached.SearchText))
		}
		items[metadata.ID] = cached
	}
	GlobalDiffCache.Lock()
	GlobalDiffCache.Items = items
	GlobalDiffCache.Unlock()
	contentCacheMu.Lock()
	contentCacheUsed = usedCacheBytes
	searchIndexUsed = usedIndexBytes
	contentCacheMu.Unlock()
	log.Printf("[cache] loaded %d diffs in %v", len(items), time.Since(start))
}

// FindDiffFile returns the stable JSON content path for a diff.
func FindDiffFile(id string) (string, error) {
	if !validStorageID(id) {
		return "", fs.ErrNotExist
	}
	GlobalDiffCache.RLock()
	cached, ok := GlobalDiffCache.Items[id]
	GlobalDiffCache.RUnlock()
	if ok && cached.DataPath != "" {
		if info, err := os.Lstat(cached.DataPath); err == nil && info.Mode().IsRegular() {
			return cached.DataPath, nil
		}
	}
	metadata, err := readMetadata(models.ItemKindDiff, id)
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

	legacyRoot := filepath.Join(DataDir, "diffs")
	entries, readErr := os.ReadDir(legacyRoot)
	if readErr != nil {
		return "", readErr
	}
	prefix := id + "_"
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(legacyRoot, entry.Name()), nil
		}
	}
	return "", fs.ErrNotExist
}
