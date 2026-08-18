package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

// handleSaveDiff creates a new diff.
func handleSaveDiff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title          string     `json:"title"`
		Base           string     `json:"base"`
		Compare        string     `json:"compare"`
		BaseContent    string     `json:"baseContent"`
		CompareContent string     `json:"compareContent"`
		Tags           []string   `json:"tags"`
		Favorite       bool       `json:"favorite"`
		ExpiresAt      *time.Time `json:"expiresAt"`
		BurnAfterRead  bool       `json:"burnAfterRead"`
	}

	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.BaseContent) == "" && strings.TrimSpace(req.CompareContent) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Base or compare content is required"})
		return
	}
	if !requireCreatePermission(w, r) {
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled Diff"
	}
	title = util.SanitizeTitle(title, "Untitled-Diff")

	tags, expiresAt, err := validateItemOptions(req.Tags, req.ExpiresAt, true)
	if err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	id, editSecret, err := storage.CreateDiffWithOptions(title, req.Base, req.Compare, req.BaseContent, req.CompareContent, storage.CreateOptions{
		Tags: tags, Favorite: req.Favorite, ExpiresAt: expiresAt, BurnAfterRead: req.BurnAfterRead,
	})
	if err != nil {
		log.Printf("[save_diff] failed to create diff: %v", err)
		respondStorageError(w, err, "Diff")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, http.StatusCreated, map[string]any{
		"id": id, "title": title, "editSecret": editSecret, "tags": tags,
		"favorite": req.Favorite, "expiresAt": expiresAt,
		"burnAfterRead": req.BurnAfterRead, "revision": int64(1),
	})
}

func handleListDiffs(w http.ResponseWriter, r *http.Request) {
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
	page, err := storage.QueryDiffsPage("", filter, cursor, limit)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid page cursor"})
		return
	}
	respondJSON(w, http.StatusOK, page)
}

func handleGetDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	cached, err := storage.GetDiff(id)
	if err != nil {
		respondStorageError(w, err, "Diff")
		return
	}
	if cached.BurnAfterRead {
		w.Header().Set("Cache-Control", "no-store")
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":             cached.ID,
		"title":          cached.Title,
		"base":           cached.Base,
		"compare":        cached.Compare,
		"baseContent":    cached.BaseContent,
		"compareContent": cached.CompareContent,
		"createdAt":      cached.CreatedAt, "updatedAt": cached.UpdatedAt,
		"tags": cached.Tags, "favorite": cached.Favorite, "expiresAt": cached.ExpiresAt,
		"burnAfterRead": cached.BurnAfterRead, "revision": cached.Revision, "size": cached.Size,
	})
}

func handleDeleteDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var err error
	if principalCanWrite(r) {
		err = storage.DeleteDiffTrusted(id)
	} else {
		err = storage.DeleteDiffAuthorized(id, requestEditSecret(r))
	}
	if err != nil {
		log.Printf("[delete_diff] failed to delete diff %s: %v", id, err)
		respondStorageError(w, err, "Diff")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUpdateDiff updates an existing saved diff.
func handleUpdateDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Title          string     `json:"title"`
		Base           string     `json:"base"`
		Compare        string     `json:"compare"`
		BaseContent    string     `json:"baseContent"`
		CompareContent string     `json:"compareContent"`
		Tags           []string   `json:"tags"`
		Favorite       bool       `json:"favorite"`
		ExpiresAt      *time.Time `json:"expiresAt"`
		BurnAfterRead  bool       `json:"burnAfterRead"`
		Revision       *int64     `json:"revision"`
	}

	if err := decodeJSONRequest(w, r, &req); err != nil {
		respondJSONDecodeError(w, err)
		return
	}
	if strings.TrimSpace(req.BaseContent) == "" && strings.TrimSpace(req.CompareContent) == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Base or compare content is required"})
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled Diff"
	}
	title = util.SanitizeTitle(title, "Untitled-Diff")

	tags, expiresAt, err := validateItemOptions(req.Tags, req.ExpiresAt, false)
	if err != nil {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	patch := storage.MetadataPatch{
		Tags: &tags, Favorite: &req.Favorite, ExpiresAt: &expiresAt,
		BurnAfterRead: &req.BurnAfterRead, ExpectedRevision: req.Revision,
	}
	var committedRevision int64
	if principalCanWrite(r) {
		committedRevision, err = storage.UpdateDiffTrustedWithRevision(id, title, req.Base, req.Compare, req.BaseContent, req.CompareContent, patch)
	} else {
		committedRevision, err = storage.UpdateDiffAuthorizedWithRevision(id, title, req.Base, req.Compare, req.BaseContent, req.CompareContent, requestEditSecret(r), patch)
	}
	if err != nil {
		log.Printf("[update_diff] failed to update diff %s: %v", id, err)
		respondStorageError(w, err, "Diff")
		return
	}

	log.Printf("[update_diff] updated diff %q", id)
	respondJSON(w, http.StatusOK, map[string]any{
		"id": id, "title": title, "tags": tags, "favorite": req.Favorite,
		"expiresAt": expiresAt, "burnAfterRead": req.BurnAfterRead, "revision": committedRevision,
	})
}

func handleSearchDiffs(w http.ResponseWriter, r *http.Request) {
	query, ok := validatedSearchQuery(w, r)
	if !ok {
		return
	}
	if query == "" {
		handleListDiffs(w, r)
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
	page, err := storage.QueryDiffsPage(query, filter, cursor, limit)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid page cursor"})
		return
	}
	respondJSON(w, http.StatusOK, page)
}
