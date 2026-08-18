const EXPORT_SCHEMA_VERSION = 1;
export const MAX_IMPORT_BYTES = 2 * 1024 * 1024;
const RESOURCE_PATHS = {
    paste: '/api/pastes',
    diff: '/api/saved_diffs'
};

function assertItemIdentity(kind, id) {
    if (kind !== 'paste' && kind !== 'diff') throw new TypeError('Invalid item kind');
    if (typeof id !== 'string' || !/^[a-zA-Z0-9_-]+$/.test(id)) {
        throw new TypeError('Invalid item ID');
    }
}

async function responseError(response, fallback) {
    try {
        const payload = await response.clone().json();
        if (typeof payload?.error === 'string' && payload.error.trim()) return payload.error;
    } catch {
        // The fallback covers empty and non-JSON error responses.
    }
    return `${fallback} (${response.status})`;
}

export function itemExportURL(kind, id) {
    assertItemIdentity(kind, id);
    return `${RESOURCE_PATHS[kind]}/${encodeURIComponent(id)}/export`;
}

export function parseItemExport(source) {
    const document = typeof source === 'string' ? JSON.parse(source) : source;
    if (!document || typeof document !== 'object' || Array.isArray(document)) {
        throw new TypeError('The import file must contain an object');
    }
    if (document.schemaVersion !== EXPORT_SCHEMA_VERSION) {
        throw new TypeError('Unsupported export schema');
    }
    if (document.kind !== 'paste' && document.kind !== 'diff') {
        throw new TypeError('The import kind must be paste or diff');
    }
    return document;
}

export async function importItem(source, request = globalThis.fetch) {
    const document = parseItemExport(source);
    const response = await request('/api/import', {
        method: 'POST',
        headers: {
            'Accept': 'application/json',
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(document)
    });
    if (!response.ok) throw new Error(await responseError(response, 'Import failed'));

    const result = await response.json();
    assertItemIdentity(result.kind, result.id);
    return result;
}

export async function fetchItemExport(kind, id, request = globalThis.fetch) {
    const fallbackName = `${id}.${kind}.json`;
    const response = await request(itemExportURL(kind, id), {
        headers: { 'Accept': 'application/json' }
    });
    if (!response.ok) throw new Error(await responseError(response, 'Export failed'));

    const disposition = response.headers.get('Content-Disposition') || '';
    const filename = disposition.match(/filename="([^"]+)"/i)?.[1] || fallbackName;
    return { blob: await response.blob(), filename };
}
