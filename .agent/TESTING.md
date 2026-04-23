# Testing Strategy & Results

_This file tracks test methods, scenarios, and results with concrete execution evidence. Bugs found here block the release of a feature. Agents must update this during the Test and Fix phases._

## 0. Local Development Setup
_Exact steps to get the application running locally so agents can execute tests._

### Prerequisites
- Go 1.22+
- Docker (optional, for compose setup)

### Start the App
- Run directly: `go run .` (Starts server on `http://localhost:8083`)
- Configure ports/data: `PORT=3000 DATA_DIR=./test_data go run .`
- Run via Docker Compose: `docker compose up -d`

### Seed / Reset Data
- The application stores pastes in the `./data` directory (or the path defined by `DATA_DIR`). To reset the environment, simply delete the files inside this directory: `rm -rf ./data/*`

### Database
- N/A — file-based storage.

### Code Generation
- N/A

---

## 1. Test Methods & Tools

### Unit / Integration Tests
- **Run all tests**: `go test ./...`
- **Run with race detector**: `go test -race ./...`
- **Run linter**: `go vet ./...`

### Backend / API Testing
- Execute `curl` commands against `http://localhost:8083/api/pastes`.
- Ensure payloads adhere to the 2MB upload limit.

### Frontend / UI Testing
- Test manually in the browser. Verify responsive drawer logic (375px mobile vs 1440px desktop).

## 2. Execution Evidence Rules
_Never mark a test as PASS without evidence._
- For Go tests, paste the output of `go test -v ./...` (showing individual test PASS/FAIL lines).
- For type checking / linting, paste the command and its output (e.g., `go vet ./... → no issues`).
- "PASS" with no evidence is treated as UNTESTED.

---

## Current Feature Scenarios

| Scenario | Status | Notes (Evidence) |
|----------|--------|------------------|
| <!-- Populated by SDET during trap phase --> | | |

---

## Backend Route Coverage Matrix

_Populated by the SDET during the Trap phase. One row per API endpoint._

| Endpoint | Method | 200 OK | 400 Bad Req | 401/403 Auth | 404 Not Found | Idempotent | Edge Cases |
|----------|--------|--------|-------------|--------------|---------------|------------|------------|
| <!-- Pre-populate by scanning Go HTTP handler registrations (http.HandleFunc, chi routes, mux routes, etc.) --> | | | | | | | |

---

## Frontend Component State Matrix

_Populated by the SDET during the Trap phase. Every template/page must be tested across all visual states._

| Component / Template | Empty | Loading | Success | Error | Partial |
|---------------------|-------|---------|---------|-------|---------|
| <!-- Pre-populate by scanning HTML templates and pages served by this application --> | | | | | |

---

## ML / AI Evaluation Thresholds

N/A — ML/AI topology is not active for this project.

## Bugs Found (Fix Phase Queue)
_List specific bugs discovered during testing. Agents in the 'Fix' phase will read this section._

*(No bugs logged yet)*

---

## Regression Scenarios (Persistent)

| Scenario | Last Verified | Notes |
|----------|---------------|-------|
| _Race detector passes on all packages_ | _YYYY-MM-DD_ | _Go: `go test -race ./...`_ |

### Frontend Component State Matrix

| Component | Loading | Empty | Error | Populated | Offline |
|-----------|---------|-------|-------|-----------|---------|
| _Fill in your components_ | | | | | |

### Backend Route Coverage

| Route | Method | Auth | Contract Test | Integration Test |
|-------|--------|------|---------------|------------------|
| _Fill in your routes_ | | | | |

