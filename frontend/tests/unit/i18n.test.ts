import { describe, expect, it } from 'vitest';
import { languages, messageKeys, messages, translate } from '../../src/lib/i18n';

describe('i18n', () => {
  it('has every key in EN, DE and FR with a non-empty string', () => {
    for (const language of languages) {
      for (const key of messageKeys) {
        const value = messages[language][key];
        expect(value, `${language}.${key}`).toBeTypeOf('string');
        expect(value?.trim(), `${language}.${key}`).not.toBe('');
      }
    }
  });

  it('does not carry keys outside the declared table', () => {
    for (const language of languages) {
      expect(Object.keys(messages[language]).sort()).toEqual([...messageKeys].sort());
    }
  });

  it('shows an unknown key by its own name', () => {
    expect(translate('en', 'NotAKey')).toBe('NotAKey');
    expect(translate('de', 'NotAKey')).toBe('NotAKey');
  });

  it('formats the same keys in all three languages', () => {
    expect(translate('en', 'PassphraseTooShort', 10)).toBe('Use at least 10 characters.');
    expect(translate('de', 'PassphraseTooShort', 10)).toBe('Verwenden Sie mindestens 10 Zeichen.');
    expect(translate('fr', 'PassphraseTooShort', 10)).toBe('Utilisez au moins 10 caractères.');

    expect(translate('en', 'UnlockProfile', 'Personal')).toBe('Profile: Personal');
    expect(translate('de', 'UnlockProfile', 'Personal')).toBe('Profil: Personal');
    expect(translate('fr', 'UnlockProfile', 'Personal')).toBe('Profil : Personal');
  });

  it('formats positional placeholders', () => {
    expect(translate('en', 'MigrationProfileHeading', 2, 2, 'Business')).toBe(
      'Profile 2 of 2: Business',
    );
    expect(translate('de', 'MigrationProfileHeading', 2, 2, 'Business')).toBe(
      'Profil 2 von 2: Business',
    );
    expect(translate('fr', 'MigrationProfileHeading', 2, 2, 'Business')).toBe(
      'Profil 2 sur 2 : Business',
    );
    expect(translate('en', 'OtherCurrenciesExcluded', 'EUR', 3)).toBe(
      'All amounts in EUR. 3 bookings in other currencies are not included.',
    );
  });

  it('applies the singular and plural rules', () => {
    expect(translate('en', 'BookingCount', 1)).toBe('1 booking');
    expect(translate('en', 'BookingCount', 2)).toBe('2 bookings');
    expect(translate('de', 'BookingCount', 2)).toBe('2 Buchungen');
    expect(translate('fr', 'BookingCount', 0)).toBe('0 écriture');
    expect(translate('fr', 'BookingCount', 1)).toBe('1 écriture');
    expect(translate('fr', 'BookingCount', 2)).toBe('2 écritures');
  });

  it('handles percent and leaves missing or wrongly typed arguments alone', () => {
    expect(translate('en', 'ExtractedTextConfidence', 87)).toBe('Extracted text (confidence 87%)');
    expect(translate('en', 'ChangeCategory')).toBe('Change category: %s');
    expect(translate('en', 'SelectedCount')).toBe('%d selected');
    expect(translate('en', 'SelectedCount', 'three')).toBe('%d selected');
    expect(translate('en', 'CategorySplitMore')).toBe('%1$d more {1:category|categories}');
    expect(translate('en', 'ChangeCategory', 7)).toBe('Change category: 7');
  });
});
