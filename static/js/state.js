export const state = {
    currentApp: 'paste', // 'paste' or 'diff'
    isSidebarOpen: true,
    currentMode: 'new', // 'new' or 'view'
    currentDiffMode: 'new', // 'new' or 'view'
    currentPasteId: null,
    currentRawContent: '',
    currentTitle: '',
    currentLang: '',
    searchTimeout: null,
    userOverrodeLang: false,
    autoDetectTimeout: null
};

export const langExtMap = {
    'text': '.txt', 'python': '.py', 'go': '.go', 'typescript': '.ts',
    'kotlin': '.kt', 'java': '.java', 'scala': '.scala', 'json': '.json',
    'bash': '.sh', 'markdown': '.md', 'html': '.html', 'css': '.css'
};
