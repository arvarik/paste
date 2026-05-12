# Paste 📝

A lightweight, self-hosted pastebin and diff viewer built with Go.

![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat-square&logo=go)
![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker)
![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)

<img width="1852" height="1449" alt="Paste — self-hosted pastebin UI" src="https://github.com/user-attachments/assets/5b4fe851-e5a9-4b25-aef3-90feeb809218" />

## Overview

Paste is a self-hosted web application for saving, sharing, and comparing text. It uses **file-based storage** — every paste is a plain file on disk, and every diff is a JSON document. No database required.

### Key Features

**Core**
- Dual workspaces — **Paste** mode and **Diff** mode, switchable from the sidebar or the `Cmd+K` command palette
- Syntax highlighting for 12 languages via [Prism.js](https://prismjs.com/), with line numbers
- Markdown rendering via [Marked.js](https://marked.js.org/) with highlighted fenced code blocks and per-block copy buttons
- Side-by-side diff viewer with structured, line-level comparison powered by [go-difflib](https://github.com/pmezard/go-difflib)
- Full-text search across all pastes and diffs, served from an in-memory cache

**UX**
- Command palette (`Cmd/Ctrl + K`) for fast navigation and actions
- Auto-detected language from content heuristics (shebangs, keywords)
- Smart sidebar grouping — Today, Yesterday, Past Week, Past Month, Beyond
- Editable pastes, one-click share/download/duplicate, raw text endpoint for CLI usage
- Dark mode, responsive layout, glassmorphism UI

**Ops**
- Single binary, multi-stage Alpine Docker image
- OpenGraph preview images generated server-side via [Chroma](https://github.com/alecthomas/chroma) and [gg](https://github.com/fogleman/gg)
- Cryptographically random 6-character IDs, 2 MB upload limit, path traversal protection

---

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/arvarik/paste.git && cd paste
docker compose up -d
```

Pulls from `ghcr.io/arvarik/paste:latest`. The app is available at **http://localhost:8083**.
To build locally, swap the `image:` line for `build: .` in `compose.yaml`.

### From Source

Requires Go 1.25+:

```bash
git clone https://github.com/arvarik/paste.git && cd paste
go run ./cmd/server/
```

Server starts on `:8083` with data in `./data/`.

---

## Configuration

| Variable   | Default     | Description                            |
|------------|-------------|----------------------------------------|
| `PORT`     | `8083`      | HTTP listen port                       |
| `DATA_DIR` | `/app/data` | Filesystem path for stored pastes/diffs|
| `PUID`     | `3000`      | Container process user ID (Docker)     |
| `PGID`     | `3000`      | Container process group ID (Docker)    |

```bash
PORT=3000 DATA_DIR=/var/pastes go run ./cmd/server/
```

For Docker, `PUID`/`PGID` are set in the `.env` file.

---

## Usage

### Keyboard Shortcuts

| Shortcut               | Action                          |
|------------------------|---------------------------------|
| `Ctrl/Cmd + S`         | Save current paste or diff      |
| `Ctrl/Cmd + K`         | Open command palette            |
| `Ctrl/Cmd + N`         | New paste or diff               |
| `Ctrl/Cmd + Shift + F` | Format code (paste editor)      |
| `Escape`               | Close palette / sidebar         |

### Diffs

Switch to the **Diff** workspace, enter text in the Base and Compare panels, and click **Compare**. Save the diff to get a shareable URL at `/diff/<id>`.

---

## API

All endpoints accept and return `application/json` unless noted.

### Pastes

| Method   | Path                          | Description                             |
|----------|-------------------------------|-----------------------------------------|
| `GET`    | `/api/pastes`                 | List all pastes (time-bucketed groups)  |
| `POST`   | `/api/pastes`                 | Create a paste                          |
| `GET`    | `/api/pastes/{id}`            | Get paste content and metadata          |
| `PUT`    | `/api/pastes/{id}`            | Update a paste                          |
| `DELETE` | `/api/pastes/{id}`            | Delete a paste                          |
| `GET`    | `/api/search?q={query}`       | Full-text search across pastes          |
| `GET`    | `/raw/{id}`                   | Raw content as `text/plain`             |
| `GET`    | `/api/pastes/{id}/preview.png`| Syntax-highlighted OG preview image     |

### Diffs

| Method   | Path                          | Description                             |
|----------|-------------------------------|-----------------------------------------|
| `POST`   | `/api/diff`                   | Compute a diff (returns opcodes + lines)|
| `POST`   | `/api/saved_diffs`            | Save a diff                             |
| `GET`    | `/api/saved_diffs`            | List saved diffs (time-bucketed groups) |
| `GET`    | `/api/saved_diffs/{id}`       | Get a saved diff                        |
| `DELETE` | `/api/saved_diffs/{id}`       | Delete a saved diff                     |
| `GET`    | `/api/search_diffs?q={query}` | Full-text search across diffs           |

Request bodies are limited to **2 MB**. All IDs are 6-character alphanumeric strings.

<details>
<summary>Example: Create and retrieve a paste</summary>

```bash
# Create
curl -s -X POST http://localhost:8083/api/pastes \
  -H 'Content-Type: application/json' \
  -d '{"title":"Hello","content":"print(\"world\")","language":"python"}'
# → {"id":"xK9mPq","title":"Hello"}

# Retrieve
curl -s http://localhost:8083/api/pastes/xK9mPq
# → {"id":"xK9mPq","title":"Hello","language":"python","content":"print(\"world\")"}

# Raw
curl -s http://localhost:8083/raw/xK9mPq
# → print("world")
```

</details>

---

## Project Structure

```
paste/
├── cmd/server/             # Entrypoint — routing, template rendering, OG tag injection
├── internal/
│   ├── api/                # HTTP handlers, middleware, diff computation, preview image gen
│   ├── models/             # Shared data types (PasteMeta, CachedPaste, DiffMeta, DiffData)
│   ├── storage/            # File I/O, in-memory cache, CRUD for pastes and diffs
│   └── util/               # ID generation, language mapping, sanitization, helpers
├── templates/
│   ├── layout/             # head.html, tail.html (shared <head> and script tags)
│   └── components/         # sidebar, paste_app, diff_app, cmdk (command palette)
├── static/js/              # ES module frontend (main, state, dom, ui, paste, diff, utils)
├── Dockerfile              # Multi-stage Alpine build
├── compose.yaml            # Production Docker Compose config
└── dev.sh                  # Local dev script (build + run + open browser)
```

Pastes are stored as `{id}_{title}.{ext}` (e.g., `aB3xYz_My-Script.py`).
Diffs are stored as `{id}_{title}.json` in a `diffs/` subdirectory.

---

## Deployment

### Volume Mapping

Edit the left side of the volume mount in `compose.yaml`:

```yaml
volumes:
  - /your/host/path:/app/data
```

### CI/CD

Push to `main` → [GitHub Actions](.github/workflows/publish.yml) builds and pushes to `ghcr.io/arvarik/paste:latest`. If running [Watchtower](https://containrrr.dev/watchtower/), the container updates automatically.

Manual update:

```bash
docker compose pull && docker compose up -d
```

### Backup

All data is plain files. Back up the data directory to preserve everything. To migrate, copy files into the new container's data volume.

---

## Development

```bash
./dev.sh          # Build, start on :8083, open browser — Ctrl+C to stop
./dev.sh 9090     # Custom port
```

Run tests:

```bash
go test ./...
go vet ./...
```

---

## Contributing

1. Fork the repo and branch from `main`.
2. Ensure `go build ./...`, `go vet ./...`, and `go test ./...` pass.
3. Open a PR with a clear description.

---

## License

MIT — see [LICENSE](LICENSE).
