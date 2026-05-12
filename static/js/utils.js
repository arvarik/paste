import { elements } from './dom.js';

export function escapeHtml(unsafe) {
    return unsafe
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

export function isMobileViewport() {
    return window.innerWidth < 640;
}

export function copyToClipboard(text) {
    if (navigator.clipboard && window.isSecureContext) {
        return navigator.clipboard.writeText(text);
    }
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '-9999px';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    return new Promise((resolve, reject) => {
        document.execCommand('copy') ? resolve() : reject(new Error('execCommand failed'));
        document.body.removeChild(ta);
    });
}

export function getLangColorClasses(lang) {
    lang = (lang || 'text').toLowerCase();
    const colors = {
        'python': { bg: 'bg-blue-500/10', text: 'text-blue-600 dark:text-blue-400', border: 'border-blue-500/20', dotClass: 'bg-blue-500' },
        'go': { bg: 'bg-cyan-500/10', text: 'text-cyan-600 dark:text-cyan-400', border: 'border-cyan-500/20', dotClass: 'bg-cyan-500' },
        'typescript': { bg: 'bg-indigo-500/10', text: 'text-indigo-600 dark:text-indigo-400', border: 'border-indigo-500/20', dotClass: 'bg-indigo-500' },
        'java': { bg: 'bg-orange-500/10', text: 'text-orange-600 dark:text-orange-400', border: 'border-orange-500/20', dotClass: 'bg-orange-500' },
        'kotlin': { bg: 'bg-purple-500/10', text: 'text-purple-600 dark:text-purple-400', border: 'border-purple-500/20', dotClass: 'bg-purple-500' },
        'scala': { bg: 'bg-red-500/10', text: 'text-red-600 dark:text-red-400', border: 'border-red-500/20', dotClass: 'bg-red-500' },
        'json': { bg: 'bg-yellow-500/10', text: 'text-yellow-600 dark:text-yellow-400', border: 'border-yellow-500/20', dotClass: 'bg-yellow-500' },
        'bash': { bg: 'bg-emerald-500/10', text: 'text-emerald-600 dark:text-emerald-400', border: 'border-emerald-500/20', dotClass: 'bg-emerald-500' },
        'html': { bg: 'bg-rose-500/10', text: 'text-rose-600 dark:text-rose-400', border: 'border-rose-500/20', dotClass: 'bg-rose-500' },
        'css': { bg: 'bg-sky-500/10', text: 'text-sky-600 dark:text-sky-400', border: 'border-sky-500/20', dotClass: 'bg-sky-500' },
        'markdown': { bg: 'bg-gray-500/10', text: 'text-gray-600 dark:text-gray-400', border: 'border-gray-500/20', dotClass: 'bg-gray-500' },
    };

    return colors[lang] || { bg: 'bg-primary-500/10', text: 'text-primary-600 dark:text-primary-400', border: 'border-primary-500/20', dotClass: 'bg-primary-500' };
}

export function generateTitle(prefix) {
    const now = new Date();
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    const month = months[now.getMonth()];
    const day = now.getDate();
    let hours = now.getHours();
    const ampm = hours >= 12 ? 'PM' : 'AM';
    hours = hours % 12 || 12;
    const mins = now.getMinutes().toString().padStart(2, '0');
    const base = `${prefix}-${month}-${day}-${hours}:${mins}${ampm}`;

    const container = prefix === 'diff' ? elements.diffList : elements.pasteList;
    const existing = container.querySelectorAll('.paste-item .truncate.font-medium');
    let count = 0;
    existing.forEach(el => {
        const t = el.textContent.trim();
        const escaped = base.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        if (t === base || t.match(new RegExp('^' + escaped + '-(\\d+)$'))) {
            count++;
        }
    });
    return count > 0 ? `${base}-${count + 1}` : base;
}

export function detectLanguage(content) {
    const lines = content.split('\n').slice(0, 20);
    const first20 = lines.join('\n');

    for (const line of lines) {
        const trimmed = line.trim();
        if (/^#!\/bin\/(ba)?sh/.test(trimmed) || /^#!\/usr\/bin\/env\s+bash/.test(trimmed)) return 'bash';
        if (/^package\s+\w+/.test(trimmed) && !trimmed.includes(';')) return 'go';
        if (/^(from\s+\S+\s+import|import\s+\S+)/.test(trimmed) && !trimmed.includes('{') && !trimmed.includes('java.')) return 'python';
        if (/^(def|class)\s+\w+.*:/.test(trimmed)) return 'python';
        if (/^public\s+class\s+/.test(trimmed) || /^import\s+java\./.test(trimmed)) return 'java';
        if (/^(fun|val|var)\s+/.test(trimmed) && !trimmed.endsWith(';')) return 'kotlin';
        if (/^import\s+.*\{/.test(trimmed) || /^import\s+.*from\s+['"]/.test(trimmed)) return 'typescript';
        if (/^(object|case\s+class|trait)\s+/.test(trimmed)) return 'scala';
        if (/^<!DOCTYPE/i.test(trimmed) || /^<html/i.test(trimmed)) return 'html';
    }

    const trimmedAll = first20.trim();
    if (trimmedAll.startsWith('{') || trimmedAll.startsWith('[')) {
        try { JSON.parse(content); return 'json'; } catch (e) { }
    }

    const braces = (first20.match(/\{/g) || []).length;
    const colons = (first20.match(/:/g) || []).length;
    const semis = (first20.match(/;/g) || []).length;
    if (braces >= 2 && colons >= 3 && semis >= 3) return 'css';

    let mdSignals = 0;
    for (const line of lines) {
        const trimmed = line.trim();
        if (/^#{1,6}\s+/.test(trimmed)) { mdSignals++; break; }
    }
    if (/^[-*]\s+/.test(lines.find(l => /^[-*]\s+/.test(l.trim()))?.trim() || '')) mdSignals++;
    if (/^\d+\.\s+/.test(lines.find(l => /^\d+\.\s+/.test(l.trim()))?.trim() || '')) mdSignals++;
    if (lines.some(l => /^>\s+/.test(l.trim()))) mdSignals++;
    if (lines.some(l => /^```/.test(l.trim()))) mdSignals++;
    if (lines.some(l => /^---\s*$|^\*\*\*\s*$/.test(l.trim()))) mdSignals++;
    if (/\[.+?\]\(.+?\)/.test(first20)) mdSignals++;
    if (mdSignals >= 2) return 'markdown';

    return null;
}
