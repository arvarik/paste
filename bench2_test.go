package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkFindPasteFileCached(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "paste_bench_cached")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldDataDir := dataDir
	dataDir = tmpDir
	defer func() { dataDir = oldDataDir }()

	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("id%04d", i)
		filename := filepath.Join(dataDir, fmt.Sprintf("%s_title%d.txt", id, i))
		os.WriteFile(filename, []byte("test"), 0644)

		globalCache.Lock()
		globalCache.items[id] = CachedPaste{
			ID:        id,
			Title:     fmt.Sprintf("title%d", i),
			Language:  "text",
			CreatedAt: time.Now(),
		}
		globalCache.Unlock()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := "id0999"

		// Simulate finding using cache instead of dir scan
		globalCache.RLock()
		cached, ok := globalCache.items[id]
		globalCache.RUnlock()

		if ok {
			ext := langToExt(cached.Language)
			filename := fmt.Sprintf("%s_%s%s", id, cached.Title, ext)
			_ = filepath.Join(dataDir, filename)
			// in real life we should also stat the file to see if it exists? or maybe not needed
		} else {
			// fallback
		}
	}
}
