# Architecture

_This document acts as the definitive anchor for understanding system design, data models, API contracts, and technology boundaries. Update this document during the Design and Review phases._

## 0. Project Topology

**Topology:** `[frontend, backend]`

_Agents: Read the corresponding Gemstack topology profiles (`frontend.md` and `backend.md`) from `~/.gemini/antigravity/global_workflows/` before proceeding with any workflow step. These profiles enforce component state coverage, state management discipline, data integrity testing, and anti-mocking rules._

## 1. Tech Stack & Infrastructure
- **Language / Runtime**: Go 1.22+
- **Frontend**: HTML / Tailwind CSS / Prism.js / Marked.js (Single Page Application served via Go `net/http`)
- **Backend / API**: Go standard library (`net/http`)
- **Database**: N/A — No database utilized. File-based storage model (`.txt`, `.go`, `.md`, etc.).
- **Deployment**: Docker Compose / Self-hosted via single binary (`go build`)
- **Package Management**: Go modules (`go.mod`) — Zero external dependencies.

## 2. System Boundaries & Data Flow

### Request / Data Flow
- **Web**: Client interacts with SPA → `fetch` API → Go `net/http` ServeMux routes → Handlers → Filesystem (`DATA_DIR`).
- **File Creation**: Payload validation → Cryptographically random ID generation → Atomic file creation via `os.O_EXCL` → In-memory search index updated.
- **Search**: Client query → `handleSearchPastes` → Iterates over `globalCache` (in-memory index) → Returns highlighted matches.
- **Raw**: Direct `GET /raw/{id}` → `handleRawPaste` → Reads file from disk → Streams `text/plain; charset=utf-8` response with no JSON wrapping.

### Concurrency / Threading Model
- **Go Handlers**: Goroutines handle concurrent HTTP requests natively.
- **Cache Synchronization**: `PasteCache` uses `sync.RWMutex` to allow concurrent search reads while safely blocking during paste creation or deletion writes.

## 3. Data Models & Database Schema
N/A — No database utilized.

### Storage Model
- Every paste is saved as a plain file directly to disk.
- **File Naming Convention**: `{id}_{title}.{ext}`
  - `{id}` is a cryptographically random 6-character alphanumeric string.
  - `{title}` is the sanitized paste title.
  - `{ext}` is determined by the selected language (e.g., `.py`, `.go`, `.md`).

### Go Struct Contracts

```go
// PasteMeta represents the metadata returned by the list and search API endpoints.
type PasteMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Language  string    `json:"language"`
	CreatedAt time.Time `json:"createdAt"`
	Preview   string    `json:"preview"`
	LineCount int       `json:"lineCount"`
}

// CachedPaste holds the metadata and full text content of a single paste.
type CachedPaste struct {
	ID            string
	Title         string
	TitleLower    string
	Content       string
	ContentLower  string
	Language      string
	LanguageLower string
	CreatedAt     time.Time
	Preview       string
	LineCount     int
}
```

## 4. API Contracts

**GET `/api/pastes`**
- **Response**: Array of time-bucketed groups (Today, Yesterday, Past Week, Past Month, Beyond) with pastes sorted newest-first.
- Example: `[{"group": "Today", "pastes": [{"id": "abc123", "title": "example", "language": "go", "createdAt": "...", "preview": "...", "lineCount": 42}]}]`

**GET `/api/pastes/{id}`**
- **Response**: Full content and metadata of a single paste.
- Example: `{"id": "abc123", "title": "example", "language": "go", "content": "..."}`

**POST `/api/pastes`**
- **Request**: `{"title": "My Snippet", "content": "print('hello')", "language": "python"}`
- **Response**: `201 Created` | `{"id": "xyz789", "title": "My-Snippet"}`
- **Constraints**: Request body is limited to 2 MB.

**DELETE `/api/pastes/{id}`**
- **Response**: `204 No Content` on success.

**GET `/api/search?q={query}`**
- **Response**: Array of matching pastes with highlighted preview snippets. Each paste includes `lineCount`.

