# Paste

A fast, private paste and diff workspace that you can run on your own server.

[![Go 1.26.6](https://img.shields.io/badge/Go-1.26.6-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![Docker ready](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)](https://www.docker.com/)
[![MIT license](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

![Paste workspace in a desktop browser](docs/screenshots/paste-desktop.png)

Paste keeps notes, source code, and visual comparisons in one responsive interface. It stores all data in local files under your control.

## What it does

- Creates, edits, searches, and shares text or source code.
- Renders safe Markdown and highlighted code.
- Compares text in unified or side-by-side views.
- Protects changes with private edit secrets and optional API tokens.
- Keeps revision history and restores earlier content.
- Supports tags, favorites, expiry, and burn-after-read items.
- Imports and exports portable item files.
- Creates verified backups with the included command-line tool.
- Runs without a database or external frontend service.

| Mobile paste | Responsive diff |
|---|---|
| ![Paste workspace on a mobile screen](docs/screenshots/paste-mobile.png) | ![Diff workspace on a mobile screen](docs/screenshots/diff-mobile.png) |

![Diff workspace in a desktop browser](docs/screenshots/diff-desktop.png)

## Start with Docker Compose

```bash
git clone https://github.com/arvarik/paste.git
cd paste
cp .env.example .env
docker compose up -d
```

Open [http://localhost:8083](http://localhost:8083).

Compose binds Paste to the local computer by default. Put an HTTPS reverse proxy in front of Paste for remote access.

## Security by default

Each new item receives a 128-bit public ID and a separate edit secret. The browser stores the edit secret only on the current device.

The server limits request rates, item creation, storage, and expensive work. It also sanitizes Markdown and serves pinned local frontend assets.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md)
- [Configuration](docs/configuration.md)
- [API reference](docs/api.md)
- [Operations and backups](docs/operations.md)
- [Security guide](docs/security.md)
- [Developer guide](docs/development.md)

## License

Paste uses the [MIT License](LICENSE).
