/**
 * The mock build's bank directory for the add-online-bank search. The Go
 * banking service has no bank-directory DTO yet (the directory's data licence
 * is still being cleared), so the real build searches an empty directory and
 * asks for the details by hand. The entries below are invented, as the mock
 * must not present real bank records.
 */
export interface BankSuggestion {
  name: string;
  blz: string;
  city: string;
  bic: string;
  detail: string;
  /** A branch code the bank maps to its central FinTS code. */
  branchBlz?: string;
}

type DirectoryEntry = Omit<BankSuggestion, 'detail'>;

const entries: DirectoryEntry[] = [
  {
    name: 'Rheintal Hypobank',
    blz: '70020270',
    city: 'München',
    bic: 'HYVEDEMM488',
    branchBlz: '10020890',
  },
  {
    name: 'Alpen Hypothekenbank',
    blz: '70110500',
    city: 'Riezlern',
    bic: 'ALPHDEMMXXX',
  },
  {
    name: 'Nordlicht Bank',
    blz: '20050550',
    city: 'Hamburg',
    bic: 'NOLADEHHXXX',
  },
  {
    name: 'Seeblick Sparkasse',
    blz: '37040044',
    city: 'Köln',
    bic: 'COLSDE33XXX',
  },
];

function detailOf(entry: DirectoryEntry): string {
  return `${entry.blz} · ${entry.city} · ${entry.bic}`;
}

/** The mock directory, with the detail line the search list shows. */
export const mockBankDirectory: BankSuggestion[] = entries.map((entry) => ({
  ...entry,
  detail: detailOf(entry),
}));

/** The directory the current build searches: the mock's, or none. */
export function bankDirectory(mock: boolean): BankSuggestion[] {
  return mock ? mockBankDirectory : [];
}

/** Searches by name, BLZ or BIC, as the Rust bank directory's Search does. */
export function searchBanks(
  directory: BankSuggestion[],
  query: string,
  limit = 6,
): BankSuggestion[] {
  const needle = query.trim().toLowerCase();
  if (needle === '' || limit <= 0) {
    return [];
  }
  return directory
    .filter(
      (entry) =>
        entry.name.toLowerCase().includes(needle) ||
        entry.blz.includes(needle) ||
        entry.bic.toLowerCase().includes(needle),
    )
    .slice(0, limit);
}

/** Finds the entry whose central FinTS code is blz. */
export function bankByBlz(directory: BankSuggestion[], blz: string): BankSuggestion | undefined {
  return directory.find((entry) => entry.blz === blz);
}

/** Finds the entry that maps a branch code to its central FinTS code. */
export function bankByBranchBlz(
  directory: BankSuggestion[],
  blz: string,
): BankSuggestion | undefined {
  return directory.find((entry) => entry.branchBlz === blz);
}
