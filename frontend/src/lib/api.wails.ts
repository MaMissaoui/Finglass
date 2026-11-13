import { Events } from '@wailsio/runtime';
import {
  Accounts,
  AppInfo,
  Dashboard,
  Shell,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index.js';
import {
  AccountsChanged,
  AppinfoTick,
  DashboardChanged,
  MenuAction,
  ShellChanged,
} from '../../bindings/events/index.js';
import type { Api, AppInfoDTO, AccountsDTO, DashboardDTO, ShellDTO, TickPayload } from './api';

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
