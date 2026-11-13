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

type MockDebug = {
  reportsStateCalls: number;
  reportsCalls: string[];
  reportsExportCalls: string[];
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
  return page.getByTestId('reports-page');
}

async function debug(page: Page): Promise<MockDebug> {
  return page.evaluate(() => (window as unknown as { __finglassMock: MockDebug }).__finglassMock);
}

async function openExport(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Export', exact: true }).click();
  await expect(page.getByTestId('reports-export-menu')).toBeVisible();
}

test('the Reports reference screenshots', async ({ page }) => {
  await goto(page, 'reports-summary-en-light', 1100, 720);
  await expect(pageRoot(page)).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Tax summary' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await shot(page, 'reports-summary-en-light');

  await goto(page, 'reports-summary-de-light', 1440, 900);
  await expect(page.getByRole('heading', { name: 'Berichte' })).toBeVisible();
  await expect(page.getByText('25.000,00 €', { exact: true })).toBeVisible();
  await shot(page, 'reports-summary-de-light');

  await goto(page, 'reports-summary-de-dark', 1100, 720, 'dark');
  await expect(page.getByRole('heading', { name: 'Berichte' })).toBeVisible();
  await shot(page, 'reports-summary-de-dark');

  await goto(page, 'reports-category-en-light', 1100, 720);
  await expect(page.getByRole('tab', { name: 'Category analysis' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await shot(page, 'reports-category-en-light');

  await goto(page, 'reports-monthly-en-light', 1100, 720);
  await expect(page.getByText('Monthly income & expenses - 2026')).toBeVisible();
  await shot(page, 'reports-monthly-en-light');

  await goto(page, 'reports-monthly-de-dark', 1100, 720, 'dark');
  await expect(page.getByText('Monatliche Einnahmen & Ausgaben – 2026')).toBeVisible();
  await shot(page, 'reports-monthly-de-dark');

  await goto(page, 'reports-recurring-en-light', 1100, 720);
  await expect(page.getByText('Subscriptions, contracts and regular income')).toBeVisible();
  await shot(page, 'reports-recurring-en-light');

  await goto(page, 'reports-recurring-de-light', 1440, 900);
  await expect(page.getByText('Abos, Verträge und regelmäßige Einnahmen')).toBeVisible();
  await shot(page, 'reports-recurring-de-light');

  await goto(page, 'reports-tax-en-light', 1100, 720);
  await expect(page.getByText('Possibly tax relevant: 2 bookings')).toBeVisible();
  await shot(page, 'reports-tax-en-light');

  await goto(page, 'reports-tax-de-dark', 1100, 720, 'dark');
  await expect(page.getByText('Möglicherweise steuerrelevant: 2 Buchungen')).toBeVisible();
  await shot(page, 'reports-tax-de-dark');

  await goto(page, 'reports-tax-missing-en-light', 1100, 720);
  await expect(page.getByRole('checkbox', { name: 'Missing receipts only' })).toBeChecked();
  await shot(page, 'reports-tax-missing-en-light');

  await goto(page, 'reports-tax-narrow-en-light', 900, 720);
  await expect(pageRoot(page)).toBeVisible();
  await shot(page, 'reports-tax-narrow-en-light');

  await goto(page, 'reports-export-menu-en-light', 1100, 720);
  await openExport(page);
  await shot(page, 'reports-export-menu-en-light');
});

test('the tabs switch to their own content', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-summary-en-light');
  await expect(page.getByText('Tax-relevant expenses by category')).toBeVisible();

  await page.getByRole('tab', { name: 'Tax bookings' }).click();
  await expect(page.getByRole('tab', { name: 'Tax bookings' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByText('Possibly tax relevant: 2 bookings')).toBeVisible();

  await page.getByRole('tab', { name: 'Monthly breakdown' }).click();
  await expect(page.getByText('Monthly income & expenses - 2026')).toBeVisible();

  await page.getByRole('tab', { name: 'Category analysis' }).click();
  await expect(page.getByText('Spending by category')).toBeVisible();
  await expect(page.getByText('Top recipients')).toBeVisible();

  await page.getByRole('tab', { name: 'Recurring' }).click();
  await expect(page.getByText('Subscriptions, contracts and regular income')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Previous year' })).toHaveCount(0);

  await page.getByRole('tab', { name: 'Tax summary' }).click();
  await expect(page.getByText('Tax-relevant expenses by category')).toBeVisible();
  await expectClean(watcher);
});

test('the year moves and the page pulls its state on mount', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-summary-en-light');
  await expect(pageRoot(page)).toBeVisible();
  await expect(page.getByText('2026', { exact: true })).toBeVisible();

  const before = (await debug(page)).reportsStateCalls;

  await page.getByRole('button', { name: 'Previous year' }).click();
  await expect(page.getByText('2025', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next year' })).toBeEnabled();

  await page.getByRole('button', { name: 'Next year' }).click();
  await expect(page.getByText('2026', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Previous year' }).click();
  await expect(page.getByText('2025', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Accounts', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible();
  await expect(pageRoot(page)).toHaveCount(0);

  // Navigation only publishes shell:changed; the year can only come back
  // through the mount pull.
  await page.getByRole('button', { name: 'Reports', exact: true }).click();
  await expect(page.getByText('2025', { exact: true })).toBeVisible();
  const after = (await debug(page)).reportsStateCalls;
  expect(after).toBeGreaterThan(before);
  await expectClean(watcher);
});

test('the custom range validates and applies', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-summary-en-light');
  await page.getByRole('radio', { name: 'Custom range' }).click();

  const from = page.getByRole('textbox', { name: 'From' });
  const to = page.getByRole('textbox', { name: 'To' });
  await expect(from).toHaveValue('01.08.2026');
  await expect(to).toHaveValue('15.10.2026');

  await from.fill('not a date');
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByText('Enter a valid date (dd.mm.yyyy)')).toBeVisible();

  await from.fill('31.12.2026');
  await to.fill('01.01.2026');
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByText('The end date must be after the start date')).toBeVisible();

  await from.fill('01.12.2025');
  await to.fill('31.01.2026');
  await page.getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByText('The end date must be after the start date')).toHaveCount(0);
  await expect(from).toHaveValue('01.12.2025');
  await expect(to).toHaveValue('31.01.2026');
  await expect(page.getByRole('radio', { name: 'Custom range' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await expect(page.getByText('Tax-relevant expenses by category')).toBeVisible();

  await page.getByRole('radio', { name: 'Tax year' }).click();
  await expect(page.getByRole('button', { name: 'Previous year' })).toBeVisible();
  await expect(page.getByText('2026', { exact: true })).toBeVisible();
  await expect(from).toHaveCount(0);
  await expectClean(watcher);
});

test('suggestions can be accepted and dismissed', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-tax-en-light');
  await expect(page.getByText('Possibly tax relevant: 2 bookings')).toBeVisible();

  await page
    .getByTestId('reports-suggestion')
    .first()
    .getByRole('button', { name: 'Accept', exact: true })
    .click();
  await expect(page.getByText('Possibly tax relevant: 1 booking')).toBeVisible();
  await expect(page.getByText('Tax status changed for 1 booking')).toBeVisible();
  await expect(page.getByText('€612.90', { exact: true })).toBeVisible();

  await page
    .getByTestId('reports-suggestion')
    .first()
    .getByRole('button', { name: 'Dismiss', exact: true })
    .click();
  await expect(page.getByText(/Possibly tax relevant/)).toHaveCount(0);
  await expect(page.getByText('Tax status changed for 1 booking')).toBeVisible();

  await page.getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(page.getByText('Tax status changed for 1 booking')).toHaveCount(0);
  await expectClean(watcher);
});

test('the tax filter and the row actions work', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-tax-missing-en-light');
  const filter = page.getByRole('checkbox', { name: 'Missing receipts only' });
  await expect(filter).toBeChecked();
  await expect(page.locator('.tax-booking')).toHaveCount(14);

  await filter.click();
  await expect(page.locator('.tax-booking')).toHaveCount(15);

  await page.getByRole('button', { name: 'Reset to automatic' }).first().click();
  await expect(page.getByText('Tax status changed for 1 booking')).toBeVisible();

  await page
    .locator('[data-tax-id="15"]')
    .getByRole('button', { name: 'Mark as not tax relevant' })
    .click();
  await expect(page.locator('.tax-booking')).toHaveCount(14);
  await expect(page.getByText('Tax status changed for 1 booking')).toBeVisible();
  await expectClean(watcher);
});

test('every export entry calls its method and shows the mock message', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-export-menu-en-light');
  const mock = await debug(page);
  expect(mock.reportsExportCalls).toEqual([]);

  await openExport(page);
  await expect(page.getByRole('menuitem')).toHaveText([
    'Export Excel',
    'Export PDF',
    'Print…',
    'Export report',
    'Export CSV',
  ]);

  await page.getByRole('menuitem', { name: 'Export Excel' }).click();
  await expect(page.getByText('Saved tax-bookings-2026.xlsx')).toBeVisible();

  await openExport(page);
  await page.getByRole('menuitem', { name: 'Export PDF' }).click();
  await expect(page.getByText('Saved tax-bookings-2026.pdf')).toBeVisible();

  await openExport(page);
  await page.getByRole('menuitem', { name: 'Print…' }).click();
  await expect(page.getByText('Sent to the printer')).toBeVisible();

  await openExport(page);
  await page.getByRole('menuitem', { name: 'Export report' }).click();
  await expect(page.getByText('Saved report-2026.pdf')).toBeVisible();

  await openExport(page);
  await page.getByRole('menuitem', { name: 'Export CSV' }).click();
  await expect(page.getByText('Saved finglass-2026.csv')).toBeVisible();

  expect((await debug(page)).reportsExportCalls).toEqual([
    'taxXLSX',
    'taxPDF',
    'print',
    'reportPDF',
    'csv',
  ]);
  await expectClean(watcher);
});

test('the Dashboard tax link opens the tax tab', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'dashboard-en-light');
  await expect(page.getByTestId('dashboard-page')).toBeVisible();

  await page.getByRole('button', { name: 'Tax-deductible expenses (year to date)' }).click();
  await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Tax bookings' })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByText('Possibly tax relevant: 2 bookings')).toBeVisible();
  await expectClean(watcher);
});

test('a linkable category row lands on Transactions', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-category-en-light');
  await expect(page.locator('[data-category-id="11"]')).toBeVisible();

  await page.locator('[data-category-id="11"]').click();
  await expect(page.getByRole('heading', { name: 'Transactions' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Food', exact: true })).toBeVisible();
  await expect(pageRoot(page)).toHaveCount(0);
  await expectClean(watcher);
});

test('the export menu closes on an outside click and Escape', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'reports-export-menu-en-light');
  await openExport(page);
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('reports-export-menu')).toHaveCount(0);

  await openExport(page);
  await page.mouse.click(400, 500);
  await expect(page.getByTestId('reports-export-menu')).toHaveCount(0);
  await expectClean(watcher);
});

test('every reports state renders without a CSP or console error', async ({ page }) => {
  test.setTimeout(120_000);
  const watcher = await watch(page);
  const scenarios = [
    'reports-summary-en-light',
    'reports-summary-de-light',
    'reports-summary-de-dark',
    'reports-category-en-light',
    'reports-monthly-en-light',
    'reports-monthly-de-dark',
    'reports-recurring-en-light',
    'reports-recurring-de-light',
    'reports-tax-en-light',
    'reports-tax-de-dark',
    'reports-tax-missing-en-light',
    'reports-tax-narrow-en-light',
    'reports-export-menu-en-light',
  ];
  for (const scenario of scenarios) {
    const dark = scenario.endsWith('-dark');
    await goto(page, scenario, 1100, 720, dark ? 'dark' : 'light');
    await expect(pageRoot(page)).toBeVisible();
  }

  await goto(page, 'reports-summary-en-light');
  await page.getByRole('button', { name: 'Previous year' }).click();
  await expect(page.getByText('2025', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next year' }).click();
  await expect(page.getByText('2026', { exact: true })).toBeVisible();

  await expectClean(watcher);
});
