package api

// dx_polish_test.go — DX Polish Release Contract Tests
//
// These tests encode the exact API contracts from ARCHITECTURE.md §4.
// They MUST FAIL before the DX Polish implementation and PASS after.
//
// Contracts tested:
//   - GET /raw/{id}     → text/plain response, no JSON wrapping
//   - GET /api/pastes   → lineCount field present in models.PasteMeta
//   - GET /api/search   → lineCount field present in search results
//   - POST /api/pastes  → lineCount computed and cached on create

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
)

// ─── Test Infrastructure ────────────────────────────────────────────

// dxSetupTestEnv creates a temporary data directory, resets the global
// cache, and returns a cleanup function. Must be called at the start
// of each test to ensure isolation.
func dxSetupTestEnv(t *testing.T) func() {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "paste-dx-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	origDataDir := storage.DataDir
	storage.DataDir = tmpDir

	// Reset global cache to empty state
	storage.GlobalCache.Lock()
	storage.GlobalCache.Items = make(map[string]models.CachedPaste)
	storage.GlobalCache.Unlock()

	return func() {
		storage.DataDir = origDataDir
		os.RemoveAll(tmpDir)
	}
}

// dxBuildMux creates an http.ServeMux with all application routes.
// This mirrors the route registration in main(). When handleRawPaste
// is implemented and registered here, the /raw/{id} tests will start
// hitting the actual handler instead of the default 404.
func dxBuildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Existing API routes
	mux.HandleFunc("POST /api/pastes", handleSavePaste)
	mux.HandleFunc("GET /api/pastes", handleListPastes)
	mux.HandleFunc("GET /api/pastes/{id}", handleGetPaste)
	mux.HandleFunc("DELETE /api/pastes/{id}", handleDeletePaste)
	mux.HandleFunc("GET /api/search", handleSearchPastes)

	// NEW: Raw paste endpoint
	mux.HandleFunc("GET /raw/{id}", handleRawPaste)

	return mux
}

// dxPostPaste creates a paste via the API and returns its ID.
func dxPostPaste(t *testing.T, srv *httptest.Server, title, content, language string) string {
	t.Helper()
	payload := struct {
		Title    string `json:"title"`
		Content  string `json:"content"`
		Language string `json:"language"`
	}{title, content, language}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(srv.URL+"/api/pastes", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST /api/pastes: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/pastes = %d (%s), want 201", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	id := result["id"]
	if id == "" {
		t.Fatal("POST /api/pastes returned empty id")
	}
	return id
}

// ─── Contract: GET /raw/{id} ────────────────────────────────────────
// ARCHITECTURE.md §4: "200 OK with Content-Type: text/plain; charset=utf-8.
// Body is the raw paste content with no JSON wrapping."

func TestRawPaste_ReturnsPlainTextContent(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"
	id := dxPostPaste(t, srv, "hello-world", content, "go")

	resp, err := http.Get(srv.URL + "/raw/" + id)
	if err != nil {
		t.Fatalf("GET /raw/%s: %v", id, err)
	}
	defer resp.Body.Close()

	// Contract: must return 200
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raw/%s status = %d, want 200", id, resp.StatusCode)
	}

	// Contract: Content-Type must be text/plain
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("GET /raw/%s Content-Type = %q, want text/plain", id, ct)
	}

	// Contract: body must be the exact raw content, no JSON wrapping
	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Errorf("GET /raw/%s body mismatch:\ngot:  %q\nwant: %q", id, string(body), content)
	}
}

func TestRawPaste_NotFoundReturns404(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/raw/zzzzzz")
	if err != nil {
		t.Fatalf("GET /raw/zzzzzz: %v", err)
	}
	defer resp.Body.Close()

	// Contract: "404 Plain text Paste not found if the ID doesn't match any file."
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /raw/zzzzzz status = %d, want 404", resp.StatusCode)
	}
}

func TestRawPaste_InvalidIDReturns400(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	// Path-traversal attempt with invalid ID characters
	resp, err := http.Get(srv.URL + "/raw/abc-def")
	if err != nil {
		t.Fatalf("GET /raw/abc-def: %v", err)
	}
	defer resp.Body.Close()

	// Invalid IDs (non-alphanumeric) should be rejected
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /raw/abc-def status = %d, want 400 or 404", resp.StatusCode)
	}
}

func TestRawPaste_MultilineContent(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	// Content with various newline patterns
	content := "#!/bin/bash\nset -euo pipefail\n\necho \"hello world\"\nexit 0"
	id := dxPostPaste(t, srv, "deploy-script", content, "bash")

	resp, err := http.Get(srv.URL + "/raw/" + id)
	if err != nil {
		t.Fatalf("GET /raw/%s: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raw/%s status = %d, want 200", id, resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Errorf("raw content mismatch for multiline bash script")
	}
}

// ─── Contract: lineCount in GET /api/pastes ─────────────────────────
// ARCHITECTURE.md §3: models.PasteMeta includes LineCount int `json:"lineCount"`
// ARCHITECTURE.md §4: Example response includes "lineCount": 42

// pasteMetaWithLineCount is a test-local struct matching the contracted
// models.PasteMeta shape including the new lineCount field.
type pasteMetaWithLineCount struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Language  string `json:"language"`
	Preview   string `json:"preview"`
	LineCount int    `json:"lineCount"`
}

