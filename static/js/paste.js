import { elements } from './dom.js';
import { state, langExtMap } from './state.js';
import { escapeHtml, detectLanguage, getLangColorClasses, generateTitle, isMobileViewport, syncLineNumbers } from './utils.js';
import { showToast, showDeleteConfirm, renderSidebarList } from './ui.js';

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
        onDelete: deletePaste
    };
}

export async function fetchPastes() {
    try {
        const query = elements.searchInput.value.toLowerCase();
        const endpoint = query ? `/api/search?q=${encodeURIComponent(query)}` : '/api/pastes';
        const res = await fetch(endpoint);
        const data = await res.json();
        renderSidebarList(data, !query, getPasteListConfig());
    } catch (e) {
        console.error('Failed to fetch pastes:', e);
        showToast('Failed to load pastes', true);
    }
}

export async function fetchSinglePaste(id) {
    try {
        const res = await fetch(`/api/pastes/${id}`);
        if (!res.ok) {
            if (res.status === 404) {
                showToast('Paste not found', true);
                window.dispatchEvent(new CustomEvent('app:action', { detail: 'new' }));
                return;
            }
            throw new Error('Failed to load paste');
        }
        const data = await res.json();
        state.currentPasteId = id;
        state.currentRawContent = data.content;
        state.currentTitle = data.title;
        state.currentLang = data.language || 'text';
        state.userOverrodeLang = true;
        
        elements.titleInput.value = data.title;
        setLangDropdown(state.currentLang);
        setPasteMode('view');
        fetchPastes();
    } catch (e) {
        console.error('Failed to load paste:', e);
        showToast('Failed to load paste', true);
    }
}

