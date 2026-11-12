/**
 * The icon names, one per SVG in `src/assets/icons/`. The union is kept in
 * step with the files on disk by `tests/unit/icons.test.ts`.
 */
export const iconNames = [
  'accounts',
  'add',
  'add_circle_outline',
  'arrow_back',
  'arrow_drop_down',
  'arrow_forward',
  'attach_file',
  'auto_fix_high',
  'backup',
  'build',
  'check',
  'check_circle',
  'close',
  'dashboard',
  'date_range',
  'delete',
  'documents',
  'done',
  'edit',
  'file_download',
  'flag-gb',
  'history',
  'image',
  'info',
  'keyboard_arrow_down',
  'keyboard_arrow_left',
  'keyboard_arrow_right',
  'keyboard_arrow_up',
  'list',
  'lock',
  'menu',
  'menu-open',
  'moon',
  'more_vert',
  'palette',
  'person',
  'refresh',
  'remove_circle_outline',
  'reports',
  'search',
  'settings',
  'storage',
  'sun',
  'sync',
  'transactions',
  'upload_file',
  'warning',
] as const;

export type IconName = (typeof iconNames)[number];

const iconModules = import.meta.glob<string>('../assets/icons/*.svg', {
  eager: true,
  query: '?url',
  import: 'default',
});

function stem(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1, -'.svg'.length);
}

/** The hashed, same-origin URL for every icon, keyed by its file stem. */
export const iconUrls: Record<string, string> = Object.fromEntries(
  Object.entries(iconModules).map(([path, url]) => [stem(path), url]),
);

/** The URL of one icon. */
export function iconUrl(name: IconName): string {
  const url = iconUrls[name];
  if (!url) {
    throw new Error(`missing icon: ${name}`);
  }
  return url;
}

/** The bank logos that `BankBadge` can draw, valued by their SVG file stem. */
export const bankLogoNames = [
  'commerzbank',
  'deutsche-bank',
  'dkb',
  'hypovereinsbank',
  'n26',
  'postbank',
  'santander',
  'sparkasse',
  'volksbank',
] as const;

export type BankLogoName = (typeof bankLogoNames)[number];

const bankModules = import.meta.glob<string>('../assets/banks/*.svg', {
  eager: true,
  query: '?url',
  import: 'default',
});

export const bankLogoUrls: Record<string, string> = Object.fromEntries(
  Object.entries(bankModules).map(([path, url]) => [stem(path), url]),
);

/** The bundled logo for a bank id, when the bank has one. */
export function bankLogoUrl(id: string): string | undefined {
  return bankLogoUrls[id];
}
