package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// loggingMiddleware wraps an http.Handler to log the remote address, method,
// path, and duration of every request to stdout.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[http] %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		log.Printf("[http] %s %s %s completed in %v", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
	})
}

// PasteMeta represents the metadata returned by the list and search API endpoints.
type PasteMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Language  string    `json:"language"`
	CreatedAt time.Time `json:"createdAt"`
	Preview   string    `json:"preview"`
}

// handleSavePaste creates a new paste from a JSON request body.
// The payload is limited to 2 MB to prevent resource exhaustion.
//
// Request:  POST /api/pastes  {"title": "...", "content": "...", "language": "..."}
// Response: 201 Created       {"id": "...", "title": "..."}
func handleSavePaste(w http.ResponseWriter, r *http.Request) {
	// Cap request body at 2 MB to prevent memory exhaustion.
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

	// Sanitize the title to prevent path traversal and filesystem issues.
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled"
	}
	title = strings.ReplaceAll(title, "/", "_")
	title = strings.ReplaceAll(title, "\\", "_")
	title = strings.ReplaceAll(title, " ", "-")

	ext := langToExt(req.Language)

	// Generate a unique ID, retrying on the (extremely unlikely) collision.
	var id string
	for {
		id = generateID()
		matches, _ := filepath.Glob(filepath.Join(dataDir, id+"_*.*"))
		if len(matches) == 0 {
			break
		}
	}

	// Filename format: {id}_{title}.{ext}
	filename := fmt.Sprintf("%s_%s%s", id, title, ext)
	filePath := filepath.Join(dataDir, filename)

	// O_EXCL guarantees atomic creation — fails if the file already exists.
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		log.Printf("[save] failed to create file %s: %v", filePath, err)
		http.Error(w, "Error saving paste", http.StatusConflict)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(req.Content); err != nil {
		log.Printf("[save] failed to write %s: %v", filePath, err)
		http.Error(w, "Error writing content", http.StatusInternalServerError)
		return
	}

	// Update the in-memory cache synchronously.
	globalCache.Lock()
	globalCache.items[id] = CachedPaste{
		ID:        id,
		Title:     title,
		Content:   req.Content,
		Language:  extToLang(ext),
		CreatedAt: time.Now(),
	}
	globalCache.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"id":    id,
		"title": title,
	}); err != nil {
		log.Printf("[save] failed to encode response: %v", err)
	}
}

