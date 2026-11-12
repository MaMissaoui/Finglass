import de from './generated/i18n/de.json';
import en from './generated/i18n/en.json';
import fr from './generated/i18n/fr.json';
import { keys, type Key } from './generated/i18n/keys';

export const languages = ['en', 'de', 'fr'] as const;
export type Language = (typeof languages)[number];

export const defaultLanguage: Language = 'en';

/** Every generated key, in the order internal/core/i18n writes them. */
export const messageKeys = keys;
export type MessageKey = Key;

type Table = Record<string, string>;

export const messages: Record<Language, Table> = { en, de, fr };

/** A value passed to format: a string becomes %s, a number becomes %d. */
export type FormatArg = string | number;

/** Returns the language for a stored code, falling back to English. */
export function languageFromCode(code: string): Language {
  switch (code.toLowerCase()) {
    case 'de':
      return 'de';
    case 'fr':
      return 'fr';
    default:
      return 'en';
  }
}

/**
 * Translates a key in a language and formats its arguments the way
 * internal/core/i18n.Format does: %s, %d, positional forms such as %1$s,
 * %%, plural markers like {1:account|accounts}, and unknown keys falling
 * back to their own name. A missing or wrongly typed argument leaves its
 * placeholder unchanged.
 */
export function translate(language: Language, key: string, ...args: FormatArg[]): string {
  const table = messages[language] ?? messages[defaultLanguage];
  const value = table[key];
  if (typeof value !== 'string') {
    return key;
  }
  if (args.length === 0) {
    return value;
  }
  return formatText(language, value, args);
}

// The format rules below are a direct port of internal/core/i18n/format.go.

function formatText(language: Language, template: string, args: FormatArg[]): string {
  return applyPlaceholders(applyPlurals(language, template, args), args);
}

function isSingular(language: Language, count: number): boolean {
  if (language === 'fr') {
    return count === 0 || count === 1;
  }
  return count === 1;
}

function applyPlurals(language: Language, template: string, args: FormatArg[]): string {
  let out = '';
  let index = 0;
  while (index < template.length) {
    if (template[index] === '{') {
      const marker = pluralMarker(template, index);
      if (marker) {
        let chosen = marker.plural;
        const argument = args[marker.index - 1];
        if (marker.index > 0 && typeof argument === 'number' && isSingular(language, argument)) {
          chosen = marker.singular;
        }
        out += chosen;
        index = marker.end;
        continue;
      }
    }
    out += template[index];
    index += 1;
  }
  return out;
}

type PluralMarker = {
  end: number;
  index: number;
  singular: string;
  plural: string;
};

function pluralMarker(template: string, start: number): PluralMarker | undefined {
  let index = start + 1;
  const digitsStart = index;
  while (index < template.length && isASCIIDigit(template[index])) {
    index += 1;
  }
  if (index === digitsStart || index >= template.length || template[index] !== ':') {
    return undefined;
  }
  const markerIndex = Number.parseInt(template.slice(digitsStart, index), 10);
  if (Number.isNaN(markerIndex)) {
    return undefined;
  }
  const singularStart = index + 1;
  index = singularStart;
  while (
    index < template.length &&
    template[index] !== '{' &&
    template[index] !== '}' &&
    template[index] !== '|'
  ) {
    index += 1;
  }
  if (index >= template.length || template[index] !== '|') {
    return undefined;
  }
  const singular = template.slice(singularStart, index);
  const pluralStart = index + 1;
  index = pluralStart;
  while (
    index < template.length &&
    template[index] !== '{' &&
    template[index] !== '}' &&
    template[index] !== '|'
  ) {
    index += 1;
  }
  if (index >= template.length || template[index] !== '}') {
    return undefined;
  }
  return {
    end: index + 1,
    index: markerIndex,
    singular,
    plural: template.slice(pluralStart, index),
  };
}

type Placeholder = {
  end: number;
  conversion: string;
  index: number;
  supported: boolean;
};

function applyPlaceholders(template: string, args: FormatArg[]): string {
  let out = '';
  let index = 0;
  let nextArg = 0;
  while (index < template.length) {
    if (template[index] !== '%') {
      out += template[index];
      index += 1;
      continue;
    }
    if (index + 1 < template.length && template[index + 1] === '%') {
      out += '%';
      index += 2;
      continue;
    }
    const placeholder = parsePlaceholder(template, index, nextArg);
    if (placeholder) {
      nextArg = placeholder.nextArg;
      if (placeholder.supported) {
        const rendered = renderArg(placeholder.conversion, placeholder.index, args);
        out += rendered ?? template.slice(index, placeholder.end);
      } else {
        out += template.slice(index, placeholder.end);
      }
      index = placeholder.end;
      continue;
    }
    out += '%';
    index += 1;
  }
  return out;
}

type ParsedPlaceholder = Placeholder & { nextArg: number };

function parsePlaceholder(
  template: string,
  start: number,
  nextArg: number,
): ParsedPlaceholder | undefined {
  let index = start + 1;
  const indexStart = index;
  while (index < template.length && isASCIIDigit(template[index])) {
    index += 1;
  }
  let explicit = -1;
  if (index > indexStart && index < template.length && template[index] === '$') {
    const parsed = Number.parseInt(template.slice(indexStart, index), 10);
    if (Number.isNaN(parsed)) {
      return undefined;
    }
    explicit = parsed;
    index += 1;
  } else {
    index = indexStart;
  }
  while (index < template.length && isFlag(template[index])) {
    index += 1;
  }
  while (index < template.length && isASCIIDigit(template[index])) {
    index += 1;
  }
  if (index < template.length && template[index] === '.') {
    index += 1;
    while (index < template.length && isASCIIDigit(template[index])) {
      index += 1;
    }
  }
  if (index >= template.length) {
    return undefined;
  }
  const conversion = template[index] ?? '';
  const end = index + 1;
  switch (conversion) {
    case 's':
    case 'd': {
      let argumentIndex: number;
      let following = nextArg;
      if (explicit >= 0) {
        if (explicit === 0) {
          return undefined;
        }
        argumentIndex = explicit - 1;
      } else {
        argumentIndex = nextArg;
        following += 1;
      }
      return {
        end,
        conversion,
        index: argumentIndex,
        supported: true,
        nextArg: following,
      };
    }
    default:
      return { end, conversion, index: 0, supported: false, nextArg };
  }
}

function renderArg(conversion: string, index: number, args: FormatArg[]): string | undefined {
  if (index < 0 || index >= args.length) {
    return undefined;
  }
  const argument = args[index];
  switch (conversion) {
    case 's':
      return String(argument);
    case 'd':
      return typeof argument === 'number' ? String(argument) : undefined;
    default:
      return undefined;
  }
}

function isASCIIDigit(value: string | undefined): boolean {
  return value !== undefined && value >= '0' && value <= '9';
}

function isFlag(value: string | undefined): boolean {
  return (
    value === '-' ||
    value === '#' ||
    value === '+' ||
    value === ' ' ||
    value === '0' ||
    value === ',' ||
    value === '('
  );
}
