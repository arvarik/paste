package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

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

// RegisterRoutes registers all API endpoints on the provided mux.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pastes", handleSavePaste)
	mux.HandleFunc("GET /api/pastes", handleListPastes)
	mux.HandleFunc("GET /api/pastes/{id}", handleGetPaste)
	mux.HandleFunc("PUT /api/pastes/{id}", handleUpdatePaste)
	mux.HandleFunc("DELETE /api/pastes/{id}", handleDeletePaste)
	mux.HandleFunc("GET /api/search", handleSearchPastes)
	mux.HandleFunc("GET /raw/{id}", handleRawPaste)
	mux.HandleFunc("GET /api/pastes/{id}/preview.png", handlePreviewImage)

	mux.HandleFunc("POST /api/diff", handleDiff)
	mux.HandleFunc("POST /api/saved_diffs", handleSaveDiff)
	mux.HandleFunc("GET /api/saved_diffs", handleListDiffs)
	mux.HandleFunc("GET /api/saved_diffs/{id}", handleGetDiff)
	mux.HandleFunc("DELETE /api/saved_diffs/{id}", handleDeleteDiff)
	mux.HandleFunc("GET /api/search_diffs", handleSearchDiffs)
}

func handleSavePaste(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var req struct {
		Title    string `json:"title"`
		Content  string `json:"content"`
		Language string `json:"language"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body or payload too large (>2MB)", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "Content is required", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled"
	}
	title = util.TitleSanitizer.Replace(title)

	id, err := storage.CreatePaste(title, req.Content, req.Language)
	if err != nil {
		log.Printf("[save] failed to create paste: %v", err)
		http.Error(w, "Error saving paste", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"id":    id,
		"title": title,
	})
}

func handleListPastes(w http.ResponseWriter, r *http.Request) {
	pastes := storage.ListPastes()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	pastWeekStart := todayStart.AddDate(0, 0, -7)
	pastMonthStart := todayStart.AddDate(0, -1, 0)

	type pasteGroup struct {
		Group  string             `json:"group"`
		Pastes []models.PasteMeta `json:"pastes"`
	}

	bucketNames := []string{"Today", "Yesterday", "Past Week", "Past Month", "Beyond"}
	buckets := make(map[string][]models.PasteMeta, len(bucketNames))
	for _, name := range bucketNames {
		buckets[name] = []models.PasteMeta{}
	}

	for _, p := range pastes {
		switch {
		case !p.CreatedAt.Before(todayStart):
			buckets["Today"] = append(buckets["Today"], p)
		case !p.CreatedAt.Before(yesterdayStart):
			buckets["Yesterday"] = append(buckets["Yesterday"], p)
		case !p.CreatedAt.Before(pastWeekStart):
			buckets["Past Week"] = append(buckets["Past Week"], p)
		case !p.CreatedAt.Before(pastMonthStart):
			buckets["Past Month"] = append(buckets["Past Month"], p)
		default:
			buckets["Beyond"] = append(buckets["Beyond"], p)
		}
	}

	grouped := []pasteGroup{}
	for _, name := range bucketNames {
		if len(buckets[name]) > 0 {
			grouped = append(grouped, pasteGroup{Group: name, Pastes: buckets[name]})
		}
	}

	respondJSON(w, http.StatusOK, grouped)
}

func handleGetPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	cached, err := storage.GetPaste(id)
	if err != nil {
		http.Error(w, "Paste not found", http.StatusNotFound)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"id":       cached.ID,
		"title":    cached.Title,
		"language": cached.Language,
		"content":  cached.Content,
	})
}

func handleDeletePaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	if err := storage.DeletePaste(id); err != nil {
		log.Printf("[delete] failed to remove paste %s: %v", id, err)
		http.Error(w, "Failed to delete paste", http.StatusInternalServerError)
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

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req struct {
		Title    string `json:"title"`
		Content  string `json:"content"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "Content is required", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled"
	}
	title = util.TitleSanitizer.Replace(title)

	if err := storage.UpdatePaste(id, title, req.Content, req.Language); err != nil {
		log.Printf("[update] failed to update paste %s: %v", id, err)
		http.Error(w, "Error updating paste", http.StatusInternalServerError)
		return
	}

	log.Printf("[update] updated paste %q", id)
	respondJSON(w, http.StatusOK, map[string]string{
		"id":    id,
		"title": title,
	})
}

func handleSearchPastes(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	if query == "" {
		handleListPastes(w, r)
		return
	}

	results := storage.SearchPastes(query)
	respondJSON(w, http.StatusOK, results)
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[http] failed to encode response: %v", err)
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
		if os.IsNotExist(err) {
			http.Error(w, "Paste not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Error reading paste", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}