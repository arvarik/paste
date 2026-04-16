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

## 4. API Contracts

**GET `/api/pastes`**
- **Response**: Array of time-bucketed groups (Today, Yesterday, Past Week, Past Month, Beyond) with pastes sorted newest-first.
- Example: `[{"group": "Today", "pastes": [{"id": "abc123", "title": "example", "language": "go", "createdAt": "...", "preview": "..."}]}]`

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
- **Response**: Array of matching pastes with highlighted preview snippets.

## 5. External Integrations / AI
- N/A

### Caching Strategy
- **In-Memory Index (`globalCache`)**: All paste metadata and content is loaded into RAM (`PasteCache`) at server startup. All searches operate against this cache instead of disk I/O. The cache is updated synchronously on every create/delete operation.

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