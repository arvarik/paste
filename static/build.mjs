import { execFileSync } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { build } from 'esbuild';
import { buildThirdPartyNotices } from './legal-notices.mjs';

const staticDir = dirname(fileURLToPath(import.meta.url));
const projectDir = dirname(staticDir);
const outputDir = resolve(staticDir, 'dist');

await rm(outputDir, { force: true, recursive: true });
await mkdir(outputDir, { recursive: true });

const commonBuildOptions = {
    bundle: true,
    legalComments: 'none',
    minify: true,
    platform: 'browser',
    target: ['es2020']
};

await Promise.all([
    build({
        ...commonBuildOptions,
        entryPoints: [resolve(staticDir, 'js/main.js')],
        format: 'esm',
        outfile: resolve(outputDir, 'app.js')
    }),
    build({
        ...commonBuildOptions,
        entryPoints: [resolve(staticDir, 'js/diff-worker.js')],
        format: 'iife',
        outfile: resolve(outputDir, 'diff-worker.js')
    }),
    build({
        ...commonBuildOptions,
        entryPoints: [resolve(staticDir, 'js/theme.js')],
        format: 'iife',
        outfile: resolve(outputDir, 'theme.js')
    }),
    build({
        ...commonBuildOptions,
        entryPoints: [resolve(staticDir, 'css/vendor.css')],
        outfile: resolve(outputDir, 'vendor.css')
    })
]);

execFileSync(process.execPath, [
    resolve(projectDir, 'node_modules/tailwindcss/lib/cli.js'),
    '--config', resolve(staticDir, 'tailwind.config.cjs'),
    '--input', resolve(staticDir, 'css/input.css'),
    '--output', resolve(outputDir, 'app.css'),
    '--minify'
], {
    cwd: projectDir,
    env: { ...process.env, BROWSERSLIST_IGNORE_OLD_DATA: 'true' },
    stdio: 'inherit'
});

await writeFile(
    resolve(outputDir, 'THIRD_PARTY_NOTICES.txt'),
    await buildThirdPartyNotices(projectDir),
    'utf8'
);
