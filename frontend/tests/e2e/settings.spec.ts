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

async function openCategories(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Categories', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Categories' })).toBeVisible();
}

async function openRules(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Auto-categorization rules', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Auto-categorization rules' })).toBeVisible();
}

test('the settings reference screenshots', async ({ page }) => {
  const light = 'light' as const;
  const dark = 'dark' as const;

  await goto(page, 'settings-appearance-de-light', 1440, 900, light);
  await shot(page, 'settings-appearance-de-light');

  await goto(page, 'settings-appearance-de-dark', 1440, 900, dark);
  await shot(page, 'settings-appearance-de-dark');

  await goto(page, 'settings-currency-en-light');
  await shot(page, 'settings-currency-en-light');

  await goto(page, 'settings-currency-de-dark', 1100, 720, dark);
  await shot(page, 'settings-currency-de-dark');

  await goto(page, 'settings-categories-en-light');
  await shot(page, 'settings-categories-en-light');

  await goto(page, 'settings-categories-de-light', 1440, 900);
  await shot(page, 'settings-categories-de-light');

  await goto(page, 'settings-categories-de-dark', 1100, 720, dark);
  await shot(page, 'settings-categories-de-dark');

  await goto(page, 'settings-categories-de-narrow');
  await shot(page, 'settings-categories-de-narrow');

  await goto(page, 'settings-category-dialog-en');
  await expect(page.getByTestId('category-dialog')).toBeVisible();
  await shot(page, 'settings-category-dialog-en');

  await goto(page, 'settings-rules-en-light');
  await shot(page, 'settings-rules-en-light');

  await goto(page, 'settings-rules-de-light', 1440, 900);
  await shot(page, 'settings-rules-de-light');

  await goto(page, 'settings-rules-de-dark', 1100, 720, dark);
  await shot(page, 'settings-rules-de-dark');

  await goto(page, 'settings-rule-dialog-en');
  await expect(page.getByTestId('rule-dialog')).toBeVisible();
  await shot(page, 'settings-rule-dialog-en');

  await goto(page, 'settings-rule-import-en');
  await expect(page.getByTestId('rule-import-dialog')).toBeVisible();
  await shot(page, 'settings-rule-import-en');

  await goto(page, 'settings-tax-keywords-en-light');
  await shot(page, 'settings-tax-keywords-en-light');

  await goto(page, 'settings-tax-keywords-de-dark', 1100, 720, dark);
  await shot(page, 'settings-tax-keywords-de-dark');

  await goto(page, 'settings-about-bank-codes-en');
  await shot(page, 'settings-about-bank-codes-en');

  await goto(page, 'settings-section-menu-en-light', 880, 720);
  await page.locator('.section-dropdown').click();
  await expect(page.getByTestId('settings-section-menu')).toBeVisible();
  await shot(page, 'settings-section-menu-en-light');

  await goto(page, 'settings-narrow-en-light', 880, 720);
  await shot(page, 'settings-narrow-en-light');

  await goto(page, 'settings-fr-light');
  await shot(page, 'settings-fr-light');

  // Playwright injects a temporary style while capturing, which the strict CSP
  // reports; that is the test tooling, not the application. The clean-render
  // test below checks the policy without taking screenshots.
});

test('every settings state renders without a CSP or console error', async ({ page }) => {
  test.setTimeout(120_000);
  const watcher = await watch(page);

  const scenarios = [
    'settings',
    'settings-empty',
    'settings-appearance-de-light',
    'settings-appearance-de-dark',
    'settings-currency-en-light',
    'settings-currency-de-dark',
    'settings-categories-en-light',
    'settings-categories-de-light',
    'settings-categories-de-dark',
    'settings-categories-de-narrow',
    'settings-category-dialog-en',
    'settings-rules-en-light',
    'settings-rules-de-light',
    'settings-rules-de-dark',
    'settings-rule-dialog-en',
    'settings-rule-import-en',
    'settings-tax-keywords-en-light',
    'settings-tax-keywords-de-dark',
    'settings-about-bank-codes-en',
    'settings-section-menu-en-light',
    'settings-narrow-en-light',
    'settings-fr-light',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario, 1100, 720, scenario.endsWith('-dark') ? 'dark' : 'light');
    await expect(page.getByTestId('settings-page')).toBeVisible();
  }

  await goto(page, 'settings');
  await openCategories(page);
  await page.getByRole('button', { name: 'Add category' }).click();
  await expect(page.getByTestId('category-dialog')).toBeVisible();
  await page.getByTestId('category-dialog').getByRole('button', { name: 'Cancel' }).click();

  await openRules(page);
  await page.getByRole('button', { name: 'Add rule' }).click();
  await expect(page.getByTestId('rule-dialog')).toBeVisible();
  await page.getByTestId('rule-dialog').getByRole('button', { name: 'Cancel' }).click();

  await page.getByRole('button', { name: 'Import rules…' }).click();
  await expect(page.getByTestId('rule-import-dialog')).toBeVisible();
  await page.getByTestId('rule-import-dialog').getByRole('button', { name: 'Cancel' }).click();

  await goto(page, 'settings-narrow-en-light', 880, 720);
  await page.locator('.section-dropdown').click();
  await expect(page.getByTestId('settings-section-menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('settings-section-menu')).not.toBeVisible();

  await expectClean(watcher);
});

test('the section menu and the narrow drop-down navigate', async ({ page }) => {
  await goto(page, 'settings');

  await openCategories(page);
  await expect(page.locator('.category-row').first()).toContainText('Donations');

  await page.getByRole('button', { name: 'About Finglass', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'About Finglass' })).toBeVisible();
  await expect(
    page.getByTestId('settings-page').getByText('Finglass', { exact: true }),
  ).toBeVisible();

  await goto(page, 'settings-narrow-en-light', 880, 720);
  await page.locator('.section-dropdown').click();
  await expect(page.getByTestId('settings-section-menu')).toBeVisible();
  await page
    .getByTestId('settings-section-menu')
    .getByRole('menuitemradio', { name: 'Auto-categorization rules' })
    .click();
  await expect(page.getByTestId('settings-section-menu')).not.toBeVisible();
  await expect(page.getByRole('heading', { name: 'Auto-categorization rules' })).toBeVisible();
});

