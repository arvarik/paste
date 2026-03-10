package main

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func BenchmarkHandleListPastes(b *testing.B) {
	globalCache.Lock()
	globalCache.items = make(map[string]CachedPaste)
	content := strings.Repeat("This is some sample content with whitespace \n\t \n ", 10)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("id%04d", i)
		globalCache.items[id] = CachedPaste{
			ID:            id,
			Title:         fmt.Sprintf("title%d", i),
			TitleLower:    fmt.Sprintf("title%d", i),
			Content:       content,
			ContentLower:  strings.ToLower(content),
			Language:      "text",
			LanguageLower: "text",
			CreatedAt:     time.Now(),
			Preview:       getPreview(content),
		}
	}
	globalCache.Unlock()

	req := httptest.NewRequest("GET", "/api/pastes", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handleListPastes(w, req)
	}
}
