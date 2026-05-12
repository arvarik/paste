# Testing Strategy & Results

_Tracks test methods, scenarios, and results with concrete execution evidence. Bugs found here block release. Agents must update during Test and Fix phases._

## 0. Local Development Setup

### Prerequisites
- Go 1.25+
- Docker (optional, for compose setup)

### Start the App
```bash
./dev.sh                                        # Build + run + open browser (Ctrl+C to stop)
go run ./cmd/server/                            # Run directly
PORT=3000 DATA_DIR=./test_data go run ./cmd/server/  # Custom config
docker compose up -d                            # Docker
```

### Seed / Reset Data
Pastes are stored in `./data/` (or `DATA_DIR`). Diffs are in `./data/diffs/`.
To reset: `rm -rf ./data/*`

### Database
N/A — file-based storage.

### Code Generation
N/A

---

## 1. Test Methods & Tools

### Unit / Integration Tests
```bash
go test ./...           # Run all tests
go test -race ./...     # Run with race detector
go vet ./...            # Lint
```

Tests are located in:
- `internal/api/*_test.go` — handler and benchmark tests
- `internal/util/helpers_test.go` — utility function tests

### Backend / API Testing
Execute `curl` commands against `http://localhost:8083/api/pastes` and `/api/saved_diffs`.
Ensure payloads stay under the 2 MB limit.

### Frontend / UI Testing
Manual browser testing. Verify:
- Responsive sidebar drawer (375px mobile vs 1440px desktop)
- Paste and Diff workspace switching
- Command palette (`Cmd+K`) navigation
- Keyboard shortcuts (`Cmd+S`, `Cmd+N`, `Cmd+Shift+F`)

## 2. Execution Evidence Rules

- For Go tests: paste output of `go test -v ./...` showing PASS/FAIL per test
- For linting: paste command and output (e.g., `go vet ./... → no issues`)
- "PASS" with no evidence is treated as **UNTESTED**

---

## Current Feature Scenarios

| Scenario | Status | Notes (Evidence) |
|----------|--------|------------------|
| <!-- Populated by SDET during trap phase --> | | |

---

## Backend Route Coverage Matrix

| Endpoint | Method | 200 OK | 400 Bad Req | 404 Not Found | Edge Cases |
|----------|--------|--------|-------------|---------------|------------|
| `/api/pastes` | GET | | | | |
| `/api/pastes` | POST | | | | 2MB limit |
| `/api/pastes/{id}` | GET | | | | Invalid ID format |
| `/api/pastes/{id}` | PUT | | | | Empty content |
| `/api/pastes/{id}` | DELETE | | | | |
| `/api/search?q=` | GET | | | | Empty query falls through to list |
| `/raw/{id}` | GET | | | | |
| `/api/pastes/{id}/preview.png` | GET | | | | |
| `/api/diff` | POST | | | | Empty base/compare |
| `/api/saved_diffs` | POST | | | | |
| `/api/saved_diffs` | GET | | | | |
| `/api/saved_diffs/{id}` | GET | | | | |
| `/api/saved_diffs/{id}` | DELETE | | | | |
| `/api/search_diffs?q=` | GET | | | | |

---

## Frontend Component State Matrix

| Component / Template | Empty | Loading | Success | Error | Partial |
|---------------------|-------|---------|---------|-------|---------|
| `sidebar.html` — Paste List | | | | | |
| `sidebar.html` — Diff List | | | | | |
| `paste_app.html` — New Mode | | | | | |
| `paste_app.html` — View Mode | | | | | |
| `paste_app.html` — Edit Mode | | | | | |
| `diff_app.html` — New Mode | | | | | |
| `diff_app.html` — View Mode | | | | | |
| `cmdk.html` — Command Palette | | | | | |

---

## Bugs Found (Fix Phase Queue)

*(No bugs logged yet)*

---

## Regression Scenarios (Persistent)

| Scenario | Last Verified | Notes |
|----------|---------------|-------|
| Race detector passes on all packages | _YYYY-MM-DD_ | `go test -race ./...` |
| Paste CRUD cycle (create → read → update → delete) | _YYYY-MM-DD_ | |
| Diff CRUD cycle (compute → save → read → delete) | _YYYY-MM-DD_ | |
| Workspace switching preserves URL state | _YYYY-MM-DD_ | |
| Search returns highlighted results | _YYYY-MM-DD_ | |
| OG preview image renders without crash | _YYYY-MM-DD_ | |
