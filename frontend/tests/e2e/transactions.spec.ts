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

function pageRoot(page: Page) {
  return page.getByTestId('transactions-page');
}

function listRows(page: Page) {
  return page
    .getByTestId('transactions-list')
    .locator('.row[role="button"]:not([aria-disabled="true"])');
}

function summaryCount(page: Page): Promise<string> {
  return page.locator('.figure-count').innerText();
}

async function pick(page: Page, trigger: string, option: string): Promise<void> {
  await page.getByRole('button', { name: trigger, exact: true }).click();
  await page.getByRole('option', { name: option, exact: true }).click();
}

test('the Transactions reference screenshots', async ({ page }) => {
  await goto(page, 'transactions-en-light', 1440, 720);
  await expect(pageRoot(page)).toBeVisible();
  await expect(page.locator('.selection-bar')).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText('34');
  await shot(page, 'transactions-en-light');

  await goto(page, 'transactions-de-light', 1440, 900);
  await expect(page.getByRole('heading', { name: 'Umsätze', exact: true })).toBeVisible();
  await shot(page, 'transactions-de-light');

  await goto(page, 'transactions-de-dark', 1440, 720, 'dark');
  await expect(page.getByRole('heading', { name: 'Umsätze', exact: true })).toBeVisible();
  await shot(page, 'transactions-de-dark');

  await goto(page, 'transactions-month-de-light', 1440, 900);
  await expect(page.getByRole('button', { name: 'Dieser Monat' })).toBeVisible();
  await expect(page.locator('.figure-count')).toHaveText('11');
  await shot(page, 'transactions-month-de-light');

  await goto(page, 'transactions-narrow-en', 900, 720);
  await expect(page.getByTestId('account-panel')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'All accounts', exact: true })).toBeVisible();
  await shot(page, 'transactions-narrow-en');

  await goto(page, 'transactions-chart-en', 1440, 720);
  await expect(page.getByTestId('balance-chart')).toBeVisible();
  await expect(page.getByText('Pending (not yet booked) · as of 12.11.2026 18:47')).toBeVisible();
  await shot(page, 'transactions-chart-en');

  await goto(page, 'transactions-chart-collapsed-en', 1440, 720);
  await expect(page.getByTestId('balance-chart')).toHaveClass(/collapsed/);
  await shot(page, 'transactions-chart-collapsed-en');

  await goto(page, 'transactions-panel-collapsed-en-dark', 1440, 1000, 'dark');
  await expect(page.getByTestId('account-panel')).toBeVisible();
  await expect(page.locator('[data-kind="account"]')).toHaveCount(0);
  await shot(page, 'transactions-panel-collapsed-en-dark');

  await goto(page, 'transactions-panel-expanded-en-dark', 1440, 1000, 'dark');
  await expect(page.locator('[data-kind="account"]')).toHaveCount(3);
  await shot(page, 'transactions-panel-expanded-en-dark');

  await goto(page, 'transactions-row-hover-en-light', 1440, 720);
  await page.mouse.move(760, 320);
  await shot(page, 'transactions-row-hover-en-light');

  await goto(page, 'transactions-selection-en', 1440, 720);
  await expect(page.getByTestId('selection-bar')).toBeVisible();
  await expect(page.getByText('3 selected', { exact: true })).toBeVisible();
  await shot(page, 'transactions-selection-en');

  await goto(page, 'transactions-detail-en', 1440, 720);
  await expect(page.getByTestId('transaction-detail')).toBeVisible();
  await shot(page, 'transactions-detail-en');

  await goto(page, 'transactions-more-menu-en', 1440, 720);
  await page
    .getByTestId('transactions-more-button')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await expect(page.getByTestId('transactions-more-menu')).toBeVisible();
  await shot(page, 'transactions-more-menu-en');

  await goto(page, 'transactions-custom-range-en', 1440, 720);
  await expect(page.getByTestId('custom-range-dialog')).toBeVisible();
  await shot(page, 'transactions-custom-range-en');

  await goto(page, 'transactions-from-dashboard-category-en-light', 1440, 720);
  await expect(page.getByRole('button', { name: 'Food', exact: true })).toBeVisible();
  await expect(page.locator('.figure-count')).toHaveText('2');
  await shot(page, 'transactions-from-dashboard-category-en-light');
});

