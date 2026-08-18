import { elements } from './dom.js';
import { state } from './state.js';
import { escapeHtml, detectLanguage, getLangColorClasses, generateTitle, syncLineNumbers } from './utils.js';
import { showToast, showDeleteConfirm, renderSidebarList } from './ui.js';
import { buildCursorURL, createCursorState, normalizeCursorPage } from './pagination.js';
import { normalizePasteMetadata, parsePasteMetadata, serializePasteMetadata } from './metadata.js';
import { sanitizeMarkdown } from './sanitize.js';
import { getEditSecret, removeEditSecret, saveEditSecret, withEditSecret } from './secrets.js';
import { createRevisionRow } from './revisions.js';
import { DOMPurify, marked, Prism } from './vendor.js';

let pasteListController = null;
let pasteDetailController = null;
let pasteRevisionsController = null;
const pastePages = createCursorState();
let pastePageItems = [];
let pastePageQuery = '';

function toDateTimeLocal(value) {
    if (!value) return '';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '';
    const offset = date.getTimezoneOffset() * 60_000;
    return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function readPasteMetadata() {
    return normalizePasteMetadata({
        tags: elements.pasteTags.value,
        favorite: elements.pasteFavorite.checked,
        expiresAt: elements.pasteExpiresAt.value || null,
        burnAfterRead: elements.pasteBurnAfterRead.checked
    });
}

export function resetPasteMetadata() {
    state.currentTags = [];
    state.currentFavorite = false;
    state.currentExpiresAt = null;
    state.currentBurnAfterRead = false;
    elements.pasteTags.value = '';
    elements.pasteFavorite.checked = false;
    elements.pasteExpiresAt.value = '';
    elements.pasteBurnAfterRead.checked = false;
    elements.pasteOptions.open = false;
    elements.pasteRevisions.hidden = true;
    elements.pasteRevisions.open = false;
    elements.pasteRevisionList.replaceChildren();
}

function applyPasteMetadata(data) {
    const metadata = normalizePasteMetadata(data);
    state.currentTags = metadata.tags;
    state.currentFavorite = metadata.favorite;
    state.currentExpiresAt = metadata.expiresAt;
    state.currentBurnAfterRead = metadata.burnAfterRead;
    elements.pasteTags.value = state.currentTags.join(', ');
    elements.pasteFavorite.checked = state.currentFavorite;
    elements.pasteExpiresAt.value = toDateTimeLocal(state.currentExpiresAt);
    elements.pasteBurnAfterRead.checked = state.currentBurnAfterRead;
}

async function fetchPasteRevisions(id) {
    pasteRevisionsController?.abort();
    pasteRevisionsController = new AbortController();
    elements.pasteRevisions.hidden = true;
    elements.pasteRevisionList.replaceChildren();

    try {
        const response = await fetch(`/api/pastes/${id}/revisions`, {
            signal: pasteRevisionsController.signal
        });
        if (!response.ok) return;
        const payload = await response.json();
        if (window.location.pathname !== `/paste/${id}`) return;
        const revisions = Array.isArray(payload)
            ? payload
            : payload.items ?? payload.revisions ?? [];
        if (!Array.isArray(revisions) || !revisions.length) return;

        const fragment = document.createDocumentFragment();
        for (const revision of revisions) {
            const revisionNumber = Number(revision.revision ?? revision.version);
            if (!Number.isSafeInteger(revisionNumber) || revisionNumber < 1) continue;
            fragment.append(createRevisionRow({
                kind: 'paste',
                id,
                revision: revisionNumber,
                createdAt: revision.createdAt,
                currentRevision: () => state.currentRevision,
                canRestore: () => Boolean(getEditSecret('paste', id)),
                restoreHeaders: () => withEditSecret({}, 'paste', id),
                onRestored: async (result) => {
                    state.currentRevision = result.revision;
                    await fetchSinglePaste(id);
                },
                onMessage: showToast,
                signal: pasteRevisionsController.signal
            }));
        }
        elements.pasteRevisionList.replaceChildren(fragment);
        elements.pasteRevisions.hidden = false;
    } catch (error) {
        if (error.name !== 'AbortError') console.error('Failed to load revisions:', error);
    }
}

export function initPasteMetadataControls() {
    elements.pasteMetadataExport.addEventListener('click', () => {
        try {
            const source = serializePasteMetadata(readPasteMetadata());
            const blob = new Blob([source], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const link = document.createElement('a');
            link.href = url;
            link.download = `${state.currentPasteId || 'paste'}-options.json`;
            link.click();
            setTimeout(() => URL.revokeObjectURL(url), 0);
        } catch (error) {
            showToast(error.message || 'Failed to export options', true);
        }
    });

    elements.pasteMetadataImport.addEventListener('click', () => {
        elements.pasteMetadataFile.value = '';
        elements.pasteMetadataFile.click();
    });
    elements.pasteMetadataFile.addEventListener('change', async () => {
        const [file] = elements.pasteMetadataFile.files;
        if (!file) return;
        try {
            applyPasteMetadata(parsePasteMetadata(await file.text()));
            elements.pasteOptions.open = true;
            showToast('Options imported');
        } catch (error) {
            showToast(error.message || 'Invalid options file', true);
        }
    });
}

export function getPasteListConfig() {
    return {
        container: elements.pasteList,
        itemsKey: 'pastes',
        routePrefix: '/paste/',
        currentId: state.currentPasteId,
        emptyIcon: 'M9 13h6m-3-3v6m5 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
        emptyLabel: 'No pastes found',
        badgeForItem: (p) => {
            const colors = getLangColorClasses(p.language);
            return { ...colors, label: p.language || 'text' };
        },
        previewForItem: (p) => p.preview,
        metaForItem: (p) => {
            const lines = p.lineCount || 0;
            return `${lines} lines • ${new Date(p.createdAt).toLocaleDateString()}`;
        },
        searchQuery: elements.searchInput.value.toLowerCase(),
        nextCursor: pastePages.cursor,
        onLoadMore: () => fetchPastes({ append: true }),
        onDelete: deletePaste
    };
}

export async function fetchPastes({ append = false } = {}) {
    pasteListController?.abort();
    pasteListController = new AbortController();

    try {
        const query = elements.searchInput.value.toLowerCase();
        if (!append || query !== pastePageQuery) {
            pastePages.reset();
            pastePageItems = [];
        }
        pastePageQuery = query;
        const endpoint = buildCursorURL(query ? '/api/search' : '/api/pastes', {
            cursor: append ? pastePages.cursor : '',
            limit: pastePages.limit,
            query
        });
        const res = await fetch(endpoint, { signal: pasteListController.signal });
        if (!res.ok) throw new Error(`Failed to load pastes (${res.status})`);
        const data = await res.json();
        const page = normalizeCursorPage(data);
        pastePages.update(page);
        if (page.isLegacy) {
            renderSidebarList(page.items, !query, getPasteListConfig());
        } else {
            const known = new Set(pastePageItems.map((item) => item.id));
            pastePageItems = append
                ? [...pastePageItems, ...page.items.filter((item) => !known.has(item.id))]
                : page.items;
            renderSidebarList(pastePageItems, false, getPasteListConfig());
        }
    } catch (e) {
        if (e.name === 'AbortError') return;
        console.error('Failed to fetch pastes:', e);
        showToast('Failed to load pastes', true);
    }
}

export async function fetchSinglePaste(id) {
    pasteDetailController?.abort();
    pasteDetailController = new AbortController();

    try {
        const res = await fetch(`/api/pastes/${id}`, { signal: pasteDetailController.signal });
        if (!res.ok) {
            if (res.status === 404) {
                showToast('Paste not found', true);
                window.dispatchEvent(new CustomEvent('app:action', { detail: 'new' }));
                return;
            }
            throw new Error('Failed to load paste');
        }
        const data = await res.json();
        if (window.location.pathname !== `/paste/${id}`) return;
        state.currentPasteId = id;
        state.currentRevision = Number.isInteger(data.revision) ? data.revision : null;
        state.currentRawContent = data.content;
        state.currentTitle = data.title;
        state.currentLang = data.language || 'text';
        state.userOverrodeLang = true;
        applyPasteMetadata(data);
        
        elements.titleInput.value = data.title;
        setLangDropdown(state.currentLang);
        setPasteMode('view');
        fetchPasteRevisions(id);
        fetchPastes();
    } catch (e) {
        if (e.name === 'AbortError') return;
        console.error('Failed to load paste:', e);
        showToast('Failed to load paste', true);
    }
}

export async function savePaste() {
    if (elements.saveBtn.getAttribute('aria-busy') === 'true') return;

    const title = elements.titleInput.value.trim() || generateTitle('paste');
    const content = elements.contentTextarea.value;
    
    if (!content.trim()) {
        showToast('Paste content cannot be empty', true);
        return;
    }

    elements.saveBtn.disabled = true;
    elements.saveBtn.setAttribute('aria-busy', 'true');
    elements.saveBtn.innerHTML = `
        <svg class="animate-spin h-5 w-5 text-white" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        Saving...`;

    try {
        const method = state.currentMode === 'new' ? 'POST' : 'PUT';
        const url = state.currentMode === 'new' ? '/api/pastes' : `/api/pastes/${state.currentPasteId}`;
        
        const metadata = readPasteMetadata();
        const headers = state.currentMode === 'new'
            ? new Headers({ 'Content-Type': 'application/json' })
            : withEditSecret({ 'Content-Type': 'application/json' }, 'paste', state.currentPasteId);
        const payload = {
            title,
            content,
            language: state.currentLang,
            ...metadata
        };
        if (method === 'PUT') payload.revision = state.currentRevision;
        const res = await fetch(url, {
            method: method,
            headers,
            body: JSON.stringify(payload)
        });

        if (res.status === 409) throw new Error('This paste changed elsewhere. Reload it before you save.');
        if (!res.ok) throw new Error('Failed to save');
        const data = await res.json();
        
        state.currentPasteId = data.id;
        state.currentTitle = data.title;
        state.currentRevision = Number.isInteger(data.revision)
            ? data.revision
            : state.currentRevision;
        state.currentRawContent = content;
        applyPasteMetadata(data);

        if (method === 'POST' && data.editSecret) {
            saveEditSecret('paste', data.id, data.editSecret);
        }
        
        elements.titleInput.value = data.title;
        
        showToast('Saved successfully!');
        const pastePath = `/paste/${data.id}`;
        if (window.location.pathname !== pastePath) window.history.pushState({}, '', pastePath);
        setPasteMode('view');
        fetchPasteRevisions(data.id);
        fetchPastes();
    } catch (e) {
        console.error('Save failed:', e);
        showToast(e.message || 'Failed to save paste', true);
    } finally {
        elements.saveBtn.setAttribute('aria-busy', 'false');
        elements.saveBtn.disabled = false;
        elements.saveBtn.innerHTML = `
            <span class="relative z-10 flex items-center gap-1.5 sm:gap-2">
                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7H5a2 2 0 00-2 2v9a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-3m-1 4l-3 3m0 0l-3-3m3 3V4"></path></svg>
                <span class="text-sm font-medium">Save</span>
            </span>
            <div class="absolute inset-0 bg-white/20 translate-y-full group-hover:translate-y-0 transition-transform duration-300 ease-out"></div>
        `;
    }
}

export async function deletePaste(id) {
    const confirmed = await showDeleteConfirm("This action cannot be undone. The paste will be permanently removed.");
    if (!confirmed) return;

    try {
        const res = await fetch(`/api/pastes/${id}`, {
            method: 'DELETE',
            headers: withEditSecret({}, 'paste', id)
        });
        if (!res.ok) throw new Error('Failed to delete');
        removeEditSecret('paste', id);
        
        showToast('Paste deleted');
        if (state.currentPasteId === id) {
            state.currentPasteId = null;
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new' }));
        }
        // Always refresh sidebar to remove the deleted item immediately
        await fetchPastes();
    } catch (e) {
        console.error('Delete failed:', e);
        showToast('Failed to delete paste', true);
    }
}

export async function formatContent() {
    if (elements.formatBtn.getAttribute('aria-busy') === 'true') return;
    if (state.currentMode !== 'new' && state.currentMode !== 'edit') return;
    
    const content = elements.contentTextarea.value;
    if (!content.trim()) return;

    if (!['go', 'json'].includes(state.currentLang)) {
        showToast(`Auto-format not supported for ${state.currentLang}`);
        return;
    }

    const icon = elements.formatBtn.querySelector('svg');
    icon.classList.add('animate-spin');
    elements.formatBtn.disabled = true;
    elements.formatBtn.setAttribute('aria-busy', 'true');

    try {
        const res = await fetch('/api/format', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                content: content,
                language: state.currentLang
            })
        });

        if (!res.ok) {
            let errMsg = 'Formatting failed';
            try {
                const data = await res.json();
                errMsg = data.error || errMsg;
            } catch (_) { /* non-JSON error response */ }
            throw new Error(errMsg);
        }

        const data = await res.json();
        if (data.formatted === content) {
            showToast('Already formatted');
        } else {
            elements.contentTextarea.value = data.formatted;
            // Update line numbers after formatting changes the content
            if (elements.lineNumbers) {
                elements.contentTextarea.dispatchEvent(new Event('input'));
            }
            showToast('Code formatted');
        }
    } catch (e) {
        console.error('Format failed:', e);
        showToast(e.message, true);
    } finally {
        icon.classList.remove('animate-spin');
        elements.formatBtn.disabled = false;
        elements.formatBtn.setAttribute('aria-busy', 'false');
    }
}

