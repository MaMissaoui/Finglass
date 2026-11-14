import { Events } from '@wailsio/runtime';
import {
  Accounts,
  AppInfo,
  Banking,
  Dashboard,
  Settings,
  Documents,
  Shell,
  Transactions,
  Reports,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index.js';
import {
  AccountsChanged,
  AppinfoTick,
  BankingChanged,
  BankingPrompt,
  DashboardChanged,
  DocumentsChanged,
  MenuAction,
  SettingsChanged,
  ShellChanged,
  TransactionsChanged,
  ReportsChanged,
} from '../../bindings/events/index.js';
import type {
  Api,
  AppInfoDTO,
  AccountsDTO,
  BankingDTO,
  DashboardDTO,
  DocumentsDTO,
  PromptDTO,
  ShellDTO,
  TickPayload,
  TransactionsDTO,
  ReportsDTO,
  SettingsDTO,
} from './api';

export function createWailsApi(): Api {
  return {
    async info(): Promise<AppInfoDTO> {
      const dto = await AppInfo.Info();
      return {
        name: dto.name,
        version: dto.version,
        goVersion: dto.goVersion,
        os: dto.os,
        arch: dto.arch,
      };
    },
    async greet(name: string): Promise<string> {
      return AppInfo.Greet(name);
    },
    onTick(callback: (payload: TickPayload) => void): () => void {
      return Events.On(AppinfoTick, (event) => {
        callback(event.data);
      });
    },

    state(): Promise<ShellDTO> {
      return Shell.State();
    },
    unlock(passphrase: string): Promise<ShellDTO> {
      return Shell.Unlock(passphrase);
    },
    create(passphrase: string, repeat: string, acknowledged: boolean): Promise<ShellDTO> {
      return Shell.Create(passphrase, repeat, acknowledged);
    },
    change(
      current: string,
      passphrase: string,
      repeat: string,
      acknowledged: boolean,
    ): Promise<ShellDTO> {
      return Shell.Change(current, passphrase, repeat, acknowledged);
    },
    showChange(change: boolean): Promise<ShellDTO> {
      return Shell.ShowChange(change);
    },
    navigate(screen: string): Promise<ShellDTO> {
      return Shell.Navigate(screen);
    },
    navigateMenuItem(index: number): Promise<ShellDTO> {
      return Shell.NavigateMenuItem(index);
    },
    back(): Promise<ShellDTO> {
      return Shell.Back();
    },
    setLanguage(code: string): Promise<ShellDTO> {
      return Shell.SetLanguage(code);
    },
    setTheme(code: string): Promise<ShellDTO> {
      return Shell.SetTheme(code);
    },
    setSidebarCollapsed(collapsed: boolean): Promise<ShellDTO> {
      return Shell.SetSidebarCollapsed(collapsed);
    },
    switchProfile(index: number): Promise<ShellDTO> {
      return Shell.SwitchProfile(index);
    },
    manageProfiles(): Promise<ShellDTO> {
      return Shell.ManageProfiles();
    },

    migrationMigrate(acknowledged: boolean): Promise<ShellDTO> {
      return Shell.MigrationMigrate(acknowledged);
    },
    migrationStartEmpty(): Promise<ShellDTO> {
      return Shell.MigrationStartEmpty();
    },
    migrationSubmit(
      passphrase: string,
      newPassphrase: string,
      repeat: string,
      samePassphrase: boolean,
    ): Promise<ShellDTO> {
      return Shell.MigrationSubmit(passphrase, newPassphrase, repeat, samePassphrase);
    },
    migrationSkip(): Promise<ShellDTO> {
      return Shell.MigrationSkip();
    },
    migrationContinue(): Promise<ShellDTO> {
      return Shell.MigrationContinue();
    },
    migrationPassphraseChanged(text: string): Promise<ShellDTO> {
      return Shell.MigrationPassphraseChanged(text);
    },

    openGitHub(): Promise<ShellDTO> {
      return Shell.OpenGitHub();
    },
    reportProblem(): Promise<ShellDTO> {
      return Shell.ReportProblem();
    },

    dashboardState(): Promise<DashboardDTO> {
      return Dashboard.State();
    },
    dashboardPreviousMonth(): Promise<DashboardDTO> {
      return Dashboard.PreviousMonth();
    },
    dashboardNextMonth(): Promise<DashboardDTO> {
      return Dashboard.NextMonth();
    },
    dashboardCurrentMonth(): Promise<DashboardDTO> {
      return Dashboard.CurrentMonth();
    },
    dashboardSelectMonth(index: number): Promise<DashboardDTO> {
      return Dashboard.SelectMonth(index);
    },
    dashboardDismissError(): Promise<DashboardDTO> {
      return Dashboard.DismissError();
    },
    dashboardOpenCategory(categoryID: number): Promise<DashboardDTO> {
      return Dashboard.OpenCategory(categoryID);
    },
    dashboardOpenUncategorized(): Promise<DashboardDTO> {
      return Dashboard.OpenUncategorized();
    },
    dashboardOpenAll(): Promise<DashboardDTO> {
      return Dashboard.OpenAll();
    },
    dashboardOpenPartner(partner: string): Promise<DashboardDTO> {
      return Dashboard.OpenPartner(partner);
    },
    dashboardOpenAccounts(): Promise<DashboardDTO> {
      return Dashboard.OpenAccounts();
    },
    dashboardOpenSettings(): Promise<DashboardDTO> {
      return Dashboard.OpenSettings();
    },
    dashboardOpenTax(): Promise<DashboardDTO> {
      return Dashboard.OpenTax();
    },
    accountsState() {
      return Accounts.State();
    },
    accountsLoad() {
      return Accounts.Load();
    },
    accountsOpenAddBank() {
      return Accounts.OpenAddBank();
    },
    accountsCloseDialog() {
      return Accounts.CloseDialog();
    },
    accountsSubmitAddBank(name: string) {
      return Accounts.SubmitAddBank(name);
    },
    accountsOpenAddAccount(bankID: number) {
      return Accounts.OpenAddAccount(bankID);
    },
    accountsSubmitAddAccount(
      bankID: number,
      name: string,
      identifier: string,
      accountType: string,
      currency: string,
      balance: string,
    ) {
      return Accounts.SubmitAddAccount(bankID, name, identifier, accountType, currency, balance);
    },
    accountsOpenBalance(accountID: number) {
      return Accounts.OpenBalance(accountID);
    },
    accountsSubmitBalance(accountID: number, balance: string, accountType: string) {
      return Accounts.SubmitBalance(accountID, balance, accountType);
    },
    accountsOpenRename(accountID: number) {
      return Accounts.OpenRename(accountID);
    },
    accountsSubmitRename(accountID: number, name: string) {
      return Accounts.SubmitRename(accountID, name);
    },
    accountsOpenDeleteAccount(accountID: number) {
      return Accounts.OpenDeleteAccount(accountID);
    },
    accountsConfirmDeleteAccount(accountID: number) {
      return Accounts.ConfirmDeleteAccount(accountID);
    },
    accountsOpenDeleteBank(bankID: number) {
      return Accounts.OpenDeleteBank(bankID);
    },
    accountsConfirmDeleteBank(bankID: number) {
      return Accounts.ConfirmDeleteBank(bankID);
    },
    accountsOpenDetail(accountID: number) {
      return Accounts.OpenDetail(accountID);
    },
    accountsSelectDays(accountID: number, days: number) {
      return Accounts.SelectDays(accountID, days);
    },
    accountsCloseDetail() {
      return Accounts.CloseDetail();
    },
    accountsFilterChanged(text: string) {
      return Accounts.FilterChanged(text);
    },
    accountsDismissMessage() {
      return Accounts.DismissMessage();
    },
    accountsSelect(kind: string, groupKind: string, id: number, accountType: string) {
      return Accounts.Select(kind, groupKind, id, accountType);
    },
    accountsToggleGroup(kind: string, groupKind: string, id: number, accountType: string) {
      return Accounts.ToggleGroup(kind, groupKind, id, accountType);
    },
    accountsMarkRead(kind: string, groupKind: string, id: number, accountType: string) {
      return Accounts.MarkRead(kind, groupKind, id, accountType);
    },
    accountsToggleBank(bankID: number) {
      return Accounts.ToggleBank(bankID);
    },
    accountsForgetDeleted(accountID: number) {
      return Accounts.ForgetDeleted(accountID);
    },
    accountsNewGroup(name: string) {
      return Accounts.NewGroup(name);
    },
    accountsRenameGroup(id: number, name: string) {
      return Accounts.RenameGroup(id, name);
    },
    accountsDeleteGroup(id: number) {
      return Accounts.DeleteGroup(id);
    },
    accountsMoveGroup(id: number, up: boolean) {
      return Accounts.MoveGroup(id, up);
    },
    accountsMoveAccount(accountID: number, groupID: number) {
      return Accounts.MoveAccount(accountID, groupID);
    },
    accountsOpenTransactions(accountID: number) {
      return Accounts.OpenTransactions(accountID);
    },

    bankingState() {
      return Banking.State();
    },
    bankingPrompt() {
      return Banking.Prompt();
    },
    bankingLoad() {
      return Banking.Load();
    },
    bankingRefresh() {
      return Banking.Refresh();
    },
    bankingOpenAddBank() {
      return Banking.OpenAddBank();
    },
    bankingSubmitAddOnlineBank(
      name: string,
      blz: string,
      bic: string,
      userID: string,
      serverURL: string,
    ) {
      return Banking.SubmitAddOnlineBank(name, blz, bic, userID, serverURL);
    },
    bankingSearchBanks(query: string) {
      return Banking.SearchBanks(query);
    },
    bankingPickBankSuggestion(name: string, blz: string, bic: string) {
      return Banking.PickBankSuggestion(name, blz, bic);
    },
    bankingBankBlzChanged(name: string, blz: string, bic: string) {
      return Banking.BankBLZChanged(name, blz, bic);
    },
    bankingSubmitConnection(
      bankID: number,
      name: string,
      blz: string,
      userID: string,
      serverURL: string,
    ) {
      return Banking.SubmitConnection(bankID, name, blz, userID, serverURL);
    },
    bankingForgetPin(bankID: number) {
      return Banking.ForgetPin(bankID);
    },
    bankingSetIncludeInSyncAll(bankID: number) {
      return Banking.SetIncludeInSyncAll(bankID);
    },
    bankingClearHistory(bankID: number) {
      return Banking.ClearHistory(bankID);
    },
    bankingSubmitHistory(bankID: number, date: string) {
      return Banking.SubmitHistory(bankID, date);
    },
    bankingTestConnection(bankID: number) {
      return Banking.TestConnection(bankID);
    },
    bankingSaveConnectionTrace() {
      return Banking.SaveConnectionTrace();
    },
    bankingStartSync(bankID: number) {
      return Banking.StartSync(bankID);
    },
    bankingStartSyncAll() {
      return Banking.StartSyncAll();
    },
    bankingDeleteBank(bankID: number) {
      return Banking.DeleteBank(bankID);
    },
    bankingOpenConnectionDialog(bankID: number) {
      return Banking.OpenConnectionDialog(bankID);
    },
    bankingOpenHistoryDialog(bankID: number) {
      return Banking.OpenHistoryDialog(bankID);
    },
    bankingHandleBankMenu(bankID: number, action: string) {
      return Banking.HandleBankMenu(bankID, action);
    },
    bankingClearMessages() {
      return Banking.ClearMessages();
    },
    bankingClearInfo() {
      return Banking.ClearInfo();
    },
    bankingAnswerPin(id: number, pin: string, remember: boolean) {
      return Banking.AnswerPin(id, pin, remember);
    },
    bankingCancelPin(id: number) {
      return Banking.CancelPin(id);
    },
    bankingAnswerTan(id: number, tan: string) {
      return Banking.AnswerTan(id, tan);
    },
    bankingCancelTan(id: number) {
      return Banking.CancelTan(id);
    },
    bankingAnswerChoice(id: number, choice: string) {
      return Banking.AnswerChoice(id, choice);
    },
    bankingCancelChoice(id: number) {
      return Banking.CancelChoice(id);
    },
    bankingAnswerProductID(id: number, value: string) {
      return Banking.AnswerProductID(id, value);
    },
    bankingAnswerDecoupled(id: number, confirmed: boolean) {
      return Banking.AnswerDecoupled(id, confirmed);
    },
    transactionsState(): Promise<TransactionsDTO> {
      return Transactions.State();
    },
    transactionsLoad(): Promise<TransactionsDTO> {
      return Transactions.Load();
    },
    transactionsOpenWithAdd(): Promise<TransactionsDTO> {
      return Transactions.OpenWithAdd();
    },
    transactionsSearch(query: string): Promise<TransactionsDTO> {
      return Transactions.Search(query);
    },
    transactionsPeriodChanged(index: number): Promise<TransactionsDTO> {
      return Transactions.PeriodChanged(index);
    },
    transactionsOpenRange(): Promise<TransactionsDTO> {
      return Transactions.OpenRange();
    },
    transactionsApplyRange(from: string, to: string): Promise<TransactionsDTO> {
      return Transactions.ApplyRange(from, to);
    },
    transactionsCloseRange(): Promise<TransactionsDTO> {
      return Transactions.CloseRange();
    },
    transactionsCategoryChanged(index: number): Promise<TransactionsDTO> {
      return Transactions.CategoryChanged(index);
    },
    transactionsDirectionChanged(index: number): Promise<TransactionsDTO> {
      return Transactions.DirectionChanged(index);
    },
    transactionsAccountChanged(index: number): Promise<TransactionsDTO> {
      return Transactions.AccountChanged(index);
    },
    transactionsOpenAmount(): Promise<TransactionsDTO> {
      return Transactions.OpenAmount();
    },
    transactionsApplyAmount(minimum: string, maximum: string): Promise<TransactionsDTO> {
      return Transactions.ApplyAmount(minimum, maximum);
    },
    transactionsResetAmount(): Promise<TransactionsDTO> {
      return Transactions.ResetAmount();
    },
    transactionsCloseAmount(): Promise<TransactionsDTO> {
      return Transactions.CloseAmount();
    },
    transactionsToggleChart(): Promise<TransactionsDTO> {
      return Transactions.ToggleChart();
    },
    transactionsAutoCategorize(): Promise<TransactionsDTO> {
      return Transactions.AutoCategorize();
    },
    transactionsReapplyRules(): Promise<TransactionsDTO> {
      return Transactions.ReapplyRules();
    },
    transactionsToggleReapplyGroup(from: number, to: number): Promise<TransactionsDTO> {
      return Transactions.ToggleReapplyGroup(from, to);
    },
    transactionsConfirmReapplyRules(): Promise<TransactionsDTO> {
      return Transactions.ConfirmReapplyRules();
    },
    transactionsCancelReapplyRules(): Promise<TransactionsDTO> {
      return Transactions.CancelReapplyRules();
    },
    transactionsUndoReapply(): Promise<TransactionsDTO> {
      return Transactions.UndoReapply();
    },
    transactionsOpenImport(): Promise<TransactionsDTO> {
      return Transactions.OpenImport();
    },
    transactionsOpenWithImport(): Promise<TransactionsDTO> {
      return Transactions.OpenWithImport();
    },
    transactionsImportAccountChanged(index: number): Promise<TransactionsDTO> {
      return Transactions.ImportAccountChanged(index);
    },
    transactionsImportCSVChanged(field: string, value: string): Promise<TransactionsDTO> {
      return Transactions.ImportCSVChanged(field, value);
    },
    transactionsConfirmImport(updateBalance: boolean): Promise<TransactionsDTO> {
      return Transactions.ConfirmImport(updateBalance);
    },
    transactionsCancelImport(): Promise<TransactionsDTO> {
      return Transactions.CancelImport();
    },
    transactionsExportCSV(): Promise<TransactionsDTO> {
      return Transactions.ExportCSV();
    },
    transactionsAttachReceipt(): Promise<TransactionsDTO> {
      return Transactions.AttachReceipt();
    },
    transactionsOpenRule(): Promise<TransactionsDTO> {
      return Transactions.OpenRule();
    },
    transactionsCloseRule(): Promise<TransactionsDTO> {
      return Transactions.CloseRule();
    },
    transactionsSaveRule(pattern: string, categoryIndex: number): Promise<TransactionsDTO> {
      return Transactions.SaveRule(pattern, categoryIndex);
    },
    transactionsBack(): Promise<TransactionsDTO> {
      return Transactions.Back();
    },
    transactionsOpenDetail(id: number): Promise<TransactionsDTO> {
      return Transactions.OpenDetail(id);
    },
    transactionsToggleSelect(id: number): Promise<TransactionsDTO> {
      return Transactions.ToggleSelect(id);
    },
    transactionsSelectRange(id: number): Promise<TransactionsDTO> {
      return Transactions.SelectRange(id);
    },
    transactionsSelectAll(): Promise<TransactionsDTO> {
      return Transactions.SelectAll();
    },
    transactionsClearSelection(): Promise<TransactionsDTO> {
      return Transactions.ClearSelection();
    },
    transactionsShowPartner(partner: string): Promise<TransactionsDTO> {
      return Transactions.ShowPartner(partner);
    },
    transactionsOpenDelete(id: number): Promise<TransactionsDTO> {
      return Transactions.OpenDelete(id);
    },
    transactionsConfirmDelete(id: number): Promise<TransactionsDTO> {
      return Transactions.ConfirmDelete(id);
    },
    transactionsCancelDelete(): Promise<TransactionsDTO> {
      return Transactions.CancelDelete();
    },
    transactionsOpenMultiDelete(): Promise<TransactionsDTO> {
      return Transactions.OpenMultiDelete();
    },
    transactionsConfirmMultiDelete(): Promise<TransactionsDTO> {
      return Transactions.ConfirmMultiDelete();
    },
    transactionsCancelMultiDelete(): Promise<TransactionsDTO> {
      return Transactions.CancelMultiDelete();
    },
    transactionsBatchCategory(index: number): Promise<TransactionsDTO> {
      return Transactions.BatchCategory(index);
    },
    transactionsBatchTax(index: number): Promise<TransactionsDTO> {
      return Transactions.BatchTax(index);
    },
    transactionsOpenAdd(): Promise<TransactionsDTO> {
      return Transactions.OpenAdd();
    },
    transactionsAddSubmit(
      accountIndex: number,
      expense: boolean,
      date: string,
      amount: string,
      partner: string,
      purpose: string,
      categoryIndex: number,
    ): Promise<TransactionsDTO> {
      return Transactions.AddSubmit(
        accountIndex,
        expense,
        date,
        amount,
        partner,
        purpose,
        categoryIndex,
      );
    },
    transactionsCloseAdd(): Promise<TransactionsDTO> {
      return Transactions.CloseAdd();
    },
    transactionsConfirmDuplicate(): Promise<TransactionsDTO> {
      return Transactions.ConfirmDuplicate();
    },
    transactionsDismissDuplicate(): Promise<TransactionsDTO> {
      return Transactions.DismissDuplicate();
    },
    transactionsCloseDetail(): Promise<TransactionsDTO> {
      return Transactions.CloseDetail();
    },
    transactionsDetailClose(): Promise<TransactionsDTO> {
      return Transactions.DetailClose();
    },
    transactionsDetailCreateRule(): Promise<TransactionsDTO> {
      return Transactions.DetailCreateRule();
    },
    transactionsDetailCategory(index: number): Promise<TransactionsDTO> {
      return Transactions.DetailCategory(index);
    },
    transactionsDetailTax(index: number): Promise<TransactionsDTO> {
      return Transactions.DetailTax(index);
    },
    transactionsDetailShowPartner(partner: string): Promise<TransactionsDTO> {
      return Transactions.DetailShowPartner(partner);
    },
    transactionsDismissMessage(): Promise<TransactionsDTO> {
      return Transactions.DismissMessage();
    },
    transactionsPanelSelect(
      kind: string,
      groupKind: string,
      id: number,
      accountType: string,
    ): Promise<TransactionsDTO> {
      return Transactions.PanelSelect(kind, groupKind, id, accountType);
    },
    transactionsPanelToggle(
      kind: string,
      groupKind: string,
      id: number,
      accountType: string,
    ): Promise<TransactionsDTO> {
      return Transactions.PanelToggle(kind, groupKind, id, accountType);
    },
    transactionsPanelMarkRead(
      kind: string,
      groupKind: string,
      id: number,
      accountType: string,
    ): Promise<TransactionsDTO> {
      return Transactions.PanelMarkRead(kind, groupKind, id, accountType);
    },
    transactionsPanelNewGroup(name: string): Promise<TransactionsDTO> {
      return Transactions.PanelNewGroup(name);
    },
    transactionsPanelRenameGroup(id: number, name: string): Promise<TransactionsDTO> {
      return Transactions.PanelRenameGroup(id, name);
    },
    transactionsPanelDeleteGroup(id: number): Promise<TransactionsDTO> {
      return Transactions.PanelDeleteGroup(id);
    },
    transactionsPanelMoveGroup(id: number, up: boolean): Promise<TransactionsDTO> {
      return Transactions.PanelMoveGroup(id, up);
    },
    transactionsPanelMoveAccount(accountID: number, groupID: number): Promise<TransactionsDTO> {
      return Transactions.PanelMoveAccount(accountID, groupID);
    },
    transactionsPanelFilterChanged(text: string): Promise<TransactionsDTO> {
      return Transactions.PanelFilterChanged(text);
    },

    reportsState(): Promise<ReportsDTO> {
      return Reports.State();
    },
    reportsChangeYear(delta: number): Promise<ReportsDTO> {
      return Reports.ChangeYear(delta);
    },
    reportsSetCustom(custom: boolean): Promise<ReportsDTO> {
      return Reports.SetCustom(custom);
    },
    reportsApplyRange(from: string, to: string): Promise<ReportsDTO> {
      return Reports.ApplyRange(from, to);
    },
    reportsSetMissingOnly(only: boolean): Promise<ReportsDTO> {
      return Reports.SetMissingOnly(only);
    },
    reportsDismiss(): Promise<ReportsDTO> {
      return Reports.Dismiss();
    },
    reportsAcceptSuggestion(id: number): Promise<ReportsDTO> {
      return Reports.AcceptSuggestion(id);
    },
    reportsDismissSuggestion(id: number): Promise<ReportsDTO> {
      return Reports.DismissSuggestion(id);
    },
    reportsMarkNotTaxRelevant(id: number): Promise<ReportsDTO> {
      return Reports.MarkNotTaxRelevant(id);
    },
    reportsResetTaxAutomatic(id: number): Promise<ReportsDTO> {
      return Reports.ResetTaxAutomatic(id);
    },
    reportsExportReportPDF(): Promise<ReportsDTO> {
      return Reports.ExportReportPDF();
    },
    reportsExportCSV(): Promise<ReportsDTO> {
      return Reports.ExportCSV();
    },
    reportsExportTaxXLSX(): Promise<ReportsDTO> {
      return Reports.ExportTaxXLSX();
    },
    reportsExportTaxPDF(): Promise<ReportsDTO> {
      return Reports.ExportTaxPDF();
    },
    reportsPrintTax(): Promise<ReportsDTO> {
      return Reports.PrintTax();
    },
    reportsOpenCategory(categoryID: number): Promise<ReportsDTO> {
      return Reports.OpenCategory(categoryID);
    },
    settingsState(): Promise<SettingsDTO> {
      return Settings.State();
    },
    settingsLoad(): Promise<SettingsDTO> {
      return Settings.Load();
    },
    settingsDismissMessage(): Promise<SettingsDTO> {
      return Settings.DismissMessage();
    },
    settingsSetPrimaryCurrency(code: string): Promise<SettingsDTO> {
      return Settings.SetPrimaryCurrency(code);
    },
    settingsAddDefaultCategories(): Promise<SettingsDTO> {
      return Settings.AddDefaultCategories();
    },
    settingsFixDuplicateColours(): Promise<SettingsDTO> {
      return Settings.FixDuplicateColours();
    },
    settingsOpenAddCategory(): Promise<SettingsDTO> {
      return Settings.OpenAddCategory();
    },
    settingsOpenEditCategory(id: number): Promise<SettingsDTO> {
      return Settings.OpenEditCategory(id);
    },
    settingsOpenDeleteCategory(id: number): Promise<SettingsDTO> {
      return Settings.OpenDeleteCategory(id);
    },
    settingsCategoryChanged(name: string, colour: string): Promise<SettingsDTO> {
      return Settings.CategoryChanged(name, colour);
    },
    settingsSaveCategory(
      name: string,
      colour: string,
      budget: string,
      taxRelevant: boolean,
      transfer: boolean,
    ): Promise<SettingsDTO> {
      return Settings.SaveCategory(name, colour, budget, taxRelevant, transfer);
    },
    settingsCloseCategoryDialog(): Promise<SettingsDTO> {
      return Settings.CloseCategoryDialog();
    },
    settingsConfirmDeleteCategory(): Promise<SettingsDTO> {
      return Settings.ConfirmDeleteCategory();
    },
    settingsCancelDeleteCategory(): Promise<SettingsDTO> {
      return Settings.CancelDeleteCategory();
    },
    settingsOpenAddRule(): Promise<SettingsDTO> {
      return Settings.OpenAddRule();
    },
    settingsOpenEditRule(id: number): Promise<SettingsDTO> {
      return Settings.OpenEditRule(id);
    },
    settingsOpenDeleteRule(id: number): Promise<SettingsDTO> {
      return Settings.OpenDeleteRule(id);
    },
    settingsRuleChanged(pattern: string, priority: string): Promise<SettingsDTO> {
      return Settings.RuleChanged(pattern, priority);
    },
    settingsSaveRule(
      pattern: string,
      categoryIndex: number,
      priority: string,
      autoApply: boolean,
      fieldIndex: number,
      flow: number,
      taxIndex: number,
    ): Promise<SettingsDTO> {
      return Settings.SaveRule(
        pattern,
        categoryIndex,
        priority,
        autoApply,
        fieldIndex,
        flow,
        taxIndex,
      );
    },
    settingsCloseRuleDialog(): Promise<SettingsDTO> {
      return Settings.CloseRuleDialog();
    },
    settingsConfirmDeleteRule(): Promise<SettingsDTO> {
      return Settings.ConfirmDeleteRule();
    },
    settingsCancelDeleteRule(): Promise<SettingsDTO> {
      return Settings.CancelDeleteRule();
    },
    settingsSaveTaxKeywords(text: string): Promise<SettingsDTO> {
      return Settings.SaveTaxKeywords(text);
    },
    settingsResetTaxKeywords(): Promise<SettingsDTO> {
      return Settings.ResetTaxKeywords();
    },
    settingsExportRules(): Promise<SettingsDTO> {
      return Settings.ExportRules();
    },
    settingsImportRules(): Promise<SettingsDTO> {
      return Settings.ImportRules();
    },
    settingsConfirmRuleImport(categorize: boolean): Promise<SettingsDTO> {
      return Settings.ConfirmRuleImport(categorize);
    },
    settingsCancelRuleImport(): Promise<SettingsDTO> {
      return Settings.CancelRuleImport();
    },
    documentsState(): Promise<DocumentsDTO> {
      return Documents.State();
    },
    documentsSetTab(tab: string): Promise<DocumentsDTO> {
      return Documents.SetTab(tab);
    },
    documentsSearch(query: string): Promise<DocumentsDTO> {
      return Documents.Search(query);
    },
    documentsSelect(id: number): Promise<DocumentsDTO> {
      return Documents.Select(id);
    },
    documentsCloseDetail(): Promise<DocumentsDTO> {
      return Documents.CloseDetail();
    },
    documentsCandidateQueryChanged(query: string): Promise<DocumentsDTO> {
      return Documents.CandidateQueryChanged(query);
    },
    documentsLink(transactionID: number): Promise<DocumentsDTO> {
      return Documents.Link(transactionID);
    },
    documentsUnlink(): Promise<DocumentsDTO> {
      return Documents.Unlink();
    },
    documentsReprocess(): Promise<DocumentsDTO> {
      return Documents.Reprocess();
    },
    documentsConfirmDelete(): Promise<DocumentsDTO> {
      return Documents.ConfirmDelete();
    },
    documentsDismiss(): Promise<DocumentsDTO> {
      return Documents.Dismiss();
    },
    documentsUpload(): Promise<DocumentsDTO> {
      return Documents.Upload();
    },
    documentsOpen(): Promise<DocumentsDTO> {
      return Documents.Open();
    },
    settingsCreateBackup(): Promise<SettingsDTO> {
      return Settings.CreateBackup();
    },
    settingsRestoreBackup(): Promise<SettingsDTO> {
      return Settings.RestoreBackup();
    },
    settingsConfirmRestore(): Promise<SettingsDTO> {
      return Settings.ConfirmRestore();
    },
    settingsCancelRestore(): Promise<SettingsDTO> {
      return Settings.CancelRestoreChoice();
    },
    settingsConfirmRestorePassphrase(passphrase: string): Promise<SettingsDTO> {
      return Settings.ConfirmRestorePassphrase(passphrase);
    },
    settingsCancelRestorePassphrase(): Promise<SettingsDTO> {
      return Settings.CancelRestorePassphrase();
    },
    settingsCancelPendingRestore(): Promise<SettingsDTO> {
      return Settings.CancelPendingRestore();
    },
    settingsImportFindesk(): Promise<SettingsDTO> {
      return Settings.ImportFindesk();
    },
    settingsSubmitFindeskPassphrase(passphrase: string): Promise<SettingsDTO> {
      return Settings.SubmitFindeskPassphrase(passphrase);
    },
    settingsCancelFindeskPassphrase(): Promise<SettingsDTO> {
      return Settings.CancelFindeskPassphrase();
    },
    settingsCloseFindeskReport(): Promise<SettingsDTO> {
      return Settings.CloseFindeskReport();
    },
    settingsNewProfile(): Promise<SettingsDTO> {
      return Settings.NewProfile();
    },
    settingsRenameProfile(index: number): Promise<SettingsDTO> {
      return Settings.RenameProfile(index);
    },
    settingsCloseProfileDialog(): Promise<SettingsDTO> {
      return Settings.CloseProfileDialog();
    },
    settingsSaveProfile(name: string): Promise<SettingsDTO> {
      return Settings.SaveProfile(name);
    },
    settingsDeleteProfile(index: number): Promise<SettingsDTO> {
      return Settings.DeleteProfile(index);
    },
    settingsCancelDeleteProfile(): Promise<SettingsDTO> {
      return Settings.CancelDeleteProfile();
    },
    settingsConfirmDeleteProfile(): Promise<SettingsDTO> {
      return Settings.ConfirmDeleteProfile();
    },

    onShellChanged(callback: (dto: ShellDTO) => void): () => void {
      return Events.On(ShellChanged, (event) => {
        callback(event.data);
      });
    },
    onAccountsChanged(callback: (dto: AccountsDTO) => void): () => void {
      return Events.On(AccountsChanged, (event) => {
        callback(event.data);
      });
    },
    onBankingChanged(callback: (dto: BankingDTO) => void): () => void {
      return Events.On(BankingChanged, (event) => {
        callback(event.data);
      });
    },
    onBankingPrompt(callback: (dto: PromptDTO) => void): () => void {
      return Events.On(BankingPrompt, (event) => {
        callback(event.data);
      });
    },
    onMenuAction(callback: (action: string) => void): () => void {
      return Events.On(MenuAction, (event) => {
        callback(event.data);
      });
    },
    onDashboardChanged(callback: (dto: DashboardDTO) => void): () => void {
      return Events.On(DashboardChanged, (event) => {
        callback(event.data);
      });
    },
    onTransactionsChanged(callback: (dto: TransactionsDTO) => void): () => void {
      return Events.On(TransactionsChanged, (event) => {
        callback(event.data);
      });
    },
    onReportsChanged(callback: (dto: ReportsDTO) => void): () => void {
      return Events.On(ReportsChanged, (event) => {
        callback(event.data);
      });
    },
    onSettingsChanged(callback: (dto: SettingsDTO) => void): () => void {
      return Events.On(SettingsChanged, (event) => {
        callback(event.data);
      });
    },
    onDocumentsChanged(callback: (dto: DocumentsDTO) => void): () => void {
      return Events.On(DocumentsChanged, (event) => {
        callback(event.data);
      });
    },
  };
}
