package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

// CreateDiff creates a new diff file on disk and updates the in-memory cache.
func CreateDiff(title, base, compare, baseContent, compareContent string) (string, error) {
	var id string
	for {
		id = util.GenerateID()
		_, err := FindDiffFile(id)
		if err == os.ErrNotExist {
			break
		}
	}

	filename := fmt.Sprintf("%s_%s.json", id, title)
	diffsDir := filepath.Join(DataDir, "diffs")
	os.MkdirAll(diffsDir, 0755)
	filePath := filepath.Join(diffsDir, filename)

	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	diffData := models.DiffData{
		Base:           base,
		Compare:        compare,
		BaseContent:    baseContent,
		CompareContent: compareContent,
	}
	if err := json.NewEncoder(file).Encode(diffData); err != nil {
		return "", err
	}

	combinedContent := baseContent + "\n" + compareContent

	GlobalDiffCache.Lock()
	GlobalDiffCache.Items[id] = models.CachedDiff{
		ID:             id,
		Title:          title,
		TitleLower:     strings.ToLower(title),
		Base:           base,
		Compare:        compare,
		BaseContent:    baseContent,
		CompareContent: compareContent,
		ContentLower:   strings.ToLower(combinedContent),
		CreatedAt:      time.Now(),
	}
	GlobalDiffCache.Unlock()

	return id, nil
}

// GetDiff returns the cached diff data.
func GetDiff(id string) (models.CachedDiff, error) {
	GlobalDiffCache.RLock()
	cached, ok := GlobalDiffCache.Items[id]
	GlobalDiffCache.RUnlock()

	if !ok {
		return models.CachedDiff{}, os.ErrNotExist
	}
	return cached, nil
}

// DeleteDiff removes the diff from disk and cache.
func DeleteDiff(id string) error {
	filePath, err := FindDiffFile(id)
	if err != nil {
		return err
	}

	if err := os.Remove(filePath); err != nil {
		return err
	}

	GlobalDiffCache.Lock()
	delete(GlobalDiffCache.Items, id)
	GlobalDiffCache.Unlock()

	return nil
}

// UpdateDiff overwrites an existing diff and updates the cache.
func UpdateDiff(id, title, base, compare, baseContent, compareContent string) error {
	oldPath, err := FindDiffFile(id)
	if err != nil {
		return err
	}

	if err := os.Remove(oldPath); err != nil {
		return err
	}

	diffsDir := filepath.Join(DataDir, "diffs")
	filename := fmt.Sprintf("%s_%s.json", id, title)
	filePath := filepath.Join(diffsDir, filename)

	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	diffData := models.DiffData{
		Base:           base,
		Compare:        compare,
		BaseContent:    baseContent,
		CompareContent: compareContent,
	}
	if err := json.NewEncoder(file).Encode(diffData); err != nil {
		return err
	}

	combinedContent := baseContent + "\n" + compareContent

	GlobalDiffCache.Lock()
	old := GlobalDiffCache.Items[id]
	GlobalDiffCache.Items[id] = models.CachedDiff{
		ID:             id,
		Title:          title,
		TitleLower:     strings.ToLower(title),
		Base:           base,
		Compare:        compare,
		BaseContent:    baseContent,
		CompareContent: compareContent,
		ContentLower:   strings.ToLower(combinedContent),
		CreatedAt:      old.CreatedAt, // retain original creation time
	}
	GlobalDiffCache.Unlock()

	return nil
}

// ListDiffs returns a slice of all DiffMeta objects from the cache.
func ListDiffs() []models.DiffMeta {
	GlobalDiffCache.RLock()
	var diffs []models.DiffMeta
	for _, cached := range GlobalDiffCache.Items {
		diffs = append(diffs, models.DiffMeta{
			ID:        cached.ID,
			Title:     cached.Title,
			CreatedAt: cached.CreatedAt,
		})
	}
	GlobalDiffCache.RUnlock()

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].CreatedAt.After(diffs[j].CreatedAt)
	})

	return diffs
}

// SearchDiffs returns diffs matching the query.
func SearchDiffs(query string) []models.DiffMeta {
	queryLower := strings.ToLower(query)
	var results []models.DiffMeta

	GlobalDiffCache.RLock()
	for _, cached := range GlobalDiffCache.Items {
		if strings.Contains(cached.TitleLower, queryLower) || strings.Contains(cached.ContentLower, queryLower) {
			results = append(results, models.DiffMeta{
				ID:        cached.ID,
				Title:     cached.Title,
				CreatedAt: cached.CreatedAt,
			})
		}
	}
	GlobalDiffCache.RUnlock()

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results
}
