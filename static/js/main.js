import { elements } from './dom.js';
import { state, SIDEBAR_ACTIVE_CLASSES, SIDEBAR_INACTIVE_CLASSES } from './state.js';
import { copyToClipboard, isMobileViewport } from './utils.js';
import { toggleSidebar, toggleWorkspaceMenu, toggleCmdK, filterCmdK, showToast } from './ui.js';
import { fetchPastes, fetchSinglePaste, savePaste, setPasteMode, formatContent, initCustomSelect } from './paste.js';
import { fetchDiffs, saveDiff, loadDiff, runDiff, setDiffMode, initDiffLineNumbers } from './diff.js';

function checkUrlMode() {
    const path = window.location.pathname;
    const parts = path.split('/').filter(Boolean);

    if (parts[0] === 'paste') {
        if (parts[1] && parts[1] !== 'new') {
            // /paste/:id — load a specific paste
            const id = parts[1];
            if (state.currentPasteId !== id) {
                state.currentApp = 'paste';
                switchApp('paste');
                fetchSinglePaste(id);
            }
        } else {
            // /paste/new or /paste — new paste mode
            resetToPasteNew();
        }
    } else if (parts[0] === 'diff') {
        if (parts[1] && parts[1] !== 'new') {
            // /diff/:id — load a specific diff
            const id = parts[1];
            if (state.currentPasteId !== id) {
                state.currentApp = 'diff';
                switchApp('diff');
                loadDiff(id);
            }
        } else {
            // /diff/new or /diff — new diff mode
            resetToDiffNew();
        }
    } else {
        // / — default to paste new
        resetToPasteNew();
    }
}

function resetToPasteNew() {
    state.currentPasteId = null;
    state.currentTitle = '';
    state.currentRawContent = '';
    elements.titleInput.value = '';
    elements.contentTextarea.value = '';
    window.history.pushState({}, '', '/paste/new');
    switchApp('paste');
    setPasteMode('new');
    clearSidebarHighlights(elements.pasteList);
    fetchPastes();
}

function resetToDiffNew() {
    state.currentPasteId = null;
    state.currentTitle = '';
    state.currentDiffResult = null;
    elements.diffTitleInput.value = '';
    elements.diffBase.value = '';
    elements.diffCompare.value = '';
    window.history.pushState({}, '', '/diff/new');
    switchApp('diff');
    setDiffMode('new');
    clearSidebarHighlights(elements.diffList);
    fetchDiffs();
}

function clearSidebarHighlights(container) {
    container.querySelectorAll('.paste-item').forEach(el => {
        el.classList.remove(...SIDEBAR_ACTIVE_CLASSES);
        el.classList.add(...SIDEBAR_INACTIVE_CLASSES);
    });
}

function switchApp(app) {
    const previousApp = state.currentApp;
    state.currentApp = app;
    elements.workspaceTitle.textContent = app === 'paste' ? 'Paste' : 'Diff';
    
    document.querySelectorAll('.workspace-check').forEach(c => c.classList.add('hidden'));
    const activeBtn = document.querySelector(`[data-app="${app}"]`);
    if (activeBtn) {
        activeBtn.querySelector('.workspace-check').classList.remove('hidden');
    }
    
    toggleWorkspaceMenu(true); // force-close dropdown if open

    // Update URL when switching between apps to prevent stale routes
    if (previousApp !== app) {
        const path = window.location.pathname;
        const currentPrefix = path.split('/')[1]; // 'paste', 'diff', or ''
        if (currentPrefix !== app) {
            // Reset state for the new app
            state.currentPasteId = null;
            if (app === 'paste') {
                window.history.pushState({}, '', '/paste/new');
            } else {
                window.history.pushState({}, '', '/diff/new');
            }
        }
    }

    if (app === 'paste') {
        elements.appPaste.classList.remove('opacity-0', 'pointer-events-none', 'z-0');
        elements.appPaste.classList.add('z-10');
        elements.appDiff.classList.add('opacity-0', 'pointer-events-none', 'z-0');
        elements.appDiff.classList.remove('z-10');
        
        elements.pasteList.classList.remove('opacity-0', 'pointer-events-none');
        elements.diffList.classList.add('opacity-0', 'pointer-events-none');
        
        elements.newItemBtn.querySelector('#new-item-label').textContent = 'New Paste';
        if (!state.currentPasteId && state.currentMode === 'new') {
            fetchPastes();
        }
    } else {
        elements.appDiff.classList.remove('opacity-0', 'pointer-events-none', 'z-0');
        elements.appDiff.classList.add('z-10');
        elements.appPaste.classList.add('opacity-0', 'pointer-events-none', 'z-0');
        elements.appPaste.classList.remove('z-10');
        
        elements.diffList.classList.remove('opacity-0', 'pointer-events-none');
        elements.pasteList.classList.add('opacity-0', 'pointer-events-none');
        
        elements.newItemBtn.querySelector('#new-item-label').textContent = 'New Diff';
        if (!state.currentPasteId && state.currentDiffMode === 'new') {
            fetchDiffs();
        }
    }
}

