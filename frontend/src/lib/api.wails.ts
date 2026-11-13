import { Events } from '@wailsio/runtime';
import {
  Accounts,
  AppInfo,
  Banking,
  Dashboard,
  Shell,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index.js';
import {
  AccountsChanged,
  AppinfoTick,
  BankingChanged,
  BankingPrompt,
  DashboardChanged,
  MenuAction,
  ShellChanged,
} from '../../bindings/events/index.js';
import type {
  Api,
  AppInfoDTO,
  AccountsDTO,
  BankingDTO,
  DashboardDTO,
  PromptDTO,
  ShellDTO,
  TickPayload,
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
    bankingAnswerDecoupled(id: number, confirmed: boolean) {
      return Banking.AnswerDecoupled(id, confirmed);
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
  };
}
