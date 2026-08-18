import { elements } from './dom.js';
import { state } from './state.js';
import { escapeHtml, generateTitle, syncLineNumbers } from './utils.js';
import { showToast, showDeleteConfirm, renderSidebarList } from './ui.js';
import { buildCursorURL, createCursorState, normalizeCursorPage } from './pagination.js';
import { getEditSecret, removeEditSecret, saveEditSecret, withEditSecret } from './secrets.js';
import { applyDiffMetadata, readDiffMetadata, resetDiffMetadata } from './diff-metadata.js';
import { createRevisionRow } from './revisions.js';
import { isPasteReference } from './identifiers.js';

let diffListController = null;
let diffDetailController = null;
let diffRevisionsController = null;
let diffRunSequence = 0;
let activeDiffTask = null;
const diffPages = createCursorState();
let diffPageItems = [];
let diffPageQuery = '';
const compareButtonContent = elements.runDiffBtn.innerHTML;
const diffMetadataControls = {
    tags: elements.diffTags,
    favorite: elements.diffFavorite,
    expiresAt: elements.diffExpiresAt,
    burnAfterRead: elements.diffBurnAfterRead
};

function resetRunButton() {
    elements.runDiffBtn.setAttribute('aria-busy', 'false');
    elements.runDiffBtn.disabled = false;
    elements.runDiffBtn.innerHTML = compareButtonContent;
}

function cancelDiffTask() {
    if (!activeDiffTask) return;
    const task = activeDiffTask;
    activeDiffTask = null;
    task.worker.terminate();
    task.reject(new DOMException('Diff request cancelled', 'AbortError'));
}

function runDiffWorker(payload, requestId) {
    cancelDiffTask();
    const version = document.documentElement.dataset.version || 'dev';
    const workerURL = `/static/dist/diff-worker.js?v=${encodeURIComponent(version)}`;
    const worker = new Worker(workerURL);

    return new Promise((resolve, reject) => {
        const timeout = window.setTimeout(() => {
            if (activeDiffTask?.worker === worker) activeDiffTask = null;
            worker.terminate();
            reject(new Error('Diff calculation timed out'));
        }, 20_000);

        const finish = (callback, value) => {
            window.clearTimeout(timeout);
            if (activeDiffTask?.worker === worker) activeDiffTask = null;
            worker.terminate();
            callback(value);
        };

        activeDiffTask = {
            worker,
            reject: (error) => {
                window.clearTimeout(timeout);
                reject(error);
            }
        };

        worker.addEventListener('message', (event) => {
            const message = event.data;
            if (message.requestId !== requestId) return;
            if (message.type === 'result') finish(resolve, message.result);
            else if (message.type === 'error') finish(reject, new Error(message.error));
        });
        worker.addEventListener('error', () => finish(reject, new Error('Diff worker failed')));
        worker.postMessage({ type: 'compare', requestId, payload });
    });
}

function renderSegments(segments) {
    return segments.map((segment) => {
        const value = escapeHtml(segment.text);
        if (segment.type === 'removed') return `<span class="font-semibold underline decoration-2 underline-offset-2">${value}</span>`;
        if (segment.type === 'added') return `<span class="font-semibold underline decoration-2 underline-offset-2">${value}</span>`;
        return value;
    }).join('');
}

function renderUnifiedResult(result) {
    const lines = [];
    const renderLine = (type, baseLine, compareLine, sign, segments) => {
        const styles = {
            removed: 'bg-rose-500/10 text-rose-700 dark:text-rose-400',
            added: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
            context: 'text-gray-600 hover:bg-gray-500/5 dark:text-gray-400'
        };
        lines.push(`<div class="grid min-w-[36rem] grid-cols-[3rem_3rem_2rem_1fr] ${styles[type]} px-2 py-0.5 whitespace-pre"><span class="select-none text-right text-gray-500 dark:text-gray-300">${baseLine ?? ''}</span><span class="select-none text-right text-gray-500 dark:text-gray-300">${compareLine ?? ''}</span><span class="select-none text-center">${sign}</span><span>${renderSegments(segments)}</span></div>`);
    };

    for (const row of result.rows) {
        if (row.kind === 'context') {
            renderLine('context', row.baseLine, row.compareLine, '', row.base);
        } else {
            if (row.baseLine !== null) renderLine('removed', row.baseLine, null, '−', row.base);
            if (row.compareLine !== null) renderLine('added', null, row.compareLine, '+', row.compare);
        }
    }
    return lines.join('');
}

