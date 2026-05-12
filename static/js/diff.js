import { elements } from './dom.js';
import { state } from './state.js';
import { escapeHtml, generateTitle, isMobileViewport } from './utils.js';
import { showToast, showDeleteConfirm, renderSidebarList } from './ui.js';

export function getDiffListConfig() {
    return {
        container: elements.diffList,
        itemsKey: 'items',
        routePrefix: '/diff/',
        currentId: state.currentPasteId,
        emptyIcon: 'M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4',
        emptyLabel: 'No diffs found',
        badgeForItem: () => null,
        previewForItem: () => null,
        metaForItem: (d) => new Date(d.createdAt).toLocaleDateString(),
        searchQuery: elements.searchInput.value.toLowerCase(),
        onDelete: deleteDiff
    };
}

export async function fetchDiffs() {
    try {
        const query = elements.searchInput.value.toLowerCase();
        const endpoint = query ? `/api/search_diffs?q=${encodeURIComponent(query)}` : '/api/saved_diffs';
        const res = await fetch(endpoint);
        const data = await res.json();
        renderSidebarList(data, !query, getDiffListConfig());
    } catch (e) {
        console.error('Failed to fetch diffs:', e);
        showToast('Failed to load diffs', true);
    }
}

export async function saveDiff(baseId, compareId, baseContent, compareContent) {
    const title = elements.diffTitleInput.value.trim() || generateTitle('diff');
    try {
        elements.saveDiffBtn.disabled = true;
        
        const method = state.currentDiffMode === 'new' ? 'POST' : 'PUT';
        const url = state.currentDiffMode === 'new' ? '/api/saved_diffs' : `/api/saved_diffs/${state.currentPasteId}`;
        
        const res = await fetch(url, {
            method: method,
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                title,
                base: baseId,
                compare: compareId,
                baseContent: baseContent,
                compareContent: compareContent
            })
        });

        if (!res.ok) throw new Error('Failed to save diff');
        const data = await res.json();
        
        state.currentPasteId = data.id;
        state.currentTitle = data.title;
        elements.diffTitleInput.value = data.title;
        
        showToast('Diff saved successfully!');
        window.history.pushState({}, '', `/diff/${data.id}`);
        setDiffMode('view');
        fetchDiffs();
    } catch (e) {
        console.error('Save diff failed:', e);
        showToast('Failed to save diff', true);
    } finally {
        elements.saveDiffBtn.disabled = false;
    }
}

export async function loadDiff(id) {
    try {
        const res = await fetch(`/api/saved_diffs/${id}`);
        if (!res.ok) {
            if (res.status === 404) {
                showToast('Diff not found', true);
                window.dispatchEvent(new CustomEvent('app:action', { detail: 'new-diff' }));
                return;
            }
            throw new Error('Failed to load diff');
        }
        const data = await res.json();
        
        state.currentPasteId = id;
        state.currentTitle = data.title;
        elements.diffTitleInput.value = data.title;
        
        // Show the original input references (paste IDs or raw text)
        elements.diffBase.value = data.baseContent || data.base;
        elements.diffCompare.value = data.compareContent || data.compare;
        
        // Re-run the diff with pre-resolved content so we don't need to re-fetch
        await runDiff({
            baseContent: data.baseContent,
            compareContent: data.compareContent,
            baseResolvedId: data.base,
            compareResolvedId: data.compare
        });
        
        setDiffMode('view');
        fetchDiffs();
    } catch (e) {
        console.error('Failed to load diff:', e);
        showToast('Failed to load diff', true);
    }
}

