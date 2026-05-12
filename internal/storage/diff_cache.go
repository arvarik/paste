package storage

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/arvarik/paste/internal/models"
)

// DiffCache is a thread-safe in-memory index of all saved diffs.
type DiffCache struct {
	sync.RWMutex
	Items map[string]models.CachedDiff
}

// GlobalDiffCache is the singleton in-memory search index for diffs.
var GlobalDiffCache = &DiffCache{
	Items: make(map[string]models.CachedDiff),
}

// LoadDiffCacheFromDisk reads all diff files from the data/diffs directory.
func LoadDiffCacheFromDisk() {
	start := time.Now()
	diffsDir := filepath.Join(DataDir, "diffs")
	
	if err := os.MkdirAll(diffsDir, 0755); err != nil {
		log.Printf("[cache] error creating diffs dir: %v", err)
		return
	}

	entries, err := os.ReadDir(diffsDir)
	if err != nil {
		log.Printf("[cache] warning: failed to read diffs dir: %v", err)
		return
	}

	GlobalDiffCache.Lock()
	defer GlobalDiffCache.Unlock()

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		id := parts[0]
		title := strings.TrimSuffix(parts[1], ".json")

		info, err := entry.Info()
		if err != nil {
			continue
		}

		filePath := filepath.Join(diffsDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Printf("[cache] warning: failed to read %s: %v", filePath, err)
			continue
		}

		var data models.DiffData
		if err := json.Unmarshal(content, &data); err != nil {
			log.Printf("[cache] warning: failed to parse json for %s: %v", filePath, err)
			continue
		}

		combinedContent := data.BaseContent + "\n" + data.CompareContent

		GlobalDiffCache.Items[id] = models.CachedDiff{
			ID:             id,
			Title:          title,
			TitleLower:     strings.ToLower(title),
			Base:           data.Base,
			Compare:        data.Compare,
			BaseContent:    data.BaseContent,
			CompareContent: data.CompareContent,
			ContentLower:   strings.ToLower(combinedContent),
			CreatedAt:      info.ModTime(),
		}
		loaded++
	}

	log.Printf("[cache] loaded %d diffs into memory in %v", loaded, time.Since(start))
}

func FindDiffFile(id string) (string, error) {
	diffsDir := filepath.Join(DataDir, "diffs")
	entries, err := os.ReadDir(diffsDir)
	if err != nil {
		return "", err
	}
	
	prefix := id + "_"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(diffsDir, entry.Name()), nil
		}
	}
	return "", os.ErrNotExist
}
