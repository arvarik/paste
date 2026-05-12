package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
)

func BenchmarkFindPasteFile(b *testing.B) {
	// Setup a temporary data directory with 1000 files
	tmpDir, err := os.MkdirTemp("", "paste_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldDataDir := storage.DataDir
	storage.DataDir = tmpDir
	defer func() { storage.DataDir = oldDataDir }()

	// Create 1000 files
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("id%04d", i)
		filename := filepath.Join(storage.DataDir, fmt.Sprintf("%s_title%d.txt", id, i))
		os.WriteFile(filename, []byte("test"), 0644)

		storage.GlobalCache.Lock()
		storage.GlobalCache.Items[id] = models.CachedPaste{
			ID:        id,
			Title:     fmt.Sprintf("title%d", i),
			Language:  "text",
			CreatedAt: time.Now(),
			Preview:   "test",
		}
		storage.GlobalCache.Unlock()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// lookup the last file, which is worst-case for the directory scan
		id := "id0999"
		_, err := storage.FindPasteFile(id)
		if err != nil {
			b.Fatal(err)
		}
	}
}
