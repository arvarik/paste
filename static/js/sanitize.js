export const MARKDOWN_ALLOWED_TAGS = Object.freeze([
    'a', 'abbr', 'b', 'blockquote', 'br', 'code', 'del', 'div', 'em',
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'hr', 'i', 'img', 'kbd',
    'li', 'mark', 'ol', 'p', 'pre', 's', 'span', 'strong', 'sub', 'sup',
    'table', 'tbody', 'td', 'tfoot', 'th', 'thead', 'tr', 'ul'
]);

export const MARKDOWN_ALLOWED_ATTRIBUTES = Object.freeze([
    'align', 'alt', 'class', 'colspan', 'height', 'href', 'reversed',
    'rowspan', 'src', 'start', 'title', 'value', 'width'
]);

export function sanitizeMarkdown(raw, { parse, sanitize }) {
    if (typeof raw !== 'string') throw new TypeError('Markdown input must be a string');
    if (typeof parse !== 'function') throw new TypeError('A Markdown parser is required');
    if (typeof sanitize !== 'function') throw new TypeError('An HTML sanitizer is required');

    const parsed = parse(raw);
    if (typeof parsed !== 'string') throw new TypeError('The Markdown parser must return a string');

    const sanitized = sanitize(parsed, {
        ALLOWED_TAGS: [...MARKDOWN_ALLOWED_TAGS],
        ALLOWED_ATTR: [...MARKDOWN_ALLOWED_ATTRIBUTES],
        ALLOW_ARIA_ATTR: false,
        ALLOW_DATA_ATTR: false,
        RETURN_DOM: true
    });

    if (!sanitized || typeof sanitized.querySelectorAll !== 'function') {
        throw new TypeError('The HTML sanitizer must return a DOM tree');
    }

    for (const element of sanitized.querySelectorAll('[class]')) {
        if (element.tagName !== 'CODE') {
            element.removeAttribute('class');
            continue;
        }

        const languageClasses = [...element.classList]
            .filter((name) => /^language-[a-z0-9][a-z0-9-]{0,31}$/.test(name));
        if (languageClasses.length === 0) {
            element.removeAttribute('class');
        } else {
            element.className = languageClasses.join(' ');
        }
    }

    return sanitized.innerHTML;
}
