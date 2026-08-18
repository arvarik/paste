package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
)

// CreateDiff creates a diff through the backward-compatible API.
func CreateDiff(title, base, compare, baseContent, compareContent string) (string, error) {
	id, _, err := CreateDiffWithOptions(title, base, compare, baseContent, compareContent, CreateOptions{})
	return id, err
}

// CreateDiffWithOptions creates a diff and returns its one-time edit secret.
func CreateDiffWithOptions(title, base, compare, baseContent, compareContent string, options CreateOptions) (string, string, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		return "", "", err
	}
	data, err := encodeDiff(base, compare, baseContent, compareContent)
	if err != nil {
		return "", "", err
	}
	if err := checkQuota(0, int64(len(data)), true); err != nil {
		return "", "", err
	}
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		id, err := generateStorageID()
		if err != nil {
			return "", "", err
		}
		if _, err := os.Stat(itemDirectory(models.ItemKindDiff, id)); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		metadata, secret, err := newMetadata(models.ItemKindDiff, id, title, "", "content-r1.json", data, options)
		if err != nil {
			return "", "", err
		}
		if err := reconcileCommittedFile(itemDataPath(metadata), data, createFileExclusive(itemDataPath(metadata), data)); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return "", "", err
		}
		if err := writeMetadata(metadata); err != nil {
			_ = os.RemoveAll(itemDirectory(models.ItemKindDiff, id))
			return "", "", err
		}
		adjustUsage(0, metadata.Size, 1)
		storeDiffCache(cachedDiffFromData(metadata, models.DiffData{
			Base: base, Compare: compare, BaseContent: baseContent, CompareContent: compareContent,
		}))
		return id, secret, nil
	}
	return "", "", fmt.Errorf("allocate diff ID after %d attempts", maxIDAttempts)
}

