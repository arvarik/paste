package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkFindPasteFile(b *testing.B) {
	// Setup a temporary data directory with 1000 files
	tmpDir, err := os.MkdirTemp("", "paste_bench")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	oldDataDir := dataDir
	dataDir = tmpDir
	defer func() { dataDir = oldDataDir }()

	// Create 1000 files
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
		// lookup the last file, which is worst-case for the directory scan
		id := "id0999"
		_, err := findPasteFile(id)
		if err != nil {
			b.Fatal(err)
		}
	}
}
