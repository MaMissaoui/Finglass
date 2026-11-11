import type { Api, AppInfoDTO, TickPayload } from './api';

const info: AppInfoDTO = {
  name: 'Finglass',
  version: '0.4.0-dev',
  goVersion: 'go1.27.4',
  os: 'darwin',
  arch: 'arm64',
};

export function createMockApi(): Api {
  return {
    async info() {
      return { ...info };
    },
    async greet(name: string) {
      const trimmed = name.trim();
      if (!trimmed) {
        throw new Error('name must not be empty');
      }
      return `Hello, ${trimmed}`;
    },
    onTick(callback: (payload: TickPayload) => void) {
      let count = 0;
      const timer = setInterval(() => {
        count += 1;
        callback({ count });
      }, 1000);
      return () => clearInterval(timer);
    },
  };
}
