# Style Guide & Code Conventions

_Enforces the visual identity and coding patterns of the project. Prevents context drift as multiple agents work on the codebase. Agents MUST follow these rules strictly._

## 1. Visual Language & Tokens

### Colors
- **Primary**: `#3b82f6` / `#2563eb` (blue-500/600) for CTAs and active states
- **Backgrounds**: Dark mode default. `bg-gray-50 dark:bg-dark-900` for base layers.
- **Glassmorphism**: `.glass` utility — `background: rgba(..., 0.7); backdrop-filter: blur(10px)` for headers and sidebars
- **Language Badges**: Dynamically color-coded per language (`getLangColorClasses`)

### Typography
- Sans-serif (Tailwind default) for UI, monospace for editors and code viewers
- `text-sm` for sidebar items, `text-lg sm:text-2xl` for titles

### Spacing & Layout
- **Sidebar**: Fixed `w-72 sm:w-80`. Mobile drawer with scrim overlay (< 640px).
- **Custom scrollbars**: Dark-mode styled via `::-webkit-scrollbar`

## 2. Component Patterns

- **Buttons**: `rounded-lg`, `gap-2`, `transition-all` with flex-centered icon + label
- **Sidebar items**: `animate-slide-in` entry animation, hover state transitions
- **Templates**: Componentized via Go `html/template` — `layout/head.html`, `layout/tail.html`, `components/{sidebar,paste_app,diff_app,cmdk}.html`

## 3. Code Conventions

### Backend (Go)

- **Package layout**: `cmd/server/` entrypoint → `internal/api/` handlers → `internal/storage/` repository → `internal/models/` types → `internal/util/` helpers
- **Routing**: `http.ServeMux` pattern-based (`GET /api/pastes/{id}`, etc.)
- **Error handling**: `if err != nil` propagation, `http.Error()` responses, `log.Printf("[component] ...")` tracing
- **Cache access**: Always acquire `sync.RWMutex` — `RLock` for reads, `Lock` for writes
- **File creation**: `os.O_EXCL` flag for atomic creation to prevent overwrite races
- **All code must pass** `go vet ./...` with zero warnings

### Frontend (JavaScript)

- **Module system**: ES modules (`import/export`) — no bundler, no build step
- **File organization**: `static/js/{main,state,dom,ui,paste,diff,utils}.js`
- **State management**: Shared `state` object imported from `state.js`
- **DOM references**: Centralized in `dom.js` (`elements` object)
- **Inter-module communication**: Custom DOM events (`app:navigate`, `app:action`)

### Import Ordering (Go)

1. Standard library
2. External dependencies (`github.com/...`)
3. Internal packages (`github.com/arvarik/paste/internal/...`)

## 4. Naming Conventions

- **Go files**: `snake_case.go`
- **Go functions**: `camelCase` unexported, `PascalCase` exported
- **Go types**: `PascalCase` (`PasteMeta`, `CachedDiff`, `PasteCache`)
- **JS files**: `snake_case.js` (e.g., `main.js`, `utils.js`)
- **JS functions**: `camelCase` (`fetchPastes`, `toggleSidebar`)
- **Template files**: `snake_case.html` (e.g., `paste_app.html`, `cmdk.html`)
- **Paste files**: `{id}_{title}.{ext}` on disk
- **CSS classes**: Tailwind kebab-case

## 5. Documentation Standards

- Godoc-compatible comments on all exported and unexported Go types and functions
- `// FunctionName does X.` format

## 6. Anti-Patterns (FORBIDDEN)

- ❌ NEVER use a database (SQLite, Postgres, etc.). Storage is file-based.
- ❌ NEVER introduce a frontend build step (Node.js, Webpack, Vite). Use CDN imports + ES modules.
- ❌ NEVER use `filepath.Glob` for file lookup. Use `os.ReadDir` + prefix matching.
- ❌ NEVER use raw string concatenation for HTML without `html.EscapeString`.
- ❌ NEVER access cache maps without acquiring the appropriate `sync.RWMutex` lock.
- ❌ NEVER add inline `<script>` blocks to templates. All JS goes in `static/js/` as ES modules.