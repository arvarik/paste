import { elements } from './dom.js';
import { state, langExtMap, SIDEBAR_ACTIVE_CLASSES, SIDEBAR_INACTIVE_CLASSES } from './state.js';
import { copyToClipboard, isMobileViewport } from './utils.js';
import { toggleSidebar, toggleWorkspaceMenu, toggleCmdK, filterCmdK, showToast } from './ui.js';
import { fetchPastes, fetchSinglePaste, savePaste, setPasteMode, formatContent, initCustomSelect, initPasteMetadataControls, resetPasteMetadata } from './paste.js';
import { fetchDiffs, saveDiff, loadDiff, runDiff, setDiffMode, initDiffLineNumbers } from './diff.js';
import { fetchItemExport, importItem, MAX_IMPORT_BYTES } from './transfer.js';
import { saveEditSecret } from './secrets.js';

function updateHistory(path, mode = 'push') {
    if (mode === 'replace') {
        window.history.replaceState({}, '', path);
    } else if (mode === 'push' && window.location.pathname !== path) {
        window.history.pushState({}, '', path);
    }
}

function checkUrlMode({ fromHistory = false } = {}) {
    const path = window.location.pathname;
    const parts = path.split('/').filter(Boolean);

    if (parts[0] === 'paste') {
        if (parts[1] && parts[1] !== 'new') {
            // /paste/:id — load a specific paste
            const id = parts[1];
            if (state.currentPasteId !== id || state.currentApp !== 'paste') {
                state.currentApp = 'paste';
                switchApp('paste');
                fetchSinglePaste(id);
            }
        } else {
            // /paste/new or /paste — new paste mode
            resetToPasteNew({ historyMode: fromHistory ? 'none' : (path === '/' ? 'replace' : 'none') });
        }
    } else if (parts[0] === 'diff') {
        if (parts[1] && parts[1] !== 'new') {
            // /diff/:id — load a specific diff
            const id = parts[1];
            if (state.currentDiffId !== id || state.currentApp !== 'diff') {
                state.currentApp = 'diff';
                switchApp('diff');
                loadDiff(id);
            }
        } else {
            // /diff/new or /diff — new diff mode
            resetToDiffNew({ historyMode: 'none' });
        }
    } else {
        // / — default to paste new
        resetToPasteNew({ historyMode: fromHistory ? 'none' : 'replace' });
    }
}

function resetToPasteNew({ historyMode = 'push' } = {}) {
    clearTimeout(state.autoDetectTimeout);
    state.autoDetectTimeout = null;
    state.currentPasteId = null;
    state.currentRevision = null;
    state.currentTitle = '';
    state.currentRawContent = '';
    state.userOverrodeLang = false;
    elements.titleInput.value = '';
    elements.contentTextarea.value = '';
    resetPasteMetadata();
    updateHistory('/paste/new', historyMode);
    switchApp('paste');
    setPasteMode('new');
    clearSidebarHighlights(elements.pasteList);
    fetchPastes();
}

function resetToDiffNew({ historyMode = 'push' } = {}) {
    state.currentDiffId = null;
    state.currentDiffRevision = null;
    state.currentTitle = '';
    state.currentDiffResult = null;
    state.currentDiffTags = [];
    state.currentDiffFavorite = false;
    state.currentDiffExpiresAt = null;
    state.currentDiffBurnAfterRead = false;
    elements.diffTitleInput.value = '';
    elements.diffBase.value = '';
    elements.diffCompare.value = '';
    elements.diffViewMode.value = 'unified';
    elements.diffIgnoreWhitespace.checked = false;
    updateHistory('/diff/new', historyMode);
    switchApp('diff');
    setDiffMode('new');
    clearSidebarHighlights(elements.diffList);
    fetchDiffs();
}

async function exportItem(kind, id, button) {
    if (!id || button.getAttribute('aria-busy') === 'true') return;
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    try {
        const { blob, filename } = await fetchItemExport(kind, id);
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = filename;
        link.click();
        setTimeout(() => URL.revokeObjectURL(url), 0);
        showToast('Item exported');
    } catch (error) {
        console.error('Export failed:', error);
        showToast(error.message || 'Export failed', true);
    } finally {
        button.disabled = false;
        button.setAttribute('aria-busy', 'false');
    }
}

