const ITEM_PATHS = Object.freeze({
    paste: '/api/pastes',
    diff: '/api/saved_diffs'
});

function revisionPath(kind, id, revision) {
    const root = ITEM_PATHS[kind];
    if (!root) throw new TypeError('Invalid revision kind');
    if (typeof id !== 'string' || !/^(?:[a-zA-Z0-9]{6}|[a-zA-Z0-9]{32})$/.test(id)) {
        throw new TypeError('Invalid revision item ID');
    }
    if (!Number.isSafeInteger(revision) || revision < 1) {
        throw new TypeError('Invalid revision number');
    }
    return `${root}/${id}/revisions/${revision}`;
}

async function responseError(response, fallback) {
    try {
        const payload = await response.clone().json();
        if (typeof payload?.error === 'string' && payload.error.trim()) return payload.error;
    } catch {
        // The fallback covers empty and non-JSON responses.
    }
    return fallback;
}

export async function fetchItemRevision(kind, id, revision, request = globalThis.fetch, options = {}) {
    const response = await request(revisionPath(kind, id, revision), {
        signal: options.signal,
        headers: { Accept: 'application/json' }
    });
    if (!response.ok) {
        throw new Error(await responseError(response, 'Failed to load revision'));
    }
    return response.json();
}

export async function restoreItemRevision(
    kind,
    id,
    targetRevision,
    currentRevision,
    headers,
    request = globalThis.fetch
) {
    if (!Number.isSafeInteger(currentRevision) || currentRevision < 1) {
        throw new TypeError('The current revision is unavailable');
    }
    const requestHeaders = new Headers(headers);
    requestHeaders.set('Content-Type', 'application/json');
    const response = await request(`${revisionPath(kind, id, targetRevision)}/restore`, {
        method: 'POST',
        headers: requestHeaders,
        body: JSON.stringify({ expectedRevision: currentRevision })
    });
    if (response.status === 409) {
        throw new Error('This item changed elsewhere. Reload it before you restore a revision.');
    }
    if (!response.ok) {
        throw new Error(await responseError(response, 'Failed to restore revision'));
    }
    const result = await response.json();
    if (!Number.isSafeInteger(result.revision) || result.revision < 1) {
        throw new TypeError('The restore response has no committed revision');
    }
    return result;
}

const VIEW_BUTTON_CLASSES = Object.freeze({
    paste: 'rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:hover:bg-dark-700',
    diff: 'rounded-md border border-gray-300 px-2.5 py-1 text-xs font-medium hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 dark:border-dark-600 dark:hover:bg-dark-700'
});

const RESTORE_BUTTON_CLASSES = Object.freeze({
    paste: 'rounded-md bg-primary-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-primary-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50',
    diff: 'rounded-md bg-indigo-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-indigo-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 disabled:cursor-not-allowed disabled:opacity-50'
});

export function createRevisionRow({
    kind,
    id,
    revision,
    createdAt,
    currentRevision,
    canRestore,
    restoreHeaders,
    onRestored,
    onMessage,
    request = globalThis.fetch,
    signal,
    ownerDocument = globalThis.document
}) {
    revisionPath(kind, id, revision);
    if (!ownerDocument) throw new TypeError('The document is unavailable');

    const row = ownerDocument.createElement('div');
    row.className = 'grid gap-2 rounded-lg px-3 py-2 text-gray-700 odd:bg-gray-100/70 dark:text-gray-200 dark:odd:bg-white/5';
    const summary = ownerDocument.createElement('div');
    summary.className = 'flex flex-wrap items-center justify-between gap-3';
    const identity = ownerDocument.createElement('div');
    identity.className = 'flex min-w-0 items-center gap-3';
    const label = ownerDocument.createElement('span');
    label.className = 'font-medium';
    label.textContent = `Revision ${revision}`;
    const time = ownerDocument.createElement('time');
    time.className = 'text-xs text-gray-500 dark:text-gray-400';
    if (createdAt) {
        time.dateTime = createdAt;
        time.textContent = new Date(createdAt).toLocaleString();
    } else {
        time.textContent = 'Date unavailable';
    }
    identity.append(label, time);

    const actions = ownerDocument.createElement('div');
    actions.className = 'flex items-center gap-2';
    const viewButton = ownerDocument.createElement('button');
    viewButton.type = 'button';
    viewButton.className = VIEW_BUTTON_CLASSES[kind];
    viewButton.textContent = 'View';
    const restoreButton = ownerDocument.createElement('button');
    restoreButton.type = 'button';
    restoreButton.className = RESTORE_BUTTON_CLASSES[kind];
    restoreButton.textContent = 'Restore';
    restoreButton.disabled = !canRestore();
    if (restoreButton.disabled) restoreButton.title = 'The edit secret is required to restore this revision';
    actions.append(viewButton, restoreButton);
    summary.append(identity, actions);

    const preview = ownerDocument.createElement('pre');
    preview.id = `revision-${kind}-${id}-${revision}`;
    preview.hidden = true;
    preview.className = 'max-h-64 overflow-auto whitespace-pre-wrap rounded-lg border border-gray-200 bg-white p-3 font-mono text-xs text-gray-800 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-200';
    preview.tabIndex = 0;
    preview.setAttribute('role', 'region');
    preview.setAttribute('aria-label', `Revision ${revision} content`);
    viewButton.setAttribute('aria-controls', preview.id);
    viewButton.setAttribute('aria-expanded', 'false');

    viewButton.addEventListener('click', async () => {
        if (preview.dataset.loaded === 'true') {
            preview.hidden = !preview.hidden;
            viewButton.textContent = preview.hidden ? 'View' : 'Hide';
            viewButton.setAttribute('aria-expanded', String(!preview.hidden));
            return;
        }
        viewButton.disabled = true;
        viewButton.setAttribute('aria-busy', 'true');
        try {
            const document = await fetchItemRevision(kind, id, revision, request, { signal });
            preview.textContent = revisionPreviewText(kind, document);
            preview.dataset.loaded = 'true';
            preview.hidden = false;
            viewButton.textContent = 'Hide';
            viewButton.setAttribute('aria-expanded', 'true');
        } catch (error) {
            if (error.name !== 'AbortError') onMessage(error.message || 'Failed to load revision', true);
        } finally {
            viewButton.disabled = false;
            viewButton.setAttribute('aria-busy', 'false');
        }
    });

    restoreButton.addEventListener('click', async () => {
        restoreButton.disabled = true;
        restoreButton.setAttribute('aria-busy', 'true');
        try {
            const result = await restoreItemRevision(
                kind,
                id,
                revision,
                currentRevision(),
                restoreHeaders(),
                request
            );
            await onRestored(result);
            onMessage(`Revision ${revision} restored`, false);
        } catch (error) {
            onMessage(error.message || 'Failed to restore revision', true);
        } finally {
            restoreButton.disabled = !canRestore();
            restoreButton.setAttribute('aria-busy', 'false');
        }
    });

    row.append(summary, preview);
    return row;
}

export function revisionPreviewText(kind, document) {
    if (kind === 'paste') return String(document?.pasteContent ?? '');
    if (kind !== 'diff') throw new TypeError('Invalid revision kind');

    const diff = document?.diff ?? {};
    const baseLabel = diff.base ? `Base: ${diff.base}\n` : '';
    const compareLabel = diff.compare ? `Compare: ${diff.compare}\n` : '';
    return `${baseLabel}${compareLabel}\n--- Base content ---\n${diff.baseContent ?? ''}\n\n--- Compare content ---\n${diff.compareContent ?? ''}`;
}