export async function deleteDiff(id) {
    const confirmed = await showDeleteConfirm("This action cannot be undone. The diff will be permanently removed.");
    if (!confirmed) return;

    try {
        const res = await fetch(`/api/saved_diffs/${id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error('Failed to delete');
        
        showToast('Diff deleted');
        if (state.currentPasteId === id) {
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new-diff' }));
        } else {
            fetchDiffs();
        }
    } catch (e) {
        console.error('Delete diff failed:', e);
        showToast('Failed to delete diff', true);
    }
}

export async function runDiff(preResolved) {
    const base = elements.diffBase.value.trim();
    const compare = elements.diffCompare.value.trim();

    if (!base || !compare) {
        showToast('Please provide both base and compare inputs', true);
        return;
    }

    elements.runDiffBtn.disabled = true;
    const originalContent = elements.runDiffBtn.innerHTML;
    elements.runDiffBtn.innerHTML = `
        <svg class="animate-spin h-5 w-5 text-white" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        Comparing...`;

    try {
        let baseContent, compareContent, baseResolvedId, compareResolvedId;

        // Guard: when called as an event handler, the Event object is passed as preResolved
        const resolved = (preResolved && !(preResolved instanceof Event) && 'baseContent' in preResolved) ? preResolved : null;

        if (resolved) {
            // Loading a saved diff — content is already resolved
            baseContent = resolved.baseContent;
            compareContent = resolved.compareContent;
            baseResolvedId = resolved.baseResolvedId;
            compareResolvedId = resolved.compareResolvedId;
        } else {
            // Fresh diff from user input
            baseContent = base;
            compareContent = compare;
            baseResolvedId = base;
            compareResolvedId = compare;

            // resolve paste IDs — if it looks like a 6-char alphanumeric ID, try to fetch paste content
            if (/^[a-zA-Z0-9]{6}$/.test(base)) {
                try {
                    const res = await fetch(`/api/pastes/${base}`);
                    if (res.ok) {
                        const d = await res.json();
                        if (d.content) baseContent = d.content;
                    } else {
                        // Paste not found — treat as raw content
                        baseResolvedId = 'RAW';
                    }
                } catch {
                    baseResolvedId = 'RAW';
                }
            } else { baseResolvedId = 'RAW'; }

            if (/^[a-zA-Z0-9]{6}$/.test(compare)) {
                try {
                    const res = await fetch(`/api/pastes/${compare}`);
                    if (res.ok) {
                        const d = await res.json();
                        if (d.content) compareContent = d.content;
                    } else {
                        compareResolvedId = 'RAW';
                    }
                } catch {
                    compareResolvedId = 'RAW';
                }
            } else { compareResolvedId = 'RAW'; }
        }

        // Use JSDiff 
        if (!window.Diff) throw new Error("Diff engine not loaded");

        const diffLines = Diff.diffLines(baseContent, compareContent);
        
        let html = '';
        let additions = 0;
        let deletions = 0;
        let baseLineNum = 1;
        let compareLineNum = 1;

        // Helper: render a line with word-level highlights for changed portions
        function renderInlineHighlight(oldText, newText) {
            const wordDiffs = Diff.diffWords(oldText, newText);
            let removedHtml = '';
            let addedHtml = '';
            wordDiffs.forEach(part => {
                const escaped = escapeHtml(part.value);
                if (part.removed) {
                    removedHtml += `<span class="bg-rose-500/30 rounded-sm px-[1px]">${escaped}</span>`;
                } else if (part.added) {
                    addedHtml += `<span class="bg-emerald-500/30 rounded-sm px-[1px]">${escaped}</span>`;
                } else {
                    removedHtml += escaped;
                    addedHtml += escaped;
                }
            });
            return { removedHtml, addedHtml };
        }

        // Collect parts into an array so we can look ahead for paired removed/added blocks
        const parts = [...diffLines];
        for (let i = 0; i < parts.length; i++) {
            const part = parts[i];

            if (part.removed && i + 1 < parts.length && parts[i + 1].added) {
                // Paired change — do word-level diff within each line pair
                const next = parts[i + 1];
                const removedLines = part.value.replace(/\n$/, '').split('\n');
                const addedLines = next.value.replace(/\n$/, '').split('\n');
                const maxLen = Math.max(removedLines.length, addedLines.length);

                for (let j = 0; j < maxLen; j++) {
                    const rLine = j < removedLines.length ? removedLines[j] : null;
                    const aLine = j < addedLines.length ? addedLines[j] : null;

                    if (rLine !== null && aLine !== null) {
                        // Both exist — inline highlight
                        const { removedHtml: rHtml, addedHtml: aHtml } = renderInlineHighlight(rLine, aLine);
                        deletions++;
                        html += `<div class="bg-rose-500/10 text-rose-700 dark:text-rose-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">${baseLineNum++}</span><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">−</span>${rHtml}</div>`;
                        additions++;
                        html += `<div class="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">+</span><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">${compareLineNum++}</span>${aHtml}</div>`;
                    } else if (rLine !== null) {
                        deletions++;
                        html += `<div class="bg-rose-500/10 text-rose-700 dark:text-rose-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">${baseLineNum++}</span><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">−</span>${escapeHtml(rLine)}</div>`;
                    } else if (aLine !== null) {
                        additions++;
                        html += `<div class="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">+</span><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">${compareLineNum++}</span>${escapeHtml(aLine)}</div>`;
                    }
                }
                i++; // skip the next (added) part since we consumed it
            } else if (part.added) {
                const lines = part.value.replace(/\n$/, '').split('\n');
                lines.forEach(line => {
                    additions++;
                    html += `<div class="bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">+</span><span class="w-12 inline-block text-right pr-4 text-emerald-500/50 select-none">${compareLineNum++}</span>${escapeHtml(line)}</div>`;
                });
            } else if (part.removed) {
                const lines = part.value.replace(/\n$/, '').split('\n');
                lines.forEach(line => {
                    deletions++;
                    html += `<div class="bg-rose-500/10 text-rose-700 dark:text-rose-400 px-2 py-0.5"><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">${baseLineNum++}</span><span class="w-12 inline-block text-right pr-4 text-rose-500/50 select-none">−</span>${escapeHtml(line)}</div>`;
                });
            } else {
                const lines = part.value.replace(/\n$/, '').split('\n');
                lines.forEach(line => {
                    html += `<div class="text-gray-600 dark:text-gray-400 px-2 py-0.5 hover:bg-gray-500/5"><span class="w-12 inline-block text-right pr-4 text-gray-400/50 select-none">${baseLineNum++}</span><span class="w-12 inline-block text-right pr-4 text-gray-400/50 select-none">${compareLineNum++}</span>${escapeHtml(line)}</div>`;
                });
            }
        }

        elements.diffOutput.innerHTML = html || '<div class="text-gray-500 italic p-4">No differences found.</div>';
        elements.diffOutputContainer.classList.remove('hidden');
        
        elements.saveDiffBtn.disabled = false;
        
        // Temporarily store these on the window or pass them via custom event so saveDiff can access them
        window._currentDiffResult = { baseResolvedId, compareResolvedId, baseContent, compareContent, html, additions, deletions };

    } catch (e) {
        console.error('Diff calculation failed:', e);
        showToast('Failed to generate diff', true);
    } finally {
        elements.runDiffBtn.disabled = false;
        elements.runDiffBtn.innerHTML = originalContent;
    }
}

export function setDiffMode(mode) {
    state.currentDiffMode = mode;
    
    elements.diffTitleInput.readOnly = mode === 'view';
    elements.diffBase.readOnly = mode === 'view';
    elements.diffCompare.readOnly = mode === 'view';
    
    elements.saveDiffBtn.style.display = mode === 'view' ? 'none' : 'flex';
    elements.diffStatusBadge.style.display = mode === 'view' ? 'inline-flex' : 'none';
    elements.runDiffBtn.style.display = mode === 'view' ? 'none' : 'flex';
    
    if (mode === 'new') {
        elements.diffOutputContainer.classList.add('hidden');
        elements.diffOutput.innerHTML = '';
        elements.saveDiffBtn.disabled = true;
    }
}

// escapeHtml is imported from utils.js
