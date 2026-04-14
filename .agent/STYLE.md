# Style Guide & Code Conventions

_This document enforces the visual identity and coding patterns of the project. It prevents context drift as multiple agents work on the codebase. Agents MUST follow these rules strictly._

## 1. Visual Language & Tokens

### Colors
- **Primary Color**: `#3b82f6` (`primary-500`) / `#2563eb` (`primary-600`) (Used for main CTAs)
- **Backgrounds**: Dark mode is standard. Use `bg-gray-50 dark:bg-dark-900` for base layers.
- **Glassmorphism**: Use the `.glass` utility class for semi-transparent headers and sidebars (`background: rgba(..., 0.7); backdrop-filter: blur(10px)`).
- **Language Badges**: Color-coded dynamically based on `getLangColorClasses` (e.g., Python is `blue`, Go is `cyan`, TS is `indigo`).

### Typography
- **Font Families**: Tailwind default sans-serif for UI, monospace for code editors and viewers.
- **Scale**: `text-sm` for sidebar items, `text-lg sm:text-2xl` for titles.

### Spacing & Layout
- **Sidebar**: Fixed width `w-72 sm:w-80`. Becomes a drawer (`mobile-open`) on mobile viewports (< 640px).
- **Custom Scrollbars**: Customized dark-mode scrollbars (`::-webkit-scrollbar` styled in CSS block).

## 2. Component Patterns
- **Buttons**: Rounded, flex centers with gaps for icons (`rounded-lg`, `gap-2`, `transition-all`).
- **Cards/Items**: Paste items in the sidebar use `animate-slide-in` and hover state transitions.

## 3. Code Conventions

### Architecture Patterns
- **Zero Dependencies**: The Go backend strictly relies on the standard library. No frameworks (like Gin or Fiber), no ORMs.
- **Single Page Application**: The frontend is entirely contained within `templates/index.html` using Tailwind via CDN and vanilla JavaScript.

### State Management
- **In-Memory Cache**: The `PasteCache` struct holds all paste data in RAM. It must be locked with `sync.RWMutex` during updates.
- **Client State**: Minimal client state managed via vanilla JS variables (`currentMode`, `currentPasteId`).

### Strict Typing
- **Go**: All code MUST pass `go vet ./...` with zero warnings. Return structured error HTTP responses instead of panicking in handlers.

## 4. Naming Conventions
- **Files**: `snake_case.go` for Go files. Paste files are `{id}_{title}.{ext}`.
- **Variables / Functions**: `camelCase` for unexported Go functions (`langToExt`, `isValidID`). `PascalCase` for exported Go types (`PasteMeta`, `PasteCache`).
- **CSS Classes**: standard Tailwind kebab-case.

## 5. Import Ordering
- **Go — enforced by `goimports`**:
  1. Standard library (`fmt`, `net/http`, `os`, `strings`, `sync`, `time`)
  2. (No third-party or internal packages exist in this project)

## 6. Documentation Standards
- **Godoc**: godoc-compatible comments on all exported and unexported types and functions (`// FunctionName does X.`).

## 7. Anti-Patterns (FORBIDDEN)
- ❌ NEVER use a database (e.g., SQLite, Postgres). Storage is strictly file-based.
- ❌ NEVER add third-party Go dependencies to `go.mod`. Stick to the standard library.
- ❌ NEVER introduce a build step for the frontend (no Node.js, Webpack, Vite, etc.). Use CDN imports.
- ❌ NEVER use `filepath.Glob` for finding files (vulnerable to traversal). Use `os.ReadDir` with prefix matching.
- ❌ NEVER allow file updates. Pastes are strictly immutable.
- ❌ NEVER use raw string concatenation for HTML output without `html.EscapeString` (prevents XSS).