type pasteGroupWithLineCount struct {
	Group  string                   `json:"group"`
	Pastes []pasteMetaWithLineCount `json:"pastes"`
}

func TestListPastes_IncludesLineCount(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	// Create a paste with exactly 5 lines
	content := "line1\nline2\nline3\nline4\nline5"
	dxPostPaste(t, srv, "five-liner", content, "text")

	resp, err := http.Get(srv.URL + "/api/pastes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var groups []pasteGroupWithLineCount
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		t.Fatalf("failed to decode /api/pastes response: %v", err)
	}

	found := false
	for _, g := range groups {
		for _, p := range g.Pastes {
			found = true
			// Contract: lineCount = strings.Count(content, "\n") + 1 = 5
			if p.LineCount != 5 {
				t.Errorf("GET /api/pastes: lineCount = %d, want 5", p.LineCount)
			}
		}
	}
	if !found {
		t.Fatal("GET /api/pastes returned no pastes")
	}
}

func TestListPastes_SingleLineHasLineCount1(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "just a single line"
	dxPostPaste(t, srv, "one-liner", content, "text")

	resp, err := http.Get(srv.URL + "/api/pastes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var groups []pasteGroupWithLineCount
	json.NewDecoder(resp.Body).Decode(&groups)

	for _, g := range groups {
		for _, p := range g.Pastes {
			if p.LineCount != 1 {
				t.Errorf("single-line paste: lineCount = %d, want 1", p.LineCount)
			}
		}
	}
}

// ─── Contract: lineCount in GET /api/search ─────────────────────────
// ARCHITECTURE.md §4: "Each paste includes lineCount."

func TestSearchPastes_IncludesLineCount(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "func hello() {\n\treturn \"world\"\n}"
	dxPostPaste(t, srv, "searchable-func", content, "go")

	resp, err := http.Get(srv.URL + "/api/search?q=hello")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var results []pasteMetaWithLineCount
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("GET /api/search?q=hello returned no results")
	}

	// 3 lines: "func hello() {", "\treturn \"world\"", "}"
	if results[0].LineCount != 3 {
		t.Errorf("search result lineCount = %d, want 3", results[0].LineCount)
	}
}

// ─── Contract: lineCount populated immediately after create ─────────

func TestLineCount_AvailableAfterCreate(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj"
	dxPostPaste(t, srv, "ten-liner", content, "text")

	// Immediately list and check lineCount is correct
	resp, err := http.Get(srv.URL + "/api/pastes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var groups []pasteGroupWithLineCount
	json.NewDecoder(resp.Body).Decode(&groups)

	for _, g := range groups {
		for _, p := range g.Pastes {
			if p.LineCount != 10 {
				t.Errorf("10-line paste: lineCount = %d, want 10", p.LineCount)
			}
		}
	}
}

// ─── Contract: lineCount survives cache reload from disk ────────────

func TestLineCount_SurvivesCacheReload(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "alpha\nbeta\ngamma"
	dxPostPaste(t, srv, "three-liner", content, "text")

	// Clear cache and reload from disk — simulates server restart
	storage.GlobalCache.Lock()
	storage.GlobalCache.Items = make(map[string]models.CachedPaste)
	storage.GlobalCache.Unlock()

	storage.LoadCacheFromDisk()

	// List pastes to verify lineCount survived the reload
	resp, err := http.Get(srv.URL + "/api/pastes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var groups []pasteGroupWithLineCount
	json.NewDecoder(resp.Body).Decode(&groups)

	found := false
	for _, g := range groups {
		for _, p := range g.Pastes {
			found = true
			if p.LineCount != 3 {
				t.Errorf("disk-reloaded paste: lineCount = %d, want 3", p.LineCount)
			}
		}
	}
	if !found {
		t.Fatal("no pastes found after cache reload")
	}
}

// ─── Edge Case: rapid creation then raw access ──────────────────────

func TestRawPaste_ImmediatelyAfterCreate(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	// Create and immediately raw-fetch — tests that the file is on disk
	// before the response returns (atomic create contract).
	content := "{\"key\": \"value\"}"
	id := dxPostPaste(t, srv, "instant-json", content, "json")

	resp, err := http.Get(srv.URL + "/raw/" + id)
	if err != nil {
		t.Fatalf("GET /raw/%s: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /raw/%s after immediate create = %d, want 200", id, resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != content {
		t.Errorf("immediate raw content mismatch")
	}
}

// ─── Edge Case: raw after delete returns 404 ────────────────────────

func TestRawPaste_AfterDeleteReturns404(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	srv := httptest.NewServer(dxBuildMux())
	defer srv.Close()

	content := "temporary content"
	id := dxPostPaste(t, srv, "temp", content, "text")

	// Delete the paste
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/pastes/"+id, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/pastes/%s: %v", id, err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", delResp.StatusCode)
	}

	// Raw fetch should now 404
	resp, err := http.Get(srv.URL + "/raw/" + id)
	if err != nil {
		t.Fatalf("GET /raw/%s after delete: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /raw/%s after delete = %d, want 404", id, resp.StatusCode)
	}
}
