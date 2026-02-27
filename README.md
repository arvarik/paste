# Paste 📝

A lightweight, self-hosted pastebin built with Go and zero external dependencies.

![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)
![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker)
![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)

<img width="1852" height="1449" alt="image" src="https://github.com/user-attachments/assets/5b4fe851-e5a9-4b25-aef3-90feeb809218" />

## Overview

Paste is a fast, single-binary web application for saving and sharing text, code, and notes. It uses **file-based storage** instead of a database — every paste is saved as a plain file (`.py`, `.go`, `.md`, etc.) directly to disk. The Go backend serves a modern SPA frontend using only the standard library.

### Features

- **Database-free** — pastes are stored as plain files on disk. No MySQL, Postgres, or Redis required.
- **Immutable pastes** — once created, a paste cannot be overwritten or edited. Links stay permanently valid.
- **Syntax highlighting** — full highlighting and line numbers for Go, Python, TypeScript, Java, Kotlin, Scala, Bash, HTML, CSS, and JSON via [Prism.js](https://prismjs.com/).
- **Markdown rendering** — `.md` pastes are rendered with headers, lists, and formatting via [Marked.js](https://marked.js.org/).
- **In-memory search** — all paste content is cached in RAM at startup for instant full-text search with highlighted results.
- **Intelligent sidebar** — pastes are automatically grouped by time: Today, Yesterday, Past Week, Past Month, and Beyond.
- **Modern UI** — dark mode, glassmorphism effects, color-coded language badges, and keyboard shortcuts.
- **Tiny Docker image** — multi-stage Alpine build results in an image under 20 MB.
- **Security guardrails** — cryptographically random 6-character IDs, 2 MB upload limit, path traversal protection, and atomic file creation.

---

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/arvarik/paste.git
cd paste
docker compose up -d --build
```

The app will be accessible at **http://localhost:8082**. Pastes are stored in the volume mapped to `/app/data`.

### Run from Source

Requires Go 1.22+:

```bash
git clone https://github.com/arvarik/paste.git
cd paste
go run .
```

The server starts on port `8082` with paste data saved to `./data/`.

---

## Configuration

All configuration is done via environment variables:

| Variable   | Default     | Description                              |
|------------|-------------|------------------------------------------|
| `PORT`     | `8082`      | HTTP server listen port                  |
| `DATA_DIR` | `/app/data` | Filesystem path for paste file storage   |
| `PUID`     | `3000`      | User ID for Docker container process     |
| `PGID`     | `3000`      | Group ID for Docker container process    |

Set `PORT` and `DATA_DIR` as runtime variables when running from source:

```bash
PORT=3000 DATA_DIR=/var/pastes go run .
```

For Docker, `PUID` and `PGID` are configured in the `.env` file and control the user/group the container process runs as.

---

## Usage

### Creating a Paste

1. Enter a title in the top input (defaults to a timestamp).
2. Select a language from the dropdown in the upper right.
3. Type or paste content into the editor.
4. Click **Save** or press `Ctrl/Cmd + S`.

### Viewing & Sharing

- Click any paste in the sidebar, or navigate directly to `http://<host>:8082/paste/<id>`.
- Code pastes are displayed with syntax highlighting and line numbers.
- Markdown pastes are rendered as formatted rich text.
- Click the copy button to copy the raw content to your clipboard.
- Share the URL directly — paste links are permanent.

### Keyboard Shortcuts

| Shortcut          | Action                                |
|-------------------|---------------------------------------|
| `Ctrl/Cmd + S`    | Save the current paste                |
| `Ctrl/Cmd + K`    | Focus the search bar                  |
| `Escape`          | Clear search, or return to new paste  |

---

## API Reference

All endpoints accept and return JSON.

### List Pastes

```
GET /api/pastes
```

Returns all pastes grouped by time bucket:

```json
{
  "Today": [{"id": "abc123", "title": "example", "language": "go", "createdAt": "...", "preview": "..."}],
  "Yesterday": [],
  "Past Week": [],
  "Past Month": [],
  "Beyond": []
}
```

### Get Paste

```
GET /api/pastes/{id}
```

Returns the full content and metadata of a single paste:

```json
{
  "id": "abc123",
  "title": "example",
  "language": "go",
  "content": "package main..."
}
```

### Create Paste

```
POST /api/pastes
Content-Type: application/json

{"title": "My Snippet", "content": "print('hello')", "language": "python"}
```

Returns `201 Created`:

```json
{"id": "xyz789", "title": "My-Snippet"}
```

Request body is limited to **2 MB**.

### Delete Paste

```
DELETE /api/pastes/{id}
```

Returns `204 No Content` on success.

### Search Pastes

```
GET /api/search?q={query}
```

Performs a case-insensitive substring search across titles, content, and languages. Returns a flat array of matching pastes with highlighted preview snippets, sorted newest-first.

---

## Project Structure

```
paste/
├── main.go          # Server setup, routing, and entrypoint
├── handlers.go      # HTTP handlers and middleware
├── cache.go         # In-memory search cache and disk loader
├── helpers.go       # ID generation, language mapping, preview utilities
├── go.mod           # Go module definition (stdlib only)
├── templates/
│   └── index.html   # Single-page application frontend
├── data/            # Paste file storage (created automatically)
├── Dockerfile       # Multi-stage production build
├── compose.yaml     # Docker Compose deployment config
├── .env             # Container user/group configuration
├── LICENSE          # MIT License
└── README.md
```

### File Naming Convention

Pastes are stored as `{id}_{title}.{ext}` where:
- `{id}` is a cryptographically random 6-character alphanumeric string
- `{title}` is the sanitized paste title
- `{ext}` is determined by the selected language (e.g., `.py`, `.go`, `.md`)

Example: `aB3xYz_My-Script.py`

---

## Deployment

### Docker Volume Mapping

The `compose.yaml` maps a host directory to `/app/data` inside the container. Edit the left side of the volume mount to point to your preferred storage location:

```yaml
volumes:
  - /path/to/your/paste/storage:/app/data
```

### NAS / TrueNAS SCALE

When deploying on a NAS, set the `.env` file to match your host user:

```env
PUID=3000
PGID=3000
```

If you encounter permission errors, ensure the host directory is owned by the configured UID/GID:

```bash
sudo chown -R 3000:3000 /path/to/your/paste/storage
```

### Backup & Migration

All data is stored as plain files. Back up the entire data directory to preserve all pastes. To migrate, copy the files into the new container's data volume before starting it.

---

## Contributing

Contributions are welcome! Please:

1. Fork the repository and create your branch from `main`.
2. Ensure your code compiles cleanly with `go build ./...` and passes `go vet ./...`.
3. Keep dependencies at zero — this project uses only the Go standard library.
4. Open a pull request with a clear description of your changes.

---

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
