import { describe, expect, it } from 'vitest';
import { isValidSnapshotPath } from '../../src/lib/snapshot.mock';

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
