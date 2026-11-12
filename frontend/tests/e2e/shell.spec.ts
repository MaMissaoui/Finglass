import { expect, test, type Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

const passphrase = 'correct horse battery staple';

type Watcher = {
  problems: string[];
  csp: () => Promise<string[]>;
};

async function watch(page: Page): Promise<Watcher> {
  const problems: string[] = [];

  await page.addInitScript(() => {
    const target = window as unknown as { __cspViolations: string[] };
    target.__cspViolations = [];
    document.addEventListener('securitypolicyviolation', (event) => {
      target.__cspViolations.push(
        `${event.violatedDirective} blocked ${event.blockedURI} (${event.sourceFile}:${event.lineNumber})`,
      );
    });
  });

  page.on('console', (message) => {
    if (message.type() !== 'error') {
      return;
    }
    // Browsers ignore frame-ancestors in a meta CSP and say so on the
    // console. The spec fixes the meta policy, so this one browser notice is
    // expected and not an application error.
    if (
      message.text() ===
      "The Content Security Policy directive 'frame-ancestors' is ignored when delivered via an HTML meta element."
    ) {
      return;
    }
    problems.push(`console: ${message.text()}`);
  });
  page.on('pageerror', (error) => {
    problems.push(`pageerror: ${error.message}`);
  });

  return {
    problems,
    csp: () =>
      page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations),
  };
}

async function goto(
  page: Page,
  scenario: string,
  width = 1100,
  height = 720,
  colorScheme: 'light' | 'dark' = 'light',
): Promise<void> {
  await page.setViewportSize({ width, height });
  await page.emulateMedia({ colorScheme });
  const query = scenario === '' ? '' : `?scenario=${scenario}`;
  await page.goto(`/${query}`);
  await expect(page.locator('#app > *').first()).toBeVisible();
  await page.evaluate(async () => {
    await document.fonts.ready;
  });
}

async function shot(page: Page, name: string): Promise<void> {
  await mkdir(screenshotDir, { recursive: true });
  await page.screenshot({
    path: path.join(screenshotDir, `${name}.png`),
    caret: 'initial',
  });
}

async function expectClean(watcher: Watcher): Promise<void> {
  expect(await watcher.csp()).toEqual([]);
  expect(watcher.problems).toEqual([]);
}

async function expectNoPassphrase(page: Page, value: string, problems: string[]): Promise<void> {
  const leaked = await page.evaluate((secret) => {
    const storage = [...Object.entries(localStorage), ...Object.entries(sessionStorage)]
      .flat()
      .join(' ');
    return {
      storage: storage.includes(secret),
      url: `${location.href} ${location.hash} ${location.search}`.includes(secret),
      dom: document.body.innerHTML.includes(secret),
    };
  }, value);
  expect(leaked).toEqual({ storage: false, url: false, dom: false });
  expect(problems.some((problem) => problem.includes(value))).toBe(false);
}

