import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { buildThirdPartyNotices, DISTRIBUTED_PACKAGES } from '../legal-notices.mjs';

const projectDir = resolve(dirname(fileURLToPath(import.meta.url)), '../..');

test('production notices include every distributed dependency license', async () => {
    const notices = await buildThirdPartyNotices(projectDir);
    for (const packageName of DISTRIBUTED_PACKAGES) {
        assert.match(notices, new RegExp(`${packageName.replace('/', '\\/')}@`));
    }
    assert.match(notices, /MIT License/);
    assert.match(notices, /BSD 3-Clause License/);
    assert.match(notices, /Apache License/);
    assert.match(notices, /Mozilla Public License Version 2\.0/);
});
