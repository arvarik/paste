// Package main implements Paste — a lightweight, self-hosted pastebin
// with file-based storage, in-memory search, and syntax highlighting.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

// dataDir is the filesystem path where paste files are stored.
// Configurable via the DATA_DIR environment variable; defaults to "/app/data".
var dataDir = getEnv("DATA_DIR", "/app/data")

// getEnv reads an environment variable or returns a fallback default.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory %q: %v", dataDir, err)
	}

	// Warm the in-memory search cache from existing paste files.
	loadCacheFromDisk()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /api/pastes", handleSavePaste)
	mux.HandleFunc("GET /api/pastes", handleListPastes)
	mux.HandleFunc("GET /api/pastes/{id}", handleGetPaste)
	mux.HandleFunc("DELETE /api/pastes/{id}", handleDeletePaste)
	mux.HandleFunc("GET /api/search", handleSearchPastes)

	// Frontend — serve the SPA for both the root and paste view routes.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/paste/") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "templates/index.html")
	})

	port := getEnv("PORT", "8082")
	log.Printf("Paste server starting on :%s", port)

	if err := http.ListenAndServe(":"+port, loggingMiddleware(mux)); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
