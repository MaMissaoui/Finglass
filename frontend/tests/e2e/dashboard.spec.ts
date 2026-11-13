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

async function expectDashboard(page: Page): Promise<void> {
  await expect(page.getByTestId('dashboard-page')).toBeVisible();
  await expect(page.getByTestId('pending-panel')).toBeVisible();
}

async function backToDashboard(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Dashboard' }).click();
  await expectDashboard(page);
}

test('the dashboard reference screenshots', async ({ page }) => {
  await goto(page, 'dashboard-en-light', 1100, 720);
  await expectDashboard(page);
  await shot(page, 'dashboard-en-light');

  await goto(page, 'dashboard-de-light', 1440, 900);
  await expectDashboard(page);
  await shot(page, 'dashboard-de-light');

  await goto(page, 'dashboard-de-dark', 1100, 720, 'dark');
  await expectDashboard(page);
  await shot(page, 'dashboard-de-dark');

  await goto(page, 'dashboard-full-de-light', 1440, 1800);
  await expectDashboard(page);
  await shot(page, 'dashboard-full-de-light');

  await goto(page, 'dashboard-first-run-en', 1100, 720);
  await expect(page.getByTestId('dashboard-first-run')).toBeVisible();
  await shot(page, 'dashboard-first-run-en');

  await goto(page, 'dashboard-no-carry-en-light', 1100, 720);
  await expectDashboard(page);
  await shot(page, 'dashboard-no-carry-en-light');

  await goto(page, 'dashboard-month-menu-en-light', 1100, 720);
  await expectDashboard(page);
  await page.getByRole('button', { name: 'October 2026' }).click();
  await expect(page.getByTestId('month-menu')).toBeVisible();
  await shot(page, 'dashboard-month-menu-en-light');

  await goto(page, 'dashboard-chart-hover-en-light', 1440, 1000);
  await expectDashboard(page);
  await page.locator('[data-chart-slot="4"]').hover();
  await expect(page.getByText('September 2026', { exact: true })).toBeVisible();
  await shot(page, 'dashboard-chart-hover-en-light');
});

