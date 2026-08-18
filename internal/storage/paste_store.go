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
	"github.com/arvarik/paste/internal/util"
)

// CreatePaste creates a paste through the backward-compatible API.
func CreatePaste(title, content, language string) (string, error) {
	id, _, err := CreatePasteWithOptions(title, content, language, CreateOptions{})
	return id, err
}

// CreatePasteWithOptions creates a paste and returns its one-time edit secret.
func CreatePasteWithOptions(title, content, language string, options CreateOptions) (string, string, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := ensureStorageLayout(); err != nil {
		return "", "", err
	}
	data := []byte(content)
	if err := checkQuota(0, int64(len(data)), true); err != nil {
		return "", "", err
	}
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		id, err := generateStorageID()
		if err != nil {
			return "", "", err
		}
		if _, err := os.Stat(itemDirectory(models.ItemKindPaste, id)); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}
		dataFile := "content-r1" + util.LangToExt(language)
		metadata, secret, err := newMetadata(models.ItemKindPaste, id, title, language, dataFile, data, options)
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
			_ = os.RemoveAll(itemDirectory(models.ItemKindPaste, id))
			return "", "", err
		}
		adjustUsage(0, metadata.Size, 1)
		storePasteCache(cachedPasteFromData(metadata, data))
		return id, secret, nil
	}
	return "", "", fmt.Errorf("allocate paste ID after %d attempts", maxIDAttempts)
}

// GetPaste reads a paste. A burn-after-read paste is deleted after this read.
func GetPaste(id string) (models.CachedPaste, error) {
	GlobalCache.RLock()
	cached, ok := GlobalCache.Items[id]
	GlobalCache.RUnlock()
	if ok && (cached.ExpiresAt == nil || cached.ExpiresAt.After(time.Now())) && !cached.BurnAfterRead {
		if cached.Content != "" || cached.Size == 0 {
			return cached, nil
		}
		content, err := readStorageFile(cached.DataPath, cached.Size)
		if err == nil && int64(len(content)) == cached.Size && checksumBytes(content) == cached.Checksum {
			cached.Content = string(content)
			GlobalCache.RLock()
			latest, stillCurrent := GlobalCache.Items[id]
			GlobalCache.RUnlock()
			if stillCurrent && latest.Revision == cached.Revision && latest.Checksum == cached.Checksum {
				storePasteCache(cached)
			}
			return cached, nil
		}
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return getPasteLocked(id)
}

func getPasteLocked(id string) (models.CachedPaste, error) {
	cached, metadata, err := readPasteLocked(id)
	if err != nil {
		return models.CachedPaste{}, err
	}
	if metadata.BurnAfterRead {
		if err := deletePasteLocked(id); err != nil {
			return models.CachedPaste{}, err
		}
	}
	return cached, nil
}

func readPasteLocked(id string) (models.CachedPaste, models.ItemMetadata, error) {
	metadata, err := readMetadata(models.ItemKindPaste, id)
	if err != nil && errors.Is(err, fs.ErrNotExist) && len(id) == 6 {
		if migrationErr := migrateLegacyPastes(); migrationErr != nil {
			return models.CachedPaste{}, models.ItemMetadata{}, migrationErr
		}
		metadata, err = readMetadata(models.ItemKindPaste, id)
	}
	if err != nil {
		return models.CachedPaste{}, models.ItemMetadata{}, err
	}
	if isExpired(metadata, time.Now()) {
		_ = deletePasteLocked(id)
		return models.CachedPaste{}, models.ItemMetadata{}, ErrExpired
	}
	GlobalCache.RLock()
	cached, ok := GlobalCache.Items[id]
	GlobalCache.RUnlock()
	if !ok || cached.Revision != metadata.Revision || cached.Checksum != metadata.Checksum || cached.Content == "" && metadata.Size > 0 {
		cached, err = loadPaste(metadata)
		if err != nil {
			return models.CachedPaste{}, models.ItemMetadata{}, err
		}
		storePasteCache(cached)
	}
	return cached, metadata, nil
}

// UsePaste reads one paste and commits burn deletion only after use succeeds.
func UsePaste(id string, use func(models.CachedPaste) error) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	paste, metadata, err := readPasteLocked(id)
	if err != nil {
		return err
	}
	if err := use(paste); err != nil {
		return err
	}
	if metadata.BurnAfterRead {
		return deletePasteLocked(id)
	}
	return nil
}

// GetRawPaste reads raw content and applies expiry and burn rules.
func GetRawPaste(id string) ([]byte, error) {
	paste, err := GetPaste(id)
	if err != nil {
		return nil, err
	}
	return []byte(paste.Content), nil
}

// UpdatePaste updates content through the backward-compatible API.
func UpdatePaste(id, title, content, language string) error {
	_, err := updatePaste(id, title, content, language, "", MetadataPatch{}, false)
	return err
}

