package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

const (
	maxRequestBodySize  = 2 << 20
	maxSearchQueryRunes = 200
)

var errUnsupportedMediaType = errors.New("unsupported media type")

// LoggingMiddleware wraps an http.Handler to log the remote address, method,
// path, and duration of every request to stdout.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[http] %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("[http] %s %s %s completed in %v", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
	})
}

// SecurityHeadersMiddleware adds browser security headers to every response.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// RegisterRoutes registers all API endpoints on the provided mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pastes", handleSavePaste)
	mux.HandleFunc("GET /api/pastes", handleListPastes)
	mux.HandleFunc("GET /api/pastes/{id}", handleGetPaste)
	mux.HandleFunc("PUT /api/pastes/{id}", handleUpdatePaste)
	mux.HandleFunc("DELETE /api/pastes/{id}", handleDeletePaste)
	mux.HandleFunc("GET /api/search", handleSearchPastes)
	mux.HandleFunc("POST /api/format", handleFormatCode)
	mux.HandleFunc("GET /raw/{id}", handleRawPaste)
	mux.HandleFunc("GET /api/pastes/{id}/preview.png", handlePreviewImage)
	mux.HandleFunc("GET /api/pastes/{id}/revisions", handleListPasteRevisions)
	mux.HandleFunc("GET /api/pastes/{id}/revisions/{revision}", handleGetPasteRevision)
	mux.HandleFunc("POST /api/pastes/{id}/revisions/{revision}/restore", handleRestorePasteRevision)
	mux.HandleFunc("GET /api/pastes/{id}/export", handleExportPaste)
	mux.HandleFunc("POST /api/import", handleImportItem)

	mux.HandleFunc("POST /api/diff", handleDiff)
	mux.HandleFunc("POST /api/saved_diffs", handleSaveDiff)
	mux.HandleFunc("GET /api/saved_diffs", handleListDiffs)
	mux.HandleFunc("GET /api/saved_diffs/{id}", handleGetDiff)
	mux.HandleFunc("PUT /api/saved_diffs/{id}", handleUpdateDiff)
	mux.HandleFunc("DELETE /api/saved_diffs/{id}", handleDeleteDiff)
	mux.HandleFunc("GET /api/search_diffs", handleSearchDiffs)
	mux.HandleFunc("GET /api/saved_diffs/{id}/revisions", handleListDiffRevisions)
	mux.HandleFunc("GET /api/saved_diffs/{id}/revisions/{revision}", handleGetDiffRevision)
	mux.HandleFunc("POST /api/saved_diffs/{id}/revisions/{revision}/restore", handleRestoreDiffRevision)
	mux.HandleFunc("GET /api/saved_diffs/{id}/export", handleExportDiff)

	mux.HandleFunc("POST /api/tokens", handleCreateAPIToken)
	mux.HandleFunc("GET /api/tokens", handleListAPITokens)
	mux.HandleFunc("DELETE /api/tokens/{id}", handleRevokeAPIToken)
}

func handleSavePaste(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title         string     `json:"title"`
		Content       string     `json:"content"`
		Language      string     `json:"language"`
		Tags          []string   `json:"tags"`
		Favorite      bool       `json:"favorite"`
		ExpiresAt     *time.Time `json:"expiresAt"`
		BurnAfterRead bool       `json:"burnAfterRead"`
	}

	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Content is required"})
		return
	}
	if !requireCreatePermission(w, r) {
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled"
	}
	title = util.SanitizeTitle(title, "Untitled")

	tags, expiresAt, err := validateItemOptions(req.Tags, req.ExpiresAt, true)
	if err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	language := util.NormalizeLanguage(req.Language)
	id, editSecret, err := storage.CreatePasteWithOptions(title, req.Content, language, storage.CreateOptions{
		Tags: tags, Favorite: req.Favorite, ExpiresAt: expiresAt, BurnAfterRead: req.BurnAfterRead,
	})
	if err != nil {
		log.Printf("[save] failed to create paste: %v", err)
		respondStorageError(w, err, "Paste")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, http.StatusCreated, map[string]any{
		"id": id, "title": title, "language": language, "editSecret": editSecret,
		"tags": tags, "favorite": req.Favorite, "expiresAt": expiresAt,
		"burnAfterRead": req.BurnAfterRead, "revision": int64(1),
	})
}