export async function savePaste() {
    const title = elements.titleInput.value.trim() || generateTitle('paste');
    const content = elements.contentTextarea.value;
    
    if (!content.trim()) {
        showToast('Paste content cannot be empty', true);
        return;
    }

    elements.saveBtn.disabled = true;
    elements.saveBtn.innerHTML = `
        <svg class="animate-spin h-5 w-5 text-white" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        Saving...`;

    try {
        const method = state.currentMode === 'new' ? 'POST' : 'PUT';
        const url = state.currentMode === 'new' ? '/api/pastes' : `/api/pastes/${state.currentPasteId}`;
        
        const res = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                title,
                content,
                language: state.currentLang
            })
        });

        if (!res.ok) throw new Error('Failed to save');
        const data = await res.json();
        
        state.currentPasteId = data.id;
        state.currentTitle = data.title;
        state.currentRawContent = content;
        
        elements.titleInput.value = data.title;
        
        showToast('Saved successfully!');
        window.history.pushState({}, '', `/paste/${data.id}`);
        setPasteMode('view');
        fetchPastes();
    } catch (e) {
        console.error('Save failed:', e);
        showToast('Failed to save paste', true);
    } finally {
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
        const res = await fetch(`/api/pastes/${id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error('Failed to delete');
        
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
    if (state.currentMode !== 'new' && state.currentMode !== 'edit') return;
    
    const content = elements.contentTextarea.value;
    if (!content.trim()) return;

    if (!['go', 'json', 'python'].includes(state.currentLang)) {
        showToast(`Auto-format not supported for ${state.currentLang}`);
        return;
    }

    const icon = elements.formatBtn.querySelector('svg');
    icon.classList.add('animate-spin');

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
    }
}

export function setPasteMode(mode) {
    state.currentMode = mode;
    const isEditing = mode === 'new' || mode === 'edit';
    elements.editorWrapper.style.display = isEditing ? 'flex' : 'none';
    elements.viewContainer.style.display = mode === 'view' ? 'block' : 'none';
    
    elements.customLangDropdown.style.display = isEditing ? 'block' : 'none';
    elements.langBadge.style.display = mode === 'view' ? 'block' : 'none';
    
    elements.titleInput.readOnly = mode === 'view';
    elements.saveBtn.style.display = isEditing ? 'flex' : 'none';
    elements.saveBtn.disabled = !isEditing;
    
    elements.statusBadge.style.display = mode === 'view' ? 'inline-flex' : 'none';
    elements.editBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.copyBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.shareBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.downloadBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.duplicateBtn.style.display = mode === 'view' ? 'block' : 'none';
    elements.viewActionsDivider.style.display = mode === 'view' ? 'block' : 'none';
    
    elements.formatBtn.style.display = isEditing ? 'block' : 'none';
    
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
        if (window.marked) {
            elements.markdownViewer.innerHTML = window.marked.parse(raw);
            elements.markdownViewer.querySelectorAll('pre code').forEach((block) => {
                const langClass = Array.from(block.classList).find(c => c.startsWith('language-'));
                if (langClass && window.Prism) {
                    const lang = langClass.replace('language-', '');
                    if (Prism.languages[lang]) {
                        block.innerHTML = Prism.highlight(block.textContent, Prism.languages[lang], lang);
                    }
                }
            });
        }
    } else {
        elements.markdownViewer.style.display = 'none';
        elements.codeViewerPre.style.display = 'block';

        const showLineNums = !['text', 'markdown'].includes(state.currentLang);

        // Base classes — Prism sets font-size, line-height, font-family via language class
        elements.codeViewerPre.className = `language-${state.currentLang} rounded-xl shadow-lg !m-0 !bg-[#1d1f21] border border-gray-800`;
        elements.codeViewer.className = `language-${state.currentLang}`;

        // Reset inline styles from any previous render
        elements.codeViewerPre.style.cssText = '';
        elements.codeViewer.style.cssText = '';

        if (window.Prism) {
            const grammar = Prism.languages[state.currentLang] || Prism.languages.markup;
            elements.codeViewer.innerHTML = Prism.highlight(raw, grammar, state.currentLang);
        } else {
            elements.codeViewer.textContent = raw;
        }

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
    const isSupported = ['go', 'json', 'python'].includes(state.currentLang);
    
    if (isEditing && isSupported) {
        elements.formatBtn.style.opacity = '1';
        elements.formatBtn.style.transform = 'scale(1)';
        elements.formatBtn.style.pointerEvents = 'auto';
    } else {
        elements.formatBtn.style.opacity = '0';
        elements.formatBtn.style.transform = 'scale(0.9)';
        elements.formatBtn.style.pointerEvents = 'none';
    }
}

export function initCustomSelect() {
    // Sync UI with initial hidden select value
    const initialVal = elements.langSelect.value || 'text';
    state.currentLang = initialVal;
    
    const options = Array.from(elements.langSelect.options);
    
    elements.langDropdownMenu.innerHTML = options.map(opt => `
        <div class="lang-option group flex items-center gap-2 px-3 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-dark-600 cursor-pointer transition-colors" data-value="${opt.value}">
            <div class="w-2 h-2 rounded-full ${getLangColorClasses(opt.value).dotClass} shadow-sm opacity-70 group-hover:opacity-100 transition-opacity"></div>
            ${opt.text}
        </div>
    `).join('');

    setLangDropdown(initialVal);
    
    elements.langDropdownBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        elements.langDropdownMenu.classList.toggle('hidden');
    });

    elements.langDropdownMenu.querySelectorAll('.lang-option').forEach(item => {
        item.addEventListener('click', () => {
            const val = item.getAttribute('data-value');
            setLangDropdown(val);
            state.userOverrodeLang = true;
            elements.langDropdownMenu.classList.add('hidden');
            
            if (state.currentMode === 'view' && state.currentPasteId) {
                state.currentMode = 'edit'; // force re-render logic via update flow
                savePaste(); // Auto save language change
            }
        });
    });

    document.addEventListener('click', (e) => {
        if (!elements.customLangDropdown.contains(e.target)) {
            elements.langDropdownMenu.classList.add('hidden');
        }
    });
}

export function setLangDropdown(val) {
    state.currentLang = val;
    elements.langSelect.value = val;
    const option = Array.from(elements.langSelect.options).find(o => o.value === val);
    if (option) {
        elements.langDropdownSelected.textContent = option.text;
    }
    
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
