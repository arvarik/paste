# Audit Findings — DX Polish Release

**Auditor**: Security Engineer + SDET
**Date**: 2026-04-16
**Scope**: `git diff` — 8 files, +330/-21 lines
**SAST**: `go vet` ✅ clean, `-race` ✅ clean, all 14 tests pass

---

## Verdict: PASS

No `[BLOCKS_RELEASE]` or `[DEGRADED]` findings. Two low-severity cosmetic observations below.

---

## Backend Review

### `handleRawPaste` — Security ✅

| Check | Status | Notes |
|-------|--------|-------|
| ID validation | ✅ PASS | Reuses `getValidPasteFile()` → `isValidID()` (alphanumeric only) |
| Path traversal | ✅ PASS | `findPasteFile()` does prefix matching, no user-controlled path components |
| File not found | ✅ PASS | Returns 404 via `getValidPasteFile()` |
| Content-Type | ✅ PASS | Hardcoded `text/plain; charset=utf-8` — no content sniffing |
| Method restriction | ✅ PASS | Go 1.22 mux `"GET /raw/{id}"` rejects POST/PUT/DELETE automatically |
| Error disclosure | ✅ PASS | Generic "Error reading paste" (no file path leaked) |
| Atomic read | ✅ PASS | `os.ReadFile` reads entire file atomically |

### `LineCount` — Logic ✅

| Check | Status | Notes |
|-------|--------|-------|
| Formula consistency | ✅ PASS | `strings.Count(content, "\n") + 1` used in all 4 locations (save, disk load, list, self-heal) |
| Cache coherence | ✅ PASS | Tests verify lineCount survives cache clear + reload from disk |
| JSON field | ✅ PASS | `json:"lineCount"` in `PasteMeta`, propagated in list, search, and get |
| Zero value | ✅ OK | `int` defaults to 0 for pre-existing cache entries — harmless, frontend guards with `lineCount > 0` |

### Logic Drift Check ✅

No hardcoded values, no static responses, no test-only shortcuts detected. The builder implemented genuine read-from-disk and compute-from-content logic.

---

## Frontend Review

### XSS Surface Analysis

| Vector | Status | Notes |
|--------|--------|-------|
| `lineCount` in sidebar | ✅ SAFE | Integer only — `paste.lineCount + ' lines'` — no user string interpolation |
| `currentTitle` in download filename | ✅ SAFE | Used in `a.download` attribute — browser sanitizes filenames |
| `currentTitle` in toast | ✅ SAFE | `showToast()` uses `textContent` assignment — not innerHTML |
| `currentTitle` in duplicate | ✅ SAFE | Directly set via `titleInput.value = dupTitle` — DOM property, not HTML |
| Share URL | ✅ SAFE | `window.location.origin + '/paste/' + currentPasteId` — ID is server-validated alphanumeric |
| `preview` in sidebar | ⚠️ PRE-EXISTING | `paste.preview` injected via innerHTML. However, this is server-escaped via Go's `html.EscapeString()` in `getPreview()`. **Not a new issue.** |

### `detectLanguage()` — Logic ✅

| Check | Status | Notes |
|-------|--------|-------|
| ReDoS risk | ✅ LOW | All regexes are anchored (`^`) with no nested quantifiers — linear-time matching |
| JSON.parse DoS | ✅ LOW | Only triggered when content starts with `{` or `[` — worst case is parsing 2MB (already the upload limit) |
| userOverrodeLang | ✅ CORRECT | Set `true` on manual dropdown click and on duplicate; reset to `false` on new paste mode |
| Debounce | ✅ CORRECT | 300ms debounce via `setTimeout` with `clearTimeout` — no accumulated handlers |
| Markdown detection | ✅ CORRECT | Requires ≥2 signals — low false positive rate |

### Download Button — Security ✅

- Creates Blob from `currentRawContent` (in-memory string, already fetched via API)
- `URL.revokeObjectURL()` called immediately after click — no dangling blob URLs
- `a.download` attribute sanitized by browser — no path traversal risk

### Duplicate Button — Logic ✅

- Preserves `userOverrodeLang = true` to prevent auto-detect from overriding the duplicated language — correct
- Sets content via DOM `.value` property — no XSS
- Calls `validateSaveBtn()` after setting content — save button correctly enables

### `Cmd+N` Shortcut — ✅

- `e.preventDefault()` called before `goToNewPaste()` — prevents browser new-window
- No conflict with other existing shortcuts (S, K, Escape)

---

## Cosmetic Observations

### `[COSMETIC]` — Line 1062 indentation mismatch

**File**: `templates/index.html:1062-1063`
**Issue**: The closing `</div>` for the `.min-w-0.flex-1` container lost 4 spaces of indentation when the `lineCount` div was inserted. It's `12 spaces` instead of `16 spaces`.
**Impact**: Zero — purely visual in the source. HTML rendering is unaffected.

### `[COSMETIC]` — Download toast doesn't escape filename

**File**: `templates/index.html:676`
**Issue**: `` showToast(`Downloaded ${filename}`) `` passes a filename directly. However, `showToast()` uses `textContent` assignment, so this is safe from XSS. Just noting for completeness.

---

## Test Coverage Assessment

| Contract | Test Count | Status |
|----------|-----------|--------|
| `GET /raw/{id}` — 200 + content match | 2 | ✅ |
| `GET /raw/{id}` — 404 not found | 1 | ✅ |
| `GET /raw/{id}` — 400 invalid ID | 1 | ✅ |
| `GET /raw/{id}` — after delete | 1 | ✅ |
| `GET /raw/{id}` — immediate after create | 1 | ✅ |
| `lineCount` in list | 2 | ✅ |
| `lineCount` in search | 1 | ✅ |
| `lineCount` after create | 1 | ✅ |
| `lineCount` after disk reload | 1 | ✅ |
| **Total** | **11** | **11/11 PASS** |
| Pre-existing tests | 3 | ✅ |
| **Grand Total** | **14** | **14/14 PASS** |

Race detector: CLEAN (no data races detected)

---

## Final Verdict

### ✅ PASS — Clear to ship

No blocking issues. No degraded functionality. No logic drift detected. Security surfaces correctly hardened. Test coverage is comprehensive for the new contracts.
