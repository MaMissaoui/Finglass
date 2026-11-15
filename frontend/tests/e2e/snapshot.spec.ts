import { expect, test } from '@playwright/test';
import path from 'node:path';

const fixture = path.resolve('tests/fixtures/snapshot.json');

test('the snapshot scenario renders the exported data read-only', async ({ page }) => {
  await page.route('**/snapshot.json', (route) =>
    route.fulfill({ path: fixture, contentType: 'application/json' }),
  );

  await page.goto('/?snapshot=snapshot.json');
  await expect(page.getByTestId('dashboard-page')).toBeVisible();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();
  await expect(page.getByText('€1,000.00')).toBeVisible();
  await expect(page.getByText('Demo Foods', { exact: true }).first()).toBeVisible();

  // The scenario is read-only: a month change keeps the exported figures.
  await page.getByRole('button', { name: 'Previous month' }).click();
  await expect(page.getByRole('button', { name: 'October 2026' })).toBeVisible();
  await expect(page.getByText('€1,000.00')).toBeVisible();
});