function renderSideBySideResult(result) {
    return result.rows.map((row) => {
        const baseType = row.kind === 'context' ? 'text-gray-600 dark:text-gray-400' : 'bg-rose-500/10 text-rose-700 dark:text-rose-400';
        const compareType = row.kind === 'context' ? 'text-gray-600 dark:text-gray-400' : 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-400';
        const base = row.baseLine === null
            ? '<span class="col-span-2 text-gray-400/50">&nbsp;</span>'
            : `<span class="select-none text-right text-gray-500 dark:text-gray-300">${row.baseLine}</span><span>${renderSegments(row.base)}</span>`;
        const compare = row.compareLine === null
            ? '<span class="col-span-2 text-gray-400/50">&nbsp;</span>'
            : `<span class="select-none text-right text-gray-500 dark:text-gray-300">${row.compareLine}</span><span>${renderSegments(row.compare)}</span>`;
        return `<div class="diff-side-by-side-row grid min-w-[64rem] grid-cols-2 border-b border-gray-500/10 last:border-b-0 whitespace-pre"><div class="diff-side-by-side-cell diff-side-by-side-base grid grid-cols-[3rem_1fr] gap-3 border-r border-gray-500/20 px-2 py-0.5 ${baseType}">${base}</div><div class="diff-side-by-side-cell grid grid-cols-[3rem_1fr] gap-3 px-2 py-0.5 ${compareType}">${compare}</div></div>`;
    }).join('');
}

function renderDiffResult(result) {
    const html = elements.diffViewMode.value === 'side-by-side'
        ? renderSideBySideResult(result)
        : renderUnifiedResult(result);
    elements.diffOutput.innerHTML = html || '<div class="p-4 italic text-gray-500 dark:text-gray-400">No differences found.</div>';
    elements.diffOutputContainer.classList.remove('hidden');
    elements.diffSummary.classList.remove('hidden');
    elements.diffAdditions.textContent = `+${result.additions}`;
    elements.diffDeletions.textContent = `-${result.deletions}`;
    elements.diffUnchanged.textContent = `${result.unchanged} unchanged`;
}

async function fetchDiffRevisions(id) {
    diffRevisionsController?.abort();
    diffRevisionsController = new AbortController();
    elements.diffRevisions.hidden = true;
    elements.diffRevisionList.replaceChildren();

    try {
        const response = await fetch(`/api/saved_diffs/${id}/revisions`, {
            signal: diffRevisionsController.signal
        });
        if (!response.ok) return;
        const payload = await response.json();
        if (window.location.pathname !== `/diff/${id}`) return;
        const revisions = Array.isArray(payload)
            ? payload
            : payload.items ?? payload.revisions ?? [];
        if (!Array.isArray(revisions) || !revisions.length) return;

        const fragment = document.createDocumentFragment();
        for (const revision of revisions) {
            const revisionNumber = Number(revision.revision ?? revision.version);
            if (!Number.isSafeInteger(revisionNumber) || revisionNumber < 1) continue;
            fragment.append(createRevisionRow({
                kind: 'diff',
                id,
                revision: revisionNumber,
                createdAt: revision.createdAt,
                currentRevision: () => state.currentDiffRevision,
                canRestore: () => Boolean(getEditSecret('diff', id)),
                restoreHeaders: () => withEditSecret({}, 'diff', id),
                onRestored: async (result) => {
                    state.currentDiffRevision = result.revision;
                    await loadDiff(id);
                },
                onMessage: showToast,
                signal: diffRevisionsController.signal
            }));
        }
        elements.diffRevisionList.replaceChildren(fragment);
        elements.diffRevisions.hidden = false;
    } catch (error) {
        if (error.name !== 'AbortError') console.error('Failed to load diff revisions:', error);
    }
}

