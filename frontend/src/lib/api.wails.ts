import { Events } from '@wailsio/runtime';
import { AppInfo } from '../../bindings/github.com/MaMissaoui/Finglass/internal/bind/index.js';
import { AppinfoTick } from '../../bindings/events/index.js';
import type { Api, AppInfoDTO, TickPayload } from './api';

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
  };
}