// handleListPastes returns all pastes grouped by time bucket (Today, Yesterday,
// Past Week, Past Month, Beyond) sorted newest-first within each group.
//
// Request:  GET /api/pastes
// Response: 200 OK  {"Today": [...], "Yesterday": [...], ...}
func handleListPastes(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		http.Error(w, "Error reading data directory", http.StatusInternalServerError)
		return
	}

	var pastes []PasteMeta
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Filename format: {id}_{title}.{ext}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			continue
		}

		id := parts[0]
		ext := filepath.Ext(entry.Name())
		title := strings.TrimSuffix(parts[1], ext)
		language := extToLang(ext)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Read preview text from cache to avoid disk I/O.
		var previewText string
		globalCache.RLock()
		if cached, ok := globalCache.items[id]; ok {
			previewText = cached.Content
		}
		globalCache.RUnlock()

		pastes = append(pastes, PasteMeta{
			ID:        id,
			Title:     title,
			Language:  language,
			CreatedAt: info.ModTime(),
			Preview:   getPreview(previewText),
		})
	}

	sort.Slice(pastes, func(i, j int) bool {
		return pastes[i].CreatedAt.After(pastes[j].CreatedAt)
	})

	// Group pastes into time-based buckets for sidebar display.
	// We use an ordered slice (not a map) so JSON output preserves the
	// newest-to-oldest group order. Go's encoding/json sorts map keys
	// alphabetically, which would place "Beyond" first.
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	pastWeekStart := todayStart.AddDate(0, 0, -7)
	pastMonthStart := todayStart.AddDate(0, -1, 0)

	type pasteGroup struct {
		Group  string     `json:"group"`
		Pastes []PasteMeta `json:"pastes"`
	}

	bucketNames := []string{"Today", "Yesterday", "Past Week", "Past Month", "Beyond"}
	buckets := make(map[string][]PasteMeta, len(bucketNames))
	for _, name := range bucketNames {
		buckets[name] = []PasteMeta{}
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

	// Build the ordered response, omitting empty groups.
	var grouped []pasteGroup
	for _, name := range bucketNames {
		if len(buckets[name]) > 0 {
			grouped = append(grouped, pasteGroup{Group: name, Pastes: buckets[name]})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(grouped); err != nil {
		log.Printf("[list] failed to encode response: %v", err)
	}
}

// handleGetPaste retrieves a single paste by its ID prefix, returning the
// full content along with metadata.
//
// Request:  GET /api/pastes/{id}
// Response: 200 OK  {"id": "...", "title": "...", "language": "...", "content": "..."}
func handleGetPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Sanitize to prevent directory traversal attacks.
	id = filepath.Base(id)
	if id == "" || id == "." || id == "/" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	matches, err := filepath.Glob(filepath.Join(dataDir, id+"_*.*"))
	if err != nil || len(matches) == 0 {
		http.Error(w, "Paste not found", http.StatusNotFound)
		return
	}

	filePath := matches[0]
	filename := filepath.Base(filePath)

	// Extract title and language from filename.
	parts := strings.SplitN(filename, "_", 2)
	title := ""
	language := "text"
	if len(parts) == 2 {
		ext := filepath.Ext(filename)
		title = strings.TrimSuffix(parts[1], ext)
		language = extToLang(ext)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Error reading paste", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"id":       id,
		"title":    title,
		"language": language,
		"content":  string(content),
	}); err != nil {
		log.Printf("[get] failed to encode response: %v", err)
	}
}

// handleDeletePaste permanently removes a paste file and its cache entry.
//
// Request:  DELETE /api/pastes/{id}
// Response: 204 No Content
func handleDeletePaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Sanitize to prevent directory traversal attacks.
	id = filepath.Base(id)
	if id == "" || id == "." || id == "/" {
		log.Printf("[delete] rejected invalid ID: %q", id)
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	matches, err := filepath.Glob(filepath.Join(dataDir, id+"_*.*"))
	if err != nil || len(matches) == 0 {
		http.Error(w, "Paste not found", http.StatusNotFound)
		return
	}

	if err := os.Remove(matches[0]); err != nil {
		log.Printf("[delete] failed to remove %s: %v", matches[0], err)
		http.Error(w, "Failed to delete paste", http.StatusInternalServerError)
		return
	}

	globalCache.Lock()
	delete(globalCache.items, id)
	globalCache.Unlock()

	log.Printf("[delete] removed paste %q", id)
	w.WriteHeader(http.StatusNoContent)
}

// handleSearchPastes performs a case-insensitive substring search across all
// cached paste titles, content, and languages. Results are sorted newest-first
// with highlighted preview snippets.
//
// Request:  GET /api/search?q={query}
// Response: 200 OK  [{"id": "...", "title": "...", ...}, ...]
func handleSearchPastes(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	if query == "" {
		handleListPastes(w, r)
		return
	}

	globalCache.RLock()
	defer globalCache.RUnlock()

	var results []PasteMeta

	for _, paste := range globalCache.items {
		titleMatch := strings.Contains(strings.ToLower(paste.Title), query)
		contentMatch := strings.Contains(strings.ToLower(paste.Content), query)
		langMatch := strings.Contains(strings.ToLower(paste.Language), query)

		if titleMatch || contentMatch || langMatch {
			results = append(results, PasteMeta{
				ID:        paste.ID,
				Title:     paste.Title,
				Language:  paste.Language,
				CreatedAt: paste.CreatedAt,
				Preview:   getHighlightedPreview(paste.Content, query),
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		log.Printf("[search] failed to encode response: %v", err)
	}
}
