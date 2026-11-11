import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Api } from '../../src/lib/api';
import { createMockApi } from '../../src/lib/api.mock';

const api: Api = createMockApi();

afterEach(() => {
  vi.useRealTimers();
});

describe('api.mock', () => {
  it('implements the Api interface and returns fixed info', async () => {
    const info = await api.info();
    expect(info).toEqual({
      name: 'Finglass',
      version: '0.4.0-dev',
      goVersion: 'go1.27.4',
      os: 'darwin',
      arch: 'arm64',
    });
  });

  it('greets a trimmed name and refuses an empty one', async () => {
    await expect(api.greet('  Ada  ')).resolves.toBe('Hello, Ada');
    await expect(api.greet('   ')).rejects.toThrow('name must not be empty');
  });

  it('ticks once per second until unsubscribed', async () => {
    vi.useFakeTimers();

    const counts: number[] = [];
    const stop = api.onTick((payload) => counts.push(payload.count));

    await vi.advanceTimersByTimeAsync(2000);
    expect(counts).toEqual([1, 2]);

    stop();
    await vi.advanceTimersByTimeAsync(2000);
    expect(counts).toEqual([1, 2]);
  });
});
