package storage

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

// CreatePaste creates a new paste file on disk and updates the in-memory cache.
func CreatePaste(title, content, language string) (string, error) {
	ext := util.LangToExt(language)

	var id string
	for {
		id = util.GenerateID()
		_, err := FindPasteFile(id)
		if err == os.ErrNotExist {
			break
		}
	}

	filename := fmt.Sprintf("%s_%s%s", id, title, ext)
	filePath := filepath.Join(DataDir, filename)

	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return "", err
	}

	GlobalCache.Lock()
	GlobalCache.Items[id] = models.CachedPaste{
		ID:            id,
		Title:         title,
		TitleLower:    strings.ToLower(title),
		Content:       content,
		ContentLower:  strings.ToLower(content),
		Language:      language,
		LanguageLower: strings.ToLower(language),
		CreatedAt:     time.Now(),
		Preview:       util.GetPreview(content),
		LineCount:     strings.Count(content, "\n") + 1,
	}
	GlobalCache.Unlock()

	return id, nil
}

// GetPaste returns the cached paste data. If not in cache but on disk, it self-heals.
func GetPaste(id string) (models.CachedPaste, error) {
	filePath, err := FindPasteFile(id)
	if err != nil {
		return models.CachedPaste{}, err
	}

	GlobalCache.RLock()
	cached, ok := GlobalCache.Items[id]
	GlobalCache.RUnlock()
	if ok {
		return cached, nil
	}

	// Self-heal
	content, err := os.ReadFile(filePath)
	if err != nil {
		return models.CachedPaste{}, err
	}

	filename := filepath.Base(filePath)
	parts := strings.SplitN(filename, "_", 2)
	title := ""
	language := "text"
	if len(parts) == 2 {
		ext := filepath.Ext(filename)
		title = strings.TrimSuffix(parts[1], ext)
		language = util.ExtToLang(ext)
	}

	info, infoErr := os.Stat(filePath)
	createdAt := time.Now()
	if infoErr == nil {
		createdAt = info.ModTime()
	}

	cached = models.CachedPaste{
		ID:            id,
		Title:         title,
		TitleLower:    strings.ToLower(title),
		Content:       string(content),
		ContentLower:  strings.ToLower(string(content)),
		Language:      language,
		LanguageLower: strings.ToLower(language),
		CreatedAt:     createdAt,
		Preview:       util.GetPreview(string(content)),
		LineCount:     strings.Count(string(content), "\n") + 1,
	}

	GlobalCache.Lock()
	GlobalCache.Items[id] = cached
	GlobalCache.Unlock()

	return cached, nil
}

// GetRawPaste reads the raw paste content directly from disk.
func GetRawPaste(id string) ([]byte, error) {
	filePath, err := FindPasteFile(id)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filePath)
}

// UpdatePaste overwrites an existing paste and updates the cache.
func UpdatePaste(id, title, content, language string) error {
	oldPath, err := FindPasteFile(id)
	if err != nil {
		return err
	}

	ext := util.LangToExt(language)

	// Remove old file
	if err := os.Remove(oldPath); err != nil {
		return err
	}

	// Write new file
	filename := fmt.Sprintf("%s_%s%s", id, title, ext)
	filePath := filepath.Join(DataDir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}

	GlobalCache.Lock()
	old := GlobalCache.Items[id]
	GlobalCache.Items[id] = models.CachedPaste{
		ID:            id,
		Title:         title,
		TitleLower:    strings.ToLower(title),
		Content:       content,
		ContentLower:  strings.ToLower(content),
		Language:      language,
		LanguageLower: strings.ToLower(language),
		CreatedAt:     old.CreatedAt, // retain original creation time
		Preview:       util.GetPreview(content),
		LineCount:     strings.Count(content, "\n") + 1,
	}
	GlobalCache.Unlock()

	return nil
}

// DeletePaste removes the paste from disk and cache.
func DeletePaste(id string) error {
	filePath, err := FindPasteFile(id)
	if err != nil {
		return err
	}

	if err := os.Remove(filePath); err != nil {
		return err
	}

	GlobalCache.Lock()
	delete(GlobalCache.Items, id)
	GlobalCache.Unlock()

	return nil
}

// ListPastes returns a slice of all PasteMeta objects from the cache.
func ListPastes() []models.PasteMeta {
	GlobalCache.RLock()
	var pastes []models.PasteMeta
	for _, cached := range GlobalCache.Items {
		pastes = append(pastes, models.PasteMeta{
			ID:        cached.ID,
			Title:     cached.Title,
			Language:  cached.Language,
			CreatedAt: cached.CreatedAt,
			Preview:   cached.Preview,
			LineCount: cached.LineCount,
		})
	}
	GlobalCache.RUnlock()

	sort.Slice(pastes, func(i, j int) bool {
		return pastes[i].CreatedAt.After(pastes[j].CreatedAt)
	})

	return pastes
}

// SearchPastes returns pastes matching the query, highlighting the preview if necessary.
func SearchPastes(query string) []models.PasteMeta {
	escapedQuery := html.EscapeString(query)
	re := regexp.MustCompile("(?i)(" + regexp.QuoteMeta(escapedQuery) + ")")

	GlobalCache.RLock()
	defer GlobalCache.RUnlock()

	var results []models.PasteMeta
	for _, paste := range GlobalCache.Items {
		if strings.Contains(paste.TitleLower, query) ||
			strings.Contains(paste.ContentLower, query) ||
			strings.Contains(paste.LanguageLower, query) {

			results = append(results, models.PasteMeta{
				ID:        paste.ID,
				Title:     paste.Title,
				Language:  paste.Language,
				CreatedAt: paste.CreatedAt,
				Preview:   util.GetHighlightedPreview(paste.Content, query, re),
				LineCount: paste.LineCount,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	return results
}
