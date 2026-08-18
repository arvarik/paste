import { elements } from './dom.js';
import { state, SIDEBAR_ACTIVE_CLASSES, SIDEBAR_INACTIVE_CLASSES } from './state.js';
import { escapeHtml, isMobileViewport } from './utils.js';

let _toastTimeout = null;
let _lastPaletteFocus = null;
let _lastSidebarFocus = null;

function setRegionHidden(element, hidden) {
    element.inert = hidden;
    element.setAttribute('aria-hidden', hidden ? 'true' : 'false');
}

function setSidebarRole(modal) {
    elements.sidebar.setAttribute('role', modal ? 'dialog' : 'navigation');
    if (modal) elements.sidebar.setAttribute('aria-modal', 'true');
    else elements.sidebar.removeAttribute('aria-modal');
}

export function showToast(message, isError = false) {
    clearTimeout(_toastTimeout);
    elements.toastMessage.textContent = message;
    const icon = elements.toast.querySelector('svg');
    icon.innerHTML = isError
        ? '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
        : '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>';

    icon.classList.remove('text-green-400', 'text-red-400');
    icon.classList.add(isError ? 'text-red-400' : 'text-green-400');
    elements.toast.setAttribute('role', isError ? 'alert' : 'status');
    elements.toast.setAttribute('aria-live', isError ? 'assertive' : 'polite');

    elements.toast.classList.remove('translate-y-full', 'opacity-0');
    _toastTimeout = setTimeout(() => {
        elements.toast.classList.add('translate-y-full', 'opacity-0');
    }, 3000);
}

export function toggleSidebar(show) {
    state.isSidebarOpen = show;
    setRegionHidden(elements.sidebar, !show);
    elements.toggleSidebarBtn.setAttribute('aria-label', show ? 'Hide sidebar' : 'Show sidebar');
    elements.toggleSidebarBtn.setAttribute('aria-expanded', show ? 'true' : 'false');
    elements.mobileMenuBtn.setAttribute('aria-expanded', show ? 'true' : 'false');
    document.getElementById('mobile-diff-menu-btn')?.setAttribute('aria-expanded', show ? 'true' : 'false');

    const isMobile = isMobileViewport();
    document.body.style.overflow = isMobile && show ? 'hidden' : '';
    const activeApp = state.currentApp === 'diff' ? elements.appDiff : elements.appPaste;

    if (isMobile) {
        if (show) {
            _lastSidebarFocus = document.activeElement;
            setRegionHidden(activeApp, true);
            setSidebarRole(true);
            elements.sidebar.classList.add('mobile-open');
            elements.mobileScrim.classList.add('active');
            setTimeout(() => elements.toggleSidebarBtn.focus(), 0);
        } else {
            setRegionHidden(activeApp, false);
            setSidebarRole(false);
            elements.sidebar.classList.remove('mobile-open');
            elements.mobileScrim.classList.remove('active');
            if (_lastSidebarFocus instanceof HTMLElement && _lastSidebarFocus.isConnected) {
                _lastSidebarFocus.focus();
            }
            _lastSidebarFocus = null;
        }
    } else {
        setRegionHidden(activeApp, false);
        setSidebarRole(false);
        elements.sidebar.classList.remove('mobile-open');
        elements.mobileScrim.classList.remove('active');
        const btns = document.querySelectorAll('.show-sidebar-btn');
        if (show) {
            elements.sidebar.classList.remove('-ml-72', 'sm:-ml-80');
            btns.forEach(b => b.classList.add('hidden'));
        } else {
            elements.sidebar.classList.add('-ml-72', 'sm:-ml-80');
            btns.forEach(b => b.classList.remove('hidden'));
        }
    }
}

export function toggleWorkspaceMenu(forceClose = false) {
    const menu = elements.workspaceMenu;
    const chevron = elements.workspaceChevron;
    const isHidden = menu.classList.contains('opacity-0');
    
    if (!forceClose && isHidden) {
        menu.classList.remove('opacity-0', 'pointer-events-none', '-translate-y-2');
        menu.classList.add('opacity-100', 'translate-y-0');
        chevron.style.transform = 'rotate(180deg)';
        setRegionHidden(menu, false);
        elements.workspaceDropdownBtn.setAttribute('aria-expanded', 'true');
    } else {
        const restoreFocus = menu.contains(document.activeElement);
        menu.classList.add('opacity-0', 'pointer-events-none', '-translate-y-2');
        menu.classList.remove('opacity-100', 'translate-y-0');
        chevron.style.transform = '';
        setRegionHidden(menu, true);
        elements.workspaceDropdownBtn.setAttribute('aria-expanded', 'false');
        if (restoreFocus) elements.workspaceDropdownBtn.focus();
    }
}

