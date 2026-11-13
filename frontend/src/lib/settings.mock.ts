import type {
  SettingsCategoryRowDTO,
  SettingsColourSwatchDTO,
  SettingsDTO,
  SettingsProfileRowDTO,
  SettingsRuleRowDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/models';
import { translate, type Language } from './i18n';
import { mockMoney } from './transactions.mock';

/**
 * The palette from internal/core/repo/categories, in the same order. The
 * category dialog lays it out as two rows of twelve.
 */
export const mockPalette = [
  '#E76530',
  '#4693F3',
  '#C38400',
  '#D56A93',
  '#867DFF',
  '#14AC77',
  '#CF4E12',
  '#2F7DDB',
  '#A77105',
  '#BD547E',
  '#7364EC',
  '#029465',
  '#DB5923',
  '#3B88E7',
  '#B57A03',
  '#C95F88',
  '#7C6FF8',
  '#00A06E',
  '#BE4403',
  '#2170CD',
  '#986603',
  '#AF4872',
  '#6857DE',
  '#07865C',
] as const;

/** The built-in tax keywords, as internal/core/report.DefaultKeywords. */
const defaultTaxKeywords = [
  'Spende',
  'Handwerker',
  'Schornsteinfeger',
  'Steuerberater',
  'Lohnsteuerhilfe',
  'Kita',
  'Kindergarten',
  'Tagesmutter',
  'Schulgeld',
  'Krankenkasse',
  'Zahnarzt',
  'Apotheke',
  'Fortbildung',
  'Fachliteratur',
  'Gewerkschaft',
  'Kirchensteuer',
  'Riester',
  'Haftpflicht',
  'donation',
  'charity',
  'tax advisor',
  'tuition',
  'assurance',
];

/** The reference page's saved custom keywords. */
const referenceTaxKeywords = ['arzt', 'apotheke', 'handwerker'];

/** The reference profiles, the default one first. */
const referenceProfiles = ['Personal', 'Business'];

/** The reference backup's file name and manifest data. */
export const mockBackupFile = 'finglass-backup-2026-10-15.zip';
const mockBackupCreated = '2026-10-15T12:34:56Z';
const mockBackupTransactions = 42;
const mockBackupDocuments = 3;

/** The invented backup passphrase the restore dialog checks once. */
export const mockBackupPassphrase = 'backup-right-6f2b91d4';

/** The folder and passphrase the FinDesk import fake uses. */
export const mockFindeskFolder = '/Users/example/.findesk';
export const mockFindeskPassphrase = 'findesk-right-8a3e5c70';

type Field = 0 | 1 | 2;
type Direction = 0 | 1 | 2;

interface MockCategory {
  id: number;
  name: string;
  colour: string;
  budgetMinor: number | null;
  taxRelevant: boolean;
  transfer: boolean;
  usage: number;
}

interface MockRule {
  id: number;
  pattern: string;
  categoryId: number;
  priority: number;
  autoApply: boolean;
  field: Field;
  direction: Direction;
  taxRelevant: boolean | null;
}

interface MockRuleImport {
  newRules: number;
  categories: string[];
  duplicates: number;
  conflicts: string[];
  errors: string[];
  matchCount: number;
}

export interface MockSettingsOptions {
  /** The UI language the DTO's texts are rendered in. */
  language?: Language;
  /** The section the page opens, as the shell's navigation records it. */
  section?: number;
  /** Adds no categories and no rules, for the empty-state tests. */
  emptyCategories?: boolean;
  /** Opens the reference category dialog on Food. */
  categoryDialogFood?: boolean;
  /** Opens the reference rule dialog on the Food rule. */
  ruleDialogFood?: boolean;
  /** Opens the reference rule-import preview. */
  ruleImportPreview?: boolean;
  /** The rule dialog's unsaved priority, as the reference screenshot shows. */
  ruleDialogPriority?: string;
  /** Opens the delete confirmation for the reference Business profile. */
  profileDelete?: boolean;
  /** Shows a restore that waits for the next start. */
  pendingRestore?: boolean;
  /** Opens the restore confirmation for the reference backup. */
  restoreDialog?: boolean;
  /** Opens the passphrase dialog for a backup made with another passphrase. */
  restorePassphrase?: boolean;
  /** Opens the FinDesk passphrase dialog for the reference folder. */
  findeskPassphrase?: boolean;
  /** Opens the successful FinDesk import report. */
  findeskReport?: boolean;
  /** Opens the failed FinDesk import report for a non-empty target. */
  findeskTargetNotEmpty?: boolean;
  /** The chosen folder is not a FinDesk folder, so the section shows the error. */
  findeskNotAFolder?: boolean;
  /** Keeps the FinDesk passphrase dialog open while it is importing. */
  findeskBusy?: boolean;
  /** Shows the note that a backup was restored at this start. */
  backupRestored?: boolean;
}

/** The stateful settings fake the mock API uses. */
export interface MockSettings {
  state(): SettingsDTO;
  subscribe(callback: (dto: SettingsDTO) => void): () => void;
  setLanguage(language: Language): void;
  setSection(section: number): void;
  load(): SettingsDTO;
  dismissMessage(): SettingsDTO;
  setPrimaryCurrency(code: string): SettingsDTO;
  addDefaultCategories(): SettingsDTO;
  fixDuplicateColours(): SettingsDTO;
  openAddCategory(): SettingsDTO;
  openEditCategory(id: number): SettingsDTO;
  openDeleteCategory(id: number): SettingsDTO;
  categoryChanged(name: string, colour: string): SettingsDTO;
  saveCategory(
    name: string,
    colour: string,
    budget: string,
    taxRelevant: boolean,
    transfer: boolean,
  ): SettingsDTO;
  closeCategoryDialog(): SettingsDTO;
  confirmDeleteCategory(): SettingsDTO;
  cancelDeleteCategory(): SettingsDTO;
  openAddRule(): SettingsDTO;
  openEditRule(id: number): SettingsDTO;
  openDeleteRule(id: number): SettingsDTO;
  ruleChanged(pattern: string, priority: string): SettingsDTO;
  saveRule(
    pattern: string,
    categoryIndex: number,
    priority: string,
    autoApply: boolean,
    fieldIndex: number,
    flow: number,
    taxIndex: number,
  ): SettingsDTO;
  closeRuleDialog(): SettingsDTO;
  confirmDeleteRule(): SettingsDTO;
  cancelDeleteRule(): SettingsDTO;
  saveTaxKeywords(text: string): SettingsDTO;
  resetTaxKeywords(): SettingsDTO;
  exportRules(): SettingsDTO;
  importRules(): SettingsDTO;
  confirmRuleImport(categorize: boolean): SettingsDTO;
  cancelRuleImport(): SettingsDTO;
  createBackup(): SettingsDTO;
  restoreBackup(): SettingsDTO;
  confirmRestore(): SettingsDTO;
  cancelRestore(): SettingsDTO;
  confirmRestorePassphrase(passphrase: string): SettingsDTO;
  cancelRestorePassphrase(): SettingsDTO;
  cancelPendingRestore(): SettingsDTO;
  importFindesk(): SettingsDTO;
  submitFindeskPassphrase(passphrase: string): SettingsDTO;
  cancelFindeskPassphrase(): SettingsDTO;
  closeFindeskReport(): SettingsDTO;
  newProfile(): SettingsDTO;
  renameProfile(index: number): SettingsDTO;
  closeProfileDialog(): SettingsDTO;
  saveProfile(name: string): SettingsDTO;
  deleteProfile(index: number): SettingsDTO;
  cancelDeleteProfile(): SettingsDTO;
  confirmDeleteProfile(): SettingsDTO;
}

const currencyOptions = [
  'EUR',
  'USD',
  'GBP',
  'CHF',
  'PLN',
  'CZK',
  'SEK',
  'DKK',
  'NOK',
  'HUF',
  'JPY',
];

function referenceCategories(): MockCategory[] {
  return [
    {
      id: 1,
      name: 'Donations',
      colour: '#14AC77',
      budgetMinor: null,
      taxRelevant: true,
      transfer: false,
      usage: 2,
    },
    {
      id: 2,
      name: 'Food',
      colour: '#E76530',
      budgetMinor: 30000,
      taxRelevant: false,
      transfer: false,
      usage: 5,
    },
    {
      id: 3,
      name: 'Health',
      colour: '#D56A93',
      budgetMinor: 20000,
      taxRelevant: true,
      transfer: false,
      usage: 0,
    },
    {
      id: 4,
      name: 'Own transfers',
      colour: '#029465',
      budgetMinor: null,
      taxRelevant: false,
      transfer: true,
      usage: 1,
    },
  ];
}

function referenceRules(): MockRule[] {
  return [
    {
      id: 1,
      pattern: 'Example Market',
      categoryId: 2,
      priority: 60,
      autoApply: true,
      field: 1,
      direction: 1,
      taxRelevant: null,
    },
    {
      id: 2,
      pattern: 'Example Aid',
      categoryId: 1,
      priority: 45,
      autoApply: true,
      field: 0,
      direction: 0,
      taxRelevant: true,
    },
    {
      id: 3,
      pattern: 'Apotheke|Arzt',
      categoryId: 3,
      priority: 20,
      autoApply: false,
      field: 2,
      direction: 0,
      taxRelevant: false,
    },
  ];
}

function defaultCategories(language: Language): MockCategory[] {
  const entries: Array<[string, string, boolean]> = [
    ['DefaultCategoryGroceries', mockPalette[0], false],
    ['DefaultCategoryDrugstore', mockPalette[1], false],
    ['DefaultCategoryHousing', mockPalette[2], false],
    ['DefaultCategoryEnergy', mockPalette[3], false],
    ['DefaultCategoryPhoneInternetMedia', mockPalette[4], false],
    ['DefaultCategoryInsurance', mockPalette[5], false],
    ['DefaultCategoryLoans', mockPalette[6], false],
    ['DefaultCategorySavings', mockPalette[7], false],
    ['DefaultCategoryTransport', mockPalette[8], false],
    ['DefaultCategoryHealth', mockPalette[9], false],
    ['DefaultCategoryClothing', mockPalette[10], false],
    ['DefaultCategoryOnlineShopping', mockPalette[11], false],
    ['DefaultCategoryLeisure', mockPalette[12], false],
    ['DefaultCategoryRestaurants', mockPalette[13], false],
    ['DefaultCategoryTaxes', mockPalette[14], false],
    ['DefaultCategoryCash', mockPalette[15], false],
    ['DefaultCategoryCreditCard', mockPalette[16], false],
    ['DefaultCategoryOwnTransfers', mockPalette[17], true],
    ['DefaultCategorySalary', mockPalette[18], false],
    ['DefaultCategoryChildBenefit', mockPalette[19], false],
    ['DefaultCategoryInterestFees', mockPalette[20], false],
    ['DefaultCategoryFamily', mockPalette[21], false],
    ['DefaultCategoryChildrenSchool', mockPalette[22], false],
    ['DefaultCategoryOtherIncome', mockPalette[23], false],
  ];
  return entries.map(([key, colour, transfer], index) => ({
    id: 100 + index,
    name: translate(language, key),
    colour,
    budgetMinor: null,
    taxRelevant: false,
    transfer,
    usage: 0,
  }));
}

function isHexColour(value: string): boolean {
  return /^#[0-9A-Fa-f]{6}$/u.test(value.trim());
}

function countKeywords(text: string): number {
  return text.split('\n').filter((line) => line.trim() !== '').length;
}

function normalizedKeywords(text: string): string {
  const seen = new Set<string>();
  const saved: string[] = [];
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (trimmed === '') {
      continue;
    }
    const key = trimmed.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    saved.push(trimmed);
  }
  return saved.join('\n');
}

