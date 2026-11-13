export interface AppInfoDTO {
  name: string;
  version: string;
  goVersion: string;
  os: string;
  arch: string;
}

export interface TickPayload {
  count: number;
}

export type {
  DashboardBudgetRowDTO,
  DashboardCategoryRowDTO,
  DashboardDTO,
  DashboardMonthBarDTO,
  DashboardStatementLineDTO,
  DashboardTransactionRowDTO,
  DashboardUpcomingRowDTO,
  ProfileDTO,
  ShellDTO,
  AccountsDTO,
  AccountsBankCardDTO,
  AccountsDialogDTO,
  BankMenuItemDTO,
  BankingBankDTO,
  BankingDTO,
  BankingPromptChoiceDTO,
  PromptDTO,
  TransactionsDTO,
  TransactionRowDTO,
  ReapplyGroupRowDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index';

import type {
  DashboardDTO,
  AccountsDTO,
  BankingDTO,
  PromptDTO,
  ShellDTO,
  TransactionsDTO,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index';

export interface Api {
  info(): Promise<AppInfoDTO>;
  greet(name: string): Promise<string>;
  onTick(callback: (payload: TickPayload) => void): () => void;

  state(): Promise<ShellDTO>;
  unlock(passphrase: string): Promise<ShellDTO>;
  create(passphrase: string, repeat: string, acknowledged: boolean): Promise<ShellDTO>;
  change(
    current: string,
    passphrase: string,
    repeat: string,
    acknowledged: boolean,
  ): Promise<ShellDTO>;
  showChange(change: boolean): Promise<ShellDTO>;
  navigate(screen: string): Promise<ShellDTO>;
  navigateMenuItem(index: number): Promise<ShellDTO>;
  back(): Promise<ShellDTO>;
  setLanguage(code: string): Promise<ShellDTO>;
  setTheme(code: string): Promise<ShellDTO>;
  setSidebarCollapsed(collapsed: boolean): Promise<ShellDTO>;
  switchProfile(index: number): Promise<ShellDTO>;
  manageProfiles(): Promise<ShellDTO>;

  migrationMigrate(acknowledged: boolean): Promise<ShellDTO>;
  migrationStartEmpty(): Promise<ShellDTO>;
  migrationSubmit(
    passphrase: string,
    newPassphrase: string,
    repeat: string,
    samePassphrase: boolean,
  ): Promise<ShellDTO>;
  migrationSkip(): Promise<ShellDTO>;
  migrationContinue(): Promise<ShellDTO>;
  migrationPassphraseChanged(text: string): Promise<ShellDTO>;

  openGitHub(): Promise<ShellDTO>;
  reportProblem(): Promise<ShellDTO>;

  dashboardState(): Promise<DashboardDTO>;
  dashboardPreviousMonth(): Promise<DashboardDTO>;
  dashboardNextMonth(): Promise<DashboardDTO>;
  dashboardCurrentMonth(): Promise<DashboardDTO>;
  dashboardSelectMonth(index: number): Promise<DashboardDTO>;
  dashboardDismissError(): Promise<DashboardDTO>;
  dashboardOpenCategory(categoryID: number): Promise<DashboardDTO>;
  dashboardOpenUncategorized(): Promise<DashboardDTO>;
  dashboardOpenAll(): Promise<DashboardDTO>;
  dashboardOpenPartner(partner: string): Promise<DashboardDTO>;
  dashboardOpenAccounts(): Promise<DashboardDTO>;
  dashboardOpenSettings(): Promise<DashboardDTO>;
  dashboardOpenTax(): Promise<DashboardDTO>;

  accountsState(): Promise<AccountsDTO>;
  accountsLoad(): Promise<AccountsDTO>;
  accountsOpenAddBank(): Promise<AccountsDTO>;
  accountsCloseDialog(): Promise<AccountsDTO>;
  accountsSubmitAddBank(name: string): Promise<AccountsDTO>;
  accountsOpenAddAccount(bankID: number): Promise<AccountsDTO>;
  accountsSubmitAddAccount(
    bankID: number,
    name: string,
    identifier: string,
    accountType: string,
    currency: string,
    balance: string,
  ): Promise<AccountsDTO>;
  accountsOpenBalance(accountID: number): Promise<AccountsDTO>;
  accountsSubmitBalance(
    accountID: number,
    balance: string,
    accountType: string,
  ): Promise<AccountsDTO>;
  accountsOpenRename(accountID: number): Promise<AccountsDTO>;
  accountsSubmitRename(accountID: number, name: string): Promise<AccountsDTO>;
  accountsOpenDeleteAccount(accountID: number): Promise<AccountsDTO>;
  accountsConfirmDeleteAccount(accountID: number): Promise<AccountsDTO>;
  accountsOpenDeleteBank(bankID: number): Promise<AccountsDTO>;
  accountsConfirmDeleteBank(bankID: number): Promise<AccountsDTO>;
  accountsOpenDetail(accountID: number): Promise<AccountsDTO>;
  accountsSelectDays(accountID: number, days: number): Promise<AccountsDTO>;
  accountsCloseDetail(): Promise<AccountsDTO>;
  accountsFilterChanged(text: string): Promise<AccountsDTO>;
  accountsDismissMessage(): Promise<AccountsDTO>;
  accountsSelect(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<AccountsDTO>;
  accountsToggleGroup(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<AccountsDTO>;
  accountsMarkRead(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<AccountsDTO>;
  accountsToggleBank(bankID: number): Promise<AccountsDTO>;
  accountsForgetDeleted(accountID: number): Promise<AccountsDTO>;
  accountsNewGroup(name: string): Promise<AccountsDTO>;
  accountsRenameGroup(id: number, name: string): Promise<AccountsDTO>;
  accountsDeleteGroup(id: number): Promise<AccountsDTO>;
  accountsMoveGroup(id: number, up: boolean): Promise<AccountsDTO>;
  accountsMoveAccount(accountID: number, groupID: number): Promise<AccountsDTO>;
  accountsOpenTransactions(accountID: number): Promise<AccountsDTO>;

  bankingState(): Promise<BankingDTO>;
  bankingPrompt(): Promise<PromptDTO>;
  bankingLoad(): Promise<BankingDTO>;
  bankingRefresh(): Promise<BankingDTO>;
  bankingOpenAddBank(): Promise<BankingDTO>;
  bankingSubmitAddOnlineBank(
    name: string,
    blz: string,
    bic: string,
    userID: string,
    serverURL: string,
  ): Promise<BankingDTO>;
  bankingSubmitConnection(
    bankID: number,
    name: string,
    blz: string,
    userID: string,
    serverURL: string,
  ): Promise<BankingDTO>;
  bankingForgetPin(bankID: number): Promise<BankingDTO>;
  bankingSetIncludeInSyncAll(bankID: number): Promise<BankingDTO>;
  bankingClearHistory(bankID: number): Promise<BankingDTO>;
  bankingSubmitHistory(bankID: number, date: string): Promise<BankingDTO>;
  bankingStartSync(bankID: number): Promise<BankingDTO>;
  bankingStartSyncAll(): Promise<BankingDTO>;
  bankingDeleteBank(bankID: number): Promise<BankingDTO>;
  bankingOpenConnectionDialog(bankID: number): Promise<BankingDTO>;
  bankingOpenHistoryDialog(bankID: number): Promise<BankingDTO>;
  bankingHandleBankMenu(bankID: number, action: string): Promise<BankingDTO>;
  bankingClearMessages(): Promise<BankingDTO>;
  bankingClearInfo(): Promise<void>;
  bankingAnswerPin(id: number, pin: string, remember: boolean): Promise<void>;
  bankingCancelPin(id: number): Promise<void>;
  bankingAnswerTan(id: number, tan: string): Promise<void>;
  bankingCancelTan(id: number): Promise<void>;
  bankingAnswerChoice(id: number, choice: string): Promise<void>;
  bankingCancelChoice(id: number): Promise<void>;
  bankingAnswerDecoupled(id: number, confirmed: boolean): Promise<void>;
  transactionsState(): Promise<TransactionsDTO>;
  transactionsLoad(): Promise<TransactionsDTO>;
  transactionsOpenWithAdd(): Promise<TransactionsDTO>;
  transactionsSearch(query: string): Promise<TransactionsDTO>;
  transactionsPeriodChanged(index: number): Promise<TransactionsDTO>;
  transactionsOpenRange(): Promise<TransactionsDTO>;
  transactionsApplyRange(from: string, to: string): Promise<TransactionsDTO>;
  transactionsCloseRange(): Promise<TransactionsDTO>;
  transactionsCategoryChanged(index: number): Promise<TransactionsDTO>;
  transactionsDirectionChanged(index: number): Promise<TransactionsDTO>;
  transactionsAccountChanged(index: number): Promise<TransactionsDTO>;
  transactionsOpenAmount(): Promise<TransactionsDTO>;
  transactionsApplyAmount(minimum: string, maximum: string): Promise<TransactionsDTO>;
  transactionsResetAmount(): Promise<TransactionsDTO>;
  transactionsCloseAmount(): Promise<TransactionsDTO>;
  transactionsToggleChart(): Promise<TransactionsDTO>;
  transactionsAutoCategorize(): Promise<TransactionsDTO>;
  transactionsReapplyRules(): Promise<TransactionsDTO>;
  transactionsToggleReapplyGroup(from: number, to: number): Promise<TransactionsDTO>;
  transactionsConfirmReapplyRules(): Promise<TransactionsDTO>;
  transactionsCancelReapplyRules(): Promise<TransactionsDTO>;
  transactionsUndoReapply(): Promise<TransactionsDTO>;
  transactionsOpenRule(): Promise<TransactionsDTO>;
  transactionsCloseRule(): Promise<TransactionsDTO>;
  transactionsSaveRule(pattern: string, categoryIndex: number): Promise<TransactionsDTO>;
  transactionsBack(): Promise<TransactionsDTO>;
  transactionsOpenDetail(id: number): Promise<TransactionsDTO>;
  transactionsToggleSelect(id: number): Promise<TransactionsDTO>;
  transactionsSelectRange(id: number): Promise<TransactionsDTO>;
  transactionsSelectAll(): Promise<TransactionsDTO>;
  transactionsClearSelection(): Promise<TransactionsDTO>;
  transactionsShowPartner(partner: string): Promise<TransactionsDTO>;
  transactionsOpenDelete(id: number): Promise<TransactionsDTO>;
  transactionsConfirmDelete(id: number): Promise<TransactionsDTO>;
  transactionsCancelDelete(): Promise<TransactionsDTO>;
  transactionsOpenMultiDelete(): Promise<TransactionsDTO>;
  transactionsConfirmMultiDelete(): Promise<TransactionsDTO>;
  transactionsCancelMultiDelete(): Promise<TransactionsDTO>;
  transactionsBatchCategory(index: number): Promise<TransactionsDTO>;
  transactionsBatchTax(index: number): Promise<TransactionsDTO>;
  transactionsOpenAdd(): Promise<TransactionsDTO>;
  transactionsAddSubmit(
    accountIndex: number,
    expense: boolean,
    date: string,
    amount: string,
    partner: string,
    purpose: string,
    categoryIndex: number,
  ): Promise<TransactionsDTO>;
  transactionsCloseAdd(): Promise<TransactionsDTO>;
  transactionsConfirmDuplicate(): Promise<TransactionsDTO>;
  transactionsDismissDuplicate(): Promise<TransactionsDTO>;
  transactionsCloseDetail(): Promise<TransactionsDTO>;
  transactionsDetailClose(): Promise<TransactionsDTO>;
  transactionsDetailCreateRule(): Promise<TransactionsDTO>;
  transactionsDetailCategory(index: number): Promise<TransactionsDTO>;
  transactionsDetailTax(index: number): Promise<TransactionsDTO>;
  transactionsDetailShowPartner(partner: string): Promise<TransactionsDTO>;
  transactionsDismissMessage(): Promise<TransactionsDTO>;
  transactionsPanelSelect(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<TransactionsDTO>;
  transactionsPanelToggle(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<TransactionsDTO>;
  transactionsPanelMarkRead(
    kind: string,
    groupKind: string,
    id: number,
    accountType: string,
  ): Promise<TransactionsDTO>;
  transactionsPanelNewGroup(name: string): Promise<TransactionsDTO>;
  transactionsPanelRenameGroup(id: number, name: string): Promise<TransactionsDTO>;
  transactionsPanelDeleteGroup(id: number): Promise<TransactionsDTO>;
  transactionsPanelMoveGroup(id: number, up: boolean): Promise<TransactionsDTO>;
  transactionsPanelMoveAccount(accountID: number, groupID: number): Promise<TransactionsDTO>;
  transactionsPanelFilterChanged(text: string): Promise<TransactionsDTO>;

  onShellChanged(callback: (dto: ShellDTO) => void): () => void;
  onAccountsChanged(callback: (dto: AccountsDTO) => void): () => void;
  onBankingChanged(callback: (dto: BankingDTO) => void): () => void;
  onBankingPrompt(callback: (dto: PromptDTO) => void): () => void;
  onMenuAction(callback: (action: string) => void): () => void;
  onDashboardChanged(callback: (dto: DashboardDTO) => void): () => void;
  onTransactionsChanged(callback: (dto: TransactionsDTO) => void): () => void;
}

let loaded: Promise<Api> | undefined;

// The mock is only imported in a mock build, so the real bundle does not
// carry it.
export function getApi(): Promise<Api> {
  loaded ??=
    import.meta.env.MODE === 'mock'
      ? import('./api.mock').then((module) => module.createMockApi())
      : import('./api.wails').then((module) => module.createWailsApi());
  return loaded;
}