test('the Transactions page renders every scenario without a CSP or console error', async ({
  page,
}) => {
  const watcher = await watch(page);
  const scenarios = [
    'transactions-en-light',
    'transactions-de-light',
    'transactions-de-dark',
    'transactions-month-de-light',
    'transactions-narrow-en',
    'transactions-chart-en',
    'transactions-chart-collapsed-en',
    'transactions-panel-collapsed-en-dark',
    'transactions-panel-expanded-en-dark',
    'transactions-row-hover-en-light',
    'transactions-selection-en',
    'transactions-detail-en',
    'transactions-more-menu-en',
    'transactions-custom-range-en',
    'transactions-from-dashboard-category-en-light',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario);
    await expect(pageRoot(page)).toBeVisible();
  }

  await goto(page, 'transactions-en-light');

  await page.getByRole('button', { name: 'All time', exact: true }).click();
  await expect(page.getByRole('listbox', { name: 'All time' })).toBeVisible();
  await page.keyboard.press('Escape');

  await page.getByLabel('Search partner or purpose…').fill('Spotify');
  await expect(page.locator('.figure-count')).toHaveText('3');
  await page.getByLabel('Clear search').click();
  await expect(page.locator('.figure-count')).toHaveText('34');

  // View > Find targets the page's own search field, not the panel's filter.
  await page.evaluate(() => {
    window.dispatchEvent(new CustomEvent('finglass:mock-menu-action', { detail: 'find' }));
  });
  await expect(page.getByLabel('Search partner or purpose…')).toBeFocused();

  const first = listRows(page).first();
  await first.getByRole('button', { name: 'More' }).click();
  await expect(page.getByRole('button', { name: 'Open details' })).toBeVisible();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByTestId('delete-transaction-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('delete-transaction-dialog')).toHaveCount(0);

  await expectClean(watcher);
});

test('every list filter changes the transactions', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light', 1440, 720);
  await expect(page.locator('.figure-count')).toHaveText('34');

  await pick(page, 'All time', 'This month');
  await expect(page.locator('.figure-count')).toHaveText('11');
  await pick(page, 'This month', 'All time');
  await expect(page.locator('.figure-count')).toHaveText('34');

  await pick(page, 'All categories', 'Food');
  await expect(page.locator('.figure-count')).toHaveText('6');
  await pick(page, 'Food', 'All categories');
  await expect(page.locator('.figure-count')).toHaveText('34');

  await pick(page, 'In and out', 'Income');
  await expect(page.locator('.figure-count')).toHaveText('7');
  await pick(page, 'Income', 'In and out');
  await expect(page.locator('.figure-count')).toHaveText('34');

  await page.getByRole('button', { name: 'Any amount', exact: true }).click();
  await page.getByLabel('min €').fill('100');
  await page.getByTestId('amount-dialog').getByRole('button', { name: 'Apply' }).click();
  await expect(page.locator('.figure-count')).toHaveText('10');
  await expect(page.getByRole('button', { name: '≥ €100.00' })).toBeVisible();
  await page.getByRole('button', { name: '≥ €100.00' }).click();
  await page.getByTestId('amount-dialog').getByRole('button', { name: 'Reset' }).click();
  await expect(page.locator('.figure-count')).toHaveText('34');

  await page
    .getByTestId('account-panel')
    .locator('[data-kind="account"][data-label="Rücklage"]')
    .click();
  await expect(page.locator('.figure-count')).toHaveText('1');
  await expect(page.locator('.col-balance')).toBeVisible();
  await expect(page.getByText('Transfer from Haushalt', { exact: true })).toBeVisible();

  await expectClean(watcher);
});

test('the search field debounces and sends only the last text', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light');
  const search = page.getByLabel('Search partner or purpose…');

  await search.fill('Spo');
  await search.fill('Spotify');
  await expect(page.locator('.figure-count')).toHaveText('3');
  const calls = await page.evaluate(
    () =>
      (window as unknown as { __finglassMock?: { transactionsSearchCalls: string[] } })
        .__finglassMock?.transactionsSearchCalls ?? [],
  );
  expect(calls).toEqual(['Spotify']);

  await expectClean(watcher);
});

test('rows select by click, shift range, select all and clear', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light', 1440, 720);
  const rows = listRows(page);

  await rows.nth(0).click({ modifiers: ['Meta'] });
  await expect(page.getByText('1 selected', { exact: true })).toBeVisible();
  await expect(page.locator('.selection-totals')).toHaveText('-€60.00');

  await rows.nth(2).click({ modifiers: ['Shift'] });
  await expect(page.getByText('3 selected', { exact: true })).toBeVisible();
  await expect(page.locator('.selection-totals')).toHaveText('+€145.00');

  await page.getByTestId('selection-bar').getByRole('button', { name: 'Select all' }).click();
  await expect(page.getByText('34 selected', { exact: true })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: 'Select all' })).toHaveAttribute(
    'aria-checked',
    'true',
  );

  await page.getByTestId('selection-bar').getByRole('button', { name: 'Clear selection' }).click();
  await expect(page.getByTestId('selection-bar')).toHaveCount(0);

  await expectClean(watcher);
});

