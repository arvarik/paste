import assert from 'node:assert/strict';
import test from 'node:test';

import { applyDiffMetadata, readDiffMetadata, resetDiffMetadata } from './diff-metadata.js';

test('saved diff metadata survives a load and save cycle', () => {
    const state = {};
    applyDiffMetadata(state, {
        tags: ['review', 'release'],
        favorite: true,
        expiresAt: '2030-01-01T00:00:00Z',
        burnAfterRead: true
    });

    assert.deepEqual(readDiffMetadata(state), {
        tags: ['review', 'release'],
        favorite: true,
        expiresAt: '2030-01-01T00:00:00.000Z',
        burnAfterRead: true
    });
});

test('saved diff controls update state and reset visibly', () => {
    const state = {};
    const controls = {
        tags: { value: 'release, urgent' },
        favorite: { checked: true },
        expiresAt: { value: '2030-01-02T03:04' },
        burnAfterRead: { checked: true }
    };

    const metadata = readDiffMetadata(state, controls);
    assert.deepEqual(metadata.tags, ['release', 'urgent']);
    assert.equal(state.currentDiffFavorite, true);
    assert.equal(Number.isNaN(Date.parse(state.currentDiffExpiresAt)), false);
    assert.equal(state.currentDiffBurnAfterRead, true);

    applyDiffMetadata(state, { tags: ['loaded'], favorite: false }, controls);
    assert.equal(controls.tags.value, 'loaded');
    assert.equal(controls.favorite.checked, false);

    resetDiffMetadata(state, controls);
    assert.equal(controls.tags.value, '');
    assert.equal(controls.expiresAt.value, '');
    assert.equal(controls.burnAfterRead.checked, false);
});
