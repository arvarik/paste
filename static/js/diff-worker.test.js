import assert from 'node:assert/strict';
import test from 'node:test';

import { MAX_DIFF_BYTES } from './diff-core.js';
import { handleDiffWorkerMessage } from './diff-worker-runtime.js';

function runMessage(payload) {
    let response;
    const accepted = handleDiffWorkerMessage(
        { type: 'compare', requestId: 'test-1', payload },
        (message) => { response = message; }
    );
    assert.equal(accepted, true);
    return response;
}

test('worker messages return structured diff rows', () => {
    const response = runMessage({ base: 'alpha\nbeta\n', compare: 'alpha\ngamma\n' });
    assert.equal(response.type, 'result');
    assert.equal(response.result.additions, 1);
    assert.equal(response.result.deletions, 1);
    assert.equal(response.result.unchanged, 1);
    assert.equal(response.result.rows[1].kind, 'change');
});

test('worker messages support whitespace-insensitive comparisons', () => {
    const response = runMessage({ base: 'alpha  \n', compare: 'alpha\n', ignoreWhitespace: true });
    assert.equal(response.type, 'result');
    assert.equal(response.result.additions, 0);
    assert.equal(response.result.deletions, 0);
});

test('worker messages enforce input size limits', () => {
    const response = runMessage({ base: 'x'.repeat(MAX_DIFF_BYTES + 1), compare: 'x' });
    assert.equal(response.type, 'error');
    assert.match(response.error, /exceeds/);
});

test('worker runtime ignores unsupported messages', () => {
    assert.equal(handleDiffWorkerMessage({ type: 'cancel' }, () => {}), false);
});