test('the reference screenshots', async ({ page }) => {
  await goto(page, '', 560, 760);
  await expect(
    page.getByRole('heading', { name: 'Protect your data with a passphrase' }),
  ).toBeVisible();
  await shot(page, 'unlock-create-en');

  await goto(page, 'unlock-wrong-de', 560, 700);
  await expect(page.getByRole('heading', { name: 'Passphrase eingeben' })).toBeVisible();
  await page.getByRole('textbox', { name: 'Passphrase' }).fill('not the passphrase');
  await page.getByRole('button', { name: 'Entsperren' }).click();
  await expect(page.getByTestId('unlock-error')).toHaveText('Falsche Passphrase.');
  await page.getByRole('textbox', { name: 'Passphrase' }).click();
  await shot(page, 'unlock-wrong-de');

  await goto(page, 'leftovers', 560, 800);
  await expect(page.getByRole('heading', { name: 'Enter your passphrase' })).toBeVisible();
  await expect(page.getByText('Unencrypted documents are still on disk')).toBeVisible();
  await shot(page, 'unlock-leftovers-en');

  await goto(page, 'restored', 560, 760);
  await expect(page.getByText('A backup was restored.')).toBeVisible();
  await shot(page, 'unlock-restored-en');

  await goto(page, 'already-running', 560, 700);
  await expect(page.getByRole('heading', { name: 'Unlock Finglass' })).toBeVisible();
  await expect(page.getByTestId('unlock-error')).toHaveText(
    'Finglass is already running with this data folder.',
  );
  await shot(page, 'unlock-already-running-en');

  await goto(page, 'target-after-switch', 560, 760);
  await expect(page.getByRole('heading', { name: 'Enter your passphrase' })).toBeVisible();
  await expect(page.getByText('Business')).toBeVisible();
  await shot(page, 'unlock-target-after-switch-en');

  await goto(page, 'migration-welcome', 560, 800);
  await expect(page.getByRole('heading', { name: 'FinDesk found on this Mac' })).toBeVisible();
  await shot(page, 'migration-welcome-en');

  await goto(page, 'migration-profile', 560, 800);
  await expect(page.getByRole('heading', { name: 'Profile 2 of 2: Business' })).toBeVisible();
  await shot(page, 'migration-profile-en');

  await goto(page, 'migration-profile-new-passphrase', 560, 800);
  await expect(page.getByRole('heading', { name: 'Profile 2 of 2: Business' })).toBeVisible();
  await expect(page.getByLabel('New passphrase')).toBeVisible();
  await shot(page, 'migration-profile-new-passphrase-en');

  await goto(page, 'migration-wrong-passphrase', 560, 800, 'dark');
  await expect(page.getByRole('heading', { name: 'Profil 2 von 2: Business' })).toBeVisible();
  await expect(page.getByTestId('migration-error')).toHaveText('Falsche Passphrase.');
  await shot(page, 'migration-wrong-passphrase-de-dark');

  await goto(page, 'migration-summary', 560, 800);
  await expect(page.getByRole('heading', { name: 'Migration finished' })).toBeVisible();
  await shot(page, 'migration-summary-en');

  await goto(page, 'shell');
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await shot(page, 'topbar-sidebar-expanded-en-light');

  await goto(page, 'shell-collapsed');
  await expect(page.getByTestId('sidebar')).toHaveClass(/collapsed/);
  await shot(page, 'topbar-sidebar-collapsed-en-light');

  await goto(page, 'shell-de-dark', 1100, 720, 'dark');
  await expect(page.getByRole('heading', { name: 'Übersicht' })).toBeVisible();
  await shot(page, 'topbar-sidebar-collapsed-de-dark');

  await goto(page, 'shell-collapsed', 900, 720);
  await shot(page, 'topbar-sidebar-collapsed-narrow-en-light');

  await goto(page, 'shell');
  await page.getByTestId('language-button').click();
  await expect(page.getByTestId('language-menu')).toBeVisible();
  await shot(page, 'topbar-language-menu-en-light');

  await goto(page, 'shell-de-dark', 1100, 720, 'dark');
  await page.getByTestId('language-button').click();
  await expect(page.getByTestId('language-menu')).toBeVisible();
  await shot(page, 'topbar-language-menu-de-dark');

  await goto(page, 'shell-profiles');
  await page.getByTestId('profile-button').click();
  await expect(page.getByTestId('profile-menu')).toBeVisible();
  await shot(page, 'topbar-profiles-en-light');

  await goto(page, 'shell-profiles-de');
  await page.getByTestId('profile-button').click();
  await expect(page.getByTestId('profile-menu')).toBeVisible();
  await shot(page, 'topbar-profiles-de-light');

  // Playwright injects a temporary <style> while capturing, which the strict
  // CSP reports; that is test tooling, not the application. The dedicated
  // clean-render test below checks the policy without taking screenshots.
});

