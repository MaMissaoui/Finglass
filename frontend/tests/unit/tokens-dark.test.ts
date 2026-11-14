import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

// Dark mode reaches the app two ways: an explicit dark theme (data-theme="dark")
// and the System theme on a dark OS (the prefers-color-scheme media block). Both
// must map the same tokens, or a token stays light in one of them.
const css = readFileSync(path.resolve(__dirname, '../../src/styles/tokens.css'), 'utf8');

function mappings(block: string): string[] {
  return [...block.matchAll(/(--color-[a-z-]+):\s*var\(--dark-color-[a-z-]+\)/g)]
    .map((match) => match[1] ?? '')
    .sort();
}

function blockAfter(marker: string): string {
  const start = css.indexOf(marker);
  expect(start, marker).toBeGreaterThanOrEqual(0);
  const open = css.indexOf('{', start + marker.length);
  let depth = 0;
  for (let index = open; index < css.length; index += 1) {
    if (css[index] === '{') {
      depth += 1;
    } else if (css[index] === '}') {
      depth -= 1;
      if (depth === 0) {
        return css.slice(open, index);
      }
    }
  }
  throw new Error(`unclosed block after ${marker}`);
}

describe('dark tokens', () => {
  it('maps the same tokens for the dark theme and the System theme on a dark OS', () => {
    const explicit = mappings(
      css.slice(
        css.lastIndexOf(":root[data-theme='dark'] {"),
        css.indexOf('@media (prefers-color-scheme: dark)'),
      ),
    );
    const system = mappings(blockAfter('@media (prefers-color-scheme: dark)'));
    expect(explicit.length).toBeGreaterThan(10);
    expect(system).toEqual(explicit);
  });
});