func handleListPastes(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := parsePageRequest(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	filter, err := parseItemFilter(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	page, err := storage.QueryPastesPage("", filter, cursor, limit)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid page cursor"})
		return
	}
	respondJSON(w, http.StatusOK, page)
}

func handleGetPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	cached, err := storage.GetPaste(id)
	if err != nil {
		log.Printf("[get] failed to read paste %s: %v", id, err)
		respondStorageError(w, err, "Paste")
		return
	}
	if cached.BurnAfterRead {
		w.Header().Set("Cache-Control", "no-store")
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"id": cached.ID, "title": cached.Title, "language": cached.Language, "content": cached.Content,
		"createdAt": cached.CreatedAt, "updatedAt": cached.UpdatedAt, "tags": cached.Tags,
		"favorite": cached.Favorite, "expiresAt": cached.ExpiresAt,
		"burnAfterRead": cached.BurnAfterRead, "revision": cached.Revision, "size": cached.Size,
	})
}

func handleDeletePaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var err error
	if principalCanWrite(r) {
		err = storage.DeletePasteTrusted(id)
	} else {
		err = storage.DeletePasteAuthorized(id, requestEditSecret(r))
	}
	if err != nil {
		log.Printf("[delete] failed to remove paste %s: %v", id, err)
		respondStorageError(w, err, "Paste")
		return
	}

	log.Printf("[delete] removed paste %q", id)
	w.WriteHeader(http.StatusNoContent)
}

func handleUpdatePaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	var req struct {
		Title         string     `json:"title"`
		Content       string     `json:"content"`
		Language      string     `json:"language"`
		Tags          []string   `json:"tags"`
		Favorite      bool       `json:"favorite"`
		ExpiresAt     *time.Time `json:"expiresAt"`
		BurnAfterRead bool       `json:"burnAfterRead"`
		Revision      *int64     `json:"revision"`
	}
	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Content is required"})
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled"
	}
	title = util.SanitizeTitle(title, "Untitled")

	tags, expiresAt, err := validateItemOptions(req.Tags, req.ExpiresAt, false)
	if err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	patch := storage.MetadataPatch{
		Tags: &tags, Favorite: &req.Favorite, ExpiresAt: &expiresAt,
		BurnAfterRead: &req.BurnAfterRead, ExpectedRevision: req.Revision,
	}
	language := util.NormalizeLanguage(req.Language)
	var committedRevision int64
	if principalCanWrite(r) {
		committedRevision, err = storage.UpdatePasteTrustedWithRevision(id, title, req.Content, language, patch)
	} else {
		committedRevision, err = storage.UpdatePasteAuthorizedWithRevision(id, title, req.Content, language, requestEditSecret(r), patch)
	}
	if err != nil {
		log.Printf("[update] failed to update paste %s: %v", id, err)
		respondStorageError(w, err, "Paste")
		return
	}

	log.Printf("[update] updated paste %q", id)
	respondJSON(w, http.StatusOK, map[string]any{
		"id": id, "title": title, "language": language, "tags": tags,
		"favorite": req.Favorite, "expiresAt": expiresAt,
		"burnAfterRead": req.BurnAfterRead, "revision": committedRevision,
	})
}

func handleSearchPastes(w http.ResponseWriter, r *http.Request) {
	query, ok := validatedSearchQuery(w, r)
	if !ok {
		return
	}

	if query == "" {
		handleListPastes(w, r)
		return
	}

	cursor, limit, err := parsePageRequest(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	filter, err := parseItemFilter(r)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	page, err := storage.QueryPastesPage(query, filter, cursor, limit)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid page cursor"})
		return
	}
	respondJSON(w, http.StatusOK, page)
}

func validatedSearchQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if utf8.RuneCountInString(query) > maxSearchQueryRunes || len(query) > 256 {
		http.Error(w, "Search query is too long", http.StatusBadRequest)
		return "", false
	}
	return query, true
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[http] failed to encode response: %v", err)
	}
}

// decodeJSONRequest decodes one size-limited JSON object.
func decodeJSONRequest(w http.ResponseWriter, r *http.Request, destination any) error {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return errUnsupportedMediaType
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return errUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body contains more than one JSON value")
		}
		return err
	}
	return nil
}

func respondJSONDecodeError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	switch {
	case errors.Is(err, errUnsupportedMediaType):
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
	case errors.As(err, &maxBytesError):
		http.Error(w, "Request body is too large", http.StatusRequestEntityTooLarge)
	default:
		http.Error(w, "Invalid request body", http.StatusBadRequest)
	}
}

func handleRawPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	content, err := storage.GetRawPaste(id)
	if err != nil {
		respondStorageError(w, err, "Paste")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}
