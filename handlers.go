package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	LineCount int       `json:"lineCount"`
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
	title = titleSanitizer.Replace(title)

	ext := langToExt(req.Language)

	// Generate a unique ID, retrying on the (extremely unlikely) collision.
	var id string
	for {
		id = generateID()
		_, err := findPasteFile(id)
		if err == os.ErrNotExist {
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
	language := extToLang(ext)
	globalCache.items[id] = CachedPaste{
		ID:            id,
		Title:         title,
		TitleLower:    strings.ToLower(title),
		Content:       req.Content,
		ContentLower:  strings.ToLower(req.Content),
		Language:      language,
		LanguageLower: strings.ToLower(language),
		CreatedAt:     time.Now(),
		Preview:       getPreview(req.Content),
		LineCount:     strings.Count(req.Content, "\n") + 1,
	}
	globalCache.Unlock()

	respondJSON(w, http.StatusCreated, map[string]string{
		"id":    id,
		"title": title,
	})
}

// handleListPastes returns all pastes as an ordered array of time-bucketed
// groups (Today, Yesterday, Past Week, Past Month, Beyond) with pastes
// sorted newest-first within each group. Empty groups are omitted.
//
// Request:  GET /api/pastes
// Response: 200 OK  [{"group": "Today", "pastes": [...]}, ...]
func handleListPastes(w http.ResponseWriter, r *http.Request) {
	globalCache.RLock()
	var pastes []PasteMeta
	for _, cached := range globalCache.items {
		pastes = append(pastes, PasteMeta{
			ID:        cached.ID,
			Title:     cached.Title,
			Language:  cached.Language,
			CreatedAt: cached.CreatedAt,
			Preview:   cached.Preview,
			LineCount: cached.LineCount,
		})
	}
	globalCache.RUnlock()

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
		Group  string      `json:"group"`
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

	respondJSON(w, http.StatusOK, grouped)
}

// getValidPasteFile extracts the ID from the request, validates it, and finds
// the corresponding file on disk. If any step fails, it writes an appropriate
// HTTP error response and returns false.
func getValidPasteFile(w http.ResponseWriter, r *http.Request) (id string, filePath string, ok bool) {
	id = r.PathValue("id")

	if !isValidID(id) {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return "", "", false
	}

	filePath, err := findPasteFile(id)
	if err != nil {
		http.Error(w, "Paste not found", http.StatusNotFound)
		return "", "", false
	}

	return id, filePath, true
}

// handleGetPaste retrieves a single paste by its ID prefix, returning the
// full content along with metadata.
//
// Request:  GET /api/pastes/{id}
// Response: 200 OK  {"id": "...", "title": "...", "language": "...", "content": "..."}
func handleGetPaste(w http.ResponseWriter, r *http.Request) {
	id, filePath, ok := getValidPasteFile(w, r)
	if !ok {
		return
	}
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

	// Self-healing: if this paste was on disk but not in cache (e.g.
	// manually added to the data directory), populate the cache so it
	// appears in future list/search results.
	globalCache.RLock()
	_, cached := globalCache.items[id]
	globalCache.RUnlock()
	if !cached {
		info, infoErr := os.Stat(filePath)
		createdAt := time.Now()
		if infoErr == nil {
			createdAt = info.ModTime()
		}
		globalCache.Lock()
		globalCache.items[id] = CachedPaste{
			ID:            id,
			Title:         title,
			TitleLower:    strings.ToLower(title),
			Content:       string(content),
			ContentLower:  strings.ToLower(string(content)),
			Language:      language,
			LanguageLower: strings.ToLower(language),
			CreatedAt:     createdAt,
			Preview:       getPreview(string(content)),
			LineCount:     strings.Count(string(content), "\n") + 1,
		}
		globalCache.Unlock()
		log.Printf("[cache] self-healed: loaded paste %q from disk into cache", id)
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"id":       id,
		"title":    title,
		"language": language,
		"content":  string(content),
	})
}

// handleDeletePaste permanently removes a paste file and its cache entry.
//
// Request:  DELETE /api/pastes/{id}
// Response: 204 No Content
func handleDeletePaste(w http.ResponseWriter, r *http.Request) {
	id, filePath, ok := getValidPasteFile(w, r)
	if !ok {
		return
	}

	if err := os.Remove(filePath); err != nil {
		log.Printf("[delete] failed to remove %s: %v", filePath, err)
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

	// Precompile regex for highlighted preview to avoid compiling it in the loop
	escapedQuery := html.EscapeString(query)
	re := regexp.MustCompile("(?i)(" + regexp.QuoteMeta(escapedQuery) + ")")

	globalCache.RLock()
	defer globalCache.RUnlock()

	var results []PasteMeta

	for _, paste := range globalCache.items {
		titleMatch := strings.Contains(paste.TitleLower, query)
		contentMatch := strings.Contains(paste.ContentLower, query)
		langMatch := strings.Contains(paste.LanguageLower, query)

		if titleMatch || contentMatch || langMatch {
			results = append(results, PasteMeta{
				ID:        paste.ID,
				Title:     paste.Title,
				Language:  paste.Language,
				CreatedAt: paste.CreatedAt,
				Preview:   getHighlightedPreview(paste.Content, query, re),
				LineCount: paste.LineCount,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	respondJSON(w, http.StatusOK, results)
}

// respondJSON encodes the given payload as JSON and writes it to the response.
// It sets the Content-Type to application/json and writes the provided status code.
func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[http] failed to encode response: %v", err)
	}
}

// handleRawPaste serves the raw content of a paste as plain text with no
// JSON wrapping. Designed for curl/wget/pipe workflows.
//
// Request:  GET /raw/{id}
// Response: 200 OK  text/plain; charset=utf-8  (raw paste content)
func handleRawPaste(w http.ResponseWriter, r *http.Request) {
	_, filePath, ok := getValidPasteFile(w, r)
	if !ok {
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Error reading paste", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}