test('the custom-range, amount and delete dialogs validate and submit', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light', 1440, 720);

  // Custom range, including both validation errors.
  await page.getByRole('button', { name: 'All time', exact: true }).click();
  await page.getByRole('option', { name: 'Custom range…', exact: true }).click();
  let dialog = page.getByTestId('custom-range-dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('textbox', { name: 'From', exact: true }).fill('not a date');
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter a valid date (dd.mm.yyyy)');
  await dialog.getByRole('textbox', { name: 'From', exact: true }).fill('15.10.2026');
  await dialog.getByRole('textbox', { name: 'To', exact: true }).fill('01.10.2026');
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(dialog.getByRole('alert')).toHaveText('The end date must be after the start date');
  await dialog.getByRole('textbox', { name: 'From', exact: true }).fill('01.10.2026');
  await dialog.getByRole('textbox', { name: 'To', exact: true }).fill('15.10.2026');
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: '01.10.2026 – 15.10.2026' })).toBeVisible();
  await expect(page.locator('.figure-count')).toHaveText('8');

  // The amount dialog refuses a non-number and applies the bounds.
  await pick(page, '01.10.2026 – 15.10.2026', 'All time');
  await page.getByRole('button', { name: 'Any amount', exact: true }).click();
  dialog = page.getByTestId('amount-dialog');
  await dialog.getByLabel('min €').fill('oops');
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter a valid amount');
  await dialog.getByLabel('min €').fill('100');
  await dialog.getByLabel('max €').fill('500');
  await dialog.getByRole('button', { name: 'Apply' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: '€100.00–€500.00' })).toBeVisible();
  await expect(page.locator('.figure-count')).toHaveText('6');

  // Deleting one booking asks first and then removes it.
  const before = Number(await summaryCount(page));
  const first = listRows(page).first();
  await first.getByRole('button', { name: 'More' }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  dialog = page.getByTestId('delete-transaction-dialog');
  await expect(dialog.getByRole('heading', { name: 'Delete transaction?' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  await first.getByRole('button', { name: 'More' }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page
    .getByTestId('delete-transaction-dialog')
    .getByRole('button', { name: 'Delete' })
    .click();
  await expect(page.getByTestId('delete-transaction-dialog')).toHaveCount(0);
  await expect(page.locator('.figure-count')).toHaveText(String(before - 1));
  await expect(page.getByRole('status')).toContainText('Deleted 1 transaction');

  await expectClean(watcher);
});

test('the localized dialog errors follow the DTO language', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-de-light', 1440, 900);

  await page.getByRole('button', { name: 'Gesamter Zeitraum', exact: true }).click();
  await page.getByRole('option', { name: 'Eigener Zeitraum…', exact: true }).click();
  const dialog = page.getByTestId('custom-range-dialog');
  await dialog.getByRole('textbox', { name: 'Von', exact: true }).fill('kein Datum');
  await dialog.getByRole('button', { name: 'Anwenden' }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Gültiges Datum eingeben (tt.mm.jjjj)');

  await dialog.getByRole('button', { name: 'Abbrechen' }).click();
  await page.getByRole('button', { name: 'Jeder Betrag', exact: true }).click();
  const amount = page.getByTestId('amount-dialog');
  await amount.getByLabel('min €').fill('x');
  await amount.getByRole('button', { name: 'Anwenden' }).click();
  await expect(amount.getByRole('alert')).toHaveText('Gültigen Betrag eingeben');

  await expectClean(watcher);
});

test('the detail panel opens, changes category and closes', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'transactions-en-light', 1440, 720);

  await listRows(page).first().click();
  const detail = page.getByTestId('transaction-detail');
  await expect(detail).toBeVisible();
  await expect(detail.getByRole('heading', { name: 'Bürobedarf GmbH' })).toBeVisible();
  await expect(detail.getByText('-€60.00', { exact: true })).toBeVisible();
  await detail.getByRole('button', { name: /Category: Office/ }).click();
  await page.getByRole('option', { name: 'Health (tax)', exact: true }).click();
  await expect(detail.getByRole('button', { name: /Category: Health/ })).toBeVisible();
  await detail.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(detail).toHaveCount(0);

  await expectClean(watcher);
});

test('a Dashboard category link lands with the filter set', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'dashboard-en-light');

  await page.locator('[data-category-id="11"]').click();
  await expect(page.getByRole('heading', { name: 'Transactions', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Food', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'This month', exact: true })).toBeVisible();
  await expect(page.locator('.figure-count')).toHaveText('2');
  await expect(page.getByText('Example Market')).toHaveCount(2);

  await expectClean(watcher);
});

test('the page pulls its state on mount', async ({ page }) => {
  await goto(page, 'transactions-en-light');
  await expect(page.locator('.figure-count')).toHaveText('34');
  const calls = await page.evaluate(
    () =>
      (window as unknown as { __finglassMock?: { transactionsStateCalls: number } }).__finglassMock
        ?.transactionsStateCalls ?? 0,
  );
  expect(calls).toBeGreaterThanOrEqual(1);
});