test('the primary currency changes through Settings', async ({ page }) => {
  await goto(page, 'settings');

  const trigger = page.getByRole('button', { name: 'EUR', exact: true });
  await expect(trigger).toBeVisible();
  await trigger.click();
  await page.getByRole('option', { name: 'USD' }).click();

  await expect(page.getByRole('button', { name: 'USD', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'EUR', exact: true })).toHaveCount(0);
});

test('a category is added, edited, validated and deleted', async ({ page }) => {
  await goto(page, 'settings');
  await openCategories(page);

  await page.getByRole('button', { name: 'Add category' }).click();
  const dialog = page.getByTestId('category-dialog');
  await expect(dialog).toBeVisible();

  const name = dialog.getByRole('textbox', { name: 'Name' });
  const budget = dialog.getByRole('textbox', { name: 'Monthly budget (€, optional)' });

  await name.fill('Food');
  await expect(dialog.getByText('Category already exists')).toBeVisible();

  await name.fill('Testkategorie');
  await budget.fill('abc');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog.getByText('Budget is not a number')).toBeVisible();

  await budget.fill('12.50');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toHaveCount(0);

  const row = page.locator('.category-row').filter({ hasText: 'Testkategorie' });
  await expect(row).toBeVisible();
  await expect(row).toContainText('Budget €12.50');
  await expect(row).toContainText('0 bookings');

  await row.getByRole('button', { name: 'Edit' }).click();
  const edit = page.getByTestId('category-dialog');
  await expect(edit.getByRole('heading', { name: 'Edit category' })).toBeVisible();
  await edit.getByRole('textbox', { name: 'Monthly budget (€, optional)' }).fill('20');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(edit).toHaveCount(0);
  await expect(row).toContainText('Budget €20.00');

  await row.getByRole('button', { name: 'Delete' }).click();
  const confirm = page.getByTestId('category-delete-dialog');
  await expect(confirm.getByText('Delete category Testkategorie?')).toBeVisible();
  await confirm.getByRole('button', { name: 'Delete' }).click();
  await expect(row).toHaveCount(0);
});

