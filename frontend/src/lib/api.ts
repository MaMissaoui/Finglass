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

export interface Api {
  info(): Promise<AppInfoDTO>;
  greet(name: string): Promise<string>;
  onTick(callback: (payload: TickPayload) => void): () => void;
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
