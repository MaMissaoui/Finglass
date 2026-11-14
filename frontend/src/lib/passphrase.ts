/**
 * Counts the characters of a passphrase the way internal/core/secret does:
 * NFC first, then one count per Unicode code point (Go's utf8.RuneCount).
 *
 * The passphrase field uses this only for its live hint; Go remains the
 * authority when the passphrase is submitted.
 */
export function passphraseLength(value: string): number {
  return [...value.normalize('NFC')].length;
}