function isInvalidPattern(pattern: string): boolean {
  if (pattern.trim() === '') {
    return true;
  }
  try {
    new RegExp(pattern);
    return false;
  } catch {
    return true;
  }
}

function sortCategories(categories: MockCategory[]): MockCategory[] {
  return [...categories].sort((left, right) => {
    if (left.name === right.name) {
      return left.id - right.id;
    }
    return left.name < right.name ? -1 : 1;
  });
}

function blankDTO(): SettingsDTO {
  return {
    loading: false,
    section: 0,
    errorText: '',
    messageText: '',
    categoriesEmpty: false,
    hasDuplicateColours: false,
    currencyOptions: [],
    primaryCurrency: 'EUR',
    primaryCurrencyIndex: 0,
    categoryRows: [],
    ruleRows: [],
    taxKeywords: '',
    taxKeywordsCount: '',
    categoryDialogSwatches: [],
    ruleDialogCategories: [],
    ruleDialogFieldOptions: [],
    ruleDialogDirectionOptions: [],
    ruleDialogTaxOptions: [],
    categoryDialogOpen: false,
    categoryDialogId: 0,
    categoryDialogName: '',
    categoryDialogColour: '',
    categoryDialogBudget: '',
    categoryDialogTax: false,
    categoryDialogTransfer: false,
    categoryDialogError: '',
    categoryDeleteOpen: false,
    categoryDeleteId: 0,
    categoryDeleteTitle: '',
    categoryDeleteText: '',
    ruleDialogOpen: false,
    ruleDialogId: 0,
    ruleDialogPattern: '',
    ruleDialogCategoryIndex: 0,
    ruleDialogPriority: '',
    ruleDialogAutoApply: true,
    ruleDialogFieldIndex: 0,
    ruleDialogDirectionIndex: 0,
    ruleDialogTaxIndex: 0,
    ruleDialogError: '',
    ruleDeleteOpen: false,
    ruleDeleteId: 0,
    ruleDeleteTitle: '',
    ruleDeleteText: '',
    ruleImportOpen: false,
    ruleImportBusy: false,
    ruleImportCategorize: true,
    ruleImportNewRulesText: '',
    ruleImportCategoriesText: '',
    ruleImportCategoriesList: '',
    ruleImportDuplicatesText: '',
    ruleImportConflictsText: '',
    ruleImportConflictsList: '',
    ruleImportErrorsText: '',
    ruleImportErrorsList: '',
    ruleImportShowCategorize: false,
    ruleImportCategorizeText: '',
    ruleImportConfirmEnabled: false,
    // Profiles, backup/restore and the FinDesk import are G8f-4; until then
    // the mock shows them closed and empty.
    profileRows: [],
    profileDialogOpen: false,
    profileDialogIndex: -1,
    profileDialogTitle: '',
    profileDialogName: '',
    profileDialogError: '',
    profileDeleteOpen: false,
    profileDeleteIndex: -1,
    profileDeleteTitle: '',
    profileDeleteText: '',
    busy: false,
    hasPendingRestore: false,
    backupRestoredText: '',
    restoreOpen: false,
    restoreTitle: '',
    restoreText: '',
    restorePassphraseOpen: false,
    restorePassphraseFile: '',
    restorePassphraseWrong: false,
    findeskBusy: false,
    findeskFolder: '',
    findeskPassphraseOpen: false,
    findeskPassphraseWrong: false,
    findeskReportOpen: false,
    findeskReportTitle: '',
    findeskReportText: '',
    findeskReportError: false,
  };
}

