import { expect, test, type Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

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

async function openImport(page: Page): Promise<void> {
  await page
    .getByTestId('transactions-more-button')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await page.getByRole('menuitem', { name: 'Import', exact: true }).click();
  await expect(page.getByTestId('import-dialog')).toBeVisible();
}

test('the Transaction dialog reference screenshots', async ({ page }) => {
  await goto(page, 'transactions-add-en', 1440, 720);
  await expect(page.getByTestId('add-transaction-dialog')).toBeVisible();
  await shot(page, 'transactions-add-en');

  await goto(page, 'transactions-duplicate-en', 1440, 720);
  await expect(page.getByTestId('duplicate-transaction-dialog')).toBeVisible();
  await shot(page, 'transactions-duplicate-en');

  await goto(page, 'transactions-multi-delete-en', 1440, 720);
  await expect(page.getByTestId('multi-delete-dialog')).toBeVisible();
  await shot(page, 'transactions-multi-delete-en');

  await goto(page, 'transactions-reapply-en', 1440, 720);
  await expect(page.getByTestId('reapply-rules-dialog')).toBeVisible();
  await shot(page, 'transactions-reapply-en');

  await goto(page, 'transactions-rule-dialog-en', 1440, 720);
  await expect(page.getByTestId('rule-dialog')).toBeVisible();
  await shot(page, 'transactions-rule-dialog-en');

  await goto(page, 'transactions-import-csv-en', 1440, 720);
  await expect(page.getByTestId('import-dialog')).toBeVisible();
  await shot(page, 'transactions-import-csv-en');

  await goto(page, 'transactions-import-en-light', 1440, 720);
  await expect(page.getByTestId('import-dialog')).toBeVisible();
  await shot(page, 'transactions-import-en-light');

  await goto(page, 'transactions-import-de-dark', 1440, 720, 'dark');
  await expect(page.getByTestId('import-dialog')).toBeVisible();
  await shot(page, 'transactions-import-de-dark');
});

test('the dialogs render without a CSP or console error', async ({ page }) => {
  const watcher = await watch(page);
  const scenarios = [
    'transactions-add-en',
    'transactions-duplicate-en',
    'transactions-multi-delete-en',
    'transactions-reapply-en',
    'transactions-rule-dialog-en',
    'transactions-import-csv-en',
    'transactions-import-en-light',
    'transactions-import-de-dark',
    'transactions-import-mt940-en',
    'transactions-import-warnings-en',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario);
    await expect(page.getByTestId('transactions-page')).toBeVisible();
  }
  await expectClean(watcher);
});

test('the add dialog validates, adds and asks about a duplicate', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-add-en', 1440, 720);
  const dialog = page.getByTestId('add-transaction-dialog');
  await expect(dialog).toBeVisible();

  await dialog.getByLabel('Date (dd.mm.yyyy)').fill('not a date');
  await dialog.getByLabel('Amount (€)').fill('oops');
  await dialog.getByLabel('Partner / payee').fill('Example Market');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter a valid date (dd.mm.yyyy)');

  await dialog.getByLabel('Date (dd.mm.yyyy)').fill('15.10.2026');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter a valid amount');

  await dialog.getByLabel('Amount (€)').fill('12.50');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('Transaction added');
  await expect(page.locator('.figure-count')).toHaveText('35');

  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Date (dd.mm.yyyy)').fill('15.10.2026');
  await dialog.getByLabel('Amount (€)').fill('12.50');
  await dialog.getByLabel('Partner / payee').fill('Example Market');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  const duplicate = page.getByTestId('duplicate-transaction-dialog');
  await expect(duplicate).toBeVisible();
  await expect(
    duplicate.getByRole('heading', { name: 'Add the same booking again?' }),
  ).toBeVisible();
  await duplicate.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(duplicate).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('35');

  await page.getByRole('button', { name: 'Add', exact: true }).click();
  await dialog.getByLabel('Date (dd.mm.yyyy)').fill('15.10.2026');
  await dialog.getByLabel('Amount (€)').fill('12.50');
  await dialog.getByLabel('Partner / payee').fill('Example Market');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await page
    .getByTestId('duplicate-transaction-dialog')
    .getByRole('button', { name: 'Add anyway', exact: true })
    .click();
  await expect(page.getByTestId('duplicate-transaction-dialog')).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('36');

  await expectClean(watcher);
});

