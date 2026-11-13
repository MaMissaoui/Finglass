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
  await page.goto(`/?scenario=${scenario}`);
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

function panel(page: Page) {
  return page.getByTestId('account-panel');
}

function panelLabels(page: Page): Promise<string[]> {
  return panel(page)
    .locator('[data-kind]')
    .evaluateAll((rows) => rows.map((row) => row.getAttribute('data-label') ?? ''));
}

test('the Accounts reference screenshots', async ({ page }) => {
  await goto(page, 'accounts', 1100, 720);
  await expect(page.getByRole('heading', { name: 'Accounts', exact: true })).toBeVisible();
  await expect(page.getByText('Net of 7 accounts')).toBeVisible();
  await shot(page, 'accounts-en-light');

  await goto(page, 'accounts-de-light', 1440, 900);
  await expect(page.getByRole('heading', { name: 'Konten', exact: true })).toBeVisible();
  await shot(page, 'accounts-de-light');

  await goto(page, 'accounts-de-dark', 1100, 720, 'dark');
  await expect(page.getByRole('heading', { name: 'Konten', exact: true })).toBeVisible();
  await shot(page, 'accounts-de-dark');

  await goto(page, 'accounts-empty', 1100, 720);
  await expect(page.getByRole('heading', { name: 'No banks yet' })).toBeVisible();
  await shot(page, 'accounts-empty-en');

  await goto(page, 'accounts-add-account', 1100, 720);
  const dialog = page.getByTestId('accounts-dialog');
  await expect(
    dialog.getByRole('heading', { name: 'Add account at Bank Norwegian' }),
  ).toBeVisible();
  await shot(page, 'accounts-add-account-en');

  await goto(page, 'accounts-detail', 1100, 720);
  await expect(
    page.getByTestId('account-detail').getByRole('heading', { name: 'Bank Norwegian · Haushalt' }),
  ).toBeVisible();
  await shot(page, 'accounts-detail-en-light');

  await goto(page, 'accounts-detail-flat-de', 1100, 720);
  await expect(
    page.getByTestId('account-detail').getByRole('heading', { name: 'Bank Norwegian · Festgeld' }),
  ).toBeVisible();
  await shot(page, 'accounts-detail-flat-de-light');

  // The panel reference is a component capture: a 360 × 720 frame with the
  // 320 px panel 24 px from the top and 20 px from the left.
  await goto(page, 'accounts-panel', 1100, 785);
  const preview = page.getByTestId('accounts-panel-preview');
  await expect(preview).toBeVisible();
  expect(await preview.boundingBox()).toEqual({ x: 241, y: 65, width: 360, height: 720 });
  await expect(panel(page)).toBeVisible();
  await preview.screenshot({
    path: path.join(screenshotDir, 'account-panel-en-light.png'),
    caret: 'initial',
  });
});