export function setPasteMode(mode) {
    state.currentMode = mode;
    const isEditing = mode === 'new' || mode === 'edit';
    elements.editorWrapper.style.display = isEditing ? 'flex' : 'none';
    elements.viewContainer.style.display = mode === 'view' ? 'block' : 'none';
    
    elements.customLangDropdown.style.display = isEditing ? 'block' : 'none';
    elements.langBadge.style.display = mode === 'view' ? '' : 'none';
    
    elements.titleInput.readOnly = mode === 'view';
    elements.saveBtn.style.display = isEditing ? 'flex' : 'none';
    elements.saveBtn.disabled = !isEditing;
    
    elements.statusBadge.style.display = mode === 'view' ? '' : 'none';
    elements.editBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.copyBtn.style.display = mode === 'view' ? 'flex' : 'none';
    elements.shareBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.downloadBtn.style.display = mode === 'view' ? 'flex' : 'none';
    elements.duplicateBtn.style.display = mode === 'view' ? 'flex' : 'none';
    elements.exportPasteBtn.style.display = mode === 'view' ? 'flex' : 'none';
    elements.mobilePasteActionsBtn.classList.toggle('hidden', mode !== 'view');
    elements.mobilePasteActionsBtn.setAttribute('aria-expanded', 'false');
    elements.pasteMoreActions.classList.add('hidden');
    elements.pasteMoreActions.style.display = mode === 'view' ? '' : 'none';
    elements.viewActionsDivider.style.display = mode === 'view' ? '' : 'none';
    
    elements.formatBtn.style.display = isEditing ? 'block' : 'none';
    elements.pasteOptions.hidden = !isEditing;
    elements.pasteRevisions.hidden = mode !== 'view' || !elements.pasteRevisionList.childElementCount;
    
    if (mode === 'view') {
        renderViewerContext();
    } else {
        if (mode === 'edit') {
            elements.contentTextarea.value = state.currentRawContent;
        }
        syncEditorLineNumbers();
        elements.contentTextarea.focus();
        if (elements.contentTextarea.value && !state.userOverrodeLang) {
            const detected = detectLanguage(elements.contentTextarea.value);
            if (detected && detected !== state.currentLang) {
                setLangDropdown(detected);
            }
        }
    }
    
    updateFormatBtnVisibility();
}

