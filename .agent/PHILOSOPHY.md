# Product Philosophy

_The soul of the product. Explains why the app exists and its core beliefs. Product Visionaries and UI/UX Designers use this to make feature and design decisions. Engineers use it to resolve ambiguity._

## 1. Why This Exists

A fast, self-hosted tool for saving code snippets, notes, and text diffs without the overhead of a database or a cloud dependency. Deploy a single binary or Docker container, point it at a directory, and everything just works — pastes are plain files you can `ls`, `grep`, and `rsync`.

## 2. Target User

Developers, sysadmins, and self-hosters who want a frictionless, permanent scratchpad with syntax highlighting, Markdown rendering, and a built-in diff viewer. They value simplicity, speed, and owning their data.

## 3. Core Beliefs

- **File-based storage**: No databases. Pastes are plain files, diffs are JSON documents. Backup is `cp -r`. Migration is `rsync`.
- **Minimal dependencies**: The Go backend uses only the libraries it genuinely needs (difflib, chroma, gg). The frontend uses CDN imports — no build step.
- **Speed**: All content is cached in RAM at startup. Search is instant. No disk I/O on reads.
- **Self-contained**: A single binary serves the entire app — API, templates, and static assets. No reverse proxy required (but works behind one).

## 4. Design & UX Principles

- **Modern aesthetics**: Dark mode, glassmorphism, color-coded language badges, responsive layout.
- **Keyboard-driven**: `Cmd+K` command palette, `Cmd+S` save, `Cmd+N` new, `Cmd+Shift+F` format.
- **Dual workspaces**: Paste and Diff modes coexist in a single UI with shared sidebar and search.
- **Rich content by default**: New pastes default to Markdown. Fenced code blocks get syntax highlighting and copy buttons.

## 5. What This Is NOT

- Not a CMS, wiki, or collaboration tool. No folders, tags, or user accounts.
- Not a real-time collaborative editor.
- Not a cloud SaaS. Data lives on your filesystem.