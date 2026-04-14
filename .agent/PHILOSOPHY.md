# Product Philosophy

_This is the soul of the product. It explains why the app exists and what its core beliefs are. Product Visionaries and UI/UX Designers use this to make feature and design decisions. Engineers use it to resolve ambiguity._

## 1. Why This Exists
- I needed a fast, self-hosted snippet tool for taking notes and sharing code without setting up a heavy database like Postgres or Redis. It relies entirely on standard library Go and flat files, ensuring it's trivial to deploy and back up.

## 2. Target User
- Developers, sysadmins, and self-hosters wanting a frictionless, permanent scratchpad. They value simplicity, speed, and standard formatting (Markdown/code highlighting).

## 3. Core Beliefs
- **Zero Dependencies**: Keep the footprint tiny. No databases, no external Go modules, no frontend build pipelines.
- **Immutability**: Once a paste is created, it cannot be edited. This ensures shared links are permanently valid and trustable.
- **Speed**: The app caches all data in memory at startup, allowing for instant, full-text search across all pastes without touching the disk.

## 4. Design & UX Principles
- **Modern aesthetics**: Glassmorphism, tailored syntax highlighting themes, and dynamic language badges give a premium feel.
- **Keyboard-driven**: `Cmd/Ctrl + S` to save, `Cmd/Ctrl + K` to search.
- **Rich content by default**: New pastes default to Markdown with robust rendering and code blocks automatically adopting line numbers and copy buttons.

## 5. What This Is NOT
- Not a fully-featured CMS or wiki. It lacks folders, tags, and user accounts.
- Not a collaborative real-time editor.
- Not a cloud-dependent SaaS. Data stays locally on disk.