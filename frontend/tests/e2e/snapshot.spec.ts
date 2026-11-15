import { expect, test } from '@playwright/test';
import path from 'node:path';

// The fixture is a real export: cmd/finglass-shots run on the synthetic backup of
// its own end-to-end test (invented data). Go writes empty lists as null, so this
// also covers the shapes the exporter really produces.
const fixture = path.resolve('tests/fixtures/snapshot.json');

const pages = [
  'dashboard-page',
  'accounts-page',
  'transactions-page',
  'documents-page',
  'reports-page',
  'settings-page',
];

test('the snapshot scenario renders the exported data read-only', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.route('**/snapshot.json', (route) =>
    route.fulfill({ path: fixture, contentType: 'application/json' }),
  );

  await page.goto('/?snapshot=snapshot.json');
  await expect(page.getByTestId('dashboard-page')).toBeVisible();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();
  await expect(page.getByText('€2,500.00')).toBeVisible();
  await expect(page.getByText('Food', { exact: true }).first()).toBeVisible();

  // The export holds one month, so the month header has nowhere to go (the
  // read-only actions are covered in tests/unit/snapshot-mock.test.ts).
  await expect(page.getByRole('button', { name: 'Previous month' })).toBeDisabled();

  // Every page and the second language render from the export, as the
  // screenshot runner needs them.
  await page.getByTestId('language-button').click();
  await page.getByTestId('language-menu').getByRole('menuitemradio', { name: 'Deutsch' }).click();
  for (const [index, testId] of pages.entries()) {
    await page.locator('[data-testid="sidebar"] .sidebar-item').nth(index).click();
    await expect(page.getByTestId(testId)).toBeVisible();
  }
  await expect(page.getByRole('heading', { name: 'Einstellungen' })).toBeVisible();
  expect(errors).toEqual([]);
});
