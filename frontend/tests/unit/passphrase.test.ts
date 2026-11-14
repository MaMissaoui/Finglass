import { describe, expect, it } from 'vitest';
import { passphraseLength } from '../../src/lib/passphrase';

// The counts are pinned in internal/core/secret's
// TestLengthCountMatchesTheFrontend, which counts the same strings with NFC and
// utf8.RuneCount. Both lists must stay in step.
describe('passphraseLength', () => {
  it('counts like Go counts NFC runes', () => {
    const cases: Array<[string, number]> = [
      ['', 0],
      ['abc', 3],
      ['correct horse battery staple', 28],
      ['Über', 4],
      ['U\u0308ber', 4],
      ['a😀b', 3],
      ['👍🏽', 2],
      ['👨\u200d👩\u200d👧\u200d👦', 7],
    ];
    for (const [value, count] of cases) {
      expect(passphraseLength(value), JSON.stringify(value)).toBe(count);
    }
  });
});