function executeCmd(cmdStr) {
    toggleCmdK(false);
    
    const [domain, action] = cmdStr.split(':');
    
    if (domain === 'app') {
        if (state.currentApp !== action) switchApp(action);
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

    elements.workspaceDropdownBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleWorkspaceMenu();
    });

    // Close workspace dropdown when clicking outside
    document.addEventListener('click', (e) => {
        if (!elements.workspaceMenu.classList.contains('opacity-0') && !elements.workspaceMenu.contains(e.target) && !elements.workspaceDropdownBtn.contains(e.target)) {
            toggleWorkspaceMenu(true);
        }
    });
    
    document.querySelectorAll('[data-action="switchApp"]').forEach(btn => {
        btn.addEventListener('click', () => switchApp(btn.getAttribute('data-app')));
    });

    elements.cmdkBackdrop.addEventListener('click', () => toggleCmdK(false));
    document.querySelectorAll('[data-cmd]').forEach(btn => {
        btn.addEventListener('click', () => executeCmd(btn.getAttribute('data-cmd')));
    });

    // Theme logic
    if (localStorage.theme === 'dark' || (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
        document.documentElement.classList.add('dark');
    }
    elements.themeToggleBtn.addEventListener('click', () => {
        if (document.documentElement.classList.contains('dark')) {
            document.documentElement.classList.remove('dark');
            localStorage.theme = 'light';
        } else {
            document.documentElement.classList.add('dark');
            localStorage.theme = 'dark';
        }
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
        const ext = state.currentLang && state.currentLang !== 'text' ? `.${state.currentLang}` : '.txt';
        a.download = `${state.currentTitle || state.currentPasteId || 'paste'}${ext}`;
        a.click();
        URL.revokeObjectURL(url);
    });
    elements.duplicateBtn.addEventListener('click', () => {
        elements.titleInput.value = (state.currentTitle || 'Paste') + ' (Copy)';
        setPasteMode('new');
        state.currentPasteId = null;
        window.history.pushState({}, '', '/paste/new');
        showToast('Duplicated as new paste');
    });

    elements.formatBtn.addEventListener('click', formatContent);
    elements.saveBtn.addEventListener('click', savePaste);

    // Diff Action buttons
    elements.runDiffBtn.addEventListener('click', runDiff);
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
            toggleCmdK(true);
        }
        // Close CmdK / Sidebar
        if (e.key === 'Escape') {
            toggleCmdK(false);
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

    window.addEventListener('popstate', checkUrlMode);
    
    // Custom Events
    window.addEventListener('app:navigate', checkUrlMode);
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
    initDiffLineNumbers();
    checkUrlMode();

    if (!isMobileViewport()) {
        toggleSidebar(true);
    }
}

document.addEventListener('DOMContentLoaded', init);
