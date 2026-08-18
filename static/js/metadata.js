export const METADATA_SCHEMA_VERSION = 1;
export const MAX_ITEM_TAGS = 32;

function normalizeTags(tags) {
    const source = Array.isArray(tags) ? tags : String(tags ?? '').split(',');
    return [...new Set(source
        .filter((tag) => typeof tag === 'string')
        .map((tag) => tag.trim().slice(0, 64))
        .filter(Boolean))]
        .slice(0, MAX_ITEM_TAGS);
}

function normalizeExpiry(value) {
    if (!value) return null;
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) throw new TypeError('Invalid expiry date');
    return date.toISOString();
}

export function normalizeItemMetadata(value = {}) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        throw new TypeError('Item metadata must be an object');
    }
    return {
        tags: normalizeTags(value.tags),
        favorite: Boolean(value.favorite),
        expiresAt: normalizeExpiry(value.expiresAt),
        burnAfterRead: Boolean(value.burnAfterRead)
    };
}

export const normalizePasteMetadata = normalizeItemMetadata;

export function serializePasteMetadata(metadata) {
    return JSON.stringify({
        schemaVersion: METADATA_SCHEMA_VERSION,
        ...normalizeItemMetadata(metadata)
    }, null, 2);
}

export function parsePasteMetadata(source) {
    const parsed = JSON.parse(source);
    if (parsed.schemaVersion !== undefined && parsed.schemaVersion !== METADATA_SCHEMA_VERSION) {
        throw new TypeError('Unsupported metadata version');
    }
    return normalizeItemMetadata(parsed);
}
