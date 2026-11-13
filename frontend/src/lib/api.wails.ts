import { Events } from '@wailsio/runtime';
import {
  AppInfo,
  Dashboard,
  Shell,
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index.js';
import {
  AppinfoTick,
  DashboardChanged,
  MenuAction,
  ShellChanged,
} from '../../bindings/events/index.js';
import type { Api, AppInfoDTO, DashboardDTO, ShellDTO, TickPayload } from './api';

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

    onShellChanged(callback: (dto: ShellDTO) => void): () => void {
      return Events.On(ShellChanged, (event) => {
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
