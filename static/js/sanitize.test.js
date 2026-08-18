import assert from 'node:assert/strict';
import test from 'node:test';

import createDOMPurify from 'dompurify';
import { JSDOM } from 'jsdom';
import { marked } from 'marked';

import { sanitizeMarkdown } from './sanitize.js';

test('sanitizeMarkdown removes executable markup', () => {
    const window = new JSDOM('').window;
    const purifier = createDOMPurify(window);
    const html = sanitizeMarkdown('[unsafe](javascript:alert(1)) <img src=x onerror=alert(2)><script>alert(3)</script>', {
        parse: marked.parse,
        sanitize: purifier.sanitize
    });

    assert.doesNotMatch(html, /javascript:/i);
    assert.doesNotMatch(html, /onerror/i);
    assert.doesNotMatch(html, /<script/i);
    window.close();
});

test('sanitizeMarkdown rejects forms and interactive controls', () => {
    const window = new JSDOM('').window;
    const purifier = createDOMPurify(window);
    const html = sanitizeMarkdown(`
# Safe heading

<form action="/submit">
  <label>Name <input name="name" autofocus></label>
  <select><option>One</option></select>
  <textarea>Text</textarea>
  <button type="submit">Submit</button>
</form>

<details open><summary>Toggle</summary>Hidden</details>

\`\`\`js
const safe = true;
\`\`\`
`, {
        parse: marked.parse,
        sanitize: purifier.sanitize
    });

    assert.match(html, /<h1>Safe heading<\/h1>/);
    assert.match(html, /<code class="language-js">/);
    assert.doesNotMatch(html, /<(?:form|input|label|select|option|textarea|button|details|summary)\b/i);
    window.close();
});

test('sanitizeMarkdown keeps only explicit Markdown attributes', () => {
    const window = new JSDOM('').window;
    const purifier = createDOMPurify(window);
    const html = sanitizeMarkdown('<a href="/safe" target="_blank" style="color:red" onclick="alert(1)" data-test="x">link</a>', {
        parse: marked.parse,
        sanitize: purifier.sanitize
    });

    assert.match(html, /href="\/safe"/);
    assert.doesNotMatch(html, /target=|style=|onclick=|data-test=/i);
    window.close();
});

test('sanitizeMarkdown strips overlay classes and keeps code language classes', () => {
    const window = new JSDOM('').window;
    const purifier = createDOMPurify(window);
    const html = sanitizeMarkdown(`
<a class="fixed inset-0 z-50" href="https://example.invalid">overlay</a>

<code class="language-js fixed inset-0">const safe = true;</code>
`, {
        parse: marked.parse,
        sanitize: purifier.sanitize
    });

    assert.doesNotMatch(html, /class="fixed|inset-0|z-50/);
    assert.match(html, /<code class="language-js">/);
    window.close();
});

test('sanitizeMarkdown rejects missing dependencies', () => {
    assert.throws(() => sanitizeMarkdown('# title', {}), /parser/);
});
