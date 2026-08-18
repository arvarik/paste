import assert from 'node:assert/strict';
import test from 'node:test';
import { JSDOM } from 'jsdom';

import { createRevisionRow, fetchItemRevision, restoreItemRevision, revisionPreviewText } from './revisions.js';

const secureID = '0123456789abcdef0123456789abcdef';

test('revision requests support secure IDs and decode content', async () => {
    let requestURL;
    const document = await fetchItemRevision('paste', secureID, 2, async (url, options) => {
        requestURL = url;
        assert.equal(options.headers.Accept, 'application/json');
        return Response.json({ pasteContent: 'older content' });
    });

    assert.equal(requestURL, `/api/pastes/${secureID}/revisions/2`);
    assert.equal(revisionPreviewText('paste', document), 'older content');
});

test('revision restore sends the edit secret and expected current revision', async () => {
    let requestOptions;
    const result = await restoreItemRevision(
        'diff',
        'abc123',
        2,
        5,
        { 'X-Edit-Secret': 'edit-secret' },
        async (url, options) => {
            assert.equal(url, '/api/saved_diffs/abc123/revisions/2/restore');
            requestOptions = options;
            return Response.json({ id: 'abc123', revision: 6 });
        }
    );

    assert.equal(requestOptions.method, 'POST');
    assert.equal(requestOptions.headers.get('X-Edit-Secret'), 'edit-secret');
    assert.deepEqual(JSON.parse(requestOptions.body), { expectedRevision: 5 });
    assert.equal(result.revision, 6);
});

test('revision restore reports conflicts', async () => {
    await assert.rejects(
        restoreItemRevision('paste', 'abc123', 1, 2, {}, async () => Response.json({}, { status: 409 })),
        /changed elsewhere/
    );
});

test('diff revision previews include both sides', () => {
    const preview = revisionPreviewText('diff', {
        diff: { base: 'one', compare: 'two', baseContent: 'before', compareContent: 'after' }
    });
    assert.match(preview, /Base: one/);
    assert.match(preview, /before/);
    assert.match(preview, /after/);
});

test('revision controls view content and restore with visible state changes', async () => {
    const window = new JSDOM('').window;
    const requests = [];
    const messages = [];
    let refreshedRevision = 0;
    const request = async (url, options) => {
        requests.push({ url, options });
        if (options.method === 'POST') return Response.json({ id: 'abc123', revision: 4 });
        return Response.json({ pasteContent: '<script>plain text only</script>' });
    };
    const row = createRevisionRow({
        kind: 'paste',
        id: 'abc123',
        revision: 2,
        createdAt: '2030-01-01T00:00:00Z',
        currentRevision: () => 3,
        canRestore: () => true,
        restoreHeaders: () => ({ 'X-Edit-Secret': 'secret' }),
        onRestored: async (result) => { refreshedRevision = result.revision; },
        onMessage: (message, error) => messages.push({ message, error }),
        request,
        ownerDocument: window.document
    });
    const [viewButton, restoreButton] = row.querySelectorAll('button');
    const preview = row.querySelector('pre');

    assert.equal(viewButton.getAttribute('aria-expanded'), 'false');
    viewButton.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(preview.hidden, false);
    assert.equal(preview.textContent, '<script>plain text only</script>');
    assert.equal(preview.querySelector('script'), null);
    assert.equal(viewButton.textContent, 'Hide');
    assert.equal(viewButton.getAttribute('aria-expanded'), 'true');

    restoreButton.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    assert.equal(requests[1].options.headers.get('X-Edit-Secret'), 'secret');
    assert.deepEqual(JSON.parse(requests[1].options.body), { expectedRevision: 3 });
    assert.equal(refreshedRevision, 4);
    assert.deepEqual(messages, [{ message: 'Revision 2 restored', error: false }]);
    window.close();
});