export function initDiffLineNumbers() {
    syncLineNumbers(elements.diffBase, elements.diffBaseLines);
    syncLineNumbers(elements.diffCompare, elements.diffCompareLines);

    const invalidate = () => {
        if (state.currentDiffMode !== 'new') return;
        diffRunSequence++;
        cancelDiffTask();
        resetRunButton();
        state.currentDiffResult = null;
        elements.saveDiffBtn.disabled = true;
        elements.diffOutputContainer.classList.add('hidden');
        elements.diffOutput.replaceChildren();
        elements.diffSummary.classList.add('hidden');
    };

    elements.diffBase.addEventListener('input', invalidate);
    elements.diffCompare.addEventListener('input', invalidate);
    elements.diffIgnoreWhitespace.addEventListener('change', invalidate);
    elements.diffViewMode.addEventListener('change', () => {
        if (state.currentDiffResult?.result) renderDiffResult(state.currentDiffResult.result);
    });
}

export function getDiffListConfig() {
    return {
        container: elements.diffList,
        itemsKey: 'items',
        routePrefix: '/diff/',
        currentId: state.currentDiffId,
        emptyIcon: 'M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4',
        emptyLabel: 'No diffs found',
        badgeForItem: () => null,
        previewForItem: () => null,
        metaForItem: (diff) => new Date(diff.createdAt).toLocaleDateString(),
        searchQuery: elements.searchInput.value.toLowerCase(),
        nextCursor: diffPages.cursor,
        onLoadMore: () => fetchDiffs({ append: true }),
        onDelete: deleteDiff
    };
}

export async function fetchDiffs({ append = false } = {}) {
    diffListController?.abort();
    diffListController = new AbortController();

    try {
        const query = elements.searchInput.value.toLowerCase();
        if (!append || query !== diffPageQuery) {
            diffPages.reset();
            diffPageItems = [];
        }
        diffPageQuery = query;
        const endpoint = buildCursorURL(query ? '/api/search_diffs' : '/api/saved_diffs', {
            cursor: append ? diffPages.cursor : '',
            limit: diffPages.limit,
            query
        });
        const response = await fetch(endpoint, { signal: diffListController.signal });
        if (!response.ok) throw new Error(`Failed to load diffs (${response.status})`);
        const data = await response.json();
        const page = normalizeCursorPage(data);
        diffPages.update(page);
        if (page.isLegacy) {
            renderSidebarList(page.items, !query, getDiffListConfig());
        } else {
            const known = new Set(diffPageItems.map((item) => item.id));
            diffPageItems = append
                ? [...diffPageItems, ...page.items.filter((item) => !known.has(item.id))]
                : page.items;
            renderSidebarList(diffPageItems, false, getDiffListConfig());
        }
    } catch (error) {
        if (error.name === 'AbortError') return;
        console.error('Failed to fetch diffs:', error);
        showToast('Failed to load diffs', true);
    }
}

