package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
)

type createdPasteResponse struct {
	ID         string `json:"id"`
	EditSecret string `json:"editSecret"`
}

func dxSetupTestEnv(t *testing.T) func() {
	t.Helper()
	originalDataDir := storage.DataDir
	storage.DataDir = t.TempDir()
	storage.GlobalCache.Lock()
	storage.GlobalCache.Items = make(map[string]models.CachedPaste)
	storage.GlobalCache.Unlock()
	storage.GlobalDiffCache.Lock()
	storage.GlobalDiffCache.Items = make(map[string]models.CachedDiff)
	storage.GlobalDiffCache.Unlock()
	return func() {
		storage.DataDir = originalDataDir
		storage.GlobalCache.Lock()
		storage.GlobalCache.Items = make(map[string]models.CachedPaste)
		storage.GlobalCache.Unlock()
		storage.GlobalDiffCache.Lock()
		storage.GlobalDiffCache.Items = make(map[string]models.CachedDiff)
		storage.GlobalDiffCache.Unlock()
	}
}

func dxRequest(t *testing.T, handler http.Handler, method, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(encoded))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func dxCreatePaste(t *testing.T, handler http.Handler, content string) createdPasteResponse {
	t.Helper()
	recorder := dxRequest(t, handler, http.MethodPost, "/api/pastes", map[string]any{
		"title": "contract", "content": content, "language": "text",
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("POST /api/pastes = %d: %s", recorder.Code, recorder.Body.String())
	}
	var created createdPasteResponse
	if err := json.NewDecoder(recorder.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" || created.EditSecret == "" {
		t.Fatalf("create response = %#v", created)
	}
	return created
}

func TestRawPasteReturnsPlainTextContent(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	content := "package main\n\nfunc main() {}"
	created := dxCreatePaste(t, handler, content)

	recorder := dxRequest(t, handler, http.MethodGet, "/raw/"+created.ID, nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET raw = %d: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if recorder.Body.String() != content {
		t.Fatalf("raw body = %q, want %q", recorder.Body.String(), content)
	}
}

func TestListAndSearchUseCursorEnvelopesWithLineCounts(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	dxCreatePaste(t, handler, "alpha\nbeta\ngamma")

	for _, target := range []string{"/api/pastes?limit=10", "/api/search?q=beta&limit=10"} {
		recorder := dxRequest(t, handler, http.MethodGet, target, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", target, recorder.Code, recorder.Body.String())
		}
		var page storage.Page[models.PasteMeta]
		if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", target, err)
		}
		if len(page.Items) != 1 || page.Items[0].LineCount != 3 {
			t.Fatalf("GET %s page = %#v", target, page)
		}
	}
}

func TestPasteMutationRequiresEditSecret(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	created := dxCreatePaste(t, handler, "before")
	body := map[string]any{"title": "changed", "content": "after", "language": "text", "revision": 1}

	denied := dxRequest(t, handler, http.MethodPut, "/api/pastes/"+created.ID, body, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("PUT without secret = %d, want 403", denied.Code)
	}
	updated := dxRequest(t, handler, http.MethodPut, "/api/pastes/"+created.ID, body, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT with secret = %d: %s", updated.Code, updated.Body.String())
	}
	deleted := dxRequest(t, handler, http.MethodDelete, "/api/pastes/"+created.ID, nil, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("DELETE with secret = %d: %s", deleted.Code, deleted.Body.String())
	}
}