func encodeDiff(base, compare, baseContent, compareContent string) ([]byte, error) {
	content, err := json.Marshal(models.DiffData{Base: base, Compare: compare, BaseContent: baseContent, CompareContent: compareContent})
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

// GetDiff reads a diff. A burn-after-read diff is deleted after this read.
func GetDiff(id string) (models.CachedDiff, error) {
	GlobalDiffCache.RLock()
	cachedFast, okFast := GlobalDiffCache.Items[id]
	GlobalDiffCache.RUnlock()
	if okFast && (cachedFast.ExpiresAt == nil || cachedFast.ExpiresAt.After(time.Now())) && !cachedFast.BurnAfterRead {
		if cachedFast.Base != "" || cachedFast.Compare != "" || cachedFast.BaseContent != "" || cachedFast.CompareContent != "" || cachedFast.Size == 0 {
			return cachedFast, nil
		}
		metadata := models.ItemMetadata{Kind: models.ItemKindDiff, ID: id, Size: cachedFast.Size, Checksum: cachedFast.Checksum, DataFile: filepath.Base(cachedFast.DataPath)}
		if loaded, err := loadDiff(metadata); err == nil {
			loaded.Title = cachedFast.Title
			loaded.TitleLower = cachedFast.TitleLower
			loaded.CreatedAt = cachedFast.CreatedAt
			loaded.UpdatedAt = cachedFast.UpdatedAt
			loaded.Tags = cachedFast.Tags
			loaded.Favorite = cachedFast.Favorite
			loaded.ExpiresAt = cachedFast.ExpiresAt
			loaded.BurnAfterRead = cachedFast.BurnAfterRead
			loaded.Revision = cachedFast.Revision
			loaded.EditSecretHash = cachedFast.EditSecretHash
			loaded.SearchText = cachedFast.SearchText
			GlobalDiffCache.RLock()
			latest, stillCurrent := GlobalDiffCache.Items[id]
			GlobalDiffCache.RUnlock()
			if stillCurrent && latest.Revision == loaded.Revision && latest.Checksum == loaded.Checksum {
				storeDiffCache(loaded)
			}
			return loaded, nil
		}
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(models.ItemKindDiff, id)
	if err != nil && errors.Is(err, fs.ErrNotExist) && len(id) == 6 {
		if migrationErr := migrateLegacyDiffs(); migrationErr != nil {
			return models.CachedDiff{}, migrationErr
		}
		metadata, err = readMetadata(models.ItemKindDiff, id)
	}
	if err != nil {
		return models.CachedDiff{}, err
	}
	if isExpired(metadata, time.Now()) {
		_ = deleteDiffLocked(id)
		return models.CachedDiff{}, ErrExpired
	}
	GlobalDiffCache.RLock()
	cached, ok := GlobalDiffCache.Items[id]
	GlobalDiffCache.RUnlock()
	if !ok || cached.Revision != metadata.Revision || cached.Checksum != metadata.Checksum ||
		cached.Base == "" && cached.Compare == "" && cached.BaseContent == "" && cached.CompareContent == "" && metadata.Size > 0 {
		cached, err = loadDiff(metadata)
		if err != nil {
			return models.CachedDiff{}, err
		}
		storeDiffCache(cached)
	}
	if metadata.BurnAfterRead {
		if err := deleteDiffLocked(id); err != nil {
			return models.CachedDiff{}, err
		}
	}
	return cached, nil
}

// UpdateDiff updates a diff through the backward-compatible API.
func UpdateDiff(id, title, base, compare, baseContent, compareContent string) error {
	_, err := updateDiff(id, title, base, compare, baseContent, compareContent, "", MetadataPatch{}, false)
	return err
}

// UpdateDiffAuthorized updates a diff after edit-secret verification.
func UpdateDiffAuthorized(id, title, base, compare, baseContent, compareContent, editSecret string, patch MetadataPatch) error {
	_, err := updateDiff(id, title, base, compare, baseContent, compareContent, editSecret, patch, true)
	return err
}

// UpdateDiffAuthorizedWithRevision updates content and returns the committed revision.
func UpdateDiffAuthorizedWithRevision(id, title, base, compare, baseContent, compareContent, editSecret string, patch MetadataPatch) (int64, error) {
	return updateDiff(id, title, base, compare, baseContent, compareContent, editSecret, patch, true)
}

// UpdateDiffTrusted updates a diff for an authenticated admin caller.
func UpdateDiffTrusted(id, title, base, compare, baseContent, compareContent string, patch MetadataPatch) error {
	_, err := updateDiff(id, title, base, compare, baseContent, compareContent, "", patch, false)
	return err
}

// UpdateDiffTrustedWithRevision updates content and returns the committed revision.
func UpdateDiffTrustedWithRevision(id, title, base, compare, baseContent, compareContent string, patch MetadataPatch) (int64, error) {
	return updateDiff(id, title, base, compare, baseContent, compareContent, "", patch, false)
}

func updateDiff(id, title, base, compare, baseContent, compareContent, editSecret string, patch MetadataPatch, requireSecret bool) (int64, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(models.ItemKindDiff, id)
	if err != nil {
		return 0, err
	}
	if isExpired(metadata, time.Now()) {
		return 0, ErrExpired
	}
	if requireSecret && !verifySecretHash(metadata.EditSecretHash, editSecret) {
		return 0, ErrUnauthorized
	}
	if patch.ExpectedRevision != nil && metadata.Revision != *patch.ExpectedRevision {
		return 0, ErrConflict
	}
	data, err := encodeDiff(base, compare, baseContent, compareContent)
	if err != nil {
		return 0, err
	}
	if err := checkQuota(metadata.Size, int64(len(data)), false); err != nil {
		return 0, err
	}
	if err := snapshotRevision(metadata, int64(len(data))-metadata.Size); err != nil {
		return 0, err
	}
	next := metadata
	next.Title = title
	next.UpdatedAt = time.Now().UTC()
	next.Revision++
	next.Size = int64(len(data))
	next.Checksum = checksumBytes(data)
	next.DataFile = fmt.Sprintf("content-r%d.json", next.Revision)
	applyMetadataPatch(&next, patch)
	if err := reconcileCommittedFile(itemDataPath(next), data, createFileExclusive(itemDataPath(next), data)); err != nil {
		return 0, err
	}
	cleanup, err := beginContentCleanup(metadata, next)
	if err != nil {
		_ = removeFileDurably(itemDataPath(next))
		return 0, err
	}
	if err := writeMetadata(next); err != nil {
		return 0, errors.Join(err, recoverContentCleanup())
	}
	adjustUsage(metadata.Size, next.Size, 0)
	storeDiffCache(cachedDiffFromData(next, models.DiffData{
		Base: base, Compare: compare, BaseContent: baseContent, CompareContent: compareContent,
	}))
	_ = finishContentCleanup(cleanup)
	return next.Revision, nil
}

// DeleteDiff deletes a diff through the backward-compatible API.
func DeleteDiff(id string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return deleteDiffLocked(id)
}

// DeleteDiffAuthorized deletes a diff after edit-secret verification.
func DeleteDiffAuthorized(id, editSecret string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(models.ItemKindDiff, id)
	if err != nil {
		return err
	}
	if !verifySecretHash(metadata.EditSecretHash, editSecret) {
		return ErrUnauthorized
	}
	return deleteDiffLocked(id)
}

// DeleteDiffTrusted deletes a diff for an authenticated admin caller.
func DeleteDiffTrusted(id string) error { return DeleteDiff(id) }

func deleteDiffLocked(id string) error {
	return deleteItemLocked(models.ItemKindDiff, id)
}

func diffMeta(cached models.CachedDiff) models.DiffMeta {
	return models.DiffMeta{
		ID: cached.ID, Title: cached.Title, CreatedAt: cached.CreatedAt, UpdatedAt: cached.UpdatedAt,
		Tags: append([]string(nil), cached.Tags...), Favorite: cached.Favorite,
		ExpiresAt: cached.ExpiresAt, BurnAfterRead: cached.BurnAfterRead,
		Revision: cached.Revision, Size: cached.Size,
	}
}

// ListDiffs returns the first bounded diff page.
func ListDiffs() []models.DiffMeta {
	page, _ := ListDiffsPage("", maxPageLimit)
	return page.Items
}

// ListDiffsPage returns a stable cursor page sorted newest first.
func ListDiffsPage(cursor string, limit int) (Page[models.DiffMeta], error) {
	now := time.Now()
	GlobalDiffCache.RLock()
	items := make([]models.DiffMeta, 0, len(GlobalDiffCache.Items))
	for _, cached := range GlobalDiffCache.Items {
		if !cached.BurnAfterRead && (cached.ExpiresAt == nil || cached.ExpiresAt.After(now)) {
			items = append(items, diffMeta(cached))
		}
	}
	GlobalDiffCache.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID < items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	start, err := cursorStart(items, cursor, func(item models.DiffMeta) (time.Time, string) { return item.CreatedAt, item.ID })
	if err != nil {
		return Page[models.DiffMeta]{}, err
	}
	limit = effectiveLimit(limit)
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := Page[models.DiffMeta]{Items: append([]models.DiffMeta{}, items[start:end]...)}
	if end < len(items) && end > start {
		last := items[end-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

// SearchDiffs returns the first bounded search page.
func SearchDiffs(query string) []models.DiffMeta {
	page, _ := SearchDiffsPage(query, "", defaultSearchLimit)
	return page.Items
}

// SearchDiffsPage searches the bounded per-item search index.
func SearchDiffsPage(query, cursor string, limit int) (Page[models.DiffMeta], error) {
	return QueryDiffsPage(query, ItemFilter{}, cursor, limit)
}

// ListDiffsFilteredPage filters metadata before applying the cursor.
func ListDiffsFilteredPage(filter ItemFilter, cursor string, limit int) (Page[models.DiffMeta], error) {
	return QueryDiffsPage("", filter, cursor, limit)
}

// QueryDiffsPage applies search and metadata filters before pagination.
func QueryDiffsPage(query string, filter ItemFilter, cursor string, limit int) (Page[models.DiffMeta], error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" && filter.Tag == "" && filter.Favorite == nil {
		return ListDiffsPage(cursor, limit)
	}
	if len(query) > 256 {
		return Page[models.DiffMeta]{}, errors.New("search query exceeds 256 bytes")
	}
	now := time.Now()
	GlobalDiffCache.RLock()
	items := make([]models.DiffMeta, 0)
	for _, cached := range GlobalDiffCache.Items {
		searchText := cached.SearchText
		if searchText == "" {
			searchText = strings.Join([]string{cached.TitleLower, cached.ContentLower}, "\n")
		}
		queryMatches := query == "" || strings.Contains(searchText, query)
		if !cached.BurnAfterRead && (cached.ExpiresAt == nil || cached.ExpiresAt.After(now)) && queryMatches && matchesItemFilter(cached.Tags, cached.Favorite, filter) {
			items = append(items, diffMeta(cached))
		}
	}
	GlobalDiffCache.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID < items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	start, err := cursorStart(items, cursor, func(item models.DiffMeta) (time.Time, string) { return item.CreatedAt, item.ID })
	if err != nil {
		return Page[models.DiffMeta]{}, err
	}
	limit = effectiveLimit(limit)
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := Page[models.DiffMeta]{Items: append([]models.DiffMeta{}, items[start:end]...)}
	if end < len(items) && end > start {
		last := items[end-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}
