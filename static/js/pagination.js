export const DEFAULT_PAGE_SIZE = 50;

export function buildCursorURL(path, { cursor = '', limit = DEFAULT_PAGE_SIZE, query = '' } = {}) {
    const url = new URL(path, 'http://local.invalid');
    url.searchParams.set('limit', String(limit));
    if (cursor) url.searchParams.set('cursor', cursor);
    if (query) url.searchParams.set('q', query);
    return `${url.pathname}${url.search}`;
}

export function normalizeCursorPage(payload) {
    if (Array.isArray(payload)) {
        return { items: payload, nextCursor: null, isLegacy: true };
    }
    if (payload && payload.items === null) {
        return { items: [], nextCursor: null, isLegacy: false };
    }
    if (!payload || !Array.isArray(payload.items)) {
        throw new TypeError('Invalid cursor page');
    }
    return {
        items: payload.items,
        nextCursor: typeof payload.nextCursor === 'string' && payload.nextCursor ? payload.nextCursor : null,
        isLegacy: false
    };
}

export function createCursorState(limit = DEFAULT_PAGE_SIZE) {
    let cursor = null;
    return {
        get cursor() {
            return cursor;
        },
        get hasNext() {
            return Boolean(cursor);
        },
        limit,
        reset() {
            cursor = null;
        },
        update(page) {
            cursor = page.nextCursor;
            return cursor;
        }
    };
}
