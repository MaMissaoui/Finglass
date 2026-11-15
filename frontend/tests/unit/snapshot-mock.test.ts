import { readFileSync } from 'node:fs';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSnapshotApi, isValidSnapshotPath } from '../../src/lib/snapshot.mock';

// A real export of the synthetic backup (invented data); Go writes empty lists
// as null.
const fixture = readFileSync(new URL('../fixtures/snapshot.json', import.meta.url), 'utf8');

describe('snapshot scenario paths', () => {
  it('accepts same-origin relative paths', () => {
    expect(isValidSnapshotPath('snapshot.json')).toBe(true);
    expect(isValidSnapshotPath('/exports/snapshot.json')).toBe(true);
    expect(isValidSnapshotPath('exports/2026/snapshot.json')).toBe(true);
  });

  it('rejects schemes, protocol-relative paths and parent segments', () => {
    expect(isValidSnapshotPath('')).toBe(false);
    expect(isValidSnapshotPath('https://example.com/snapshot.json')).toBe(false);
    expect(isValidSnapshotPath('file:///tmp/snapshot.json')).toBe(false);
    expect(isValidSnapshotPath('//example.com/snapshot.json')).toBe(false);
    expect(isValidSnapshotPath('../snapshot.json')).toBe(false);
    expect(isValidSnapshotPath('exports/../../snapshot.json')).toBe(false);
    expect(isValidSnapshotPath('exports%2F..%2F..%2Fsnapshot.json')).toBe(false);
    expect(isValidSnapshotPath('exports\\snapshot.json')).toBe(false);
  });
});

describe('snapshot scenario data', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function stubFetch(): void {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(fixture, { status: 200 })),
    );
  }

  it('turns the null lists of a Go export into empty lists', async () => {
    expect(fixture).toContain('"leftoverLines": null');
    stubFetch();
    const api = createSnapshotApi('snapshot.json');
    const shell = await api.state();
    expect(shell.leftoverLines).toEqual([]);
    expect(shell.migrationSummaryLines).toEqual([]);
  });

  it('is read-only: a month change returns the exported dashboard', async () => {
    stubFetch();
    const api = createSnapshotApi('snapshot.json');
    const before = await api.dashboardState();
    const after = await api.dashboardPreviousMonth();
    expect(after).toEqual(before);
  });
});
