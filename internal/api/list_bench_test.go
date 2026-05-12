package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

func BenchmarkHandleListPastes(b *testing.B) {
	storage.GlobalCache.Lock()
	storage.GlobalCache.Items = make(map[string]models.CachedPaste)
	content := strings.Repeat("This is some sample content with whitespace \n\t \n ", 10)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("id%04d", i)
		storage.GlobalCache.Items[id] = models.CachedPaste{
			ID:            id,
			Title:         fmt.Sprintf("title%d", i),
			TitleLower:    fmt.Sprintf("title%d", i),
			Content:       content,
			ContentLower:  strings.ToLower(content),
			Language:      "text",
			LanguageLower: "text",
			CreatedAt:     time.Now(),
			Preview:       util.GetPreview(content),
		}
	}
	storage.GlobalCache.Unlock()

	req := httptest.NewRequest("GET", "/api/pastes", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handleListPastes(w, req)
	}
}
