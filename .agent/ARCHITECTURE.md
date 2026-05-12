# Architecture

_Definitive anchor for system design, data models, API contracts, and technology boundaries. Update during Design and Review phases._

## 0. Project Topology

**Topology:** `[frontend, backend]`

_Agents: Read the corresponding Gemstack topology profiles (`frontend.md` and `backend.md`) from `~/.gemini/antigravity/global_workflows/` before proceeding with any workflow step._

## 1. Tech Stack & Infrastructure

- **Language / Runtime**: Go 1.25+
- **Frontend**: Go `html/template` components, Tailwind CSS (CDN), Prism.js, Marked.js, ES module JavaScript (`static/js/`)
- **Backend / API**: Go `net/http` with `http.ServeMux` pattern-based routing
- **Dependencies**: [go-difflib](https://github.com/pmezard/go-difflib) (diff computation), [Chroma](https://github.com/alecthomas/chroma) (syntax highlighting for OG images), [gg](https://github.com/fogleman/gg) + [x/image](https://pkg.go.dev/golang.org/x/image) (PNG rendering)
- **Database**: None — file-based storage (pastes as `{id}_{title}.{ext}`, diffs as `{id}_{title}.json`)
- **Deployment**: Docker Compose / single binary (`go build ./cmd/server/`)

## 2. System Boundaries & Data Flow

### Request / Data Flow

```
Browser SPA  ──fetch──▶  Go ServeMux  ──▶  api.Handler  ──▶  storage.Repository  ──▶  Filesystem (DATA_DIR)
                                                                    ▲
                                                              PasteCache / DiffCache
                                                              (in-memory, sync.RWMutex)
```

- **Paste CRUD**: JSON payload → `internal/api/handlers.go` → `internal/storage/paste_store.go` → file on disk + cache update
- **Diff CRUD**: JSON payload → `internal/api/diff_handlers.go` → `internal/storage/diff_store.go` → JSON file in `diffs/` subdirectory + cache update
- **Diff Computation**: `POST /api/diff` → `internal/api/diff.go` → go-difflib `SequenceMatcher` → structured opcode response
- **Search**: Query → iterate `GlobalCache` / `GlobalDiffCache` in-memory → return highlighted matches
- **Raw**: `GET /raw/{id}` → read file from disk → stream `text/plain`
- **OG Preview**: `GET /api/pastes/{id}/preview.png` → Chroma tokenization → gg canvas rendering → PNG response

### Concurrency Model

- Go HTTP handlers run in goroutines — concurrent by default
- `PasteCache` and `DiffCache` use `sync.RWMutex` — concurrent reads, exclusive writes
- File creation uses `os.O_EXCL` for atomic creation (no overwrite race)

## 3. Data Models

### Storage Model

- **Pastes**: `{DATA_DIR}/{id}_{title}.{ext}` — plain text files, extension maps to language
- **Diffs**: `{DATA_DIR}/diffs/{id}_{title}.json` — JSON documents containing base/compare labels and raw content

### Go Struct Contracts

Source: [`internal/models/models.go`](../internal/models/models.go)

```go
// Paste API response metadata
type PasteMeta struct {
    ID, Title, Language, Preview string
    CreatedAt                    time.Time
    LineCount                    int
}

// In-memory paste cache entry (includes lowercased fields for search)
type CachedPaste struct {
    ID, Title, TitleLower, Content, ContentLower string
    Language, LanguageLower                      string
    CreatedAt                                    time.Time
    Preview                                      string
    LineCount                                    int
}

// Diff API response metadata
type DiffMeta struct {
    ID, Title string
    CreatedAt time.Time
}

// In-memory diff cache entry
type CachedDiff struct {
    ID, Title, TitleLower                       string
    Base, Compare, BaseContent, CompareContent  string
    ContentLower                                string
    CreatedAt                                   time.Time
}

// On-disk diff JSON format
type DiffData struct {
    Base, Compare, BaseContent, CompareContent string
}
```

## 4. API Contracts

### Paste Endpoints

| Method   | Path                            | Request Body                                      | Response                                |
|----------|---------------------------------|---------------------------------------------------|-----------------------------------------|
| `GET`    | `/api/pastes`                   | —                                                 | Time-bucketed groups of `PasteMeta[]`   |
| `POST`   | `/api/pastes`                   | `{title, content, language}`                      | `201` `{id, title}`                     |
| `GET`    | `/api/pastes/{id}`              | —                                                 | `{id, title, language, content}`        |
| `PUT`    | `/api/pastes/{id}`              | `{title, content, language}`                      | `200` `{id, title}`                     |
| `DELETE` | `/api/pastes/{id}`              | —                                                 | `204`                                   |
| `GET`    | `/api/search?q={query}`         | —                                                 | `PasteMeta[]` with highlighted previews |
| `GET`    | `/raw/{id}`                     | —                                                 | `text/plain` raw content                |
| `GET`    | `/api/pastes/{id}/preview.png`  | —                                                 | `image/png` OG preview image            |

### Diff Endpoints

| Method   | Path                            | Request Body                                              | Response                              |
|----------|---------------------------------|-----------------------------------------------------------|---------------------------------------|
| `POST`   | `/api/diff`                     | `{base, compare}`                                         | `{opCodes[], baseLines[], compareLines[]}` |
| `POST`   | `/api/saved_diffs`              | `{title, base, compare, baseContent, compareContent}`     | `201` `{id, title}`                   |
| `GET`    | `/api/saved_diffs`              | —                                                         | Time-bucketed groups of `DiffMeta[]`  |
| `GET`    | `/api/saved_diffs/{id}`         | —                                                         | Full diff with base/compare content   |
| `DELETE` | `/api/saved_diffs/{id}`         | —                                                         | `204`                                 |
| `GET`    | `/api/search_diffs?q={query}`   | —                                                         | `DiffMeta[]` search results           |

**Constraints**: All request bodies limited to 2 MB. IDs are 6-character alphanumeric strings.

### Frontend Routes (SPA)

| URL Pattern       | Behavior                        |
|--------------------|---------------------------------|
| `/`                | Redirect to paste new           |
| `/paste/new`       | New paste editor                |
| `/paste/{id}`      | View/edit existing paste        |
| `/diff/new`        | New diff workspace              |
| `/diff/{id}`       | View existing saved diff        |

## 5. Caching Strategy

- **`GlobalCache` (PasteCache)**: All paste metadata + content loaded into RAM at startup via `LoadCacheFromDisk()`. Searched in O(N) with lowercased field comparisons. Updated synchronously on every create/update/delete.
- **`GlobalDiffCache` (DiffCache)**: Same pattern for diffs. Loaded from `diffs/` subdirectory.
- **Self-healing**: `GetPaste()` falls back to disk read if cache miss, then populates the cache entry.
- **`FindPasteFile()`**: O(1) cache-assisted lookup with O(N) `os.ReadDir` fallback.

## 6. Invariants & Safety Rules

- ❌ NEVER use a database. Storage must remain file-based.
- ❌ NEVER use `filepath.Glob` to find files. Use `os.ReadDir` with prefix matching to prevent globbing attacks.
- ❌ NEVER introduce a frontend build step (no Node.js, Webpack, Vite). Use CDN imports and ES modules.
- ❌ NEVER use raw string concatenation for HTML output without `html.EscapeString`.
- ❌ NEVER skip `sync.RWMutex` locking when accessing cache maps.

## 7. Error Handling

- `if err != nil` error propagation in all Go code
- `http.Error(w, message, status)` for handler error responses
- `log.Printf("[component] ...")` for structured server-side logging

## 8. Directory Structure

```
cmd/server/main.go         — Entrypoint, routing, template rendering, OG tag injection
internal/api/              — HTTP handlers, middleware, diff computation, preview image generation
internal/models/           — Shared data types (PasteMeta, CachedPaste, DiffMeta, DiffData)
internal/storage/           — File I/O, in-memory cache, CRUD for pastes and diffs
internal/util/             — ID generation, language mapping, sanitization, helpers
templates/                 — Go html/template components (layout/, components/, index.html)
static/js/                 — ES module frontend (main, state, dom, ui, paste, diff, utils)
data/                      — Runtime paste/diff storage (auto-created, Docker volume mount)
```

## 9. Local Development

```bash
./dev.sh              # Build, start on :8083, open browser — Ctrl+C stops
./dev.sh 9090         # Custom port
go run ./cmd/server/  # Run directly
go test ./...         # Run all tests
go vet ./...          # Lint
```

## 10. Environment Variables

| Variable   | Default     | Description                            |
|------------|-------------|----------------------------------------|
| `PORT`     | `8083`      | HTTP listen port                       |
| `DATA_DIR` | `/app/data` | Filesystem path for stored pastes/diffs|
| `PUID`     | `3000`      | Container process user ID (Docker)     |
| `PGID`     | `3000`      | Container process group ID (Docker)    |