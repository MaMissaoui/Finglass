import { expect, test, type Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

// The invented values the mock's two passphrase checks accept. The wrong
// values are equally distinctive, so a leak in any sink is easy to spot.
const wrongRestorePassphrase = 'wrong-restore-1d7c4a90';
const rightRestorePassphrase = 'backup-right-6f2b91d4';
const wrongFindeskPassphrase = 'wrong-findesk-3e8b5f21';
const rightFindeskPassphrase = 'findesk-right-8a3e5c70';

type Watcher = {
  problems: string[];
  consoleLines: string[];
  csp: () => Promise<string[]>;
};

async function watch(page: Page): Promise<Watcher> {
  const problems: string[] = [];
  const consoleLines: string[] = [];

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
    consoleLines.push(message.text());
    if (message.type() !== 'error') {
      return;
    }
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
    consoleLines,
    csp: () =>
      page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations),
  };
}

async function goto(
  page: Page,
  scenario: string,
  colorScheme: 'light' | 'dark' = 'light',
): Promise<void> {
  await page.setViewportSize({ width: 1100, height: 720 });
  await page.emulateMedia({ colorScheme });
  await page.goto(`/?scenario=${scenario}`);
  await expect(page.locator('#app > *').first()).toBeVisible();
  await expect(page.getByTestId('settings-page')).toBeVisible();
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

async function sinks(page: Page, consoleLines: string[]): Promise<string> {
  const browser = await page.evaluate(() => {
    const values = Array.from(
      document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea'),
    ).map((field) => field.value);
    return {
      localStorage: JSON.stringify({ ...localStorage }),
      sessionStorage: JSON.stringify({ ...sessionStorage }),
      url: location.href,
      dom: `${document.documentElement.outerHTML}\n${values.join('\n')}`,
      mock: JSON.stringify(
        (window as unknown as { __finglassMock?: unknown }).__finglassMock ?? null,
      ),
    };
  });
  return JSON.stringify({ ...browser, console: consoleLines.join('\n') });
}

async function expectNoSecrets(
  page: Page,
  consoleLines: string[],
  secrets: string[],
): Promise<void> {
  const collected = await sinks(page, consoleLines);
  for (const secret of secrets) {
    expect(collected).not.toContain(secret);
  }
}

test('the settings data reference screenshots', async ({ page }) => {
  await goto(page, 'settings-profiles-en-light');
  await shot(page, 'settings-profiles-en-light');

  await goto(page, 'settings-profiles-de-dark', 'dark');
  await shot(page, 'settings-profiles-de-dark');

  await goto(page, 'settings-profile-delete-en');
  await expect(page.getByTestId('profile-delete-dialog')).toBeVisible();
  await shot(page, 'settings-profile-delete-en');

  await goto(page, 'settings-backup-en-light');
  await shot(page, 'settings-backup-en-light');

  await goto(page, 'settings-backup-de-dark', 'dark');
  await shot(page, 'settings-backup-de-dark');

  await goto(page, 'settings-backup-pending-en');
  await expect(page.getByText('A restore is waiting. Restart Finglass to apply it.')).toBeVisible();
  await shot(page, 'settings-backup-pending-en');

  await goto(page, 'settings-restore-dialog-en');
  await expect(page.getByTestId('restore-dialog')).toBeVisible();
  await shot(page, 'settings-restore-dialog-en');

  await goto(page, 'settings-findesk-import-en-light');
  await shot(page, 'settings-findesk-import-en-light');

  await goto(page, 'settings-findesk-import-de-dark', 'dark');
  await shot(page, 'settings-findesk-import-de-dark');

  await goto(page, 'settings-findesk-passphrase-en');
  await expect(page.getByTestId('findesk-passphrase-dialog')).toBeVisible();
  await shot(page, 'settings-findesk-passphrase-en');

  await goto(page, 'settings-findesk-report-en');
  await expect(page.getByTestId('findesk-report-dialog')).toBeVisible();
  await shot(page, 'settings-findesk-report-en');

  await goto(page, 'settings-findesk-target-not-empty-en');
  await expect(page.getByTestId('findesk-report-dialog')).toBeVisible();
  await shot(page, 'settings-findesk-target-not-empty-en');

  // Playwright injects a temporary style while capturing, which the strict CSP
  // reports; that is the test tooling, not the application. The clean-render
  // test below checks the policy without taking screenshots.
});

test('every settings data state renders without a CSP or console error', async ({ page }) => {
  test.setTimeout(120_000);
  const watcher = await watch(page);

  const scenarios = [
    'settings-profiles-en-light',
    'settings-profiles-de-dark',
    'settings-profile-delete-en',
    'settings-backup-en-light',
    'settings-backup-de-dark',
    'settings-backup-pending-en',
    'settings-backup-restored-en',
    'settings-restore-dialog-en',
    'settings-restore-passphrase-en',
    'settings-findesk-import-en-light',
    'settings-findesk-import-de-dark',
    'settings-findesk-passphrase-en',
    'settings-findesk-importing-en',
    'settings-findesk-report-en',
    'settings-findesk-target-not-empty-en',
    'settings-findesk-not-a-folder-en',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario, scenario.endsWith('-dark') ? 'dark' : 'light');
    await expect(page.getByTestId('settings-page')).toBeVisible();
  }

  await expectClean(watcher);
});

