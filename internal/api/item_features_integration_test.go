package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arvarik/paste/internal/auth"
	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
)

func TestBurnPreviewCommitsOnlyAfterRenderAndSkipsCache(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	originalRender := renderPreviewPNG
	originalCache := generatedPreviews.Load()
	generatedPreviews.Store(newPreviewCache(1 << 20))
	t.Cleanup(func() {
		renderPreviewPNG = originalRender
		generatedPreviews.Store(originalCache)
	})

	failedID, _, err := storage.CreatePasteWithOptions("preview", "retain on error", "text", storage.CreateOptions{BurnAfterRead: true})
	if err != nil {
		t.Fatal(err)
	}
	renderPreviewPNG = func(string, []byte) ([]byte, error) {
		return nil, errors.New("injected render failure")
	}
	failed := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+failedID+"/preview.png", nil, nil)
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("failed preview = %d", failed.Code)
	}
	if items, used := generatedPreviews.Load().size(); items != 0 || used != 0 {
		t.Fatalf("failed burn preview cache = %d items, %d bytes", items, used)
	}
	paste, err := storage.GetPaste(failedID)
	if err != nil || paste.Content != "retain on error" {
		t.Fatalf("paste after failed preview = %#v, %v", paste, err)
	}

	renderPreviewPNG = originalRender
	successID, _, err := storage.CreatePasteWithOptions("preview", "consume on success", "text", storage.CreateOptions{BurnAfterRead: true})
	if err != nil {
		t.Fatal(err)
	}
	success := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+successID+"/preview.png", nil, nil)
	if success.Code != http.StatusOK {
		t.Fatalf("successful preview = %d: %s", success.Code, success.Body.String())
	}
	if items, used := generatedPreviews.Load().size(); items != 0 || used != 0 {
		t.Fatalf("successful burn preview cache = %d items, %d bytes", items, used)
	}
	if _, err := storage.GetPaste(successID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful burn preview did not consume paste: %v", err)
	}
}

func TestPasteMetadataConflictRevisionsRestoreAndTransfer(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	create := dxRequest(t, handler, http.MethodPost, "/api/pastes", map[string]any{
		"title": "features", "content": "version one", "language": "go",
		"tags": []string{"Review", "Go"}, "favorite": true, "expiresAt": expiresAt,
	}, nil)
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", create.Code, create.Body.String())
	}
	var created createdPasteResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	list := dxRequest(t, handler, http.MethodGet, "/api/pastes?tag=review&favorite=true", nil, nil)
	var page storage.Page[models.PasteMeta]
	if err := json.NewDecoder(list.Body).Decode(&page); err != nil || len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatalf("filtered page = %#v, %v", page, err)
	}

	updateBody := map[string]any{
		"title": "features", "content": "version two", "language": "go",
		"tags": []string{"Go"}, "favorite": false, "expiresAt": expiresAt, "revision": 1,
	}
	update := dxRequest(t, handler, http.MethodPut, "/api/pastes/"+created.ID, updateBody, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if update.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", update.Code, update.Body.String())
	}
	conflict := dxRequest(t, handler, http.MethodPut, "/api/pastes/"+created.ID, updateBody, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale update = %d, want 409", conflict.Code)
	}

	revisions := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+created.ID+"/revisions", nil, nil)
	if strings.Contains(revisions.Body.String(), "checksum") || strings.Contains(revisions.Body.String(), "dataFile") {
		t.Fatalf("revision list exposed storage fingerprints: %s", revisions.Body.String())
	}
	var revisionPage struct {
		Items           []models.RevisionInfo `json:"items"`
		CurrentRevision int64                 `json:"currentRevision"`
	}
	if err := json.NewDecoder(revisions.Body).Decode(&revisionPage); err != nil || len(revisionPage.Items) != 1 || revisionPage.CurrentRevision != 2 {
		t.Fatalf("revision page = %#v, %v", revisionPage, err)
	}
	revision := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+created.ID+"/revisions/1", nil, nil)
	if revision.Code != http.StatusOK || !containsJSONText(revision.Body.Bytes(), "version one") {
		t.Fatalf("revision = %d: %s", revision.Code, revision.Body.String())
	}
	if strings.Contains(revision.Body.String(), "editSecretHash") || strings.Contains(revision.Body.String(), "dataFile") {
		t.Fatalf("revision exposed internal metadata: %s", revision.Body.String())
	}
	staleRestore := dxRequest(t, handler, http.MethodPost, "/api/pastes/"+created.ID+"/revisions/1/restore", map[string]any{
		"expectedRevision": 1,
	}, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if staleRestore.Code != http.StatusConflict {
		t.Fatalf("stale restore = %d, want 409: %s", staleRestore.Code, staleRestore.Body.String())
	}
	restore := dxRequest(t, handler, http.MethodPost, "/api/pastes/"+created.ID+"/revisions/1/restore", map[string]any{
		"expectedRevision": 2,
	}, map[string]string{
		editSecretHeader: created.EditSecret,
	})
	if restore.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restore.Code, restore.Body.String())
	}

	exported := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+created.ID+"/export", nil, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", exported.Code, exported.Body.String())
	}
	var document map[string]any
	if err := json.NewDecoder(exported.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	if document["content"] != "version one" || document["editSecret"] != nil {
		t.Fatalf("export document = %#v", document)
	}
	imported := dxRequest(t, handler, http.MethodPost, "/api/import", document, nil)
	if imported.Code != http.StatusCreated {
		t.Fatalf("import = %d: %s", imported.Code, imported.Body.String())
	}
	var importedItem createdPasteResponse
	if err := json.NewDecoder(imported.Body).Decode(&importedItem); err != nil || importedItem.ID == created.ID || importedItem.EditSecret == "" {
		t.Fatalf("imported item = %#v, %v", importedItem, err)
	}
}

