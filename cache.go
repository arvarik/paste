package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// PasteCache is a thread-safe in-memory index of all paste content.
// It enables instant search without disk I/O on every query.
type PasteCache struct {
	sync.RWMutex
	items map[string]CachedPaste
}

// CachedPaste holds the metadata and full text content of a single paste
// as loaded into the in-memory cache.
type CachedPaste struct {
	ID            string
	Title         string
	TitleLower    string
	Content       string
	ContentLower  string
	Language      string
	LanguageLower string
	CreatedAt     time.Time
}

// globalCache is the singleton in-memory search index, populated at startup
// and kept in sync with every create/delete operation.
var globalCache = &PasteCache{
	items: make(map[string]CachedPaste),
}

// loadCacheFromDisk reads all paste files from the data directory into the
// in-memory cache. Called once on server startup to warm the search index.
func loadCacheFromDisk() {
	start := time.Now()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		log.Printf("[cache] warning: failed to read data dir: %v", err)
		return
	}

	globalCache.Lock()
	defer globalCache.Unlock()

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
		language := extToLang(ext)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(dataDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("[cache] warning: failed to read %s: %v", filePath, err)
			continue
		}

		globalCache.items[id] = CachedPaste{
			ID:            id,
			Title:         title,
			TitleLower:    strings.ToLower(title),
			Content:       string(content),
			ContentLower:  strings.ToLower(string(content)),
			Language:      language,
			LanguageLower: strings.ToLower(language),
			CreatedAt:     info.ModTime(),
		}
		loaded++
	}

	log.Printf("[cache] loaded %d pastes into memory in %v", loaded, time.Since(start))
}