test('profiles are created, renamed, validated, deleted and opened', async ({ page }) => {
  await goto(page, 'settings-profiles-en-light');

  const personal = page.locator('.profile-row').filter({ hasText: 'Personal' });
  const business = page.locator('.profile-row').filter({ hasText: 'Business' });
  await expect(personal).toContainText('open now');
  await expect(personal.getByRole('button', { name: 'Delete' })).toHaveCount(0);
  await expect(business.getByRole('button', { name: 'Open' })).toBeVisible();

  await page.getByRole('button', { name: 'New profile' }).click();
  const dialog = page.getByTestId('profile-dialog');
  await expect(dialog).toBeVisible();
  const name = dialog.getByRole('textbox', { name: 'Profile name' });
  await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled();

  await name.fill('   ');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog.getByText('Name is required')).toBeVisible();

  await name.fill('Work');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toHaveCount(0);
  const work = page.locator('.profile-row').filter({ hasText: 'Work' });
  await expect(work).toBeVisible();

  await work.getByRole('button', { name: 'Edit' }).click();
  const rename = page.getByTestId('profile-dialog');
  await expect(rename.getByRole('heading', { name: 'Rename Work' })).toBeVisible();
  const renameField = rename.getByRole('textbox', { name: 'Profile name' });
  await expect(renameField).toHaveValue('Work');
  await renameField.fill('Work two');
  await rename.getByRole('button', { name: 'Save' }).click();
  await expect(rename).toHaveCount(0);
  const renamed = page.locator('.profile-row').filter({ hasText: 'Work two' });
  await expect(renamed).toBeVisible();

  await renamed.getByRole('button', { name: 'Delete' }).click();
  const confirm = page.getByTestId('profile-delete-dialog');
  await expect(confirm.getByRole('heading', { name: 'Delete Work two' })).toBeVisible();
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(confirm).toHaveCount(0);
  await expect(renamed).toBeVisible();

  await renamed.getByRole('button', { name: 'Delete' }).click();
  await page.getByTestId('profile-delete-dialog').getByRole('button', { name: 'Delete' }).click();
  await expect(renamed).toHaveCount(0);

  await page
    .locator('.profile-row')
    .filter({ hasText: 'Business' })
    .getByRole('button', { name: 'Open' })
    .click();
  await expect(page.getByRole('heading', { name: 'Enter your passphrase' })).toBeVisible();
});

test('a backup is created and a restore is confirmed, cancelled and pending-cancelled', async ({
  page,
}) => {
  await goto(page, 'settings-backup-en-light');

  await page.getByRole('button', { name: 'Create backup' }).click();
  await expect(
    page.getByText('Backup saved to finglass-backup-2026-10-15.zip (42 transactions, 3 documents)'),
  ).toBeVisible();

  await page.getByRole('button', { name: 'Restore from backup…' }).click();
  const restore = page.getByTestId('restore-dialog');
  await expect(
    restore.getByRole('heading', { name: 'Restore finglass-backup-2026-10-15.zip?' }),
  ).toBeVisible();
  await restore.getByRole('button', { name: 'Cancel' }).click();
  await expect(restore).toHaveCount(0);

  await page.getByRole('button', { name: 'Restore from backup…' }).click();
  await page.getByTestId('restore-dialog').getByRole('button', { name: 'Prepare restore' }).click();
  await expect(page.getByText('A restore is waiting. Restart Finglass to apply it.')).toBeVisible();
  await expect(
    page.getByText(
      'Restore prepared. Restart Finglass to finish; your current data will be kept in a pre-restore folder.',
    ),
  ).toBeVisible();

  await page.getByRole('button', { name: 'Cancel restore' }).click();
  await expect(page.getByText('A restore is waiting. Restart Finglass to apply it.')).toHaveCount(
    0,
  );
  await expect(page.getByText('Pending restore cancelled')).toBeVisible();
});