export function toggleCmdK(show) {
    const palette = elements.cmdkPalette;
    const modal = elements.cmdkModal;
    const input = elements.cmdkInput;
    
    if (show) {
        _lastPaletteFocus = document.activeElement;
        setRegionHidden(palette, false);
        palette.classList.remove('opacity-0', 'pointer-events-none');
        modal.classList.remove('scale-95');
        modal.classList.add('scale-100');
        input.value = '';
        filterCmdK();
        setTimeout(() => input.focus(), 100);
    } else {
        palette.classList.add('opacity-0', 'pointer-events-none');
        modal.classList.remove('scale-100');
        modal.classList.add('scale-95');
        input.blur();
        setRegionHidden(palette, true);
        if (_lastPaletteFocus instanceof HTMLElement && _lastPaletteFocus.isConnected) {
            _lastPaletteFocus.focus();
        }
        _lastPaletteFocus = null;
    }
}

export function filterCmdK() {
    const query = elements.cmdkInput.value.toLowerCase();
    const items = document.querySelectorAll('.cmdk-item');
    
    items.forEach(item => {
        const text = item.textContent.toLowerCase();
        item.style.display = text.includes(query) ? 'flex' : 'none';
    });
}

// Search highlight helper
export function highlightMatch(text, query) {
    if (!query) return escapeHtml(text);
    const escaped = escapeHtml(text);
    const escapedQuery = escapeHtml(query);
    const safeQuery = escapedQuery.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const regex = new RegExp('(' + safeQuery + ')', 'gi');
    return escaped.replace(regex, '<mark class="bg-yellow-200 dark:bg-yellow-500/30 text-inherit rounded px-0.5">$1</mark>');
}

export function showEmptyState(config) {
    config.container.innerHTML = `
        <div class="flex flex-col items-center justify-center h-32 text-gray-600 dark:text-gray-400">
            <svg class="w-8 h-8 mb-2 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="${config.emptyIcon}"></path></svg>
            <span class="text-sm font-medium">${config.emptyLabel}</span>
        </div>`;
}

export function renderSidebarList(data, isGrouped, config) {
    const buckets = isGrouped
        ? data
        : [{ group: 'Search Results', [config.itemsKey]: data }];

    if (!buckets || buckets.length === 0) {
        showEmptyState(config);
        return;
    }

    // Snapshot which items are already visible so we only animate genuinely new ones
    const existingIds = new Set();
    config.container.querySelectorAll('.paste-item').forEach(el => {
        const id = el.dataset.itemId;
        if (id) existingIds.add(id);
    });

    // Build new content off-DOM to avoid flicker
    const fragment = document.createDocumentFragment();
    let hasItems = false;

    for (const bucket of buckets) {
        const items = bucket[config.itemsKey];
        if (!items || items.length === 0) continue;
        hasItems = true;

        const groupDiv = document.createElement('div');

        const groupTitle = document.createElement('h2');
        groupTitle.className = "text-[11px] font-bold text-gray-600 dark:text-gray-400 uppercase tracking-widest pl-1 mb-3";
        groupTitle.textContent = bucket.group;
        groupDiv.appendChild(groupTitle);

        const list = document.createElement('div');
        list.className = "space-y-1.5";

        items.forEach(item => {
            const isNew = !existingIds.has(item.id);
            list.appendChild(createSidebarItem(item, config, isNew));
        });

        groupDiv.appendChild(list);
        fragment.appendChild(groupDiv);
    }

    if (!hasItems) {
        showEmptyState(config);
        return;
    }

    if (config.nextCursor && config.onLoadMore) {
        const controls = document.createElement('div');
        controls.className = 'pt-3';
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'w-full rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-wait disabled:opacity-60 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700';
        button.textContent = 'Load more';
        button.addEventListener('click', async () => {
            button.disabled = true;
            button.setAttribute('aria-busy', 'true');
            try {
                await config.onLoadMore();
            } finally {
                button.setAttribute('aria-busy', 'false');
                button.disabled = false;
            }
        });
        controls.append(button);
        fragment.append(controls);
    }

    // Atomic swap — old content is replaced in a single frame
    config.container.replaceChildren(fragment);
}

