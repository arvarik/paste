package main

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/arvarik/paste/internal/api"
	"github.com/arvarik/paste/internal/storage"
	"github.com/arvarik/paste/internal/util"
)

// buildVersion is set via -ldflags at build time (e.g. git short hash).
// Falls back to a server-start timestamp so cache busting always works.
var buildVersion string

var tmpl *template.Template

// templateData is passed to every template render.
type templateData struct {
	Version string
}

func init() {
	if buildVersion == "" {
		buildVersion = fmt.Sprintf("%x", time.Now().Unix())
	}
	tmpl = template.Must(template.ParseGlob("templates/*/*.html"))
	tmpl = template.Must(tmpl.ParseFiles("templates/index.html"))
}

func main() {
	if err := os.MkdirAll(storage.DataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory %q: %v", storage.DataDir, err)
	}

	// Warm the in-memory search cache from existing paste files.
	storage.LoadCacheFromDisk()
	storage.LoadDiffCacheFromDisk()

	mux := http.NewServeMux()

	// Serve static assets with no-cache so browsers always revalidate.
	staticFS := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		staticFS.ServeHTTP(w, r)
	}))

	// API routes
	api.RegisterRoutes(mux)

	// Frontend — serve the SPA for both the root and paste/diff view routes.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/paste/") && !strings.HasPrefix(r.URL.Path, "/diff/") {
			http.NotFound(w, r)
			return
		}
		
		data := templateData{Version: buildVersion}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "index.html", data); err != nil {
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
			log.Printf("Template rendering error: %v", err)
			return
		}
		
		htmlStr := buf.String()

		// If it's a paste view, inject OpenGraph tags dynamically
		if strings.HasPrefix(r.URL.Path, "/paste/") {
			id := strings.TrimPrefix(r.URL.Path, "/paste/")
			// Get paste metadata from cache
			storage.GlobalCache.RLock()
			paste, ok := storage.GlobalCache.Items[id]
			storage.GlobalCache.RUnlock()

			if ok {
				// Build OG tags
				ogTags := `
    <meta property="og:title" content="` + html.EscapeString(paste.Title) + `">
    <meta property="og:description" content="` + html.EscapeString(paste.Preview) + `">
    <meta property="og:image" content="/api/pastes/` + id + `/preview.png">
    <meta name="twitter:card" content="summary_large_image">
</head>`
				// Inject before </head>
				htmlStr = strings.Replace(htmlStr, "</head>", ogTags, 1)
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(htmlStr))
	})

	port := util.GetEnv("PORT", "8083")
	log.Printf("Paste server starting on :%s", port)

	if err := http.ListenAndServe(":"+port, api.LoggingMiddleware(mux)); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
