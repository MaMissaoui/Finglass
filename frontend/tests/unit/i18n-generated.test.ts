import { describe, expect, it } from 'vitest';
import de from '../../src/lib/generated/i18n/de.json';
import en from '../../src/lib/generated/i18n/en.json';
import fr from '../../src/lib/generated/i18n/fr.json';
import { keys } from '../../src/lib/generated/i18n/keys';

const tables = { en, de, fr };

describe('generated i18n tables', () => {
  for (const [language, table] of Object.entries(tables)) {
    it(`${language} has exactly the generated keys in order`, () => {
      expect(Object.keys(table)).toEqual([...keys]);
    });

    it(`${language} has a non-empty string for every key`, () => {
      for (const key of keys) {
        const value = table[key];
        expect(value, `${language}.${key}`).toBeTypeOf('string');
        expect(value.length, `${language}.${key}`).toBeGreaterThan(0);
      }
    });
  }
});