test('the default categories are added once', async ({ page }) => {
  await goto(page, 'settings-empty');
  await expect(page.getByText('No categories yet.')).toBeVisible();

  await page.getByRole('button', { name: 'Add default categories' }).click();
  await expect(page.getByText('24 default categories added')).toBeVisible();
  await expect(page.getByText('Groceries', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('menuitem', { name: 'Add default categories' }).click();
  await expect(page.getByText('All default categories already exist')).toBeVisible();
});

test('duplicate category colours are fixed', async ({ page }) => {
  await goto(page, 'settings');
  await openCategories(page);

  const food = page.locator('.category-row').filter({ hasText: 'Food' });
  await food.getByRole('button', { name: 'Edit' }).click();
  const dialog = page.getByTestId('category-dialog');
  await dialog.getByRole('button', { name: '#D56A93' }).click();
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toHaveCount(0);

  await page.getByRole('button', { name: 'More' }).click();
  await page.getByRole('menuitem', { name: 'Give each category its own colour' }).click();
  await expect(page.getByText('1 category recoloured')).toBeVisible();

  await page.getByRole('button', { name: 'More' }).click();
  await expect(
    page.getByRole('menuitem', { name: 'Give each category its own colour' }),
  ).toHaveCount(0);
  await page.keyboard.press('Escape');
});

test('a rule is added, edited, validated and deleted', async ({ page }) => {
  await goto(page, 'settings');
  await openRules(page);

  await page.getByRole('button', { name: 'Add rule' }).click();
  const dialog = page.getByTestId('rule-dialog');
  await expect(dialog).toBeVisible();

  const pattern = dialog.getByRole('textbox', { name: 'Pattern (e.g. rewe|edeka|aldi)' });
  const priority = dialog.getByRole('textbox', { name: 'Priority' });

  await pattern.fill('(');
  await expect(dialog.getByText(/Invalid regular expression/)).toBeVisible();

  await pattern.fill('Test');
  await priority.fill('abc');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog.getByText('Priority must be a whole number')).toBeVisible();

  await priority.fill('7');
  await dialog.getByRole('button', { name: 'Save' }).click();
  await expect(dialog).toHaveCount(0);

  const row = page.locator('.rule-row').filter({ hasText: 'Test' });
  await expect(row).toBeVisible();
  await expect(row).toContainText('prio 7');

  await row.getByRole('button', { name: 'Edit' }).click();
  const edit = page.getByTestId('rule-dialog');
  await expect(edit.getByRole('heading', { name: 'Edit rule' })).toBeVisible();
  await edit.getByRole('textbox', { name: 'Priority' }).fill('8');
  await edit.getByRole('button', { name: 'Save' }).click();
  await expect(edit).toHaveCount(0);
  await expect(row).toContainText('prio 8');

  await row.getByRole('button', { name: 'Delete' }).click();
  const confirm = page.getByTestId('rule-delete-dialog');
  await expect(confirm.getByText('Delete rule Test?')).toBeVisible();
  await confirm.getByRole('button', { name: 'Delete' }).click();
  await expect(row).toHaveCount(0);
});

test('rules export and the import preview confirms and cancels', async ({ page }) => {
  await goto(page, 'settings');
  await openRules(page);

  await page.getByRole('button', { name: 'Export rules…' }).click();
  await expect(page.getByText('3 rules exported')).toBeVisible();

  await page.getByRole('button', { name: 'Import rules…' }).click();
  const preview = page.getByTestId('rule-import-dialog');
  await expect(preview).toBeVisible();
  await expect(preview.getByText('3 new rules')).toBeVisible();
  await expect(preview.getByText('2 categories to create')).toBeVisible();
  await expect(preview.getByText('Household, Salary')).toBeVisible();
  await expect(preview.getByText('1 duplicate skipped')).toBeVisible();
  await expect(preview.getByText('1 conflict skipped')).toBeVisible();
  await expect(
    preview.getByText('Line 7: Example Market → Household (existing: Food)'),
  ).toBeVisible();
  await expect(preview.getByText('1 invalid row')).toBeVisible();
  await expect(preview.getByText('Line 8: Expected 6 columns, found 2')).toBeVisible();
  await expect(preview.getByText('Categorize 2 uncategorized bookings now')).toBeVisible();

  await preview.getByRole('button', { name: 'Cancel' }).click();
  await expect(preview).toHaveCount(0);
  await page.getByRole('button', { name: 'Categories', exact: true }).click();
  await expect(page.getByText('Household', { exact: true })).toHaveCount(0);

  await page.getByRole('button', { name: 'Auto-categorization rules', exact: true }).click();
  await page.getByRole('button', { name: 'Import rules…' }).click();
  await expect(page.getByTestId('rule-import-dialog')).toBeVisible();
  await page.getByTestId('rule-import-dialog').getByRole('button', { name: 'Import' }).click();
  await expect(
    page.getByText('3 rules and 2 categories added, 2 bookings categorized'),
  ).toBeVisible();

  await page.getByRole('button', { name: 'Categories', exact: true }).click();
  await expect(page.getByText('Household', { exact: true })).toBeVisible();
  await expect(page.getByText('Salary', { exact: true })).toBeVisible();
});

test('tax keywords are saved and reset to the defaults', async ({ page }) => {
  await goto(page, 'settings-tax-keywords-en-light');

  const card = page.getByTestId('tax-keywords-card');
  const field = card.getByRole('textbox', { name: 'Tax suggestions' });
  await expect(card.getByText('3 keywords')).toBeVisible();

  await field.fill('eins\nzwei');
  await card.getByRole('button', { name: 'Save' }).click();
  await expect(card.getByText('2 keywords')).toBeVisible();

  await card.getByRole('button', { name: 'Reset to defaults' }).click();
  await expect(card.getByText('23 keywords')).toBeVisible();
  await expect(field).toHaveValue(/Spende/);
});

test('the page pulls its state on mount', async ({ page }) => {
  await goto(page, 'settings');
  const calls = await page.evaluate(() => {
    const mock = (window as unknown as { __finglassMock?: { settingsStateCalls: number } })
      .__finglassMock;
    return mock?.settingsStateCalls ?? 0;
  });
  expect(calls).toBeGreaterThan(0);
});