export async function saveDiff(baseId, compareId, baseContent, compareContent) {
    if (elements.saveDiffBtn.getAttribute('aria-busy') === 'true') return;

    const title = elements.diffTitleInput.value.trim() || generateTitle('diff');
    try {
        elements.saveDiffBtn.disabled = true;
        elements.saveDiffBtn.setAttribute('aria-busy', 'true');
        const method = state.currentDiffMode === 'new' ? 'POST' : 'PUT';
        const url = method === 'POST' ? '/api/saved_diffs' : `/api/saved_diffs/${state.currentDiffId}`;
        const headers = method === 'POST'
            ? new Headers({ 'Content-Type': 'application/json' })
            : withEditSecret({ 'Content-Type': 'application/json' }, 'diff', state.currentDiffId);
        const payload = {
            title,
            base: baseId,
            compare: compareId,
            baseContent,
            compareContent,
            ...readDiffMetadata(state, diffMetadataControls)
        };
        if (method === 'PUT') payload.revision = state.currentDiffRevision;
        const response = await fetch(url, {
            method,
            headers,
            body: JSON.stringify(payload)
        });
        if (response.status === 409) throw new Error('This diff changed elsewhere. Reload it before you save.');
        if (!response.ok) throw new Error('Failed to save diff');
        const data = await response.json();

        state.currentDiffId = data.id;
        state.currentTitle = data.title;
        state.currentDiffRevision = Number.isInteger(data.revision)
            ? data.revision
            : state.currentDiffRevision;
        applyDiffMetadata(state, data, diffMetadataControls);
        elements.diffTitleInput.value = data.title;
        if (method === 'POST' && data.editSecret) saveEditSecret('diff', data.id, data.editSecret);

        showToast('Diff saved successfully!');
        const path = `/diff/${data.id}`;
        if (window.location.pathname !== path) window.history.pushState({}, '', path);
        setDiffMode('view');
        fetchDiffRevisions(data.id);
        fetchDiffs();
    } catch (error) {
        console.error('Save diff failed:', error);
        showToast(error.message || 'Failed to save diff', true);
    } finally {
        elements.saveDiffBtn.setAttribute('aria-busy', 'false');
        elements.saveDiffBtn.disabled = !state.currentDiffResult;
    }
}

export async function loadDiff(id) {
    diffDetailController?.abort();
    diffDetailController = new AbortController();

    try {
        const response = await fetch(`/api/saved_diffs/${id}`, { signal: diffDetailController.signal });
        if (!response.ok) {
            if (response.status === 404) {
                showToast('Diff not found', true);
                window.dispatchEvent(new CustomEvent('app:action', { detail: 'new-diff' }));
                return;
            }
            throw new Error('Failed to load diff');
        }
        const data = await response.json();
        if (window.location.pathname !== `/diff/${id}`) return;

        state.currentDiffId = id;
        state.currentDiffRevision = Number.isInteger(data.revision) ? data.revision : null;
        state.currentTitle = data.title;
        applyDiffMetadata(state, data, diffMetadataControls);
        elements.diffTitleInput.value = data.title;
        elements.diffBase.value = data.baseContent ?? data.base ?? '';
        elements.diffCompare.value = data.compareContent ?? data.compare ?? '';
        await runDiff({
            baseContent: data.baseContent,
            compareContent: data.compareContent,
            baseResolvedId: data.base,
            compareResolvedId: data.compare
        });
        if (window.location.pathname !== `/diff/${id}`) return;
        setDiffMode('view');
        fetchDiffRevisions(id);
        fetchDiffs();
    } catch (error) {
        if (error.name === 'AbortError') return;
        console.error('Failed to load diff:', error);
        showToast('Failed to load diff', true);
    }
}

export async function deleteDiff(id) {
    const confirmed = await showDeleteConfirm('This action cannot be undone. The diff will be permanently removed.');
    if (!confirmed) return;
    try {
        const response = await fetch(`/api/saved_diffs/${id}`, {
            method: 'DELETE',
            headers: withEditSecret({}, 'diff', id)
        });
        if (!response.ok) throw new Error('Failed to delete');
        removeEditSecret('diff', id);
        showToast('Diff deleted');
        if (state.currentDiffId === id) {
            state.currentDiffId = null;
            window.dispatchEvent(new CustomEvent('app:action', { detail: 'new-diff' }));
        }
        await fetchDiffs();
    } catch (error) {
        console.error('Delete diff failed:', error);
        showToast('Failed to delete diff', true);
    }
}

