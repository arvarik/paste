import assert from 'node:assert/strict';
import test from 'node:test';

import { isPasteReference } from './identifiers.js';

test('paste references accept legacy and current public IDs', () => {
    assert.equal(isPasteReference('abc123'), true);
    assert.equal(isPasteReference('0123456789abcdef0123456789abcdef'), true);
    assert.equal(isPasteReference('too-short'), false);
    assert.equal(isPasteReference('0123456789abcdef0123456789abcde-'), false);
});
