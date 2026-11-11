import { describe, expect, it } from 'vitest';
import { languages, messageKeys, messages } from '../../src/lib/i18n';

describe('i18n', () => {
  it('has every key in EN, DE and FR with a non-empty string', () => {
    for (const language of languages) {
      for (const key of messageKeys) {
        const value = messages[language][key];
        expect(value, `${language}.${key}`).toBeTypeOf('string');
        expect(value.trim(), `${language}.${key}`).not.toBe('');
      }
    }
  });

  it('does not carry keys outside the declared table', () => {
    for (const language of languages) {
      expect(Object.keys(messages[language]).sort()).toEqual([...messageKeys].sort());
    }
  });
});