/** Builds the stateful Settings fake for the reference screenshots and tests. */
export function createMockSettings(options: MockSettingsOptions = {}): MockSettings {
  let language: Language = options.language ?? 'en';
  let section = options.section ?? 0;
  let categories = options.emptyCategories ? [] : referenceCategories();
  let rules = options.emptyCategories ? [] : referenceRules();
  let primaryCurrency = 'EUR';
  let taxKeywords = referenceTaxKeywords.join('\n');
  let errorText = '';
  let messageText = '';
  const listeners = new Set<(dto: SettingsDTO) => void>();

  let categoryDialogOpen = false;
  let categoryDialogID = -1;
  let categoryDialogName = '';
  let categoryDialogColour: string = mockPalette[0];
  let categoryDialogBudget = '';
  let categoryDialogTax = false;
  let categoryDialogTransfer = false;
  let categoryDialogError = '';

  let categoryDeleteOpen = false;
  let categoryDeleteID = 0;
  let categoryDeleteTitle = '';
  let categoryDeleteText = '';

  let ruleDialogOpen = false;
  let ruleDialogID = -1;
  let ruleDialogPattern = '';
  let ruleDialogCategoryIndex = 0;
  let ruleDialogPriority = '0';
  let ruleDialogAutoApply = true;
  let ruleDialogFieldIndex = 0;
  let ruleDialogDirectionIndex = 0;
  let ruleDialogTaxIndex = 0;
  let ruleDialogError = '';

  let ruleDeleteOpen = false;
  let ruleDeleteID = 0;
  let ruleDeleteTitle = '';
  let ruleDeleteText = '';

  let ruleImport: MockRuleImport | null = null;
  let ruleImportCategorize = true;

  let profiles = [...referenceProfiles];
  const openProfile = referenceProfiles[0] ?? 'Personal';

  let profileDialogOpen = false;
  let profileDialogIndex = -1;
  let profileDialogTitle = '';
  let profileDialogName = '';
  let profileDialogError = '';

  let profileDeleteOpen = false;
  let profileDeleteIndex = -1;
  let profileDeleteTitle = '';
  let profileDeleteText = '';

  let busy = false;
  let hasPendingRestore = false;
  let backupRestoredText = '';
  let restoreTitle = '';
  let restoreText = '';
  let restoreOpen = false;
  let restorePassphraseOpen = false;
  let restorePassphraseWrong = false;

  let findeskBusy = false;
  let findeskFolder = '';
  let findeskPassphraseOpen = false;
  let findeskPassphraseWrong = false;
  let findeskReportOpen = false;
  let findeskReportTitle = '';
  let findeskReportText = '';
  let findeskReportError = false;
  const findeskNotAFolder = options.findeskNotAFolder ?? false;
  const restoreNeedsPassphrase = options.restorePassphrase ?? false;

  function nextCategoryID(): number {
    return Math.max(0, ...categories.map((category) => category.id)) + 1;
  }

  function nextRuleID(): number {
    return Math.max(0, ...rules.map((rule) => rule.id)) + 1;
  }

  function categoryByID(id: number): MockCategory | undefined {
    return categories.find((category) => category.id === id);
  }

  function ruleByID(id: number): MockRule | undefined {
    return rules.find((rule) => rule.id === id);
  }

  function categoryName(id: number): string {
    return categoryByID(id)?.name ?? '';
  }

  function categoryRow(category: MockCategory): SettingsCategoryRowDTO {
    const budgetText =
      category.budgetMinor === null
        ? ''
        : translate(language, 'BudgetAmount', mockMoney(category.budgetMinor, 'EUR', language));
    return {
      id: category.id,
      name: category.name,
      colour: category.colour.toUpperCase(),
      budgetText,
      usageText: translate(language, 'CategoryUsage', category.usage),
      taxRelevant: category.taxRelevant,
      transfer: category.transfer,
    };
  }

  function ruleRow(rule: MockRule): SettingsRuleRowDTO {
    const category = categoryByID(rule.categoryId);
    let categoryText = category?.name ?? '';
    if (rule.taxRelevant === true) {
      categoryText += ` · ${translate(language, 'RuleTaxMarkerYes')}`;
    } else if (rule.taxRelevant === false) {
      categoryText += ` · ${translate(language, 'RuleTaxMarkerNo')}`;
    }

    const restrictions: string[] = [];
    if (rule.field === 1) {
      restrictions.push(translate(language, 'RuleFieldPartner'));
    } else if (rule.field === 2) {
      restrictions.push(translate(language, 'RuleFieldPurpose'));
    }
    if (rule.direction === 1) {
      restrictions.push(translate(language, 'RuleDirectionExpense'));
    } else if (rule.direction === 2) {
      restrictions.push(translate(language, 'RuleDirectionIncome'));
    }

    let prioText = translate(language, 'Prio', rule.priority);
    if (!rule.autoApply) {
      prioText += ` · ${translate(language, 'Manual')}`;
    }

    return {
      id: rule.id,
      pattern: rule.pattern,
      categoryText,
      colour: category?.colour.toUpperCase() ?? '#757575',
      detail: restrictions.join(' · '),
      prioText,
    };
  }

  function hasDuplicateColours(): boolean {
    const seen = new Map<string, number>();
    for (const category of categories) {
      const key = category.colour.toUpperCase();
      seen.set(key, (seen.get(key) ?? 0) + 1);
    }
    return [...seen.values()].some((count) => count > 1);
  }

  function swatches(): SettingsColourSwatchDTO[] {
    return mockPalette.map((hex) => ({ hex, colour: hex }));
  }

  function categoryNames(): string[] {
    return sortCategories(categories).map((category) => category.name);
  }

  function sortedRules(): MockRule[] {
    return [...rules].sort((left, right) => right.priority - left.priority || left.id - right.id);
  }

  function profileRow(name: string, index: number): SettingsProfileRowDTO {
    const open = name === openProfile;
    return {
      index,
      name,
      open,
      nextStart: false,
      canSwitch: !open,
      canDelete: !open && index !== 0,
    };
  }

  function restoreDialogText(): string {
    const created = translate(
      language,
      'RestoreCreated',
      mockBackupCreated,
      mockBackupTransactions,
      mockBackupDocuments,
    );
    return `${created}\n\n${translate(language, 'RestoreExplanation')}`;
  }

  function findeskReport(): string {
    const summary = translate(
      language,
      'FinDeskImportSummary',
      1,
      2,
      3,
      4,
      5,
      42,
      0,
      0,
      2,
      0,
      8,
      8,
      2,
      1,
      1,
    );
    return `${summary}\n${translate(language, 'FinDeskImportSkipped', 1, 1, 1, 2)}`;
  }

  function openFindeskPassphrase(): void {
    findeskFolder = mockFindeskFolder;
    findeskPassphraseOpen = true;
    findeskPassphraseWrong = false;
  }

  function validateCategory(name: string, colour: string, excludeID: number): string {
    if (name.trim() === '') {
      return translate(language, 'ErrorNameRequired');
    }
    if (!isHexColour(colour)) {
      return translate(language, 'ErrorColorFormat');
    }
    const candidate = name.trim().toLowerCase();
    for (const category of categories) {
      if (category.id === excludeID) {
        continue;
      }
      if (category.name.toLowerCase() === candidate) {
        return translate(language, 'ErrorCategoryExists');
      }
    }
    return '';
  }

  function validateRulePattern(pattern: string): string {
    if (pattern.trim() === '') {
      return translate(language, 'ErrorPatternRequired');
    }
    if (isInvalidPattern(pattern)) {
      return translate(
        language,
        'ErrorInvalidRegex',
        `Parsing error at position ${pattern.length}: Invalid pattern`,
      );
    }
    return '';
  }

  function validateRulePriority(priority: string): string {
    if (!/^-?\d+$/u.test(priority.trim())) {
      return translate(language, 'PriorityMustBeNumber');
    }
    return '';
  }

  function parseBudget(text: string): { ok: boolean; minor: number | null } {
    if (text.trim() === '') {
      return { ok: true, minor: null };
    }
    const trimmed = text.trim().replace(/^€/u, '').replace(/€$/u, '').trim();
    if (!/^-?\d+(?:\.\d{1,2})?$/u.test(trimmed)) {
      return { ok: false, minor: null };
    }
    return { ok: true, minor: Math.round(Number.parseFloat(trimmed) * 100) };
  }

  function applyRuleImportPreview(): void {
    ruleImport = {
      newRules: 3,
      categories: ['Household', 'Salary'],
      duplicates: 1,
      conflicts: ['Line 7: Example Market → Household (existing: Food)'],
      errors: ['Line 8: Expected 6 columns, found 2'],
      matchCount: 2,
    };
  }

  function dto(): SettingsDTO {
    const data = blankDTO();
    const rows = sortCategories(categories);

    data.loading = false;
    data.section = section;
    data.errorText = errorText;
    data.messageText = messageText;
    data.categoriesEmpty = categories.length === 0;
    data.hasDuplicateColours = hasDuplicateColours();
    data.currencyOptions = [...currencyOptions];
    data.primaryCurrency = primaryCurrency;
    data.primaryCurrencyIndex = Math.max(0, currencyOptions.indexOf(primaryCurrency));
    data.categoryRows = rows.map(categoryRow);
    data.ruleRows = sortedRules().map(ruleRow);
    data.taxKeywords = taxKeywords;
    data.taxKeywordsCount = translate(language, 'TaxKeywordsCount', countKeywords(taxKeywords));
    data.categoryDialogSwatches = swatches();
    data.ruleDialogCategories = categoryNames();
    data.ruleDialogFieldOptions = [
      translate(language, 'RuleFieldAny'),
      translate(language, 'RuleFieldPartner'),
      translate(language, 'RuleFieldPurpose'),
    ];
    data.ruleDialogDirectionOptions = [
      translate(language, 'RuleDirectionAny'),
      translate(language, 'RuleDirectionExpense'),
      translate(language, 'RuleDirectionIncome'),
    ];
    data.ruleDialogTaxOptions = [
      translate(language, 'RuleTaxAsCategory'),
      translate(language, 'TaxRelevantChoice'),
      translate(language, 'TaxNotRelevantChoice'),
    ];

    data.categoryDialogOpen = categoryDialogOpen;
    data.categoryDialogId = categoryDialogID;
    data.categoryDialogName = categoryDialogName;
    data.categoryDialogColour = categoryDialogColour;
    data.categoryDialogBudget = categoryDialogBudget;
    data.categoryDialogTax = categoryDialogTax;
    data.categoryDialogTransfer = categoryDialogTransfer;
    data.categoryDialogError = categoryDialogError;

    data.categoryDeleteOpen = categoryDeleteOpen;
    data.categoryDeleteId = categoryDeleteID;
    data.categoryDeleteTitle = categoryDeleteTitle;
    data.categoryDeleteText = categoryDeleteText;

    data.ruleDialogOpen = ruleDialogOpen;
    data.ruleDialogId = ruleDialogID;
    data.ruleDialogPattern = ruleDialogPattern;
    data.ruleDialogCategoryIndex = ruleDialogCategoryIndex;
    data.ruleDialogPriority = ruleDialogPriority;
    data.ruleDialogAutoApply = ruleDialogAutoApply;
    data.ruleDialogFieldIndex = ruleDialogFieldIndex;
    data.ruleDialogDirectionIndex = ruleDialogDirectionIndex;
    data.ruleDialogTaxIndex = ruleDialogTaxIndex;
    data.ruleDialogError = ruleDialogError;

    data.ruleDeleteOpen = ruleDeleteOpen;
    data.ruleDeleteId = ruleDeleteID;
    data.ruleDeleteTitle = ruleDeleteTitle;
    data.ruleDeleteText = ruleDeleteText;

    if (ruleImport !== null) {
      data.ruleImportOpen = true;
      data.ruleImportBusy = false;
      data.ruleImportCategorize = ruleImportCategorize;
      data.ruleImportNewRulesText = translate(language, 'RuleImportNewRules', ruleImport.newRules);
      data.ruleImportCategoriesText =
        ruleImport.categories.length === 0
          ? ''
          : translate(language, 'RuleImportCategories', ruleImport.categories.length);
      data.ruleImportCategoriesList = ruleImport.categories.join(', ');
      data.ruleImportDuplicatesText =
        ruleImport.duplicates === 0
          ? ''
          : translate(language, 'RuleImportDuplicates', ruleImport.duplicates);
      data.ruleImportConflictsText =
        ruleImport.conflicts.length === 0
          ? ''
          : translate(language, 'RuleImportConflicts', ruleImport.conflicts.length);
      data.ruleImportConflictsList = ruleImport.conflicts.join('\n');
      data.ruleImportErrorsText =
        ruleImport.errors.length === 0
          ? ''
          : translate(language, 'RuleImportErrors', ruleImport.errors.length);
      data.ruleImportErrorsList = ruleImport.errors.join('\n');
      data.ruleImportShowCategorize = ruleImport.matchCount > 0;
      data.ruleImportCategorizeText = translate(
        language,
        'RuleImportCategorize',
        ruleImport.matchCount,
      );
      data.ruleImportConfirmEnabled = true;
    }

    data.profileRows = profiles.map(profileRow);
    data.profileDialogOpen = profileDialogOpen;
    data.profileDialogIndex = profileDialogIndex;
    data.profileDialogTitle = profileDialogTitle;
    data.profileDialogName = profileDialogName;
    data.profileDialogError = profileDialogError;
    data.profileDeleteOpen = profileDeleteOpen;
    data.profileDeleteIndex = profileDeleteIndex;
    data.profileDeleteTitle = profileDeleteTitle;
    data.profileDeleteText = profileDeleteText;

    data.busy = busy;
    data.hasPendingRestore = hasPendingRestore;
    data.backupRestoredText = backupRestoredText;
    data.restoreOpen = restoreOpen;
    data.restoreTitle = restoreTitle;
    data.restoreText = restoreText;
    data.restorePassphraseOpen = restorePassphraseOpen;
    data.restorePassphraseFile = restorePassphraseOpen ? mockBackupFile : '';
    data.restorePassphraseWrong = restorePassphraseWrong;

    data.findeskBusy = findeskBusy;
    data.findeskFolder = findeskFolder;
    data.findeskPassphraseOpen = findeskPassphraseOpen;
    data.findeskPassphraseWrong = findeskPassphraseWrong;
    data.findeskReportOpen = findeskReportOpen;
    data.findeskReportTitle = findeskReportTitle;
    data.findeskReportText = findeskReportText;
    data.findeskReportError = findeskReportError;
    return data;
  }

  function publish(): SettingsDTO {
    const next = dto();
    for (const listener of listeners) {
      listener(next);
    }
    return next;
  }

  const settings: MockSettings = {
    state() {
      return dto();
    },
    subscribe(callback) {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
    setLanguage(next) {
      language = next;
      publish();
    },
    setSection(next) {
      section = next;
    },
    load() {
      return publish();
    },
    dismissMessage() {
      errorText = '';
      messageText = '';
      return publish();
    },
    setPrimaryCurrency(code) {
      if (currencyOptions.includes(code)) {
        primaryCurrency = code;
      }
      return publish();
    },
    addDefaultCategories() {
      let added = 0;
      for (const candidate of defaultCategories(language)) {
        const exists = categories.some(
          (category) => category.name.toLowerCase() === candidate.name.toLowerCase(),
        );
        if (exists) {
          continue;
        }
        categories = [...categories, candidate];
        added += 1;
      }
      messageText =
        added === 0
          ? translate(language, 'MessageDefaultCategoriesNone')
          : translate(language, 'MessageDefaultCategoriesAdded', added);
      errorText = '';
      return publish();
    },
    fixDuplicateColours() {
      const used = new Set(categories.map((category) => category.colour.toUpperCase()));
      const kept = new Set<string>();
      let recoloured = 0;
      for (const category of [...categories].sort((left, right) => left.id - right.id)) {
        const key = category.colour.toUpperCase();
        if (!kept.has(key)) {
          kept.add(key);
          continue;
        }
        const replacement = mockPalette.find((colour) => !used.has(colour.toUpperCase()));
        if (replacement === undefined) {
          continue;
        }
        category.colour = replacement;
        used.add(replacement.toUpperCase());
        recoloured += 1;
      }
      messageText =
        recoloured === 0
          ? translate(language, 'MessageColoursAlreadyDistinct')
          : translate(language, 'MessageColoursFixed', recoloured);
      errorText = '';
      return publish();
    },
    openAddCategory() {
      categoryDialogOpen = true;
      categoryDialogID = -1;
      categoryDialogName = '';
      categoryDialogColour = mockPalette[0];
      categoryDialogBudget = '';
      categoryDialogTax = false;
      categoryDialogTransfer = false;
      categoryDialogError = '';
      return publish();
    },
    openEditCategory(id) {
      const category = categoryByID(id);
      if (category === undefined) {
        return dto();
      }
      categoryDialogOpen = true;
      categoryDialogID = category.id;
      categoryDialogName = category.name;
      categoryDialogColour = category.colour.toUpperCase();
      categoryDialogBudget =
        category.budgetMinor === null ? '' : (category.budgetMinor / 100).toFixed(2);
      categoryDialogTax = category.taxRelevant;
      categoryDialogTransfer = category.transfer;
      categoryDialogError = '';
      return publish();
    },
    openDeleteCategory(id) {
      const category = categoryByID(id);
      if (category === undefined) {
        return dto();
      }
      let text = translate(language, 'DeleteCategoryText');
      if (category.usage > 0) {
        text += ` ${translate(language, 'DeleteCategoryUsage', category.usage)}`;
      }
      categoryDeleteOpen = true;
      categoryDeleteID = category.id;
      categoryDeleteTitle = translate(language, 'DeleteCategoryTitle', category.name);
      categoryDeleteText = text;
      return publish();
    },
    categoryChanged(name, colour) {
      categoryDialogError = validateCategory(name, colour, categoryDialogID);
      return publish();
    },
    saveCategory(name, colour, budgetText, taxRelevant, transfer) {
      const validation = validateCategory(name, colour, categoryDialogID);
      if (validation !== '') {
        categoryDialogError = validation;
        return publish();
      }
      const budget = parseBudget(budgetText);
      if (!budget.ok) {
        categoryDialogError = translate(language, 'BudgetNotANumber');
        return publish();
      }
      if (categoryDialogID >= 0) {
        const category = categoryByID(categoryDialogID);
        if (category !== undefined) {
          category.name = name.trim();
          category.colour = colour.toUpperCase();
          category.budgetMinor = budget.minor;
          category.taxRelevant = taxRelevant;
          category.transfer = transfer;
        }
      } else {
        categories = [
          ...categories,
          {
            id: nextCategoryID(),
            name: name.trim(),
            colour: colour.toUpperCase(),
            budgetMinor: budget.minor,
            taxRelevant,
            transfer,
            usage: 0,
          },
        ];
      }
      categoryDialogOpen = false;
      categoryDialogError = '';
      errorText = '';
      messageText = '';
      return publish();
    },
    closeCategoryDialog() {
      categoryDialogOpen = false;
      categoryDialogError = '';
      return publish();
    },
    confirmDeleteCategory() {
      categories = categories.filter((category) => category.id !== categoryDeleteID);
      rules = rules.filter((rule) => rule.categoryId !== categoryDeleteID);
      categoryDeleteOpen = false;
      return publish();
    },
    cancelDeleteCategory() {
      categoryDeleteOpen = false;
      return publish();
    },
    openAddRule() {
      if (categories.length === 0) {
        return dto();
      }
      ruleDialogOpen = true;
      ruleDialogID = -1;
      ruleDialogPattern = '';
      ruleDialogCategoryIndex = 0;
      ruleDialogPriority = '0';
      ruleDialogAutoApply = true;
      ruleDialogFieldIndex = 0;
      ruleDialogDirectionIndex = 0;
      ruleDialogTaxIndex = 0;
      ruleDialogError = '';
      return publish();
    },
    openEditRule(id) {
      const rule = ruleByID(id);
      if (rule === undefined) {
        return dto();
      }
      const index = categoryNames().indexOf(categoryName(rule.categoryId));
      ruleDialogOpen = true;
      ruleDialogID = rule.id;
      ruleDialogPattern = rule.pattern;
      ruleDialogCategoryIndex = Math.max(0, index);
      ruleDialogPriority = String(rule.priority);
      ruleDialogAutoApply = rule.autoApply;
      ruleDialogFieldIndex = rule.field;
      ruleDialogDirectionIndex = rule.direction;
      ruleDialogTaxIndex = rule.taxRelevant === null ? 0 : rule.taxRelevant ? 1 : 2;
      ruleDialogError = '';
      return publish();
    },
    openDeleteRule(id) {
      const rule = ruleByID(id);
      if (rule === undefined) {
        return dto();
      }
      ruleDeleteOpen = true;
      ruleDeleteID = rule.id;
      ruleDeleteTitle = translate(language, 'DeleteRuleTitle', rule.pattern);
      ruleDeleteText = translate(language, 'DeleteRuleText');
      return publish();
    },
    ruleChanged(pattern, priority) {
      ruleDialogError = validateRulePattern(pattern) || validateRulePriority(priority);
      return publish();
    },
    saveRule(pattern, categoryIndex, priority, autoApply, fieldIndex, flow, taxIndex) {
      const validation = validateRulePattern(pattern) || validateRulePriority(priority);
      if (validation !== '') {
        ruleDialogError = validation;
        return publish();
      }
      const ordered = sortCategories(categories);
      const category = ordered[categoryIndex];
      if (category === undefined) {
        return dto();
      }
      const taxRelevant = taxIndex === 1 ? true : taxIndex === 2 ? false : null;
      const value: MockRule = {
        id: ruleDialogID >= 0 ? ruleDialogID : nextRuleID(),
        pattern: pattern.trim(),
        categoryId: category.id,
        priority: Number.parseInt(priority.trim(), 10),
        autoApply,
        field: (fieldIndex === 1 ? 1 : fieldIndex === 2 ? 2 : 0) as Field,
        direction: (flow === 1 ? 1 : flow === 2 ? 2 : 0) as Direction,
        taxRelevant,
      };
      if (ruleDialogID >= 0) {
        const index = rules.findIndex((rule) => rule.id === ruleDialogID);
        if (index >= 0) {
          rules[index] = value;
        }
      } else {
        rules = [...rules, value];
      }
      ruleDialogOpen = false;
      ruleDialogError = '';
      errorText = '';
      messageText = '';
      return publish();
    },
    closeRuleDialog() {
      ruleDialogOpen = false;
      ruleDialogError = '';
      return publish();
    },
    confirmDeleteRule() {
      rules = rules.filter((rule) => rule.id !== ruleDeleteID);
      ruleDeleteOpen = false;
      return publish();
    },
    cancelDeleteRule() {
      ruleDeleteOpen = false;
      return publish();
    },
    saveTaxKeywords(text) {
      taxKeywords = normalizedKeywords(text);
      return publish();
    },
    resetTaxKeywords() {
      taxKeywords = defaultTaxKeywords.join('\n');
      return publish();
    },
    exportRules() {
      messageText = translate(language, 'MessageRulesExported', rules.length);
      errorText = '';
      return publish();
    },
    importRules() {
      applyRuleImportPreview();
      errorText = '';
      messageText = '';
      return publish();
    },
    confirmRuleImport(categorize) {
      if (ruleImport === null) {
        return dto();
      }
      const imported = ruleImport;
      ruleImport = null;
      ruleImportCategorize = categorize;
      const food = categories.find((category) => category.name.toLowerCase() === 'food');
      for (const name of imported.categories) {
        if (!categories.some((category) => category.name === name)) {
          categories = [
            ...categories,
            {
              id: nextCategoryID(),
              name,
              colour: mockPalette[categories.length % mockPalette.length] ?? '#757575',
              budgetMinor: null,
              taxRelevant: false,
              transfer: false,
              usage: 0,
            },
          ];
        }
      }
      const target = food?.id ?? categories[0]?.id ?? 0;
      for (let index = 0; index < imported.newRules; index += 1) {
        rules = [
          ...rules,
          {
            id: nextRuleID(),
            pattern: `Imported ${index + 1}`,
            categoryId: target,
            priority: 0,
            autoApply: true,
            field: 0,
            direction: 0,
            taxRelevant: null,
          },
        ];
      }
      messageText = translate(
        language,
        'MessageRulesImported',
        imported.newRules,
        imported.categories.length,
        categorize ? imported.matchCount : 0,
      );
      errorText = '';
      return publish();
    },
    cancelRuleImport() {
      ruleImport = null;
      return publish();
    },
    createBackup() {
      busy = false;
      errorText = '';
      messageText = translate(
        language,
        'MessageBackupSaved',
        mockBackupFile,
        mockBackupTransactions,
        mockBackupDocuments,
      );
      return publish();
    },
    restoreBackup() {
      restoreTitle = translate(language, 'RestoreFileTitle', mockBackupFile);
      restoreText = restoreDialogText();
      restoreOpen = true;
      restorePassphraseOpen = false;
      restorePassphraseWrong = false;
      errorText = '';
      messageText = '';
      return publish();
    },
    confirmRestore() {
      restoreOpen = false;
      if (restoreNeedsPassphrase) {
        restorePassphraseOpen = true;
        restorePassphraseWrong = false;
        return publish();
      }
      hasPendingRestore = true;
      messageText = translate(language, 'MessageRestorePrepared');
      errorText = '';
      return publish();
    },
    cancelRestore() {
      restoreOpen = false;
      return publish();
    },
    confirmRestorePassphrase(passphrase) {
      // The input is compared once and never stored or echoed.
      if (passphrase !== mockBackupPassphrase) {
        restorePassphraseWrong = true;
        return publish();
      }
      restorePassphraseOpen = false;
      restorePassphraseWrong = false;
      hasPendingRestore = true;
      messageText = translate(language, 'MessageRestorePrepared');
      errorText = '';
      return publish();
    },
    cancelRestorePassphrase() {
      restorePassphraseOpen = false;
      restorePassphraseWrong = false;
      return publish();
    },
    cancelPendingRestore() {
      hasPendingRestore = false;
      messageText = translate(language, 'MessageRestoreCancelled');
      errorText = '';
      return publish();
    },
    importFindesk() {
      if (findeskNotAFolder) {
        errorText = translate(language, 'ErrorFinDeskNotAFolder');
        messageText = '';
        return publish();
      }
      errorText = '';
      messageText = '';
      openFindeskPassphrase();
      return publish();
    },
    submitFindeskPassphrase(passphrase) {
      // The input is compared once and never stored or echoed.
      if (passphrase !== mockFindeskPassphrase) {
        findeskPassphraseWrong = true;
        findeskBusy = false;
        return publish();
      }
      findeskPassphraseOpen = false;
      findeskPassphraseWrong = false;
      findeskBusy = false;
      findeskReportOpen = true;
      findeskReportTitle = translate(language, 'FinDeskImportReportTitle');
      findeskReportText = findeskReport();
      findeskReportError = false;
      errorText = '';
      messageText = '';
      return publish();
    },
    cancelFindeskPassphrase() {
      findeskPassphraseOpen = false;
      findeskPassphraseWrong = false;
      findeskFolder = '';
      findeskBusy = false;
      return publish();
    },
    closeFindeskReport() {
      findeskReportOpen = false;
      findeskReportTitle = '';
      findeskReportText = '';
      findeskReportError = false;
      return publish();
    },
    newProfile() {
      profileDialogOpen = true;
      profileDialogIndex = -1;
      profileDialogTitle = translate(language, 'NewProfile');
      profileDialogName = '';
      profileDialogError = '';
      return publish();
    },
    renameProfile(index) {
      const name = profiles[index];
      if (name === undefined) {
        return dto();
      }
      profileDialogOpen = true;
      profileDialogIndex = index;
      profileDialogTitle = translate(language, 'RenameProfile', name);
      profileDialogName = name;
      profileDialogError = '';
      return publish();
    },
    closeProfileDialog() {
      profileDialogOpen = false;
      profileDialogError = '';
      return publish();
    },
    saveProfile(name) {
      profileDialogName = name;
      if (name.trim() === '') {
        profileDialogError = translate(language, 'ErrorNameRequired');
        return publish();
      }
      if (profileDialogIndex >= 0 && profileDialogIndex < profiles.length) {
        profiles = profiles.map((profile, index) =>
          index === profileDialogIndex ? name.trim() : profile,
        );
      } else {
        profiles = [...profiles, name.trim()];
      }
      profileDialogOpen = false;
      profileDialogError = '';
      errorText = '';
      messageText = '';
      return publish();
    },
    deleteProfile(index) {
      const name = profiles[index];
      if (name === undefined) {
        return dto();
      }
      profileDeleteOpen = true;
      profileDeleteIndex = index;
      profileDeleteTitle = translate(language, 'DeleteProfile', name);
      profileDeleteText = translate(language, 'DeleteProfileText', name);
      return publish();
    },
    cancelDeleteProfile() {
      profileDeleteOpen = false;
      return publish();
    },
    confirmDeleteProfile() {
      if (
        profileDeleteIndex >= 0 &&
        profileDeleteIndex < profiles.length &&
        profiles[profileDeleteIndex] !== openProfile
      ) {
        profiles = profiles.filter((_, index) => index !== profileDeleteIndex);
      }
      profileDeleteOpen = false;
      return publish();
    },
  };

  // The reference states seed what the screenshots show.
  if (options.categoryDialogFood) {
    const food = categories.find((category) => category.name === 'Food');
    if (food !== undefined) {
      settings.openEditCategory(food.id);
      // The reference dialog was captured with the box ticked.
      categoryDialogTax = true;
      messageText = '';
    }
  }
  if (options.ruleDialogFood) {
    const foodRule = rules.find((rule) => rule.pattern === 'Example Market');
    if (foodRule !== undefined) {
      settings.openEditRule(foodRule.id);
      if (options.ruleDialogPriority !== undefined) {
        ruleDialogPriority = options.ruleDialogPriority;
      }
      // The reference dialog shows the "Tax relevant" choice.
      ruleDialogTaxIndex = 1;
      messageText = '';
    }
  }
  if (options.ruleImportPreview) {
    settings.importRules();
    messageText = '';
  }
  if (options.profileDelete) {
    settings.deleteProfile(Math.min(1, profiles.length - 1));
  }
  if (options.pendingRestore) {
    hasPendingRestore = true;
  }
  if (options.backupRestored) {
    backupRestoredText = translate(language, 'BackupRestoredAt', 'pre-restore-20261015-123456');
  }
  if (options.restoreDialog) {
    settings.restoreBackup();
    messageText = '';
  }
  if (options.restorePassphrase) {
    restorePassphraseOpen = true;
    restorePassphraseWrong = false;
  }
  if (options.findeskPassphrase) {
    openFindeskPassphrase();
  }
  if (options.findeskBusy) {
    openFindeskPassphrase();
    findeskBusy = true;
  }
  if (options.findeskReport) {
    findeskReportOpen = true;
    findeskReportTitle = translate(language, 'FinDeskImportReportTitle');
    findeskReportText = findeskReport();
    findeskReportError = false;
  }
  if (options.findeskTargetNotEmpty) {
    findeskReportOpen = true;
    findeskReportTitle = translate(language, 'FinDeskImportFailedTitle');
    findeskReportText = translate(language, 'ErrorFinDeskTargetNotEmpty');
    findeskReportError = true;
  }

  return settings;
}