// UpdatePasteAuthorized updates content after edit-secret verification.
func UpdatePasteAuthorized(id, title, content, language, editSecret string, patch MetadataPatch) error {
	_, err := updatePaste(id, title, content, language, editSecret, patch, true)
	return err
}

// UpdatePasteAuthorizedWithRevision updates content and returns the committed revision.
func UpdatePasteAuthorizedWithRevision(id, title, content, language, editSecret string, patch MetadataPatch) (int64, error) {
	return updatePaste(id, title, content, language, editSecret, patch, true)
}

// UpdatePasteTrusted updates a paste for an authenticated admin caller.
func UpdatePasteTrusted(id, title, content, language string, patch MetadataPatch) error {
	_, err := updatePaste(id, title, content, language, "", patch, false)
	return err
}

// UpdatePasteTrustedWithRevision updates content and returns the committed revision.
func UpdatePasteTrustedWithRevision(id, title, content, language string, patch MetadataPatch) (int64, error) {
	return updatePaste(id, title, content, language, "", patch, false)
}

func updatePaste(id, title, content, language, editSecret string, patch MetadataPatch, requireSecret bool) (int64, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(models.ItemKindPaste, id)
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
	data := []byte(content)
	if err := checkQuota(metadata.Size, int64(len(data)), false); err != nil {
		return 0, err
	}
	if err := snapshotRevision(metadata, int64(len(data))-metadata.Size); err != nil {
		return 0, err
	}
	next := metadata
	next.Title = title
	next.Language = language
	next.UpdatedAt = time.Now().UTC()
	next.Revision++
	next.Size = int64(len(data))
	next.Checksum = checksumBytes(data)
	next.DataFile = fmt.Sprintf("content-r%d%s", next.Revision, util.LangToExt(language))
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
	storePasteCache(cachedPasteFromData(next, data))
	_ = finishContentCleanup(cleanup)
	return next.Revision, nil
}

// UpdateItemMetadata changes metadata and creates a revision snapshot.
func UpdateItemMetadata(kind models.ItemKind, id, editSecret string, patch MetadataPatch) error {
	return updateItemMetadata(kind, id, editSecret, patch, true)
}

// UpdateItemMetadataTrusted changes metadata for an authenticated admin caller.
func UpdateItemMetadataTrusted(kind models.ItemKind, id string, patch MetadataPatch) error {
	return updateItemMetadata(kind, id, "", patch, false)
}

func updateItemMetadata(kind models.ItemKind, id, editSecret string, patch MetadataPatch, requireSecret bool) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(kind, id)
	if err != nil {
		return err
	}
	if isExpired(metadata, time.Now()) {
		return ErrExpired
	}
	if requireSecret && !verifySecretHash(metadata.EditSecretHash, editSecret) {
		return ErrUnauthorized
	}
	if patch.ExpectedRevision != nil && metadata.Revision != *patch.ExpectedRevision {
		return ErrConflict
	}
	data, err := readStorageFile(itemDataPath(metadata), metadata.Size)
	if err != nil {
		return err
	}
	var diffData models.DiffData
	if kind == models.ItemKindDiff {
		if err := json.Unmarshal(data, &diffData); err != nil {
			return fmt.Errorf("%w: decode diff metadata update: %v", ErrCorrupt, err)
		}
	}
	if err := snapshotRevision(metadata, 0); err != nil {
		return err
	}
	metadata.UpdatedAt = time.Now().UTC()
	metadata.Revision++
	applyMetadataPatch(&metadata, patch)
	if err := writeMetadata(metadata); err != nil {
		return err
	}
	if kind == models.ItemKindPaste {
		storePasteCache(cachedPasteFromData(metadata, data))
	} else {
		storeDiffCache(cachedDiffFromData(metadata, diffData))
	}
	return nil
}

func applyMetadataPatch(metadata *models.ItemMetadata, patch MetadataPatch) {
	if patch.Tags != nil {
		metadata.Tags = canonicalTags(*patch.Tags)
	}
	if patch.Favorite != nil {
		metadata.Favorite = *patch.Favorite
	}
	if patch.ExpiresAt != nil {
		metadata.ExpiresAt = *patch.ExpiresAt
	}
	if patch.BurnAfterRead != nil {
		metadata.BurnAfterRead = *patch.BurnAfterRead
	}
}

// DeletePaste deletes a paste through the backward-compatible API.
func DeletePaste(id string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return deletePasteLocked(id)
}

// DeletePasteAuthorized deletes a paste after edit-secret verification.
func DeletePasteAuthorized(id, editSecret string) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	metadata, err := readMetadata(models.ItemKindPaste, id)
	if err != nil {
		return err
	}
	if !verifySecretHash(metadata.EditSecretHash, editSecret) {
		return ErrUnauthorized
	}
	return deletePasteLocked(id)
}

// DeletePasteTrusted deletes a paste for an authenticated admin caller.
func DeletePasteTrusted(id string) error { return DeletePaste(id) }

