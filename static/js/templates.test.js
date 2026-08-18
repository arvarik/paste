import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

import { JSDOM } from 'jsdom';

const templateFiles = [
    'templates/layout/head.html',
    'templates/components/sidebar.html',
    'templates/components/paste_app.html',
    'templates/components/diff_app.html',
    'templates/components/cmdk.html',
    'templates/layout/tail.html'
];

async function assembledDocument() {
    const parts = await Promise.all(templateFiles.map((file) => readFile(file, 'utf8')));
    const html = parts.join('\n').replaceAll('{{.Version}}', 'test');
    return new JSDOM(html).window.document;
}

test('frontend templates use unique IDs and local runtime assets', async () => {
    const document = await assembledDocument();
    const ids = [...document.querySelectorAll('[id]')].map((element) => element.id);
    assert.equal(new Set(ids).size, ids.length);

    const remoteAssets = [...document.querySelectorAll('script[src], link[href]')]
        .map((element) => element.src || element.href)
        .filter((url) => /^https?:/i.test(url));
    assert.deepEqual(remoteAssets, []);
    const inlineScripts = [...document.querySelectorAll('script:not([src])')]
        .filter((script) => script.textContent.trim());
    assert.deepEqual(inlineScripts, []);
});

test('each DOM registry ID exists in the templates', async () => {
    const document = await assembledDocument();
    const source = await readFile('static/js/dom.js', 'utf8');
    const ids = [...source.matchAll(/getElementById\('([^']+)'\)/g)].map((match) => match[1]);
    assert.ok(ids.length > 20);
    for (const id of ids) assert.ok(document.getElementById(id), `Missing template ID: ${id}`);
});

test('frontend templates expose valid page structure and control references', async () => {
    const document = await assembledDocument();
    const headings = [...document.querySelectorAll('h1')];
    assert.equal(headings.length, 1);
    assert.equal(headings[0].id, 'page-heading');
    assert.equal(document.getElementById('sidebar').getAttribute('role'), 'navigation');

    for (const control of document.querySelectorAll('[aria-controls]')) {
        const target = control.getAttribute('aria-controls');
        assert.ok(document.getElementById(target), `Missing aria-controls target: ${target}`);
    }
    assert.equal(document.getElementById('workspace-dropdown-btn').hasAttribute('aria-controls'), false);

    const uiSource = await readFile('static/js/ui.js', 'utf8');
    assert.match(uiSource, /createElement\('h2'\)/);
    assert.doesNotMatch(uiSource, /<h3\b|createElement\('h3'\)/);
    assert.match(uiSource, /modal \? 'dialog' : 'navigation'/);
    const mainSource = await readFile('static/js/main.js', 'utf8');
    assert.match(mainSource, /elements\.sidebar\.addEventListener\('keydown'/);
});

test('file inputs have accessible names and content panes support keyboard scrolling', async () => {
    const document = await assembledDocument();

    for (const input of document.querySelectorAll('input[type="file"]')) {
        const label = input.getAttribute('aria-label');
        assert.ok(label?.trim(), `Missing accessible name for file input: ${input.id}`);
    }

    for (const id of ['view-container', 'diff-output']) {
        const region = document.getElementById(id);
        assert.equal(region.getAttribute('tabindex'), '0', `Missing keyboard scroll support: ${id}`);
        assert.equal(region.getAttribute('role'), 'region', `Missing region role: ${id}`);
        assert.ok(region.getAttribute('aria-label')?.trim(), `Missing region name: ${id}`);
    }

    const revisionSource = await readFile('static/js/revisions.js', 'utf8');
    assert.match(revisionSource, /preview\.tabIndex = 0/);
    assert.match(revisionSource, /Revision \$\{revision\} content/);
});

test('mobile paste actions use a compact accessible overflow menu', async () => {
    const document = await assembledDocument();
    const trigger = document.getElementById('mobile-paste-actions-btn');
    const menu = document.getElementById('paste-more-actions');
    assert.equal(trigger.getAttribute('aria-controls'), menu.id);
    assert.equal(trigger.getAttribute('aria-expanded'), 'false');
    assert.equal(trigger.hasAttribute('aria-haspopup'), false);
    assert.equal(trigger.classList.contains('lg:hidden'), true);
    assert.equal(menu.classList.contains('opaque-popover'), true);
    assert.equal(menu.classList.contains('lg:flex'), true);
    for (const label of ['Copy content', 'Download file', 'Export with metadata', 'Duplicate as new']) {
        assert.ok([...menu.querySelectorAll('span')].some((span) => span.textContent.trim() === label));
    }

    const pasteSource = await readFile('static/js/paste.js', 'utf8');
    assert.match(pasteSource, /mobilePasteActionsBtn\.classList\.toggle\('hidden', mode !== 'view'\)/);
    const mainSource = await readFile('static/js/main.js', 'utf8');
    assert.match(mainSource, /mobilePasteActionsBtn\.setAttribute\('aria-expanded', open \? 'true' : 'false'\)/);
});

test('mobile headers preserve space for titles and palette search', async () => {
    const document = await assembledDocument();
    assert.equal(document.getElementById('save-btn').getAttribute('aria-label'), 'Save paste');
    assert.equal(document.getElementById('save-diff-btn').getAttribute('aria-label'), 'Save diff');
    const close = document.getElementById('cmdk-close-btn');
    assert.equal(close.getAttribute('aria-label'), 'Close command palette');
    assert.equal(close.classList.contains('sm:hidden'), true);
    assert.equal(document.getElementById('cmdk-input').classList.contains('min-w-0'), true);

    const mainSource = await readFile('static/js/main.js', 'utf8');
    assert.match(mainSource, /cmdkCloseBtn\.addEventListener\('click', \(\) => toggleCmdK\(false\)\)/);
});

test('dark interface text uses accessible contrast classes', async () => {
    const files = [
        'templates/components/sidebar.html',
        'templates/components/paste_app.html',
        'templates/components/cmdk.html',
        'static/js/ui.js'
    ];
    const sources = await Promise.all(files.map((file) => readFile(file, 'utf8')));
    assert.doesNotMatch(sources.join('\n'), /dark:text-gray-500/);

    const tailwindConfig = await readFile('static/tailwind.config.cjs', 'utf8');
    assert.match(tailwindConfig, /400:\s*'#60a5fa'/);

    const diffSource = await readFile('static/js/diff.js', 'utf8');
    assert.doesNotMatch(diffSource, /text-gray-400\/60/);
    assert.doesNotMatch(diffSource, /bg-(?:emerald|rose)-500\/30/);
    assert.match(diffSource, /underline decoration-2 underline-offset-2/);
    assert.match(diffSource, /diff-side-by-side-row/);

    const headSource = await readFile('templates/layout/head.html', 'utf8');
    assert.match(headSource, /@media \(max-width: 767px\)[\s\S]*\.diff-side-by-side-row/);
});

test('small light-theme interface text avoids low contrast gray', async () => {
    const uiSource = await readFile('static/js/ui.js', 'utf8');
    assert.doesNotMatch(uiSource, /text-\[10px\] text-gray-400/);
    assert.doesNotMatch(uiSource, /text-\[11px\] font-bold text-gray-400/);
    assert.doesNotMatch(uiSource, /text-xs text-gray-500 dark:text-gray-400 truncate/);

    const sidebarSource = await readFile('templates/components/sidebar.html', 'utf8');
    assert.doesNotMatch(sidebarSource, /text-\[10px\] text-gray-400/);
    const cmdkSource = await readFile('templates/components/cmdk.html', 'utf8');
    assert.doesNotMatch(cmdkSource, /text-\[10px\][^"']* text-gray-400 dark:text-gray-400/);
    assert.doesNotMatch(cmdkSource, /text-xs text-gray-400 dark:text-gray-400/);

    const headSource = await readFile('templates/layout/head.html', 'utf8');
    assert.match(headSource, /:root\s*\{[\s\S]*--surface-code:\s*#f8f9fb/);
    assert.match(headSource, /:root\.dark\s*\{[\s\S]*--surface-code:\s*#151619/);

    const buildSource = await readFile('static/build.mjs', 'utf8');
    assert.match(buildSource, /BROWSERSLIST_IGNORE_OLD_DATA: 'true'/);
});

test('each workspace supplies an active main landmark', async () => {
    const document = await assembledDocument();
    assert.equal(document.getElementById('app-paste').tagName, 'MAIN');
    assert.equal(document.getElementById('app-diff').tagName, 'MAIN');
    assert.equal(document.getElementById('app-diff').hasAttribute('inert'), true);
});

test('saved diff options expose retention and organization controls', async () => {
    const document = await assembledDocument();
    for (const id of ['diff-tags', 'diff-favorite', 'diff-expires-at', 'diff-burn-after-read']) {
        assert.ok(document.getElementById(id), `Missing saved diff option: ${id}`);
    }
});