test('every view renders without a CSP or console error', async ({ page }) => {
  test.setTimeout(120_000);
  const watcher = await watch(page);

  const scenarios = [
    '',
    'leftovers',
    'restored',
    'already-running',
    'target-after-switch',
    'unlock-wrong-de',
    'migration-welcome',
    'migration-profile',
    'migration-profile-new-passphrase',
    'migration-wrong-passphrase',
    'migration-summary',
    'shell',
    'shell-collapsed',
    'shell-de-dark',
    'shell-profiles',
    'shell-profiles-de',
    'shell-back',
  ];
  for (const scenario of scenarios) {
    await goto(
      page,
      scenario,
      560,
      800,
      scenario === 'migration-wrong-passphrase' ? 'dark' : 'light',
    );
    await expect(page.locator('#app > *').first()).toBeVisible();
  }

  await goto(page, 'shell');
  await page.getByTestId('language-button').click();
  await expect(page.getByTestId('language-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('language-menu')).not.toBeVisible();

  await goto(page, 'shell-profiles');
  await page.getByTestId('profile-button').click();
  await expect(page.getByTestId('profile-menu')).toBeVisible();

  await expectClean(watcher);
});

test('create validates every rule, then opens the shell', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, '', 1100, 720);

  await expect(
    page.getByRole('heading', { name: 'Protect your data with a passphrase' }),
  ).toBeVisible();
  await expect(page.getByLabel('New passphrase')).toHaveAttribute('type', 'password');
  await expect(page.getByLabel('New passphrase')).toHaveAttribute('autocomplete', 'off');
  await expect(page.getByLabel('New passphrase')).toHaveAttribute('spellcheck', 'false');

  const newPassphrase = page.getByLabel('New passphrase');
  const repeatPassphrase = page.getByLabel('Repeat the passphrase');
  const acknowledge = page.getByRole('checkbox', {
    name: /nobody can recover my data/,
  });

  await newPassphrase.fill('short');
  await repeatPassphrase.fill('short');
  await acknowledge.check();
  await page.getByRole('button', { name: 'Encrypt and open' }).click();
  await expect(page.getByTestId('unlock-error')).toHaveText('Use at least 10 characters.');
  await expect(newPassphrase).toHaveValue('');
  await expect(repeatPassphrase).toHaveValue('');

  const spaced = ` ${passphrase} `;
  await newPassphrase.fill(spaced);
  await repeatPassphrase.fill(spaced);
  await page.getByRole('button', { name: 'Encrypt and open' }).click();
  await expect(page.getByTestId('unlock-error')).toHaveText(
    "The passphrase can't start or end with a space.",
  );
  await expect(newPassphrase).toHaveValue('');
  await expect(repeatPassphrase).toHaveValue('');

  await newPassphrase.fill(passphrase);
  await repeatPassphrase.fill('another good passphrase');
  await acknowledge.check();
  await page.getByRole('button', { name: 'Encrypt and open' }).click();
  await expect(page.getByTestId('unlock-error')).toHaveText("The passphrases don't match.");
  await expect(newPassphrase).toHaveValue('');
  await expect(repeatPassphrase).toHaveValue('');

  await newPassphrase.fill(passphrase);
  await repeatPassphrase.fill(passphrase);
  await acknowledge.uncheck();
  await page.getByRole('button', { name: 'Encrypt and open' }).click();
  await expect(page.getByTestId('unlock-error')).toHaveText(
    "Confirm that you understand there's no recovery.",
  );
  await expect(newPassphrase).toHaveValue('');
  await expect(repeatPassphrase).toHaveValue('');
  expect(await page.content()).not.toContain(passphrase);

  await newPassphrase.fill(passphrase);
  await repeatPassphrase.fill(passphrase);
  await acknowledge.check();
  await page.getByRole('button', { name: 'Encrypt and open' }).click();

  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByText('0.4.0-dev', { exact: true })).toBeVisible();
  await expectNoPassphrase(page, passphrase, watcher.problems);

  await expectClean(watcher);
});

