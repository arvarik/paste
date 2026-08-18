import assert from 'node:assert/strict';
import test from 'node:test';

import { MAX_ITEM_TAGS, normalizePasteMetadata, parsePasteMetadata, serializePasteMetadata } from './metadata.js';

test('paste metadata normalization limits and deduplicates tags', () => {
    const metadata = normalizePasteMetadata({
        tags: [' api ', 'api', 'example'],
        favorite: 1,
        expiresAt: '2030-01-01T00:00:00Z'
    });
    assert.deepEqual(metadata.tags, ['api', 'example']);
    assert.equal(metadata.favorite, true);
    assert.equal(metadata.expiresAt, '2030-01-01T00:00:00.000Z');

    const tags = Array.from({ length: MAX_ITEM_TAGS + 1 }, (_, index) => `tag-${index}`);
    assert.equal(normalizePasteMetadata({ tags }).tags.length, MAX_ITEM_TAGS);
});

test('paste metadata exports and imports a versioned document', () => {
    const source = serializePasteMetadata({ tags: ['one'], burnAfterRead: true });
    assert.deepEqual(parsePasteMetadata(source), {
        tags: ['one'],
        favorite: false,
        expiresAt: null,
        burnAfterRead: true
    });
});

test('paste metadata rejects invalid expiry dates', () => {
    assert.throws(() => normalizePasteMetadata({ expiresAt: 'never' }), /expiry/);
});
