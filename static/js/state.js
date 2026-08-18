export const state = {
    currentApp: 'paste', // 'paste' or 'diff'
    isSidebarOpen: true,
    currentMode: 'new', // 'new' or 'view'
    currentDiffMode: 'new', // 'new' or 'view'
    currentPasteId: null,
    currentDiffId: null,
    currentRevision: null,
    currentDiffRevision: null,
    currentRawContent: '',
    currentTitle: '',
    currentLang: '',
    currentTags: [],
    currentFavorite: false,
    currentExpiresAt: null,
    currentBurnAfterRead: false,
    currentDiffTags: [],
    currentDiffFavorite: false,
    currentDiffExpiresAt: null,
    currentDiffBurnAfterRead: false,
    currentDiffResult: null,
    searchTimeout: null,
    userOverrodeLang: false,
    autoDetectTimeout: null
};

export const langExtMap = {
    'text': '.txt', 'python': '.py', 'go': '.go', 'typescript': '.ts',
    'kotlin': '.kt', 'java': '.java', 'scala': '.scala', 'json': '.json',
    'bash': '.sh', 'markdown': '.md', 'html': '.html', 'css': '.css'
};

// Sidebar item highlight classes — single source of truth
export const SIDEBAR_ACTIVE_CLASSES = ['bg-gray-200', 'dark:bg-dark-700', 'border-primary-500/50', 'text-gray-900', 'dark:text-white'];
export const SIDEBAR_INACTIVE_CLASSES = ['border-transparent', 'text-gray-500', 'dark:text-gray-400'];