test('the month header moves, selects and returns to the current month', async ({ page }) => {
  await goto(page, 'dashboard-en-light');
  await expectDashboard(page);

  const title = page.getByRole('button', { name: 'October 2026' });
  await expect(title).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next month' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Current month' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Previous month' }).click();
  await expect(page.getByRole('button', { name: 'September 2026' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Current month' })).toBeVisible();

  await page.getByRole('button', { name: 'Next month' }).click();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();

  await page.getByRole('button', { name: 'Previous month' }).click();
  await page.getByRole('button', { name: 'Current month' }).click();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();

  await page.getByRole('button', { name: 'October 2026' }).click();
  await expect(page.getByTestId('month-menu')).toBeVisible();
  await expect(page.getByRole('menuitemradio', { name: 'October 2026' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await page.getByRole('menuitemradio', { name: 'March 2026' }).click();
  await expect(page.getByRole('button', { name: 'March 2026' })).toBeVisible();
  await expect(page.getByTestId('month-menu')).toHaveCount(0);

  await page.getByRole('button', { name: 'Current month' }).click();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();
});

test('every dashboard link lands on its screen', async ({ page }) => {
  await goto(page, 'dashboard-en-light');
  await expectDashboard(page);

  await page.locator('[data-category-id="11"]').click();
  await expect(page.getByRole('heading', { name: 'Transactions' })).toBeVisible();

  await backToDashboard(page);
  await page.getByRole('button', { name: '4 uncategorized' }).click();
  await expect(page.getByRole('heading', { name: 'Transactions' })).toBeVisible();

  await backToDashboard(page);
  await page.getByRole('button', { name: 'View all' }).click();
  await expect(page.getByRole('heading', { name: 'Transactions' })).toBeVisible();

  await backToDashboard(page);
  await page.getByRole('button', { name: /Interest/ }).click();
  await expect(page.getByRole('heading', { name: 'Transactions' })).toBeVisible();

  await backToDashboard(page);
  await page.getByRole('button', { name: 'Tax-deductible expenses (year to date)' }).click();
  await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();

  await goto(page, 'dashboard-first-run-en');
  await expect(page.getByTestId('dashboard-first-run')).toBeVisible();
  await page.getByRole('button', { name: 'Add bank' }).click();
  await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible();

  await goto(page, 'dashboard-first-run-en');
  await page.getByRole('button', { name: 'Open settings' }).click();
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
});

test('the error text can be dismissed', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'dashboard-error-en-light');
  await expectDashboard(page);
  const error = page.getByTestId('dashboard-error');
  await expect(error).toBeVisible();
  await expect(error).toContainText('an unexpected error');

  await page.getByRole('button', { name: 'Dismiss' }).click();
  await expect(error).toHaveCount(0);
  await expectDashboard(page);
  await expectClean(watcher);
});

test('hovering a chart month shows its band, tooltip and figures', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'dashboard-chart-hover-en-light', 1440, 1000);
  await expectDashboard(page);

  await expect(page.getByText('September 2026', { exact: true })).toHaveCount(0);
  await page.locator('[data-chart-slot="4"]').hover();
  const tooltip = page.getByTestId('chart-tooltip');
  await expect(tooltip).toBeVisible();
  await expect(tooltip.getByText('September 2026', { exact: true })).toBeVisible();
  await expect(tooltip.getByText('+€1,590.00', { exact: true })).toBeVisible();
  await expect(tooltip.getByText('-€179.99', { exact: true })).toBeVisible();
  await expect(tooltip.getByText('Income', { exact: true })).toBeVisible();
  await expect(tooltip.getByText('Expenses', { exact: true })).toBeVisible();

  await page.locator('[data-chart-slot="5"]').hover();
  await expect(tooltip.getByText('October 2026', { exact: true })).toBeVisible();
  await expect(tooltip.getByText('so far', { exact: true })).toBeVisible();

  await page.locator('[data-chart-slot="0"]').hover();
  await expect(tooltip.getByText('May 2026', { exact: true })).toBeVisible();
  await expectClean(watcher);
});

test('the page pulls the state when it mounts after a finished load', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'dashboard-en-light');
  await expectDashboard(page);

  await page.getByRole('button', { name: 'Previous month' }).click();
  await expect(page.getByRole('button', { name: 'September 2026' })).toBeVisible();

  await page.getByRole('button', { name: 'Accounts' }).click();
  await expect(page.getByRole('heading', { name: 'Accounts' })).toBeVisible();
  await expect(page.getByTestId('dashboard-page')).toHaveCount(0);

  // Navigation only publishes shell:changed; no dashboard event follows, so
  // the month can only come back through the mount pull.
  await page.getByRole('button', { name: 'Dashboard' }).click();
  await expectDashboard(page);
  await expect(page.getByRole('button', { name: 'September 2026' })).toBeVisible();
  await expect(page.getByText('Carried over from August 2026')).toBeVisible();
  await expectClean(watcher);
});

test('every dashboard state renders without a CSP or console error', async ({ page }) => {
  test.setTimeout(120_000);
  const watcher = await watch(page);

  const scenarios = [
    'dashboard-en-light',
    'dashboard-de-light',
    'dashboard-de-dark',
    'dashboard-full-de-light',
    'dashboard-first-run-en',
    'dashboard-no-carry-en-light',
    'dashboard-month-menu-en-light',
    'dashboard-chart-hover-en-light',
    'dashboard-error-en-light',
    'dashboard-loading-en-light',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario, 1100, 720, scenario === 'dashboard-de-dark' ? 'dark' : 'light');
    await expect(page.getByTestId('dashboard-page')).toBeVisible();
  }

  await goto(page, 'dashboard-en-light');
  await expectDashboard(page);
  await page.getByRole('button', { name: 'October 2026' }).click();
  await expect(page.getByTestId('month-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('month-menu')).toHaveCount(0);

  await page.getByRole('button', { name: 'Previous month' }).click();
  await expect(page.getByRole('button', { name: 'September 2026' })).toBeVisible();
  await page.getByRole('button', { name: 'Current month' }).click();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();

  await page.locator('[data-chart-slot="4"]').hover();
  await expect(page.getByText('September 2026', { exact: true })).toBeVisible();

  await expectClean(watcher);
});
