import { diffLines, diffWordsWithSpace } from 'diff';

export const MAX_DIFF_BYTES = 1024 * 1024;
export const MAX_DIFF_LINES = 20_000;

function countLines(value) {
    if (!value) return 0;
    return (value.match(/\n/g)?.length ?? 0) + 1;
}

function splitPartLines(value) {
    const lines = value.split('\n');
    if (value.endsWith('\n')) lines.pop();
    return lines;
}

function inlineSegments(oldText, newText) {
    const parts = diffWordsWithSpace(oldText, newText);
    const base = [];
    const compare = [];

    for (const part of parts) {
        if (part.removed) {
            base.push({ type: 'removed', text: part.value });
        } else if (part.added) {
            compare.push({ type: 'added', text: part.value });
        } else {
            base.push({ type: 'same', text: part.value });
            compare.push({ type: 'same', text: part.value });
        }
    }
    return { base, compare };
}

export function validateDiffInput(base, compare, limits = {}) {
    if (typeof base !== 'string' || typeof compare !== 'string') {
        throw new TypeError('Diff input must be text');
    }
    const maxBytes = limits.maxBytes ?? MAX_DIFF_BYTES;
    const maxLines = limits.maxLines ?? MAX_DIFF_LINES;
    const bytes = new TextEncoder().encode(base).byteLength + new TextEncoder().encode(compare).byteLength;
    const lines = countLines(base) + countLines(compare);
    if (bytes > maxBytes) throw new RangeError(`Diff input exceeds ${maxBytes} bytes`);
    if (lines > maxLines) throw new RangeError(`Diff input exceeds ${maxLines} lines`);
    return { bytes, lines };
}

export function computeDiff({ base, compare, ignoreWhitespace = false }, limits) {
    const metrics = validateDiffInput(base, compare, limits);
    const parts = diffLines(base, compare, { ignoreWhitespace, newlineIsToken: false });
    const rows = [];
    let additions = 0;
    let deletions = 0;
    let unchanged = 0;
    let baseLine = 1;
    let compareLine = 1;

    for (let index = 0; index < parts.length; index++) {
        const part = parts[index];
        if (part.removed && parts[index + 1]?.added) {
            const next = parts[++index];
            const removedLines = splitPartLines(part.value);
            const addedLines = splitPartLines(next.value);
            const count = Math.max(removedLines.length, addedLines.length);

            for (let lineIndex = 0; lineIndex < count; lineIndex++) {
                const oldText = removedLines[lineIndex];
                const newText = addedLines[lineIndex];
                const segments = oldText !== undefined && newText !== undefined
                    ? inlineSegments(oldText, newText)
                    : {
                        base: oldText === undefined ? [] : [{ type: 'removed', text: oldText }],
                        compare: newText === undefined ? [] : [{ type: 'added', text: newText }]
                    };
                rows.push({
                    kind: 'change',
                    baseLine: oldText === undefined ? null : baseLine++,
                    compareLine: newText === undefined ? null : compareLine++,
                    base: segments.base,
                    compare: segments.compare
                });
                if (oldText !== undefined) deletions++;
                if (newText !== undefined) additions++;
            }
        } else if (part.added || part.removed) {
            for (const text of splitPartLines(part.value)) {
                const isAdded = Boolean(part.added);
                rows.push({
                    kind: 'change',
                    baseLine: isAdded ? null : baseLine++,
                    compareLine: isAdded ? compareLine++ : null,
                    base: isAdded ? [] : [{ type: 'removed', text }],
                    compare: isAdded ? [{ type: 'added', text }] : []
                });
                if (isAdded) additions++;
                else deletions++;
            }
        } else {
            for (const text of splitPartLines(part.value)) {
                rows.push({
                    kind: 'context',
                    baseLine: baseLine++,
                    compareLine: compareLine++,
                    base: [{ type: 'same', text }],
                    compare: [{ type: 'same', text }]
                });
                unchanged++;
            }
        }
    }

    return { rows, additions, deletions, unchanged, metrics };
}