func TestBurnAfterReadAndExpiredResponses(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	handler := qualityMux()
	create := dxRequest(t, handler, http.MethodPost, "/api/pastes", map[string]any{
		"title": "burn", "content": "one read", "language": "text", "burnAfterRead": true,
	}, nil)
	var created createdPasteResponse
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	first := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+created.ID, nil, nil)
	second := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+created.ID, nil, nil)
	if first.Code != http.StatusOK || second.Code != http.StatusNotFound {
		t.Fatalf("burn responses = %d then %d", first.Code, second.Code)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("burn paste Cache-Control = %q", first.Header().Get("Cache-Control"))
	}
	diffID, _, err := storage.CreateDiffWithOptions("burn diff", "text", "text", "left", "right", storage.CreateOptions{BurnAfterRead: true})
	if err != nil {
		t.Fatal(err)
	}
	diff := dxRequest(t, handler, http.MethodGet, "/api/saved_diffs/"+diffID, nil, nil)
	if diff.Code != http.StatusOK || diff.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("burn diff = %d, cache=%q", diff.Code, diff.Header().Get("Cache-Control"))
	}
	revisionID, _, err := storage.CreatePasteWithOptions("burn revision", "first", "text", storage.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	burnRevision := true
	if err := storage.UpdatePasteTrusted(revisionID, "burn revision", "second", "text", storage.MetadataPatch{BurnAfterRead: &burnRevision}); err != nil {
		t.Fatal(err)
	}
	revisionResponse := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+revisionID+"/revisions/1", nil, nil)
	if revisionResponse.Code != http.StatusOK || revisionResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("burn revision = %d, cache=%q", revisionResponse.Code, revisionResponse.Header().Get("Cache-Control"))
	}
	previewID, _, err := storage.CreatePasteWithOptions("preview", "one image", "text", storage.CreateOptions{BurnAfterRead: true})
	if err != nil {
		t.Fatal(err)
	}
	preview := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+previewID+"/preview.png", nil, nil)
	if preview.Code != http.StatusOK || preview.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("burn preview = %d, cache=%q", preview.Code, preview.Header().Get("Cache-Control"))
	}
	if afterPreview := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+previewID, nil, nil); afterPreview.Code != http.StatusNotFound {
		t.Fatalf("burn preview did not consume paste: %d", afterPreview.Code)
	}

	past := time.Now().Add(-time.Minute)
	expiredID, _, err := storage.CreatePasteWithOptions("expired", "gone", "text", storage.CreateOptions{ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	expired := dxRequest(t, handler, http.MethodGet, "/api/pastes/"+expiredID, nil, nil)
	if expired.Code != http.StatusGone {
		t.Fatalf("expired response = %d, want 410", expired.Code)
	}
}

func TestWriteTokenCanUpdateLegacyReadOnlyPaste(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	if _, err := storage.CreatePaste("temporary", "unused", "text"); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(storage.DataDir, "abc123_Legacy.txt")
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage.LoadCacheFromDisk()
	store, err := auth.NewStore(filepath.Join(storage.DataDir, "auth", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := store.Create("writer", []string{"write"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureAPIAuth(store, "")
	defer ConfigureAPIAuth(nil, "")
	handler := APIAuthenticationMiddleware(qualityMux())
	body := map[string]any{"title": "Legacy", "content": "updated", "language": "text", "revision": 1}
	denied := dxRequest(t, handler, http.MethodPut, "/api/pastes/abc123", body, nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("anonymous legacy update = %d", denied.Code)
	}
	allowed := dxRequest(t, handler, http.MethodPut, "/api/pastes/abc123", body, map[string]string{
		"Authorization": "Bearer " + raw,
	})
	if allowed.Code != http.StatusOK {
		t.Fatalf("token legacy update = %d: %s", allowed.Code, allowed.Body.String())
	}
}

func TestCreateCanRequireWriteToken(t *testing.T) {
	cleanup := dxSetupTestEnv(t)
	defer cleanup()
	ConfigureItemFeatures(ItemFeatureConfig{RequireTokenForCreate: true})
	defer ConfigureItemFeatures(ItemFeatureConfig{})
	store, err := auth.NewStore(filepath.Join(storage.DataDir, "auth", "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, readToken, err := store.Create("reader", []string{"read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, writeToken, err := store.Create("writer", []string{"write"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureAPIAuth(store, "")
	defer ConfigureAPIAuth(nil, "")
	handler := APIAuthenticationMiddleware(qualityMux())
	body := map[string]any{"title": "secured", "content": "value", "language": "text"}

	anonymous := dxRequest(t, handler, http.MethodPost, "/api/pastes", body, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create = %d, want 401", anonymous.Code)
	}
	readOnly := dxRequest(t, handler, http.MethodPost, "/api/pastes", body, map[string]string{
		"Authorization": "Bearer " + readToken,
	})
	if readOnly.Code != http.StatusForbidden {
		t.Fatalf("read-token create = %d, want 403", readOnly.Code)
	}
	write := dxRequest(t, handler, http.MethodPost, "/api/pastes", body, map[string]string{
		"Authorization": "Bearer " + writeToken,
	})
	if write.Code != http.StatusCreated {
		t.Fatalf("write-token create = %d: %s", write.Code, write.Body.String())
	}
}

func containsJSONText(encoded []byte, text string) bool {
	var value any
	if json.Unmarshal(encoded, &value) != nil {
		return false
	}
	return strings.Contains(string(encoded), text)
}
