package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/models"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

// handleSaveDiff creates a new diff.
func handleSaveDiff(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)

	var req struct {
		Title          string `json:"title"`
		Base           string `json:"base"`
		Compare        string `json:"compare"`
		BaseContent    string `json:"baseContent"`
		CompareContent string `json:"compareContent"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled Diff"
	}
	title = util.TitleSanitizer.Replace(title)

	id, err := storage.CreateDiff(title, req.Base, req.Compare, req.BaseContent, req.CompareContent)
	if err != nil {
		log.Printf("[save_diff] failed to create diff: %v", err)
		http.Error(w, "Error saving diff", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"id":    id,
		"title": title,
	})
}

func handleListDiffs(w http.ResponseWriter, r *http.Request) {
	diffs := storage.ListDiffs()

	type Bucket struct {
		Group string            `json:"group"`
		Items []models.DiffMeta `json:"items"`
	}
	buckets := []Bucket{}

	add := func(group string, items []models.DiffMeta) {
		if len(items) > 0 {
			buckets = append(buckets, Bucket{Group: group, Items: items})
		}
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	pastWeek := today.AddDate(0, 0, -7)
	pastMonth := today.AddDate(0, -1, 0)

	var bToday, bYesterday, bWeek, bMonth, bOlder []models.DiffMeta
	for _, p := range diffs {
		if p.CreatedAt.After(today) {
			bToday = append(bToday, p)
		} else if p.CreatedAt.After(yesterday) {
			bYesterday = append(bYesterday, p)
		} else if p.CreatedAt.After(pastWeek) {
			bWeek = append(bWeek, p)
		} else if p.CreatedAt.After(pastMonth) {
			bMonth = append(bMonth, p)
		} else {
			bOlder = append(bOlder, p)
		}
	}

	add("Today", bToday)
	add("Yesterday", bYesterday)
	add("Past Week", bWeek)
	add("Past Month", bMonth)
	add("Beyond", bOlder)

	respondJSON(w, http.StatusOK, buckets)
}

func handleGetDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	cached, err := storage.GetDiff(id)
	if err != nil {
		http.Error(w, "Diff not found", http.StatusNotFound)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":             cached.ID,
		"title":          cached.Title,
		"base":           cached.Base,
		"compare":        cached.Compare,
		"baseContent":    cached.BaseContent,
		"compareContent": cached.CompareContent,
		"createdAt":      cached.CreatedAt,
	})
}

func handleDeleteDiff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !util.IsValidID(id) {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := storage.DeleteDiff(id); err != nil {
		log.Printf("[delete_diff] failed to delete diff %s: %v", id, err)
		http.Error(w, "Failed to delete diff", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleSearchDiffs(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		handleListDiffs(w, r)
		return
	}

	results := storage.SearchDiffs(query)

	respondJSON(w, http.StatusOK, []interface{}{
		map[string]interface{}{
			"group": "Search Results",
			"items": results,
		},
	})
}