test('unlock rejects a wrong passphrase and opens with the right one', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'restored', 1100, 720);

  const field = page.getByRole('textbox', { name: 'Passphrase' });
  await field.fill('not the passphrase');
  await field.press('Enter');
  await expect(page.getByTestId('unlock-error')).toHaveText('Wrong passphrase.');
  await expect(field).toHaveValue('');
  await expectNoPassphrase(page, 'not the passphrase', watcher.problems);

  await field.fill(passphrase);
  await field.press('Enter');
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.getByText('0.4.0-dev', { exact: true })).toBeVisible();
  await expectNoPassphrase(page, passphrase, watcher.problems);

  // The language switch changes every visible text.
  await page.getByTestId('language-button').click();
  await page.getByRole('menuitemradio', { name: 'Deutsch' }).click();
  await expect(page.getByRole('heading', { name: 'Übersicht' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Konten' })).toBeVisible();
  await page.getByTestId('language-button').click();
  await page.getByRole('menuitemradio', { name: 'English' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();

  // The theme toggle stores an explicit theme through the shell.
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
  await page.getByRole('button', { name: 'Toggle theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.getByRole('button', { name: 'Toggle theme' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  // Collapsing shows icons only and the tooltip label flips.
  await page.getByRole('button', { name: 'Collapse sidebar' }).click();
  await expect(page.getByTestId('sidebar')).toHaveClass(/collapsed/);
  await expect.poll(async () => (await page.getByTestId('sidebar').boundingBox())?.width).toBe(72);
  await page.getByRole('button', { name: 'Expand sidebar' }).click();
  await expect(page.getByTestId('sidebar')).not.toHaveClass(/collapsed/);

  // Navigation replaces the page title.
  await page.getByRole('button', { name: 'Accounts' }).click();
  await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible();
  await page.getByRole('button', { name: 'Documents' }).click();
  await expect(page.getByRole('heading', { name: 'Documents' })).toBeVisible();

  await expectClean(watcher);
});

test('back returns to the page a link left and a profile switch locks the shell', async ({
  page,
}) => {
  const watcher = await watch(page);

  await goto(page, 'shell-back');
  await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
  await page.locator('.topbar').getByRole('button', { name: 'Dashboard' }).click();
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible();
  await expect(page.locator('.topbar').getByRole('button', { name: 'Dashboard' })).toHaveCount(0);

  await goto(page, 'shell-profiles');
  await page.getByTestId('profile-button').click();
  await expect(page.getByTestId('profile-menu')).toBeVisible();
  await expect(page.getByRole('menuitemradio', { name: 'Personal' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await page.getByRole('menuitemradio', { name: 'Business' }).click();

  await expect(page.getByRole('heading', { name: 'Enter your passphrase' })).toBeVisible();
  await expect(page.getByText('Business')).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Passphrase' })).toHaveValue('');
  await expect(page.getByText('0.4.0-dev', { exact: true })).toHaveCount(0);

  await expectClean(watcher);
});

test('command-F and the find menu action focus the page search field', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'shell');

  await page.evaluate(() => {
    const area = document.querySelector('[data-page-area]');
    if (!area) {
      throw new Error('missing page area');
    }
    const holder = document.createElement('div');
    holder.setAttribute('data-page-search', '');
    const input = document.createElement('input');
    input.id = 'stub-search';
    holder.appendChild(input);
    area.appendChild(holder);
  });

  await page.keyboard.press('Meta+f');
  await expect(page.locator('#stub-search')).toBeFocused();

  await page.evaluate(() => {
    (document.activeElement as HTMLElement | null)?.blur();
    window.dispatchEvent(new CustomEvent('finglass:mock-menu-action', { detail: 'find' }));
  });
  await expect(page.locator('#stub-search')).toBeFocused();

  await expectClean(watcher);
});