func deletePasteLocked(id string) error {
	return deleteItemLocked(models.ItemKindPaste, id)
}

func pasteMeta(cached models.CachedPaste) models.PasteMeta {
	preview := cached.Preview
	if cached.BurnAfterRead {
		preview = ""
	}
	return models.PasteMeta{
		ID: cached.ID, Title: cached.Title, Language: cached.Language,
		CreatedAt: cached.CreatedAt, UpdatedAt: cached.UpdatedAt,
		Preview: preview, LineCount: cached.LineCount,
		Tags: append([]string(nil), cached.Tags...), Favorite: cached.Favorite,
		ExpiresAt: cached.ExpiresAt, BurnAfterRead: cached.BurnAfterRead,
		Revision: cached.Revision, Size: cached.Size,
	}
}

// ListPastes returns the first bounded paste page.
func ListPastes() []models.PasteMeta {
	page, _ := ListPastesPage("", maxPageLimit)
	return page.Items
}

// ListPastesPage returns a stable cursor page sorted newest first.
func ListPastesPage(cursor string, limit int) (Page[models.PasteMeta], error) {
	now := time.Now()
	GlobalCache.RLock()
	items := make([]models.PasteMeta, 0, len(GlobalCache.Items))
	for _, cached := range GlobalCache.Items {
		if !cached.BurnAfterRead && (cached.ExpiresAt == nil || cached.ExpiresAt.After(now)) {
			items = append(items, pasteMeta(cached))
		}
	}
	GlobalCache.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID < items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	start, err := cursorStart(items, cursor, func(item models.PasteMeta) (time.Time, string) { return item.CreatedAt, item.ID })
	if err != nil {
		return Page[models.PasteMeta]{}, err
	}
	limit = effectiveLimit(limit)
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := Page[models.PasteMeta]{Items: append([]models.PasteMeta{}, items[start:end]...)}
	if end < len(items) && end > start {
		last := items[end-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

// SearchPastes returns the first bounded search page.
func SearchPastes(query string) []models.PasteMeta {
	page, _ := SearchPastesPage(query, "", defaultSearchLimit)
	return page.Items
}

// SearchPastesPage searches the bounded per-item search index.
func SearchPastesPage(query, cursor string, limit int) (Page[models.PasteMeta], error) {
	return QueryPastesPage(query, ItemFilter{}, cursor, limit)
}

// ListPastesFilteredPage filters metadata before applying the cursor.
func ListPastesFilteredPage(filter ItemFilter, cursor string, limit int) (Page[models.PasteMeta], error) {
	return QueryPastesPage("", filter, cursor, limit)
}

// QueryPastesPage applies search and metadata filters before pagination.
func QueryPastesPage(query string, filter ItemFilter, cursor string, limit int) (Page[models.PasteMeta], error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" && filter.Tag == "" && filter.Favorite == nil {
		return ListPastesPage(cursor, limit)
	}
	if len(query) > 256 {
		return Page[models.PasteMeta]{}, errors.New("search query exceeds 256 bytes")
	}
	now := time.Now()
	GlobalCache.RLock()
	items := make([]models.PasteMeta, 0)
	for _, cached := range GlobalCache.Items {
		searchText := cached.SearchText
		if searchText == "" {
			searchText = strings.Join([]string{cached.TitleLower, cached.LanguageLower, cached.ContentLower}, "\n")
		}
		queryMatches := query == "" || strings.Contains(searchText, query)
		if !cached.BurnAfterRead && (cached.ExpiresAt == nil || cached.ExpiresAt.After(now)) && queryMatches && matchesItemFilter(cached.Tags, cached.Favorite, filter) {
			item := pasteMeta(cached)
			if !cached.BurnAfterRead {
				item.Preview = util.GetHighlightedPreview(cached.Content, query)
			}
			items = append(items, item)
		}
	}
	GlobalCache.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID < items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})
	start, err := cursorStart(items, cursor, func(item models.PasteMeta) (time.Time, string) { return item.CreatedAt, item.ID })
	if err != nil {
		return Page[models.PasteMeta]{}, err
	}
	limit = effectiveLimit(limit)
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := Page[models.PasteMeta]{Items: append([]models.PasteMeta{}, items[start:end]...)}
	if end < len(items) && end > start {
		last := items[end-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

// PurgeExpired removes all expired pastes and diffs.
func PurgeExpired(now time.Time) (int, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	removed := 0
	for _, kind := range []models.ItemKind{models.ItemKindPaste, models.ItemKindDiff} {
		entries, err := os.ReadDir(itemRoot(kind))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, err
		}
		for _, entry := range entries {
			metadata, err := readMetadata(kind, entry.Name())
			if err != nil || !isExpired(metadata, now) {
				continue
			}
			if err := deleteItemLocked(kind, metadata.ID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

var _ = filepath.Base
