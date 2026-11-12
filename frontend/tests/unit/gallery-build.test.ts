import { readdir, readFile, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, describe, expect, it } from 'vitest';
import { build } from 'vite';

const frontendRoot = fileURLToPath(new URL('../..', import.meta.url));
const outDirName = 'dist-gallery-check';
const outDir = path.join(frontendRoot, outDirName);

// This string exists only in Gallery.svelte. If the production build ever
// pulls the gallery in, the marker appears in a bundle.
const GALLERY_MARKER = 'FINGLASS_GALLERY_ONLY_7A';

async function filesUnder(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const files: string[] = [];

  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await filesUnder(fullPath)));
    } else {
      files.push(fullPath);
    }
  }

  return files;
}

afterAll(async () => {
  await rm(outDir, { recursive: true, force: true });
});

describe('production bundle', () => {
  it('does not contain the gallery', async () => {
    await build({
      root: frontendRoot,
      mode: 'production',
      logLevel: 'silent',
      build: {
        outDir: outDirName,
        emptyOutDir: true,
      },
    });

    const files = await filesUnder(outDir);
    expect(files.length).toBeGreaterThan(0);

    const offenders: string[] = [];
    for (const file of files) {
      if (!file.endsWith('.js')) {
        continue;
      }
      const contents = await readFile(file, 'utf8');
      if (contents.includes(GALLERY_MARKER)) {
        offenders.push(path.relative(outDir, file));
      }
    }

    expect(offenders).toEqual([]);
  }, 120_000);
});
