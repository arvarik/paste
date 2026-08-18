import assert from 'node:assert/strict';
import test from 'node:test';

import { fetchItemExport, importItem, itemExportURL, MAX_IMPORT_BYTES, parseItemExport } from './transfer.js';

const pasteDocument = {
    schemaVersion: 1,
    kind: 'paste',
    title: 'Imported paste',
    content: 'hello'
};

test('item transfer validates documents and builds export routes', () => {
    assert.deepEqual(parseItemExport(JSON.stringify(pasteDocument)), pasteDocument);
    assert.equal(itemExportURL('paste', 'abc123'), '/api/pastes/abc123/export');
    assert.equal(itemExportURL('diff', 'xyz789'), '/api/saved_diffs/xyz789/export');
    assert.equal(MAX_IMPORT_BYTES, 2 * 1024 * 1024);
    assert.throws(() => parseItemExport('{"schemaVersion":2,"kind":"paste"}'), /schema/);
});

test('item import sends the server export contract', async () => {
    let request;
    const result = await importItem(pasteDocument, async (url, options) => {
        request = { url, options };
        return Response.json({ id: 'abc123', kind: 'paste', editSecret: 'secret', revision: 1 }, { status: 201 });
    });

    assert.equal(request.url, '/api/import');
    assert.equal(request.options.method, 'POST');
    assert.deepEqual(JSON.parse(request.options.body), pasteDocument);
    assert.equal(result.editSecret, 'secret');
});

test('item export uses the response filename and reports API errors', async () => {
    const exported = await fetchItemExport('diff', 'xyz789', async () => new Response('{}', {
        headers: { 'Content-Disposition': 'attachment; filename="xyz789.diff.json"' }
    }));
    assert.equal(exported.filename, 'xyz789.diff.json');

    await assert.rejects(
        importItem(pasteDocument, async () => Response.json({ error: 'A write API token is required' }, { status: 401 })),
        /write API token/
    );
});