function syncEditorLineNumbers() {
    syncLineNumbers(elements.contentTextarea, elements.lineNumbers);
}

function renderViewerContext() {
    const raw = state.currentRawContent || '';
    if (state.currentLang === 'markdown') {
        elements.markdownViewer.style.display = 'block';
        elements.codeViewerPre.style.display = 'none';
        try {
            elements.markdownViewer.innerHTML = sanitizeMarkdown(raw, {
                parse: marked.parse,
                sanitize: DOMPurify.sanitize
            });

            elements.markdownViewer.querySelectorAll('a[href]').forEach((link) => {
                link.setAttribute('rel', 'noopener noreferrer');
            });
            elements.markdownViewer.querySelectorAll('pre code').forEach((block) => {
                const langClass = Array.from(block.classList).find(c => c.startsWith('language-'));
                if (langClass) {
                    const lang = langClass.replace('language-', '');
                    if (Prism.languages[lang]) {
                        block.innerHTML = Prism.highlight(block.textContent, Prism.languages[lang], lang);
                    }
                }
            });
        } catch (error) {
            console.error('Markdown preview failed:', error);
            elements.markdownViewer.textContent = raw;
            showToast('Markdown preview is unavailable', true);
        }
    } else {
        elements.markdownViewer.style.display = 'none';
        elements.codeViewerPre.style.display = 'block';

        const showLineNums = !['text', 'markdown'].includes(state.currentLang);

        // Base classes — Prism sets font-size, line-height, font-family via language class
        elements.codeViewerPre.className = `result-surface language-${state.currentLang} rounded-xl !m-0 !bg-[#1d1f21] border border-gray-800`;
        elements.codeViewer.className = `language-${state.currentLang}`;

        // Reset inline styles from any previous render
        elements.codeViewerPre.style.cssText = '';
        elements.codeViewer.style.cssText = '';

        const grammar = Prism.languages[state.currentLang] || Prism.languages.markup;
        elements.codeViewer.innerHTML = Prism.highlight(raw, grammar, state.currentLang);

        if (showLineNums) {
            renderViewerLineNumbers(raw);
        } else {
            removeViewerLineNumbers();
            elements.codeViewerPre.classList.add('no-line-nums');
        }
    }
}

