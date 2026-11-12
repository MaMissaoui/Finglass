export type ThemeMode = 'system' | 'light' | 'dark';

/**
 * Applies a theme choice to the document root. `system` removes the override so
 * the `prefers-color-scheme` rules in `tokens.css` decide the palette. The
 * choice itself is persisted by the Go shell, never here.
 */
export function applyTheme(mode: ThemeMode): void {
  const root = document.documentElement;
  if (mode === 'system') {
    root.removeAttribute('data-theme');
    return;
  }
  root.dataset.theme = mode;
}

/**
 * The eight fixed monogram backgrounds from `theme.slint`'s `MonogramColours`,
 * indexed by the bank logo colour index. White letters reach at least 4.5:1 on
 * every one of them.
 */
export const MONOGRAM_COLOURS = [
  '#B45309',
  '#0E7490',
  '#7C3AED',
  '#BE123C',
  '#15803D',
  '#1D4ED8',
  '#A21CAF',
  '#C2410C',
] as const;

/** The monogram colour for a stable bank colour index, as `MonogramColours.colour`. */
export function monogramColour(index: number): string {
  return MONOGRAM_COLOURS[index] ?? MONOGRAM_COLOURS[0];
}
