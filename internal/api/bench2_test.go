package api

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

func BenchmarkFindPasteFileCached(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "paste_bench_cached")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldDataDir := storage.DataDir
	storage.DataDir = tmpDir
	defer func() { storage.DataDir = oldDataDir }()

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
		id := "id0999"

		// Simulate finding using cache instead of dir scan
		storage.GlobalCache.RLock()
		cached, ok := storage.GlobalCache.Items[id]
		storage.GlobalCache.RUnlock()

		if ok {
			ext := util.LangToExt(cached.Language)
			filename := fmt.Sprintf("%s_%s%s", id, cached.Title, ext)
			_ = filepath.Join(storage.DataDir, filename)
			// in real life we should also stat the file to see if it exists? or maybe not needed
		} else {
			// fallback
		}
	}
}