// Creates a line number gutter that inherits font metrics from the Prism-styled <pre>.
// All styling is handled by CSS classes in head.html — no inline styles needed.
function renderViewerLineNumbers(content) {
    const lineCount = content.split('\n').length;

    // Toggle CSS class — all layout/font rules live in the stylesheet
    elements.codeViewerPre.classList.add('has-line-nums');
    elements.codeViewerPre.classList.remove('no-line-nums');

    // Create or reuse gutter element
    let gutter = elements.codeViewerPre.querySelector('.viewer-gutter');
    if (!gutter) {
        gutter = document.createElement('div');
        gutter.className = 'viewer-gutter';
        gutter.setAttribute('aria-hidden', 'true');
        elements.codeViewerPre.insertBefore(gutter, elements.codeViewer);
    }

    // Build line numbers — one number per line, separated by <br>
    const nums = [];
    for (let i = 1; i <= lineCount; i++) nums.push(i);
    gutter.innerHTML = nums.join('<br>');
}

// Removes the gutter element and associated classes.
function removeViewerLineNumbers() {
    const gutter = elements.codeViewerPre.querySelector('.viewer-gutter');
    if (gutter) gutter.remove();
    elements.codeViewerPre.classList.remove('has-line-nums');
}

export function updateFormatBtnVisibility() {
    const isEditing = state.currentMode === 'new' || state.currentMode === 'edit';
    const isSupported = ['go', 'json'].includes(state.currentLang);
    
    if (isEditing && isSupported) {
        elements.formatBtn.style.display = 'block';
        elements.formatBtn.style.opacity = '1';
        elements.formatBtn.style.transform = 'scale(1)';
        elements.formatBtn.style.pointerEvents = 'auto';
        elements.formatBtn.disabled = false;
        elements.formatBtn.removeAttribute('aria-hidden');
    } else {
        elements.formatBtn.style.display = 'none';
        elements.formatBtn.style.opacity = '0';
        elements.formatBtn.style.transform = 'scale(0.9)';
        elements.formatBtn.style.pointerEvents = 'none';
        elements.formatBtn.disabled = true;
        elements.formatBtn.setAttribute('aria-hidden', 'true');
    }
}

