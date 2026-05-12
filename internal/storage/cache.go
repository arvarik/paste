package storage

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/util"
)

// DataDir is the filesystem path where paste files are stored.
var DataDir = util.GetEnv("DATA_DIR", "/app/data")

// PasteCache is a thread-safe in-memory index of all paste content.
// It enables instant search without disk I/O on every query.
type PasteCache struct {
	sync.RWMutex
	Items map[string]models.CachedPaste
}

// GlobalCache is the singleton in-memory search index, populated at startup
// and kept in sync with every create/delete operation.
var GlobalCache = &PasteCache{
	Items: make(map[string]models.CachedPaste),
}

// LoadCacheFromDisk reads all paste files from the data directory into the
// in-memory cache. Called once on server startup to warm the search index.
func LoadCacheFromDisk() {
	start := time.Now()
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		log.Printf("[cache] warning: failed to read data dir: %v", err)
		return
	}

	GlobalCache.Lock()
	defer GlobalCache.Unlock()

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Filename format: {id}_{title}.{ext}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			continue
		}

		id := parts[0]
		ext := filepath.Ext(entry.Name())
		title := strings.TrimSuffix(parts[1], ext)
		language := util.ExtToLang(ext)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(DataDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("[cache] warning: failed to read %s: %v", filePath, err)
			continue
		}

		GlobalCache.Items[id] = models.CachedPaste{
			ID:            id,
			Title:         title,
			TitleLower:    strings.ToLower(title),
			Content:       string(content),
			ContentLower:  strings.ToLower(string(content)),
			Language:      language,
			LanguageLower: strings.ToLower(language),
			CreatedAt:     info.ModTime(),
			Preview:       util.GetPreview(string(content)),
			LineCount:     strings.Count(string(content), "\n") + 1,
		}
		loaded++
	}

	log.Printf("[cache] loaded %d pastes into memory in %v", loaded, time.Since(start))
}

// FindPasteFile searches the data directory for a file matching the given ID prefix.
// It avoids filepath.Glob to prevent wildcard expansion vulnerabilities.
func FindPasteFile(id string) (string, error) {
	// Fast path: attempt O(1) in-memory cache lookup
	GlobalCache.RLock()
	cached, ok := GlobalCache.Items[id]
	GlobalCache.RUnlock()

	if ok {
		ext := util.LangToExt(cached.Language)
		filename := id + "_" + cached.Title + ext
		filePath := filepath.Join(DataDir, filename)
		// Verify file actually exists on disk (handles manual deletions)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, nil
		}
	}

	// Slow path: fallback to O(N) directory scan
	// This happens for entirely new IDs or self-healing uncached files
	entries, err := os.ReadDir(DataDir)
	if err != nil {
		return "", err
	}

	prefix := id + "_"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(DataDir, entry.Name()), nil
		}
	}

	return "", os.ErrNotExist
}
