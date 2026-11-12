import { readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { bankLogoNames, iconNames } from '../../src/lib/icons';

const assetsDir = fileURLToPath(new URL('../../src/assets/', import.meta.url));

async function stems(folder: string): Promise<string[]> {
  const entries = await readdir(path.join(assetsDir, folder), { withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile() && entry.name.endsWith('.svg'))
    .map((entry) => entry.name.slice(0, -'.svg'.length))
    .sort();
}

describe('icons', () => {
  it('has exactly one IconName for every SVG on disk', async () => {
    expect([...iconNames].sort()).toEqual(await stems('icons'));
  });

  it('has exactly one bank logo name for every bank SVG on disk', async () => {
    expect([...bankLogoNames].sort()).toEqual(await stems('banks'));
  });
});
