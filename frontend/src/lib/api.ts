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
} from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index';

import type {
  DashboardDTO,
  ShellDTO,
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

  onShellChanged(callback: (dto: ShellDTO) => void): () => void;
  onMenuAction(callback: (action: string) => void): () => void;
  onDashboardChanged(callback: (dto: DashboardDTO) => void): () => void;
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