async function importSelectedItem() {
    const [file] = elements.importItemFile.files;
    if (!file) return;
    if (file.size > MAX_IMPORT_BYTES) {
        elements.importItemFile.value = '';
        showToast('The import file exceeds the 2 MiB limit', true);
        return;
    }
    elements.importItemBtn.disabled = true;
    elements.importItemBtn.setAttribute('aria-busy', 'true');
    try {
        const result = await importItem(await file.text());
        if (result.editSecret) saveEditSecret(result.kind, result.id, result.editSecret);
        const path = result.kind === 'paste' ? `/paste/${result.id}` : `/diff/${result.id}`;
        if (result.kind === 'paste') state.currentPasteId = null;
        else state.currentDiffId = null;
        toggleWorkspaceMenu(true);
        updateHistory(path);
        checkUrlMode();
        if (isMobileViewport()) toggleSidebar(false);
        showToast('Item imported');
    } catch (error) {
        console.error('Import failed:', error);
        showToast(error.message || 'Import failed', true);
    } finally {
        elements.importItemFile.value = '';
        elements.importItemBtn.disabled = false;
        elements.importItemBtn.setAttribute('aria-busy', 'false');
    }
}

function clearSidebarHighlights(container) {
    container.querySelectorAll('.paste-item').forEach(el => {
        el.classList.remove(...SIDEBAR_ACTIVE_CLASSES);
        el.classList.add(...SIDEBAR_INACTIVE_CLASSES);
        el.dataset.current = 'false';
    });
}

function openApp(app) {
    if (app === 'paste') {
        resetToPasteNew();
    } else if (app === 'diff') {
        resetToDiffNew();
    }
}

function switchApp(app) {
    state.currentApp = app;
    elements.workspaceTitle.textContent = app === 'paste' ? 'Paste' : 'Diff';
    elements.pageHeading.textContent = app === 'paste' ? 'Paste workspace' : 'Diff workspace';
    
    document.querySelectorAll('.workspace-check').forEach(c => c.classList.add('hidden'));
    const activeBtn = document.querySelector(`[data-app="${app}"]`);
    if (activeBtn) {
        activeBtn.querySelector('.workspace-check').classList.remove('hidden');
    }
    
    toggleWorkspaceMenu(true); // force-close dropdown if open

    if (app === 'paste') {
        elements.appPaste.classList.remove('opacity-0', 'pointer-events-none', 'z-0');
        elements.appPaste.classList.add('z-10');
        elements.appDiff.classList.add('opacity-0', 'pointer-events-none', 'z-0');
        elements.appDiff.classList.remove('z-10');
        elements.appPaste.inert = false;
        elements.appPaste.setAttribute('aria-hidden', 'false');
        elements.appDiff.inert = true;
        elements.appDiff.setAttribute('aria-hidden', 'true');
        
        elements.pasteList.classList.remove('opacity-0', 'pointer-events-none');
        elements.diffList.classList.add('opacity-0', 'pointer-events-none');
        elements.pasteList.inert = false;
        elements.pasteList.setAttribute('aria-hidden', 'false');
        elements.diffList.inert = true;
        elements.diffList.setAttribute('aria-hidden', 'true');
        
        elements.newItemBtn.querySelector('#new-item-label').textContent = 'New Paste';
    } else {
        elements.appDiff.classList.remove('opacity-0', 'pointer-events-none', 'z-0');
        elements.appDiff.classList.add('z-10');
        elements.appPaste.classList.add('opacity-0', 'pointer-events-none', 'z-0');
        elements.appPaste.classList.remove('z-10');
        elements.appDiff.inert = false;
        elements.appDiff.setAttribute('aria-hidden', 'false');
        elements.appPaste.inert = true;
        elements.appPaste.setAttribute('aria-hidden', 'true');
        
        elements.diffList.classList.remove('opacity-0', 'pointer-events-none');
        elements.pasteList.classList.add('opacity-0', 'pointer-events-none');
        elements.diffList.inert = false;
        elements.diffList.setAttribute('aria-hidden', 'false');
        elements.pasteList.inert = true;
        elements.pasteList.setAttribute('aria-hidden', 'true');
        
        elements.newItemBtn.querySelector('#new-item-label').textContent = 'New Diff';
    }
}

function executeCmd(cmdStr) {
    toggleCmdK(false);
    
    const [domain, action] = cmdStr.split(':');
    
    if (domain === 'app') {
        if (state.currentApp !== action) openApp(action);
    } else if (domain === 'action') {
        if (action === 'new') {
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new' }));
        } else if (action === 'format') {
            if (state.currentApp === 'paste') formatContent();
        }
    }
}

