package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/arvarik/paste/internal/api"
	"github.com/arvarik/paste/internal/auth"
	"github.com/arvarik/paste/internal/config"
	"github.com/arvarik/paste/internal/storage"
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
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	storage.DataDir = cfg.DataDir
	storage.SetStorageLimits(storage.StorageLimits{
		MaxTotalBytes: cfg.MaxStorageBytes, MaxItemBytes: cfg.MaxItemBytes,
		MaxItems: cfg.MaxItems, MaxSearchResults: 100,
		MaxSearchIndexBytes: int(cfg.SearchIndexBytes), MaxCachedContentBytes: cfg.ContentCacheBytes,
		MaxBackupBytes: cfg.BackupLimitBytes,
	})
	api.ConfigurePreviewCache(cfg.PreviewCacheBytes)
	if err := api.ConfigureWorkLimits(api.WorkLimitConfig{
		DiffLimit: cfg.DiffWorkers, FormatLimit: cfg.FormatWorkers,
		PreviewLimit: cfg.PreviewWorkers, WaitTimeout: cfg.WorkWaitTimeout,
	}); err != nil {
		log.Fatalf("Invalid work limits: %v", err)
	}
	api.ConfigureItemFeatures(api.ItemFeatureConfig{
		RequireTokenForCreate: cfg.RequireTokenForCreate,
		DefaultExpiry:         cfg.DefaultExpiry, MaximumExpiry: cfg.MaxExpiry,
	})
	if err := storage.Initialize(); err != nil {
		log.Fatalf("Storage initialization failed for %q: %v", storage.DataDir, err)
	}
	tokenStore, err := auth.NewStore(filepath.Join(cfg.DataDir, "auth", "tokens.json"))
	if err != nil {
		log.Fatalf("Failed to load API tokens: %v", err)
	}
	api.ConfigureAPIAuth(tokenStore, cfg.AdminToken)

	// Warm the in-memory search cache from existing paste files.
	storage.LoadCacheFromDisk()
	storage.LoadDiffCacheFromDisk()

	mux := http.NewServeMux()

	// Serve versioned static assets with immutable caching.
	staticFS := http.StripPrefix("/static/dist/", http.FileServer(http.Dir("static/dist")))
	mux.Handle("GET /static/dist/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") == buildVersion {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		staticFS.ServeHTTP(w, r)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

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

			if ok && !paste.BurnAfterRead && (paste.ExpiresAt == nil || paste.ExpiresAt.After(time.Now())) {
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
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(htmlStr))
	})

	middlewareConfig := api.DefaultHTTPMiddlewareConfig()
	middlewareConfig.RateLimit.AnonymousPerIP = api.RateLimitPolicy{
		Requests: cfg.AnonymousRate, Window: time.Minute, Burst: cfg.RateBurst,
	}
	middlewareConfig.RateLimit.AuthenticatedPerToken = api.RateLimitPolicy{
		Requests: cfg.AuthenticatedRate, Window: time.Minute, Burst: cfg.RateBurst,
	}
	middlewareConfig.RateLimit.CreatePerIdentity = api.RateLimitPolicy{
		Requests: cfg.CreateLimitPerHour, Window: time.Hour, Burst: cfg.CreateLimitPerHour,
	}
	middlewareConfig.RateLimit.AuthenticatedIdentity = func(request *http.Request) (string, bool) {
		principal, ok := api.PrincipalFromRequest(request)
		return principal.TokenID, ok
	}
	for _, prefix := range cfg.TrustedProxies {
		middlewareConfig.ClientIP.TrustedProxyCIDRs = append(middlewareConfig.ClientIP.TrustedProxyCIDRs, prefix.String())
	}
	middlewareConfig.ClientIP.TrustForwardedHeaders = len(middlewareConfig.ClientIP.TrustedProxyCIDRs) > 0
	middlewareConfig.RequestID.AcceptIncoming = false
	middleware, err := api.NewHTTPMiddleware(middlewareConfig)
	if err != nil {
		log.Fatalf("Invalid HTTP middleware configuration: %v", err)
	}
	defer middleware.Close()

	log.Printf("Paste server starting on :%s", cfg.Port)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           middleware.WrapWithAuthentication(mux, api.APIAuthenticationMiddleware),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go purgeExpiredItems(shutdownSignal)
	go func() {
		<-shutdownSignal.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("Server shutdown failed: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server failed: %v", err)
	}
}

func purgeExpiredItems(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			removed, err := storage.PurgeExpired(now)
			if err != nil {
				log.Printf("Expired item cleanup failed: %v", err)
			} else if removed > 0 {
				log.Printf("Expired item cleanup removed %d items", removed)
			}
		}
	}
}
