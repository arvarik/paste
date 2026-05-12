export const elements = {
    sidebar: document.getElementById('sidebar'),
    toggleSidebarBtn: document.getElementById('toggle-sidebar'),
    showSidebarBtns: document.querySelectorAll('.show-sidebar-btn'),
    mobileMenuBtn: document.getElementById('mobile-menu-btn'),
    mobileScrim: document.getElementById('mobile-scrim'),
    pasteList: document.getElementById('paste-list'),
    titleInput: document.getElementById('paste-title'),
    contentTextarea: document.getElementById('paste-content'),
    viewContainer: document.getElementById('view-container'),
    markdownViewer: document.getElementById('markdown-viewer'),
    codeViewerPre: document.getElementById('code-viewer-pre'),
    codeViewer: document.getElementById('code-viewer'),

    langDropdownBtn: document.getElementById('lang-dropdown-btn'),
    langDropdownMenu: document.getElementById('lang-dropdown-menu'),
    langDropdownSelected: document.getElementById('lang-dropdown-selected'),

    langSelect: document.getElementById('lang-select'),
    customLangDropdown: document.getElementById('custom-lang-dropdown'),
    langBadge: document.getElementById('lang-badge'),
    formatBtn: document.getElementById('format-btn'),
    copyBtn: document.getElementById('copy-btn'),
    shareBtn: document.getElementById('share-btn'),
    downloadBtn: document.getElementById('download-btn'),
    duplicateBtn: document.getElementById('duplicate-btn'),
    editBtn: document.getElementById('edit-btn'),
    viewActionsDivider: document.getElementById('view-actions-divider'),
    saveBtn: document.getElementById('save-btn'),
    statusBadge: document.getElementById('status-badge'),
    toast: document.getElementById('toast'),
    toastMessage: document.getElementById('toast-message'),
    themeToggleBtn: document.getElementById('theme-toggle'),
    searchInput: document.getElementById('search-input'),
    newItemBtn: document.getElementById('new-item-btn'),

    // Diff Engine Elements
    diffTitleInput: document.getElementById('diff-title'),
    saveDiffBtn: document.getElementById('save-diff-btn'),
    diffList: document.getElementById('diff-list'),
    diffBase: document.getElementById('diff-base'),
    diffCompare: document.getElementById('diff-compare'),
    runDiffBtn: document.getElementById('run-diff-btn'),
    diffOutputContainer: document.getElementById('diff-output-container'),
    diffOutput: document.getElementById('diff-output'),
    diffStatusBadge: document.getElementById('diff-status-badge')
};

export function updateLangBadge(newBadge) {
    elements.langBadge = newBadge;
}
