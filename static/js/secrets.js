const STORAGE_PREFIX = 'paste.edit-secret.v1';
const VALID_RESOURCE_TYPES = new Set(['paste', 'diff']);

function getStorage(storage) {
    return storage ?? globalThis.localStorage;
}

function storageKey(resourceType, id) {
    if (!VALID_RESOURCE_TYPES.has(resourceType)) throw new TypeError('Invalid resource type');
    if (!/^[a-zA-Z0-9_-]+$/.test(id)) throw new TypeError('Invalid resource ID');
    return `${STORAGE_PREFIX}.${resourceType}.${id}`;
}

export function saveEditSecret(resourceType, id, secret, storage) {
    if (typeof secret !== 'string' || !secret) return false;
    try {
        getStorage(storage).setItem(storageKey(resourceType, id), secret);
        return true;
    } catch {
        return false;
    }
}

export function getEditSecret(resourceType, id, storage) {
    if (!id) return null;
    try {
        return getStorage(storage).getItem(storageKey(resourceType, id));
    } catch {
        return null;
    }
}

export function removeEditSecret(resourceType, id, storage) {
    if (!id) return false;
    try {
        getStorage(storage).removeItem(storageKey(resourceType, id));
        return true;
    } catch {
        return false;
    }
}

export function withEditSecret(headers, resourceType, id, storage) {
    const result = new Headers(headers);
    const secret = getEditSecret(resourceType, id, storage);
    if (secret) result.set('X-Edit-Secret', secret);
    return result;
}
