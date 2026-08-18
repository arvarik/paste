import assert from 'node:assert/strict';
import test from 'node:test';

import { buildCursorURL, createCursorState, normalizeCursorPage } from './pagination.js';

test('buildCursorURL encodes cursor pagination values', () => {
    assert.equal(
        buildCursorURL('/api/pastes', { cursor: 'next/page', limit: 25, query: 'go api' }),
        '/api/pastes?limit=25&cursor=next%2Fpage&q=go+api'
    );
});

test('normalizeCursorPage supports legacy and cursor responses', () => {
    assert.deepEqual(normalizeCursorPage([1, 2]), { items: [1, 2], nextCursor: null, isLegacy: true });
    assert.deepEqual(
        normalizeCursorPage({ items: [3], nextCursor: 'cursor-2' }),
        { items: [3], nextCursor: 'cursor-2', isLegacy: false }
    );
    assert.deepEqual(
        normalizeCursorPage({ items: null }),
        { items: [], nextCursor: null, isLegacy: false }
    );
});

test('cursor state tracks and resets the next cursor', () => {
    const state = createCursorState(25);
    state.update({ nextCursor: 'next' });
    assert.equal(state.hasNext, true);
    assert.equal(state.cursor, 'next');
    assert.equal(state.limit, 25);
    state.reset();
    assert.equal(state.hasNext, false);
});
