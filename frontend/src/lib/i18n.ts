export const languages = ['en', 'de', 'fr'] as const;
export type Language = (typeof languages)[number];

export const defaultLanguage: Language = 'en';

export const messageKeys = [
  'appName',
  'appTagline',
  'infoHeading',
  'version',
  'goVersion',
  'os',
  'arch',
  'nameLabel',
  'greetButton',
  'greetingLabel',
  'tickLabel',
  'languageLabel',
  'loading',
  'loadError',
  'errorEmptyName',
] as const;
export type MessageKey = (typeof messageKeys)[number];

type Messages = Record<MessageKey, string>;

const en: Messages = {
  appName: 'Finglass',
  appTagline: 'Read-only, local-first personal finance',
  infoHeading: 'Application',
  version: 'Version',
  goVersion: 'Go version',
  os: 'Operating system',
  arch: 'Architecture',
  nameLabel: 'Name',
  greetButton: 'Greet',
  greetingLabel: 'Greeting',
  tickLabel: 'Tick count',
  languageLabel: 'Language',
  loading: 'Loading…',
  loadError: 'The application information could not be loaded.',
  errorEmptyName: 'Please enter a name.',
};

const de: Messages = {
  appName: 'Finglass',
  appTagline: 'Schreibgeschützte, lokale Finanzverwaltung',
  infoHeading: 'Anwendung',
  version: 'Version',
  goVersion: 'Go-Version',
  os: 'Betriebssystem',
  arch: 'Architektur',
  nameLabel: 'Name',
  greetButton: 'Begrüßen',
  greetingLabel: 'Begrüßung',
  tickLabel: 'Taktanzahl',
  languageLabel: 'Sprache',
  loading: 'Wird geladen…',
  loadError: 'Die Anwendungsinformationen konnten nicht geladen werden.',
  errorEmptyName: 'Bitte einen Namen eingeben.',
};

const fr: Messages = {
  appName: 'Finglass',
  appTagline: 'Finances personnelles locales, en lecture seule',
  infoHeading: 'Application',
  version: 'Version',
  goVersion: 'Version de Go',
  os: "Système d'exploitation",
  arch: 'Architecture',
  nameLabel: 'Nom',
  greetButton: 'Saluer',
  greetingLabel: 'Salutation',
  tickLabel: 'Nombre de tics',
  languageLabel: 'Langue',
  loading: 'Chargement…',
  loadError: "Les informations de l'application n'ont pas pu être chargées.",
  errorEmptyName: 'Veuillez saisir un nom.',
};

export const messages: Record<Language, Messages> = { en, de, fr };

export function translate(language: Language, key: MessageKey): string {
  return messages[language][key];
}
