import { elements } from './dom.js';
import { state, SIDEBAR_ACTIVE_CLASSES, SIDEBAR_INACTIVE_CLASSES } from './state.js';
import { escapeHtml, isMobileViewport, getLangColorClasses } from './utils.js';

let _toastTimeout = null;

export function showToast(message, isError = false) {
    clearTimeout(_toastTimeout);
    elements.toastMessage.textContent = message;
    const icon = elements.toast.querySelector('svg');
    icon.innerHTML = isError
        ? '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>'
        : '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>';

    icon.classList.remove('text-green-400', 'text-red-400');
    icon.classList.add(isError ? 'text-red-400' : 'text-green-400');

    elements.toast.classList.remove('translate-y-full', 'opacity-0');
    _toastTimeout = setTimeout(() => {
        elements.toast.classList.add('translate-y-full', 'opacity-0');
    }, 3000);
}

export function toggleSidebar(show) {
    state.isSidebarOpen = show;

    if (isMobileViewport()) {
        if (show) {
            elements.sidebar.classList.add('mobile-open');
            elements.mobileScrim.classList.add('active');
            document.body.style.overflow = 'hidden';
        } else {
            elements.sidebar.classList.remove('mobile-open');
            elements.mobileScrim.classList.remove('active');
            document.body.style.overflow = '';
        }
    } else {
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
    } else {
        menu.classList.add('opacity-0', 'pointer-events-none', '-translate-y-2');
        menu.classList.remove('opacity-100', 'translate-y-0');
        chevron.style.transform = '';
    }
}

export function toggleCmdK(show) {
    const palette = elements.cmdkPalette;
    const modal = elements.cmdkModal;
    const input = elements.cmdkInput;
    
    if (show) {
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
        <div class="flex flex-col items-center justify-center h-32 text-gray-400 dark:text-gray-500">
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
        const href = el.getAttribute('href') || '';
        const id = href.split('/').pop();
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

        const groupTitle = document.createElement('h3');
        groupTitle.className = "text-[11px] font-bold text-gray-400 dark:text-gray-500 uppercase tracking-widest pl-1 mb-3";
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

    // Atomic swap — old content is replaced in a single frame
    config.container.replaceChildren(fragment);
}

export function createSidebarItem(item, config, animate = false) {
    const isCurrent = config.currentId === item.id;
    const a = document.createElement('a');
    a.href = `${config.routePrefix}${item.id}`;
    a.setAttribute('tabindex', '0');
    const activeClasses = SIDEBAR_ACTIVE_CLASSES.join(' ');
    const inactiveClasses = SIDEBAR_INACTIVE_CLASSES.join(' ') + ' hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-200/50 dark:hover:bg-dark-700/50 hover:border-gray-300 dark:hover:border-dark-600';
    a.className = `group paste-item flex items-center justify-between px-3 py-2 rounded-lg text-sm transition-all duration-200 cursor-pointer border ${isCurrent ? activeClasses : inactiveClasses}${animate ? ' animate-slide-in' : ''}`;

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
            ${previewHtml ? `<div class="text-xs text-gray-500 dark:text-gray-400 truncate">${previewHtml}</div>` : ''}
            ${meta ? `<div class="text-[10px] text-gray-400 dark:text-gray-500 mt-0.5">${meta}</div>` : ''}
        </div>
        <button
            class="delete-btn opacity-0 group-hover:opacity-100 transition-opacity p-1.5 ml-2 flex-shrink-0 text-gray-400 hover:text-red-500 hover:bg-gray-300 dark:hover:bg-dark-600 rounded-md focus:outline-none"
            title="Delete"
        >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
        </button>
    `;

    const deleteBtn = a.querySelector('.delete-btn');
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
        });
        a.classList.add(...SIDEBAR_ACTIVE_CLASSES);
        a.classList.remove(...SIDEBAR_INACTIVE_CLASSES);

        if (isMobileViewport()) {
            toggleSidebar(false);
        }
    });

    return a;
}

export function showDeleteConfirm(message) {
    return new Promise((resolve) => {
        const overlay = document.createElement('div');
        overlay.className = 'fixed inset-0 z-[100] flex items-center justify-center';
        overlay.innerHTML = `
            <div class="absolute inset-0 bg-gray-900/40 dark:bg-black/60 backdrop-blur-sm"></div>
            <div class="relative w-11/12 max-w-md bg-white dark:bg-[#16191f] rounded-2xl shadow-2xl border border-gray-200 dark:border-gray-800 overflow-hidden p-6 transform scale-95 animate-modal-in">
                <div class="flex items-center gap-3 mb-4">
                    <div class="w-10 h-10 rounded-full bg-red-500/10 flex items-center justify-center flex-shrink-0">
                        <svg class="w-5 h-5 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                    </div>
                    <div>
                        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">Delete permanently?</h3>
                        <p class="text-sm text-gray-500 dark:text-gray-400 mt-0.5">${message}</p>
                    </div>
                </div>
                <div class="flex justify-end gap-3 mt-6">
                    <button id="delete-cancel" class="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-dark-700 hover:bg-gray-200 dark:hover:bg-dark-600 rounded-lg transition-colors">Cancel</button>
                    <button id="delete-confirm" class="px-4 py-2 text-sm font-medium text-white bg-red-600 hover:bg-red-500 rounded-lg transition-colors shadow-sm">Delete</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);
        requestAnimationFrame(() => {
            const modal = overlay.querySelector('.transform');
            if (modal) { modal.classList.remove('scale-95'); modal.classList.add('scale-100'); }
        });
        const cleanup = (result) => { overlay.remove(); resolve(result); };
        overlay.querySelector('#delete-cancel').addEventListener('click', () => cleanup(false));
        overlay.querySelector('#delete-confirm').addEventListener('click', () => cleanup(true));
        overlay.querySelector('.absolute').addEventListener('click', () => cleanup(false));
        setTimeout(() => overlay.querySelector('#delete-confirm').focus(), 100);
    });
}