export function createSidebarItem(item, config, animate = false) {
    const isCurrent = config.currentId === item.id;
    const row = document.createElement('div');
    row.dataset.itemId = item.id;
    row.setAttribute('data-current', isCurrent ? 'true' : 'false');
    const a = document.createElement('a');
    a.href = `${config.routePrefix}${item.id}`;
    const activeClasses = SIDEBAR_ACTIVE_CLASSES.join(' ');
    const inactiveClasses = SIDEBAR_INACTIVE_CLASSES.join(' ') + ' hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-200/50 dark:hover:bg-dark-700/50 hover:border-gray-300 dark:hover:border-dark-600';
    row.className = `group paste-item flex items-center rounded-lg text-sm transition-all duration-200 border ${isCurrent ? activeClasses : inactiveClasses}${animate ? ' animate-slide-in' : ''}`;
    a.className = 'min-w-0 flex flex-1 items-center px-3 py-2 rounded-lg focus-visible:outline-none';

    const badge = config.badgeForItem(item);
    const preview = config.previewForItem(item);
    const meta = config.metaForItem(item);
    const sq = config.searchQuery || '';

    let badgeHtml = '';
    if (badge) {
        badgeHtml = `<span class="px-1.5 py-0.5 rounded text-[9px] font-bold uppercase tracking-wider ${badge.bg} ${badge.text} ${badge.border} border shrink-0">${escapeHtml(badge.label)}</span>`;
    }

    const titleHtml = sq ? highlightMatch(item.title, sq) : escapeHtml(item.title);
    const previewHtml = preview ? (sq ? highlightMatch(preview, sq) : escapeHtml(preview)) : '';

    a.innerHTML = `
        <div class="min-w-0 flex-1 pr-3">
            <div class="flex items-center gap-2 mb-1">
                <div class="truncate font-medium">${titleHtml}</div>
                ${badgeHtml}
            </div>
            ${previewHtml ? `<div class="text-xs text-gray-600 dark:text-gray-400 truncate">${previewHtml}</div>` : ''}
            ${meta ? `<div class="text-[10px] text-gray-600 dark:text-gray-400 mt-0.5">${escapeHtml(meta)}</div>` : ''}
        </div>
    `;

    const deleteBtn = document.createElement('button');
    deleteBtn.type = 'button';
    deleteBtn.className = 'delete-btn opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-opacity p-2 mr-1 flex-shrink-0 text-gray-400 hover:text-red-500 hover:bg-gray-300 dark:hover:bg-dark-600 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500';
    deleteBtn.setAttribute('aria-label', `Delete ${item.title}`);
    deleteBtn.innerHTML = '<svg aria-hidden="true" class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>';

    row.append(a, deleteBtn);

    deleteBtn.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        config.onDelete(item.id);
    });

    a.addEventListener('click', (e) => {
        e.preventDefault();
        window.history.pushState({}, '', `${config.routePrefix}${item.id}`);
        // Dispatch custom event to trigger checkUrlMode without circular dependency
        window.dispatchEvent(new CustomEvent('app:navigate'));

        config.container.querySelectorAll('.paste-item').forEach(el => {
            el.classList.remove(...SIDEBAR_ACTIVE_CLASSES);
            el.classList.add(...SIDEBAR_INACTIVE_CLASSES);
            el.dataset.current = 'false';
        });
        row.classList.add(...SIDEBAR_ACTIVE_CLASSES);
        row.classList.remove(...SIDEBAR_INACTIVE_CLASSES);
        row.dataset.current = 'true';

        if (isMobileViewport()) {
            toggleSidebar(false);
        }
    });

    return row;
}

export function showDeleteConfirm(message) {
    return new Promise((resolve) => {
        const previousFocus = document.activeElement;
        const overlay = document.createElement('div');
        overlay.className = 'fixed inset-0 z-[100] flex items-center justify-center';
        overlay.innerHTML = `
            <div class="absolute inset-0 bg-gray-900/40 dark:bg-black/60 backdrop-blur-sm"></div>
            <div role="dialog" aria-modal="true" aria-labelledby="delete-dialog-title" aria-describedby="delete-dialog-description" class="app-dialog relative w-11/12 max-w-md rounded-2xl overflow-hidden p-6 transform scale-95 animate-modal-in">
                <div class="flex items-center gap-3 mb-4">
                    <div class="w-10 h-10 rounded-full bg-red-500/10 flex items-center justify-center flex-shrink-0">
                        <svg class="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                    </div>
                    <div>
                        <h2 id="delete-dialog-title" class="text-lg font-semibold text-gray-900 dark:text-white">Delete permanently?</h2>
                        <p id="delete-dialog-description" class="text-sm text-gray-500 dark:text-gray-400 mt-0.5"></p>
                    </div>
                </div>
                <div class="flex justify-end gap-3 mt-6">
                    <button id="delete-cancel" class="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-dark-700 hover:bg-gray-200 dark:hover:bg-dark-600 rounded-lg transition-colors">Cancel</button>
                    <button id="delete-confirm" class="px-4 py-2 text-sm font-medium text-white bg-red-600 hover:bg-red-500 rounded-lg transition-colors shadow-sm">Delete</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);
        overlay.querySelector('#delete-dialog-description').textContent = message;
        requestAnimationFrame(() => {
            const modal = overlay.querySelector('.transform');
            if (modal) { modal.classList.remove('scale-95'); modal.classList.add('scale-100'); }
        });
        const focusable = Array.from(overlay.querySelectorAll('button'));
        const onKeyDown = (event) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                cleanup(false);
                return;
            }
            if (event.key !== 'Tab') return;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        };
        const cleanup = (result) => {
            document.removeEventListener('keydown', onKeyDown);
            overlay.remove();
            if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
            resolve(result);
        };
        document.addEventListener('keydown', onKeyDown);
        overlay.querySelector('#delete-cancel').addEventListener('click', () => cleanup(false));
        overlay.querySelector('#delete-confirm').addEventListener('click', () => cleanup(true));
        overlay.querySelector('.absolute').addEventListener('click', () => cleanup(false));
        setTimeout(() => overlay.querySelector('#delete-cancel').focus(), 100);
    });
}