**GET `/raw/{id}`**
- **Response**: `200 OK` with `Content-Type: text/plain; charset=utf-8`. Body is the raw paste content with no JSON wrapping.
- **404**: Plain text `Paste not found` if the ID doesn't match any file.
- **Use case**: `curl http://host:8083/raw/abc123` returns raw text directly suitable for piping.

## 5. External Integrations / AI
- N/A

### Caching Strategy
- **In-Memory Index (`globalCache`)**: All paste metadata and content is loaded into RAM (`PasteCache`) at server startup. All searches operate against this cache instead of disk I/O. The cache is updated synchronously on every create/delete operation.

### Frontend Interaction Contracts (DX Polish Release)

**View-Mode Header Action Bar** — Button order left to right:
`[Lang Badge]  [Read Only]  [Copy Content]  [Share URL]  [Download]  [Duplicate]`

| Feature | Trigger | Behavior | Backend? |
|---------|---------|----------|----------|
| **Duplicate** | Click button (view mode) | Pre-fills new paste with content/title/lang from current paste. Title suffixed with " copy". Calls `goToNewPaste()` variant. | No |
| **Download** | Click button (view mode) | Creates `Blob` from `currentRawContent`, triggers `<a download="{title}.{ext}">` click. | No |
| **Share URL** | Click button (view mode) | Copies `origin + '/paste/' + id` to clipboard. Toast: "Link copied!" | No |
| **Auto-detect** | Debounced `input` event on textarea (new mode only) | Runs heuristic regex against first 20 lines. Silently updates `langSelect.value` + dropdown display. Disabled if user manually changed dropdown (`userOverrodeLang` flag). | No |
| **Cmd+N** | `keydown` event | `e.preventDefault()` + `goToNewPaste()`. Prevents browser default new-window. | No |

**Keyboard Shortcuts (Complete)**:
| Shortcut | Action |
|----------|--------|
| `Ctrl/Cmd + S` | Save paste |
| `Ctrl/Cmd + K` | Focus search |
| `Ctrl/Cmd + N` | New paste |
| `Escape` | Clear search / return to new paste |

## 6. Invariants & Safety Rules
- ❌ NEVER use a database. Storage must remain file-based to preserve immutability and zero-dependency guarantees.
- ❌ NEVER allow pastes to be updated/edited. Pastes are strictly immutable after creation.
- ❌ NEVER write to an existing file ID. ALWAYS use `os.O_EXCL` (`os.OpenFile(..., os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)`) to guarantee atomic creation and prevent overwriting.
- ❌ NEVER use `filepath.Glob` to find files. It is vulnerable to globbing attacks. ALWAYS iterate `os.ReadDir` and match prefix.
- ❌ NEVER run code generation or require external build steps for the frontend. The SPA must remain a single, statically served `index.html` file.
- ❌ NEVER add external Go dependencies (`go.mod` must only contain the module declaration and go version).

## 7. Error Handling Patterns
- Explicit `if err != nil` error propagation in Go.
- `http.Error(w, message, status)` used consistently in handlers to return structured HTTP status codes.
- `log.Printf("[component] ...")` used for server-side error logging and tracing.

## 8. Directory Structure
- `paste/` — Go application root and entrypoint (`main.go`, `handlers.go`, `helpers.go`, `cache.go`)
- `paste/templates/` — Contains the SPA frontend (`index.html`)
- `paste/data/` — Paste file storage (created automatically, mounted volume in Docker)

## 9. Local Development
- **Install & Build**: `go build .`
- **Start Dev Server**: `go run .` (runs on port 8083 by default)
- **Docker Compose**: `docker compose up -d` (builds or pulls the GHCR image)

## 10. Environment Variables

| Variable | Default | Description |
|----------|----------|-------------|
| `PORT` | `8083` | HTTP server listen port |
| `DATA_DIR` | `/app/data` | Filesystem path for paste file storage |
| `PUID` | `3000` | User ID for Docker container process |
| `PGID` | `3000` | Group ID for Docker container process |