export function initCustomSelect() {
    // Sync UI with initial hidden select value
    const initialVal = elements.langSelect.value || 'text';
    state.currentLang = initialVal;
    
    const options = Array.from(elements.langSelect.options);
    
    elements.langDropdownMenu.innerHTML = options.map(opt => `
        <div role="option" tabindex="-1" aria-selected="false" class="lang-option group flex items-center gap-2 px-3 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-dark-600 cursor-pointer transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500" data-value="${opt.value}">
            <div class="w-2 h-2 rounded-full ${getLangColorClasses(opt.value).dotClass} shadow-sm opacity-70 group-hover:opacity-100 transition-opacity"></div>
            ${opt.text}
        </div>
    `).join('');

    setLangDropdown(initialVal);

    const menuItems = Array.from(elements.langDropdownMenu.querySelectorAll('.lang-option'));
    const setMenuOpen = (open, focusFirst = false) => {
        elements.langDropdownMenu.classList.toggle('hidden', !open);
        elements.langDropdownMenu.inert = !open;
        elements.langDropdownMenu.setAttribute('aria-hidden', open ? 'false' : 'true');
        elements.langDropdownBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
        if (open && focusFirst) {
            const selected = menuItems.find((item) => item.getAttribute('aria-selected') === 'true');
            (selected || menuItems[0])?.focus();
        }
    };
    
    elements.langDropdownBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        setMenuOpen(elements.langDropdownMenu.classList.contains('hidden'));
    });

    elements.langDropdownBtn.addEventListener('keydown', (event) => {
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault();
            setMenuOpen(true, true);
        }
    });

    menuItems.forEach(item => {
        item.addEventListener('click', () => {
            const val = item.getAttribute('data-value');
            setLangDropdown(val);
            state.userOverrodeLang = true;
            setMenuOpen(false);
            elements.langDropdownBtn.focus();
            
            if (state.currentMode === 'view' && state.currentPasteId) {
                state.currentMode = 'edit'; // force re-render logic via update flow
                savePaste(); // Auto save language change
            }
        });
    });

    elements.langDropdownMenu.addEventListener('keydown', (event) => {
        const currentIndex = menuItems.indexOf(document.activeElement);
        if (event.key === 'Escape') {
            event.preventDefault();
            setMenuOpen(false);
            elements.langDropdownBtn.focus();
        } else if (event.key === 'ArrowDown') {
            event.preventDefault();
            menuItems[(currentIndex + 1) % menuItems.length].focus();
        } else if (event.key === 'ArrowUp') {
            event.preventDefault();
            menuItems[(currentIndex - 1 + menuItems.length) % menuItems.length].focus();
        } else if (event.key === 'Home') {
            event.preventDefault();
            menuItems[0].focus();
        } else if (event.key === 'End') {
            event.preventDefault();
            menuItems[menuItems.length - 1].focus();
        } else if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            document.activeElement.click();
        }
    });

    document.addEventListener('click', (e) => {
        if (!elements.customLangDropdown.contains(e.target)) {
            setMenuOpen(false);
        }
    });

    elements.contentTextarea.addEventListener('input', () => {
        if (state.userOverrodeLang) return;
        clearTimeout(state.autoDetectTimeout);
        state.autoDetectTimeout = setTimeout(() => {
            const detected = detectLanguage(elements.contentTextarea.value);
            if (detected && detected !== state.currentLang) setLangDropdown(detected);
        }, 200);
    });
}

export function setLangDropdown(val) {
    state.currentLang = val;
    elements.langSelect.value = val;
    const option = Array.from(elements.langSelect.options).find(o => o.value === val);
    if (option) {
        elements.langDropdownSelected.textContent = option.text;
    }

    elements.langDropdownMenu.querySelectorAll('.lang-option').forEach((item) => {
        item.setAttribute('aria-selected', item.dataset.value === val ? 'true' : 'false');
    });
    
    // Update badge in-place instead of cloning
    const colors = getLangColorClasses(val);
    const badge = elements.langBadge;
    badge.className = `hidden px-2 py-1 text-[10px] font-bold uppercase tracking-wider rounded ${colors.bg} ${colors.text} ${colors.border} border cursor-default transition-all duration-200`;
    badge.textContent = option ? option.text : val;
    
    if (state.currentMode === 'view') {
        badge.style.display = 'block';
    }
    
    updateFormatBtnVisibility();
}
