import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyTheme, MONOGRAM_COLOURS, monogramColour } from '../../src/lib/theme';

// The same eight colours, in the same order, as `MONOGRAM_COLOURS` in
// rust-ref/crates/finglass/src/lib.rs.
const COLOURS = [0xb45309, 0x0e7490, 0x7c3aed, 0xbe123c, 0x15803d, 0x1d4ed8, 0xa21caf, 0xc2410c];

function relativeLuminance(hex: number): number {
  const channels = [(hex >> 16) & 0xff, (hex >> 8) & 0xff, hex & 0xff].map((channel) => {
    const value = channel / 255;
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

describe('theme', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('keeps the monogram colours in the Rust order', () => {
    expect(MONOGRAM_COLOURS.map((colour) => Number.parseInt(colour.slice(1), 16))).toEqual(COLOURS);
  });

  it('monogram_colours_reach_white_contrast', () => {
    for (const hex of COLOURS) {
      const ratio = 1.05 / (relativeLuminance(hex) + 0.05);
      expect(ratio, `#${hex.toString(16).toUpperCase()} contrast`).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('returns the stable colour for an index and the first colour otherwise', () => {
    expect(monogramColour(0)).toBe('#B45309');
    expect(monogramColour(7)).toBe('#C2410C');
    expect(monogramColour(8)).toBe('#B45309');
  });

  it('sets, replaces and removes data-theme without storing anything', () => {
    const root = {
      dataset: {} as Record<string, string>,
      removeAttribute(name: string) {
        if (name === 'data-theme') {
          delete this.dataset.theme;
        }
      },
    };
    vi.stubGlobal('document', { documentElement: root });

    applyTheme('dark');
    expect(root.dataset.theme).toBe('dark');
    applyTheme('light');
    expect(root.dataset.theme).toBe('light');
    applyTheme('system');
    expect(root.dataset.theme).toBeUndefined();
  });
});
