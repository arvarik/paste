package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

func BenchmarkSearchPastes(b *testing.B) {
	// Setup cache with dummy data
	storage.GlobalCache.Lock()
	storage.GlobalCache.Items = make(map[string]models.CachedPaste)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("id%d", i)
		title := fmt.Sprintf("Title %d", i)
		content := fmt.Sprintf("This is the content of paste %d which is a bit long to simulate real content. Here is some more content so that strings.ToLower takes some time to process this string.", i)
		language := "text"
		storage.GlobalCache.Items[id] = models.CachedPaste{
			ID:            id,
			Title:         title,
			TitleLower:    strings.ToLower(title),
			Content:       content,
			ContentLower:  strings.ToLower(content),
			Language:      language,
			LanguageLower: strings.ToLower(language),
			CreatedAt:     time.Now(),
			Preview:       util.GetPreview(content),
		}
	}
	storage.GlobalCache.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=content+of+paste+5000", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handleSearchPastes(w, req)
	}
}
