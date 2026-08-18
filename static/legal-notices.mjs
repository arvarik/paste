import { readdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

export const DISTRIBUTED_PACKAGES = Object.freeze([
    'marked',
    'dompurify',
    'prismjs',
    'diff',
    'tailwindcss',
    '@tailwindcss/typography'
]);

export async function buildThirdPartyNotices(projectDir) {
    const sections = [
        'THIRD-PARTY SOFTWARE NOTICES',
        '',
        'This distribution contains software from the packages listed below.',
        'Each package retains its applicable copyright and license terms.'
    ];

    for (const packageName of DISTRIBUTED_PACKAGES) {
        const packageDir = resolve(projectDir, 'node_modules', packageName);
        const metadata = JSON.parse(await readFile(resolve(packageDir, 'package.json'), 'utf8'));
        const licenseFiles = (await readdir(packageDir))
            .filter((name) => /^licen[cs]e(?:[.-]|$)/i.test(name))
            .sort();
        if (!licenseFiles.length) throw new Error(`No license file found for ${packageName}`);

        sections.push('', '='.repeat(80), `${metadata.name}@${metadata.version}`, `Declared license: ${metadata.license || 'See license text'}`);
        for (const licenseFile of licenseFiles) {
            sections.push('', `--- ${licenseFile} ---`, '', (await readFile(resolve(packageDir, licenseFile), 'utf8')).trim());
        }
    }

    return `${sections.join('\n')}\n`;
}
