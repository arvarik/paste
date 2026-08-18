import assert from 'node:assert/strict';
import test from 'node:test';

import { getEditSecret, removeEditSecret, saveEditSecret, withEditSecret } from './secrets.js';

function memoryStorage() {
    const data = new Map();
    return {
        getItem: (key) => data.get(key) ?? null,
        removeItem: (key) => data.delete(key),
        setItem: (key, value) => data.set(key, value)
    };
}

test('edit secrets stay scoped to a resource type and ID', () => {
    const storage = memoryStorage();
    assert.equal(saveEditSecret('paste', 'abc123', 'secret-a', storage), true);
    assert.equal(getEditSecret('paste', 'abc123', storage), 'secret-a');
    assert.equal(getEditSecret('diff', 'abc123', storage), null);
    assert.equal(withEditSecret({}, 'paste', 'abc123', storage).get('X-Edit-Secret'), 'secret-a');
    assert.equal(removeEditSecret('paste', 'abc123', storage), true);
    assert.equal(getEditSecret('paste', 'abc123', storage), null);
});

test('edit secret storage rejects invalid resource keys', () => {
    assert.equal(saveEditSecret('paste', '../bad', 'secret', memoryStorage()), false);
});
