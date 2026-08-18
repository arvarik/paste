package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
)

// qualityMux returns the production API route table.
func qualityMux() *http.ServeMux {
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	return mux
}

func TestEmptyListResponsesUseJSONArrays(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()

	for _, path := range []string{"/api/pastes", "/api/search?q=missing", "/api/saved_diffs", "/api/search_diffs?q=missing"} {
		recorder := httptest.NewRecorder()
		qualityMux().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, recorder.Code)
		}
		var response struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("decode GET %s: %v", path, err)
		}
		if response.Items == nil || len(response.Items) != 0 {
			t.Fatalf("GET %s items = %#v, want an empty array", path, response.Items)
		}
	}
}

func TestCreatePasteRejectsUnknownAndTrailingJSON(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()

	tests := []string{
		`{"title":"test","content":"value","language":"text","unknown":true}`,
		`{"title":"test","content":"value","language":"text"} {"second":true}`,
	}
	for _, body := range tests {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		qualityMux().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/pastes with %q = %d, want 400", body, recorder.Code)
		}
	}
}

func TestJSONRequestsReportMediaTypeAndSizeErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader(`{"content":"value"}`))
	request.Header.Set("Content-Type", "text/plain")
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("POST text body = %d, want 415", recorder.Code)
	}

	largeBody := `{"content":"` + strings.Repeat("x", maxRequestBodySize+1) + `"}`
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader(largeBody))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST oversized body = %d, want 413", recorder.Code)
	}
}

func TestPasteAPICompleteCRUD(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()

	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/pastes", strings.NewReader(
		`{"title":"API Paste","content":"first","language":"text"}`,
	))
	createRequest.Header.Set("Content-Type", "application/json")
	qualityMux().ServeHTTP(create, createRequest)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST paste = %d, want 201: %s", create.Code, create.Body.String())
	}
	var created createdPasteResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id := created.ID

	get := httptest.NewRecorder()
	qualityMux().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/pastes/"+id, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"content":"first"`) {
		t.Fatalf("GET paste = %d %s", get.Code, get.Body.String())
	}

	update := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/pastes/"+id, strings.NewReader(
		`{"title":"API Paste Updated","content":"second","language":"text","revision":1}`,
	))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set(editSecretHeader, created.EditSecret)
	qualityMux().ServeHTTP(update, updateRequest)
	if update.Code != http.StatusOK {
		t.Fatalf("PUT paste = %d, want 200: %s", update.Code, update.Body.String())
	}

	remove := httptest.NewRecorder()
	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/pastes/"+id, nil)
	removeRequest.Header.Set(editSecretHeader, created.EditSecret)
	qualityMux().ServeHTTP(remove, removeRequest)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("DELETE paste = %d, want 204", remove.Code)
	}
}

func TestDiffAPICompleteCRUD(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()

	create := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/saved_diffs", strings.NewReader(
		`{"title":"API Diff","base":"RAW","compare":"RAW","baseContent":"first","compareContent":"second"}`,
	))
	createRequest.Header.Set("Content-Type", "application/json")
	qualityMux().ServeHTTP(create, createRequest)
	if create.Code != http.StatusCreated {
		t.Fatalf("POST diff = %d, want 201: %s", create.Code, create.Body.String())
	}
	var created createdPasteResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatalf("decode diff response: %v", err)
	}
	id := created.ID

	get := httptest.NewRecorder()
	qualityMux().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/saved_diffs/"+id, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"compareContent":"second"`) {
		t.Fatalf("GET diff = %d %s", get.Code, get.Body.String())
	}

	update := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(http.MethodPut, "/api/saved_diffs/"+id, strings.NewReader(
		`{"title":"Updated Diff","base":"RAW","compare":"RAW","baseContent":"first","compareContent":"third","revision":1}`,
	))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRequest.Header.Set(editSecretHeader, created.EditSecret)
	qualityMux().ServeHTTP(update, updateRequest)
	if update.Code != http.StatusOK {
		t.Fatalf("PUT diff = %d, want 200: %s", update.Code, update.Body.String())
	}

	remove := httptest.NewRecorder()
	removeRequest := httptest.NewRequest(http.MethodDelete, "/api/saved_diffs/"+id, nil)
	removeRequest.Header.Set(editSecretHeader, created.EditSecret)
	qualityMux().ServeHTTP(remove, removeRequest)
	if remove.Code != http.StatusNoContent {
		t.Fatalf("DELETE diff = %d, want 204", remove.Code)
	}
}

func TestMissingMutationsReturnNotFound(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()

	deleteRecorder := httptest.NewRecorder()
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/pastes/abc123", nil)
	qualityMux().ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing paste = %d, want 404", deleteRecorder.Code)
	}

	updateRecorder := httptest.NewRecorder()
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/saved_diffs/abc123",
		strings.NewReader(`{"title":"missing","base":"RAW","compare":"RAW","baseContent":"a","compareContent":"b"}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	qualityMux().ServeHTTP(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusNotFound {
		t.Fatalf("PUT missing diff = %d, want 404", updateRecorder.Code)
	}
}

func TestSearchDiffsReturnsFlatItems(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	t.Cleanup(func() {
		storage.GlobalDiffCache.Lock()
		storage.GlobalDiffCache.Items = make(map[string]models.CachedDiff)
		storage.GlobalDiffCache.Unlock()
	})
	storage.GlobalDiffCache.Lock()
	storage.GlobalDiffCache.Items = map[string]models.CachedDiff{
		"abc123": {
			ID:           "abc123",
			Title:        "Matching Diff",
			TitleLower:   "matching diff",
			ContentLower: "before after",
			CreatedAt:    time.Now(),
		},
	}
	storage.GlobalDiffCache.Unlock()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/search_diffs?q=matching", nil)
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/search_diffs = %d, want 200", recorder.Code)
	}
	var results storage.Page[models.DiffMeta]
	if err := json.NewDecoder(recorder.Body).Decode(&results); err != nil {
		t.Fatalf("search response is not a cursor page: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].ID != "abc123" {
		t.Fatalf("search results = %#v, want abc123", results)
	}
}

func TestSearchRejectsLongQuery(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/search?q="+strings.Repeat("a", maxSearchQueryRunes+1), nil)
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("GET long search = %d, want 400", recorder.Code)
	}
}

func TestFormatGoUsesStandardLibrary(t *testing.T) {
	requestBody := []byte(`{"content":"package main\nfunc main(){}","language":"go"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/format", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /api/format = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode format response: %v", err)
	}
	if response["formatted"] != "package main\n\nfunc main() {}\n" {
		t.Fatalf("formatted Go = %q", response["formatted"])
	}
}

func TestDiffRejectsExpensiveInput(t *testing.T) {
	payload, err := json.Marshal(DiffRequest{
		Base:    strings.Repeat("a", maxDiffInputBytes),
		Compare: "b",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/diff", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	qualityMux().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST large diff = %d, want 413", recorder.Code)
	}
}

func TestPreviewUsesContentETag(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	id, err := storage.CreatePaste("Preview", "package main", "go")
	if err != nil {
		t.Fatalf("CreatePaste() error = %v", err)
	}

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/api/pastes/"+id+"/preview.png", nil)
	qualityMux().ServeHTTP(first, firstRequest)
	if first.Code != http.StatusOK {
		t.Fatalf("first preview = %d, want 200", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("preview response has no ETag")
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/pastes/"+id+"/preview.png", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	qualityMux().ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified {
		t.Fatalf("cached preview = %d, want 304", second.Code)
	}
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security middleware did not set X-Content-Type-Options")
	}
	if recorder.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security middleware did not set X-Frame-Options")
	}
}
