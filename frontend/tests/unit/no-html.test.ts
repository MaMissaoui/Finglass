import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const srcDir = fileURLToPath(new URL('../../src/', import.meta.url));

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

describe('template safety', () => {
  it('never uses {@html} in src/', async () => {
    const offenders: string[] = [];

    for (const file of await filesUnder(srcDir)) {
      const contents = await readFile(file, 'utf8');
      if (contents.includes('{@html')) {
        offenders.push(path.relative(srcDir, file));
      }
    }

    expect(offenders).toEqual([]);
  });
});