export async function runDiff(preResolved) {
    if (elements.runDiffBtn.getAttribute('aria-busy') === 'true') return;
    const runId = ++diffRunSequence;
    const base = elements.diffBase.value;
    const compare = elements.diffCompare.value;
    const baseReference = base.trim();
    const compareReference = compare.trim();
    if (!baseReference || !compareReference) {
        showToast('Please provide both base and compare inputs', true);
        return;
    }

    elements.runDiffBtn.disabled = true;
    elements.runDiffBtn.setAttribute('aria-busy', 'true');
    elements.runDiffBtn.innerHTML = '<svg class="h-5 w-5 animate-spin text-white" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>Comparing...';

    try {
        let baseContent;
        let compareContent;
        let baseResolvedId;
        let compareResolvedId;
        const resolved = preResolved && !(preResolved instanceof Event) && 'baseContent' in preResolved
            ? preResolved
            : null;

        if (resolved) {
            baseContent = resolved.baseContent ?? base;
            compareContent = resolved.compareContent ?? compare;
            baseResolvedId = resolved.baseResolvedId ?? 'RAW';
            compareResolvedId = resolved.compareResolvedId ?? 'RAW';
        } else {
            const resolveInput = async (content, reference) => {
                const isId = content === reference && isPasteReference(reference);
                if (!isId) return { content, resolvedId: 'RAW' };
                try {
                    const response = await fetch(`/api/pastes/${reference}`);
                    if (!response.ok) return { content, resolvedId: 'RAW' };
                    const data = await response.json();
                    return typeof data.content === 'string'
                        ? { content: data.content, resolvedId: reference }
                        : { content, resolvedId: 'RAW' };
                } catch {
                    return { content, resolvedId: 'RAW' };
                }
            };
            const [resolvedBase, resolvedCompare] = await Promise.all([
                resolveInput(base, baseReference),
                resolveInput(compare, compareReference)
            ]);
            baseContent = resolvedBase.content;
            compareContent = resolvedCompare.content;
            baseResolvedId = resolvedBase.resolvedId;
            compareResolvedId = resolvedCompare.resolvedId;
        }

        if (runId !== diffRunSequence) return;
        const result = await runDiffWorker({
            base: baseContent,
            compare: compareContent,
            ignoreWhitespace: elements.diffIgnoreWhitespace.checked
        }, String(runId));
        if (runId !== diffRunSequence) return;

        renderDiffResult(result);
        state.currentDiffResult = { baseResolvedId, compareResolvedId, baseContent, compareContent, result };
        elements.saveDiffBtn.disabled = false;
    } catch (error) {
        if (error.name === 'AbortError') return;
        console.error('Diff calculation failed:', error);
        showToast(error.message || 'Failed to generate diff', true);
    } finally {
        if (runId === diffRunSequence) resetRunButton();
    }
}

export function setDiffMode(mode) {
    state.currentDiffMode = mode;
    elements.diffOptions.hidden = mode === 'view';
    elements.diffTitleInput.readOnly = mode === 'view';
    elements.diffBase.readOnly = mode === 'view';
    elements.diffCompare.readOnly = mode === 'view';
    elements.diffIgnoreWhitespace.disabled = mode === 'view';
    elements.saveDiffBtn.style.display = mode === 'view' ? 'none' : 'flex';
    elements.exportDiffBtn.style.display = mode === 'view' ? 'flex' : 'none';
    elements.diffStatusBadge.style.display = mode === 'view' ? 'inline-flex' : 'none';
    elements.runDiffBtn.style.display = mode === 'view' ? 'none' : 'flex';
    elements.diffRevisions.hidden = mode !== 'view' || !elements.diffRevisionList.childElementCount;

    if (mode === 'new') {
        resetDiffMetadata(state, diffMetadataControls);
        diffRunSequence++;
        cancelDiffTask();
        resetRunButton();
        state.currentDiffResult = null;
        elements.diffOutputContainer.classList.add('hidden');
        elements.diffOutput.replaceChildren();
        elements.diffSummary.classList.add('hidden');
        elements.diffRevisions.hidden = true;
        elements.diffRevisions.open = false;
        elements.diffRevisionList.replaceChildren();
        elements.saveDiffBtn.disabled = true;
    }
}