function setupEventListeners() {
    // Top-level DOM Events
    elements.toggleSidebarBtn.addEventListener('click', () => toggleSidebar(false));
    document.querySelectorAll('.show-sidebar-btn').forEach(btn => {
        btn.addEventListener('click', () => toggleSidebar(true));
    });
    document.getElementById('mobile-diff-menu-btn')?.addEventListener('click', () => toggleSidebar(true));
    elements.mobileMenuBtn.addEventListener('click', () => toggleSidebar(true));
    elements.mobileScrim.addEventListener('click', () => toggleSidebar(false));
    elements.sidebar.addEventListener('keydown', (event) => {
        if (!isMobileViewport() || !state.isSidebarOpen || event.key !== 'Tab') return;
        const focusable = Array.from(elements.sidebar.querySelectorAll('a[href], button:not([disabled]), input:not([disabled])'))
            .filter((element) => !element.inert && element.getAttribute('aria-hidden') !== 'true' && element.offsetParent !== null);
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
        }
    });

    elements.workspaceDropdownBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleWorkspaceMenu();
    });

    elements.mobilePasteActionsBtn.addEventListener('click', (event) => {
        event.stopPropagation();
        const open = elements.pasteMoreActions.classList.toggle('hidden') === false;
        elements.mobilePasteActionsBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
    });

    // Close workspace dropdown when clicking outside
    document.addEventListener('click', (e) => {
        if (!elements.workspaceMenu.classList.contains('opacity-0') && !elements.workspaceMenu.contains(e.target) && !elements.workspaceDropdownBtn.contains(e.target)) {
            toggleWorkspaceMenu(true);
        }
        if (!elements.pasteMoreActions.classList.contains('hidden') && !elements.pasteMoreActions.contains(e.target) && !elements.mobilePasteActionsBtn.contains(e.target)) {
            elements.pasteMoreActions.classList.add('hidden');
            elements.mobilePasteActionsBtn.setAttribute('aria-expanded', 'false');
        }
    });
    
    document.querySelectorAll('[data-action="switchApp"]').forEach(btn => {
        btn.addEventListener('click', () => {
            openApp(btn.getAttribute('data-app'));
            if (isMobileViewport()) toggleSidebar(false);
        });
    });

    elements.cmdkBackdrop.addEventListener('click', () => toggleCmdK(false));
    elements.cmdkCloseBtn.addEventListener('click', () => toggleCmdK(false));
    document.querySelectorAll('[data-cmd]').forEach(btn => {
        btn.addEventListener('click', () => executeCmd(btn.getAttribute('data-cmd')));
    });

    // Theme logic
    const storedTheme = localStorage.getItem('theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    document.documentElement.classList.toggle('dark', storedTheme ? storedTheme === 'dark' : prefersDark);
    elements.themeToggleBtn.setAttribute('aria-pressed', document.documentElement.classList.contains('dark') ? 'true' : 'false');
    elements.themeToggleBtn.addEventListener('click', () => {
        if (document.documentElement.classList.contains('dark')) {
            document.documentElement.classList.remove('dark');
            localStorage.theme = 'light';
        } else {
            document.documentElement.classList.add('dark');
            localStorage.theme = 'dark';
        }
        elements.themeToggleBtn.setAttribute('aria-pressed', document.documentElement.classList.contains('dark') ? 'true' : 'false');
    });

    // Sidebar New Item Button
    elements.newItemBtn.addEventListener('click', () => {
        toggleWorkspaceMenu(true); // close workspace dropdown
        if (state.currentApp === 'paste') {
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new' }));
        } else {
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new-diff' }));
        }
        if (isMobileViewport()) toggleSidebar(false);
    });
    elements.importItemBtn.addEventListener('click', () => {
        elements.importItemFile.value = '';
        elements.importItemFile.click();
    });
    elements.importItemFile.addEventListener('change', importSelectedItem);

    // Search input
    elements.searchInput.addEventListener('input', () => {
        clearTimeout(state.searchTimeout);
        state.searchTimeout = setTimeout(() => {
            if (state.currentApp === 'paste') fetchPastes();
            else fetchDiffs();
        }, 300);
    });

    // Paste Action buttons
    elements.editBtn.addEventListener('click', () => setPasteMode('edit'));
    elements.copyBtn.addEventListener('click', async () => {
        try {
            await copyToClipboard(state.currentRawContent);
            showToast('Copied to clipboard');
        } catch (e) {
            console.error('Copy failed:', e);
            showToast('Failed to copy', true);
        }
    });
    elements.shareBtn.addEventListener('click', async () => {
        try {
            await copyToClipboard(window.location.href);
            showToast('Link copied to clipboard');
        } catch (e) {
            console.error('Share failed:', e);
        }
    });
    elements.downloadBtn.addEventListener('click', () => {
        const blob = new Blob([state.currentRawContent], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        const ext = langExtMap[state.currentLang] || '.txt';
        const safeTitle = (state.currentTitle || state.currentPasteId || 'paste')
            .replace(/[\\/:*?"<>|\u0000-\u001f]/g, '-')
            .replace(/\s+/g, ' ')
            .trim() || 'paste';
        a.download = `${safeTitle}${ext}`;
        a.click();
        setTimeout(() => URL.revokeObjectURL(url), 0);
    });
    elements.duplicateBtn.addEventListener('click', () => {
        elements.titleInput.value = (state.currentTitle || 'Paste') + ' (Copy)';
        resetPasteMetadata();
        setPasteMode('new');
        state.currentPasteId = null;
        state.currentRevision = null;
        window.history.pushState({}, '', '/paste/new');
        showToast('Duplicated as new paste');
    });
    elements.exportPasteBtn.addEventListener('click', () => {
        exportItem('paste', state.currentPasteId, elements.exportPasteBtn);
    });

    elements.formatBtn.addEventListener('click', formatContent);
    elements.saveBtn.addEventListener('click', savePaste);

    // Diff Action buttons
    elements.runDiffBtn.addEventListener('click', runDiff);
    elements.exportDiffBtn.addEventListener('click', () => {
        exportItem('diff', state.currentDiffId, elements.exportDiffBtn);
    });
    elements.saveDiffBtn.addEventListener('click', () => {
        if (!state.currentDiffResult) {
            showToast('Please run compare first', true);
            return;
        }
        const { baseResolvedId, compareResolvedId, baseContent, compareContent } = state.currentDiffResult;
        saveDiff(baseResolvedId, compareResolvedId, baseContent, compareContent);
    });

    // Keyboard Shortcuts
    document.addEventListener('keydown', (e) => {
        // Save
        if ((e.metaKey || e.ctrlKey) && e.key === 's') {
            e.preventDefault();
            if (state.currentApp === 'paste' && (state.currentMode === 'new' || state.currentMode === 'edit')) {
                savePaste();
            } else if (state.currentApp === 'diff' && state.currentDiffMode === 'new' && state.currentDiffResult) {
                const r = state.currentDiffResult;
                saveDiff(r.baseResolvedId, r.compareResolvedId, r.baseContent, r.compareContent);
            }
        }
        // New
        if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
            e.preventDefault();
            window.dispatchEvent(new CustomEvent('app:action', { detail: state.currentApp === 'paste' ? 'new' : 'new-diff' }));
        }
        // Format
        if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'f') {
            e.preventDefault();
            if (state.currentApp === 'paste' && (state.currentMode === 'new' || state.currentMode === 'edit')) {
                formatContent();
            }
        }
        // Cmd+K Palette
        if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
            e.preventDefault();
            toggleCmdK(elements.cmdkPalette.getAttribute('aria-hidden') === 'true');
        }
        // Close CmdK / Sidebar
        if (e.key === 'Escape') {
            toggleCmdK(false);
            toggleWorkspaceMenu(true);
            elements.pasteMoreActions.classList.add('hidden');
            elements.mobilePasteActionsBtn.setAttribute('aria-expanded', 'false');
            if (isMobileViewport() && state.isSidebarOpen) {
                toggleSidebar(false);
            }
        }
    });

    elements.searchInput.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            elements.searchInput.blur();
        }
    });

    elements.cmdkInput.addEventListener('input', filterCmdK);
    elements.cmdkPalette.addEventListener('keydown', (event) => {
        const items = Array.from(elements.cmdkPalette.querySelectorAll('.cmdk-item'))
            .filter((item) => item.style.display !== 'none');
        if (!items.length) return;

        const currentIndex = items.indexOf(document.activeElement);
        if (event.key === 'Tab') {
            const focusable = [elements.cmdkInput, ...items];
            const focusIndex = focusable.indexOf(document.activeElement);
            if (event.shiftKey && focusIndex <= 0) {
                event.preventDefault();
                focusable[focusable.length - 1].focus();
            } else if (!event.shiftKey && focusIndex === focusable.length - 1) {
                event.preventDefault();
                focusable[0].focus();
            }
        } else if (event.key === 'ArrowDown') {
            event.preventDefault();
            items[currentIndex < 0 ? 0 : (currentIndex + 1) % items.length].focus();
        } else if (event.key === 'ArrowUp') {
            event.preventDefault();
            items[currentIndex < 0 ? items.length - 1 : (currentIndex - 1 + items.length) % items.length].focus();
        } else if (event.key === 'Enter' && document.activeElement === elements.cmdkInput) {
            event.preventDefault();
            items[0].click();
        }
    });

    window.addEventListener('popstate', () => checkUrlMode({ fromHistory: true }));
    
    // Custom Events
    window.addEventListener('app:navigate', () => checkUrlMode());
    window.addEventListener('app:action', (e) => {
        if (e.detail === 'new') {
            resetToPasteNew();
        } else if (e.detail === 'new-diff') {
            resetToDiffNew();
        }
    });
}

function init() {
    setupEventListeners();
    initCustomSelect();
    initPasteMetadataControls();
    initDiffLineNumbers();
    checkUrlMode();

    if (isMobileViewport()) {
        toggleSidebar(false);
    } else {
        toggleSidebar(true);
    }

    const mobileQuery = window.matchMedia('(max-width: 639px)');
    mobileQuery.addEventListener('change', (event) => {
        toggleSidebar(!event.matches);
    });
}

document.addEventListener('DOMContentLoaded', init);