test('the restore passphrase is checked wrong then right and forgotten', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'settings-restore-passphrase-en');

  const dialog = page.getByTestId('restore-passphrase-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('finglass-backup-2026-10-15.zip')).toBeVisible();
  const field = dialog.getByRole('textbox', { name: 'Passphrase' });

  await field.fill(wrongRestorePassphrase);
  await dialog.getByRole('button', { name: 'Prepare restore' }).click();
  await expect(dialog.getByText('Wrong passphrase.')).toBeVisible();
  await expect(dialog).toBeVisible();
  await expect(field).toHaveValue('');
  await expectNoSecrets(page, watcher.consoleLines, [wrongRestorePassphrase]);

  await field.fill(rightRestorePassphrase);
  await dialog.getByRole('button', { name: 'Prepare restore' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText('A restore is waiting. Restart Finglass to apply it.')).toBeVisible();
  await expectNoSecrets(page, watcher.consoleLines, [
    wrongRestorePassphrase,
    rightRestorePassphrase,
  ]);

  await goto(page, 'settings-restore-passphrase-en');
  const reopened = page.getByTestId('restore-passphrase-dialog');
  const reopenedField = reopened.getByRole('textbox', { name: 'Passphrase' });
  await reopenedField.fill(rightRestorePassphrase);
  await reopened.getByRole('button', { name: 'Cancel' }).click();
  await expect(reopened).toHaveCount(0);
  await expectNoSecrets(page, watcher.consoleLines, [
    wrongRestorePassphrase,
    rightRestorePassphrase,
  ]);

  await expectClean(watcher);
});

test('the FinDesk import asks for its passphrase, reports, closes and cancels', async ({
  page,
}) => {
  const watcher = await watch(page);
  await goto(page, 'settings-findesk-import-en-light');

  await page.getByRole('button', { name: 'Choose FinDesk folder…' }).click();
  let dialog = page.getByTestId('findesk-passphrase-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('/Users/example/.findesk')).toBeVisible();
  let field = dialog.getByRole('textbox', { name: 'Passphrase' });

  await field.fill(wrongFindeskPassphrase);
  await dialog.getByRole('button', { name: 'Import' }).click();
  await expect(dialog.getByText('Wrong passphrase.')).toBeVisible();
  await expect(dialog).toBeVisible();
  await expect(field).toHaveValue('');
  await expectNoSecrets(page, watcher.consoleLines, [wrongFindeskPassphrase]);

  await field.fill(rightFindeskPassphrase);
  await dialog.getByRole('button', { name: 'Import' }).click();
  await expect(dialog).toHaveCount(0);

  let report = page.getByTestId('findesk-report-dialog');
  await expect(report).toBeVisible();
  await expect(report.getByRole('heading', { name: 'FinDesk import finished' })).toBeVisible();
  await expect(report.getByText('Imported 1 account groups')).toBeVisible();
  await expectNoSecrets(page, watcher.consoleLines, [
    wrongFindeskPassphrase,
    rightFindeskPassphrase,
  ]);

  await report.getByRole('button', { name: 'Close' }).click();
  await expect(report).toHaveCount(0);

  await page.getByRole('button', { name: 'Choose FinDesk folder…' }).click();
  dialog = page.getByTestId('findesk-passphrase-dialog');
  field = dialog.getByRole('textbox', { name: 'Passphrase' });
  await field.fill(rightFindeskPassphrase);
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  await expectNoSecrets(page, watcher.consoleLines, [
    wrongFindeskPassphrase,
    rightFindeskPassphrase,
  ]);

  report = page.getByTestId('findesk-report-dialog');
  await expect(report).toHaveCount(0);
  await expectClean(watcher);
});

test('the FinDesk folder and target errors are shown', async ({ page }) => {
  await goto(page, 'settings-findesk-not-a-folder-en');
  await page.getByRole('button', { name: 'Choose FinDesk folder…' }).click();
  await expect(page.getByText('The selected folder is not a FinDesk folder.')).toBeVisible();
  await expect(page.getByTestId('findesk-passphrase-dialog')).toHaveCount(0);

  await goto(page, 'settings-findesk-target-not-empty-en');
  const report = page.getByTestId('findesk-report-dialog');
  await expect(report.getByRole('heading', { name: 'FinDesk import failed' })).toBeVisible();
  await expect(
    report.getByText(
      'The Finglass profile already contains data. Create a new profile in the Profiles section and import there.',
    ),
  ).toBeVisible();
  await report.getByRole('button', { name: 'Close' }).click();
  await expect(report).toHaveCount(0);
});
