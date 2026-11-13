import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Api } from '../../src/lib/api';
import { createMockApi } from '../../src/lib/api.mock';
import { translate } from '../../src/lib/i18n';

const passphrase = 'correct horse battery staple';

function fresh(): Api {
  return createMockApi();
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('api.mock', () => {
  it('implements the Api interface and returns fixed info', async () => {
    const info = await fresh().info();
    expect(info).toEqual({
      name: 'Finglass',
      version: '0.4.0-dev',
      goVersion: 'go1.27.4',
      os: 'darwin',
      arch: 'arm64',
    });
  });

  it('greets a trimmed name and refuses an empty one', async () => {
    const api = fresh();
    await expect(api.greet('  Ada  ')).resolves.toBe('Hello, Ada');
    await expect(api.greet('   ')).rejects.toThrow('name must not be empty');
  });

  it('ticks once per second until unsubscribed', async () => {
    vi.useFakeTimers();
    const api = fresh();

    const counts: number[] = [];
    const stop = api.onTick((payload) => counts.push(payload.count));

    await vi.advanceTimersByTimeAsync(2000);
    expect(counts).toEqual([1, 2]);

    stop();
    await vi.advanceTimersByTimeAsync(2000);
    expect(counts).toEqual([1, 2]);
  });

  it('starts in create mode with the profile and the hint', async () => {
    const dto = await fresh().state();
    expect(dto.mode).toBe('create');
    expect(dto.shellOpen).toBe(false);
    expect(dto.profileName).toBe('Personal');
    expect(dto.profileLabel).toBe(translate('en', 'UnlockProfile', 'Personal'));
    expect(dto.passphraseHint).toBe(translate('en', 'PassphraseHint', 10));
  });

  it('refuses each invalid new passphrase with its translation', async () => {
    const short = await fresh().create('short', 'short', true);
    expect(short.shellOpen).toBe(false);
    expect(short.errorText).toBe(translate('en', 'PassphraseTooShort', 10));

    const spaced = await fresh().create(` ${passphrase} `, ` ${passphrase} `, true);
    expect(spaced.errorText).toBe(translate('en', 'PassphraseSpaces'));

    const mismatch = await fresh().create(passphrase, 'another good passphrase', true);
    expect(mismatch.errorText).toBe(translate('en', 'PassphraseMismatch'));

    const unacknowledged = await fresh().create(passphrase, passphrase, false);
    expect(unacknowledged.errorText).toBe(translate('en', 'PassphraseAcknowledge'));
  });

  it('creates the database with a valid acknowledged passphrase', async () => {
    const dto = await fresh().create(passphrase, passphrase, true);
    expect(dto.shellOpen).toBe(true);
    expect(dto.screen).toBe('dashboard');
    expect(dto.errorText).toBe('');
  });

  it('refuses a wrong passphrase and opens with the right one', async () => {
    const api = fresh();
    const wrong = await api.unlock('this is not the passphrase');
    expect(wrong.shellOpen).toBe(false);
    expect(wrong.errorText).toBe(translate('en', 'WrongPassphrase'));

    const opened = await api.unlock(passphrase);
    expect(opened.shellOpen).toBe(true);
    expect(opened.screen).toBe('dashboard');
    expect(opened.errorText).toBe('');
  });

  it('never echoes a passphrase in the DTO', async () => {
    const api = fresh();
    const created = await api.create(passphrase, passphrase, true);
    expect(JSON.stringify(created)).not.toContain(passphrase);
    const wrong = await api.unlock('another secret value');
    expect(JSON.stringify(wrong)).not.toContain('another secret value');
  });

  it('records only the passphrase call counts, never the values', async () => {
    const fakeWindow = { addEventListener: () => undefined };
    vi.stubGlobal('window', fakeWindow);
    const api = createMockApi('settings-restore-passphrase-en');

    const secret = 'unit-restore-secret-4c9a';
    await api.settingsConfirmRestorePassphrase(secret);
    const findeskSecret = 'unit-findesk-secret-8b3d';
    await api.settingsSubmitFindeskPassphrase(findeskSecret);

    const debug = (fakeWindow as unknown as { __finglassMock?: Record<string, unknown> })
      .__finglassMock;
    expect(debug?.settingsConfirmRestorePassphraseCalls).toBe(1);
    expect(debug?.settingsSubmitFindeskPassphraseCalls).toBe(1);
    expect(JSON.stringify(debug)).not.toContain(secret);
    expect(JSON.stringify(debug)).not.toContain(findeskSecret);
  });

  it('publishes every change to the shell listeners', async () => {
    const api = fresh();
    const seen: string[] = [];
    const stop = api.onShellChanged((dto) => seen.push(dto.mode));

    await api.showChange(true);
    await api.showChange(false);
    stop();
    await api.showChange(true);

    expect(seen).toEqual(['change', 'unlock']);
  });
});