test('the Accounts page renders every scenario without a CSP or console error', async ({
  page,
}) => {
  const watcher = await watch(page);

  const scenarios = [
    'accounts',
    'accounts-de-light',
    'accounts-de-dark',
    'accounts-empty',
    'accounts-add-account',
    'accounts-detail',
    'accounts-detail-flat-de',
    'accounts-panel',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario);
    await expect(page.locator('#app > *').first()).toBeVisible();
  }

  await goto(page, 'accounts');
  await page.getByRole('button', { name: 'Add bank', exact: true }).click();
  await expect(page.getByTestId('accounts-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('accounts-dialog')).not.toBeVisible();

  await page
    .getByTestId('bank-card-1')
    .getByRole('button', { name: 'Account', exact: true })
    .click();
  await expect(page.getByTestId('accounts-dialog')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('accounts-dialog')).not.toBeVisible();

  await page.getByTestId('account-row-1').click();
  await expect(page.getByTestId('account-detail')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('account-detail')).not.toBeVisible();

  await expectClean(watcher);
});

test('the page pulls its state on mount', async ({ page }) => {
  await goto(page, 'accounts');
  await expect(page.getByText('Net of 7 accounts')).toBeVisible();
  const calls = await page.evaluate(
    () =>
      (window as unknown as { __finglassMock?: { accountsStateCalls: number } }).__finglassMock
        ?.accountsStateCalls ?? 0,
  );
  expect(calls).toBeGreaterThanOrEqual(1);
});

test('every dialog validates and submits', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts');

  // Add an offline bank, first with a blank name.
  await page.getByRole('button', { name: 'Add bank', exact: true }).click();
  let dialog = page.getByTestId('accounts-dialog');
  await dialog.getByLabel('Bank name').fill('   ');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Bank name is required');
  await dialog.getByLabel('Bank name').fill('Test Bank');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('status')).toContainText('Added Test Bank');
  await expect(page.getByTestId('bank-card-3')).toContainText('Test Bank');

  // Add an account and walk through both validation errors.
  const bank = page.getByTestId('bank-card-1');
  await bank.getByRole('button', { name: 'Account', exact: true }).click();
  dialog = page.getByTestId('accounts-dialog');
  await expect(
    dialog.getByRole('heading', { name: 'Add account at Bank Norwegian' }),
  ).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Add', exact: true })).toBeDisabled();
  await dialog.getByLabel('Account name').fill('Testkonto');
  await dialog.getByLabel('IBAN or last 4 digits of the card').fill('123');
  await dialog.getByLabel('Balance').fill('0');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter only the last 4 digits of the card.');
  await dialog.getByLabel('IBAN or last 4 digits of the card').fill('9999');
  await dialog.getByLabel('Balance').fill('not money');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Balance is not a number');
  await dialog.getByLabel('Balance').fill('12.34');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('status')).toContainText('Account added');
  await expect(bank).toContainText('Testkonto');

  // Update the balance, validating the number first.
  let row = page.getByTestId('account-row-1');
  await row.getByRole('button', { name: 'More' }).click();
  await page
    .locator('[data-testid^="account-actions-"]')
    .getByRole('button', { name: 'Edit balance' })
    .click();
  dialog = page.getByTestId('accounts-dialog');
  await expect(dialog.getByRole('heading', { name: 'Update balance' })).toBeVisible();
  await dialog.getByLabel('Balance').fill('oops');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Enter a valid balance');
  await dialog.getByLabel('Balance').fill('2500.00');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(row).toContainText('€2,500.00');

  // Rename the account.
  await row.getByRole('button', { name: 'More' }).click();
  await page
    .locator('[data-testid^="account-actions-"]')
    .getByRole('button', { name: 'Rename account' })
    .click();
  dialog = page.getByTestId('accounts-dialog');
  await expect(dialog.getByRole('heading', { name: 'Rename account' })).toBeVisible();
  await dialog.getByLabel('Account name').fill('Zuhause');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(row).toContainText('Zuhause');

  // Delete the new account.
  row = page.getByTestId(/^account-row-/).filter({ hasText: 'Testkonto' });
  await row.getByRole('button', { name: 'More' }).click();
  await page
    .locator('[data-testid^="account-actions-"]')
    .getByRole('button', { name: 'Delete account' })
    .click();
  dialog = page.getByTestId('accounts-dialog');
  await expect(dialog.getByRole('heading', { name: 'Delete account?' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await row.getByRole('button', { name: 'More' }).click();
  await page
    .locator('[data-testid^="account-actions-"]')
    .getByRole('button', { name: 'Delete account' })
    .click();
  dialog = page.getByTestId('accounts-dialog');
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('status')).toContainText('Account deleted');

  // Remove the added bank.
  const addedBank = page.getByTestId('bank-card-3');
  await addedBank.locator('.bank-head').getByRole('button', { name: 'More' }).click();
  await addedBank.getByRole('menuitem', { name: 'Remove bank' }).click();
  dialog = page.getByTestId('accounts-dialog');
  await expect(dialog.getByRole('heading', { name: 'Remove Test Bank?' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Remove bank', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('status')).toContainText('Bank removed');
  await expect(addedBank).toHaveCount(0);

  // Forget deleted bookings and dismiss the message.
  row = page.getByTestId('account-row-1');
  await row.getByRole('button', { name: 'More' }).click();
  await page
    .locator('[data-testid^="account-actions-"]')
    .getByRole('button', { name: 'Allow deleted bookings again' })
    .click();
  await expect(page.getByRole('status')).toContainText(
    'No deleted bookings are remembered for this account',
  );
  await page.getByRole('status').getByRole('button', { name: 'Dismiss' }).click();
  await expect(page.getByRole('status')).toHaveCount(0);

  // The validation message follows the language the DTO was built in.
  await goto(page, 'accounts-de-light');
  await page.getByRole('button', { name: 'Bank hinzufügen', exact: true }).click();
  dialog = page.getByTestId('accounts-dialog');
  await dialog.getByLabel('Bankname').fill('   ');
  await dialog.getByRole('button', { name: 'Hinzufügen', exact: true }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Bankname ist erforderlich');

  await expectClean(watcher);
});

test('the detail view selects a window and closes', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts-detail');

  const detail = page.getByTestId('account-detail');
  await expect(detail.getByRole('heading', { name: 'Bank Norwegian · Haushalt' })).toBeVisible();
  await expect(detail.getByText('Last 90 days: in €3,000.00 · out €167.00')).toBeVisible();
  const line = detail.locator('svg.plot path');
  await expect(line).toHaveAttribute('d', /^M /);
  await expect(detail.locator('circle.dot')).toHaveCount(1);

  await detail.getByRole('radio', { name: '30 days' }).click();
  await expect(detail.getByText('Last 30 days: in €3,000.00 · out €167.00')).toBeVisible();
  await detail.getByRole('radio', { name: '1 year' }).click();
  await expect(detail.getByText('Last 365 days: in €3,000.00 · out €167.00')).toBeVisible();

  await detail.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(detail).not.toBeVisible();

  // An account with no bookings shows the empty period text.
  await goto(page, 'accounts');
  await page.getByTestId('account-row-3').click();
  const flat = page.getByTestId('account-detail');
  await expect(flat.getByRole('heading', { name: 'Bank Norwegian · Festgeld' })).toBeVisible();
  await expect(flat.getByText('None in this period')).toBeVisible();
  await flat.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(flat).not.toBeVisible();

  await expectClean(watcher);
});

test('the account panel filters, selects and manages groups', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts-panel', 1100, 785);
  const board = panel(page);

  // Filter by a name.
  const search = board.getByLabel('Filter accounts');
  await search.fill('Dollar');
  await expect(board.getByText('1 of 7')).toBeVisible();
  await expect(board.locator('[data-kind="account"]')).toHaveCount(1);
  await expect(board.locator('[data-kind="account"]')).toContainText('Dollar');
  await search.fill('nothing matches this');
  await expect(board.getByText('0 of 7')).toBeVisible();
  await search.fill('');
  await expect(board.getByText('1 of 7')).toHaveCount(0);

  // Select an account row.
  const dollar = board.locator('[data-kind="account"][data-label="Dollar"]');
  await dollar.click();
  await expect(dollar).toHaveClass(/selected/);

  // Create a group.
  await board.getByRole('button', { name: 'New group', exact: true }).click();
  await board.getByLabel('Group name').fill('Urlaub');
  await board.locator('.form').getByRole('button', { name: 'Save', exact: true }).click();
  await expect(board.locator('[data-kind="group"][data-group-kind="custom"]')).toHaveCount(2);
  let labels = await panelLabels(page);
  expect(labels.indexOf('Urlaub')).toBeGreaterThan(labels.indexOf('Reisen'));

  // Rename it.
  let custom = board.locator('[data-kind="group"][data-group-kind="custom"]', {
    hasText: 'Urlaub',
  });
  await custom.getByRole('button', { name: 'More' }).click();
  await board.getByTestId('panel-actions').getByRole('button', { name: 'Rename group' }).click();
  await board.getByLabel('Group name').fill('Urlaub 2');
  await board.locator('.form').getByRole('button', { name: 'Save', exact: true }).click();
  await expect(
    board.locator('[data-kind="group"][data-group-kind="custom"]', { hasText: 'Urlaub 2' }),
  ).toHaveCount(1);

  // Move it above Reisen.
  custom = board.locator('[data-kind="group"][data-group-kind="custom"]', { hasText: 'Urlaub 2' });
  await custom.getByRole('button', { name: 'More' }).click();
  await board.getByTestId('panel-actions').getByRole('button', { name: 'Move up' }).click();
  labels = await panelLabels(page);
  expect(labels.indexOf('Urlaub 2')).toBeLessThan(labels.indexOf('Reisen'));

  // Expand Checking and move Haushalt into the custom group.
  const checking = board.locator('[data-kind="group"][data-label="Checking"]');
  await checking.getByRole('button', { name: 'Expand' }).click();
  const haushalt = board.locator('[data-kind="account"][data-label="Haushalt"]');
  await expect(haushalt).toBeVisible();
  await haushalt.getByRole('button', { name: 'More' }).click();
  await board.getByTestId('panel-actions').getByRole('button', { name: 'Move to group…' }).click();
  await board.locator('.form').getByRole('button', { name: 'Urlaub 2', exact: true }).click();
  await board.locator('.form').getByRole('button', { name: 'Save', exact: true }).click();
  labels = await panelLabels(page);
  expect(labels.indexOf('Haushalt')).toBeGreaterThan(labels.indexOf('Urlaub 2'));

  // Mark the all-accounts row read.
  const all = board.locator('[data-kind="all"]');
  await expect(all.locator('.count')).toHaveCount(1);
  await all.getByRole('button', { name: 'More' }).click();
  await board.getByTestId('panel-actions').getByRole('button', { name: 'Mark as read' }).click();
  await expect(all.locator('.count')).toHaveCount(0);

  // Delete the custom group; its account returns to the type groups.
  custom = board.locator('[data-kind="group"][data-group-kind="custom"]', { hasText: 'Urlaub 2' });
  await custom.getByRole('button', { name: 'More' }).click();
  await board
    .getByTestId('panel-actions')
    .getByRole('button', { name: 'Delete', exact: true })
    .click();
  await expect(board.getByText('Delete group “Urlaub 2”?')).toBeVisible();
  await board.locator('.form').getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(
    board.locator('[data-kind="group"][data-group-kind="custom"]', { hasText: 'Urlaub 2' }),
  ).toHaveCount(0);
  await expect(board.locator('[data-kind="account"][data-label="Haushalt"]')).toHaveCount(1);

  await expectClean(watcher);
});