test('the multi-delete dialog cancels and confirms', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-multi-delete-en', 1440, 720);
  let dialog = page.getByTestId('multi-delete-dialog');
  await expect(dialog.getByRole('heading', { name: 'Delete 3 transactions?' })).toBeVisible();
  await expect(dialog.getByText('18.10.2026–26.10.2026', { exact: true })).toBeVisible();
  await expect(dialog.getByText('+€145.00', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('34');

  await page.getByRole('button', { name: 'Delete…', exact: true }).click();
  dialog = page.getByTestId('multi-delete-dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('31');
  await expect(page.getByRole('status')).toContainText('Deleted 3 transactions');

  await expectClean(watcher);
});

test('the rule dialog validates and saves', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-rule-dialog-en', 1440, 720);
  const dialog = page.getByTestId('rule-dialog');
  await expect(dialog.getByRole('heading', { name: 'Add rule' })).toBeVisible();
  await dialog.getByLabel('Pattern (e.g. rewe|edeka|aldi)').fill('');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Pattern is required');

  await dialog.getByLabel('Pattern (e.g. rewe|edeka|aldi)').fill('Spotify');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText(
    'Rule created and applied to 0 uncategorized transactions',
  );

  await expectClean(watcher);
});

test('the reapply dialog toggles a group, confirms and undoes', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-reapply-en', 1440, 720);
  const dialog = page.getByTestId('reapply-rules-dialog');
  await expect(dialog.getByText('New category for 2 of 11 shown bookings.')).toBeVisible();
  await expect(dialog.getByText('Food → Health', { exact: true })).toBeVisible();
  await expect(dialog.getByText('2 · -€165.00', { exact: true })).toBeVisible();
  const checkbox = dialog.locator('input[type="checkbox"]');
  await expect(checkbox).toBeChecked();

  await checkbox.click();
  await expect(checkbox).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Change 0', exact: true })).toBeDisabled();
  await checkbox.click();
  await expect(checkbox).toBeChecked();
  await dialog.getByRole('button', { name: 'Change 2', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('2 bookings recategorized');

  await page
    .getByTestId('transactions-more-button')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await page.getByRole('menuitem', { name: 'Undo re-applied rules', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('2 bookings restored');

  await expectClean(watcher);
});

test('the import preview remaps, picks an account, ticks the balance and confirms', async ({
  page,
}) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light', 1440, 720);
  await expect(page.locator('.figure-count')).toHaveText('34');
  await openImport(page);

  const dialog = page.getByTestId('import-dialog');
  await expect(dialog.getByRole('heading', { name: 'Import 3 files' })).toBeVisible();
  await expect(dialog.getByText('4 bookings', { exact: true })).toBeVisible();
  await expect(dialog.getByText('File not found: missing.csv', { exact: true })).toBeVisible();

  await dialog.getByRole('button', { name: 'Partner: Auftraggeber/Empfänger' }).click();
  await page.getByRole('option', { name: 'Saldo', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Partner: Saldo' })).toBeVisible();

  await dialog
    .getByRole('button', { name: 'Into account: Gemeinschaftskonto Haushalt und Reisen' })
    .click();
  await page.getByRole('option', { name: 'Rücklage', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Into account: Rücklage' })).toBeVisible();

  const balance = dialog.locator('input[type="checkbox"]');
  await expect(balance).toBeChecked();
  await balance.click();
  await expect(balance).not.toBeChecked();
  await balance.click();
  await expect(balance).toBeChecked();

  await dialog.getByRole('button', { name: 'Import 4', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('Imported 4 transactions from 3 files');
  await expect(page.locator('.figure-count')).toHaveText('38');

  await openImport(page);
  await page
    .getByTestId('import-dialog')
    .getByRole('button', { name: 'Cancel', exact: true })
    .click();
  await expect(page.getByTestId('import-dialog')).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('38');

  await expectClean(watcher);
});

test('export and attach receipt show the mock messages', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-detail-en', 1440, 720);
  const detail = page.getByTestId('transaction-detail');
  await expect(detail).toBeVisible();
  await detail.getByRole('button', { name: 'Attach receipt…', exact: true }).click();
  await detail.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Document linked');
  await page.getByRole('button', { name: 'Dismiss', exact: true }).click();

  await page
    .getByTestId('transactions-more-button')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await page.getByRole('menuitem', { name: 'Export CSV', exact: true }).click();
  await expect(page.getByRole('status')).toContainText(
    'Exported 30 transactions to transactions-2026-10-15.csv',
  );

  await expectClean(watcher);
});
