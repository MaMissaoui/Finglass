// Screenshots the exported real-data snapshot in every page, language, theme
// and width. Run by scripts/real-data-shots.sh; the data stays in the user's
// output folder.
/* global process, console, document, requestAnimationFrame */
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { webkit } from '@playwright/test';

const [baseURL, outDir] = process.argv.slice(2);
if (baseURL === undefined || outDir === undefined) {
  console.error('usage: real-data-shots.mjs <base-url> <out-dir>');
  process.exit(2);
}

const languages = [
  { code: 'en', label: 'English' },
  { code: 'de', label: 'Deutsch' },
];
const themes = ['light', 'dark'];
const widths = [1100, 900];
const pages = [
  { index: 0, name: 'dashboard', testId: 'dashboard-page' },
  { index: 1, name: 'accounts', testId: 'accounts-page' },
  { index: 2, name: 'transactions', testId: 'transactions-page' },
  { index: 3, name: 'documents', testId: 'documents-page' },
  { index: 4, name: 'reports', testId: 'reports-page' },
  { index: 5, name: 'settings', testId: 'settings-page' },
];

await mkdir(outDir, { recursive: true });
const browser = await webkit.launch();

try {
  for (const language of languages) {
    for (const theme of themes) {
      for (const width of widths) {
        const context = await browser.newContext({
          viewport: { width, height: 720 },
          colorScheme: theme,
        });
        const page = await context.newPage();

        await page.goto(`${baseURL}/?snapshot=snapshot.json`, { waitUntil: 'load' });
        await page.locator('#app > *').first().waitFor({ state: 'visible' });
        await page.evaluate(async () => {
          await document.fonts.ready;
        });

        await page.getByTestId('language-button').click();
        await page
          .getByTestId('language-menu')
          .getByRole('menuitemradio', { name: language.label })
          .click();

        if (theme === 'dark') {
          await page.locator('.topbar button').last().click();
          await page.waitForFunction(() => document.documentElement.dataset.theme === 'dark');
        }

        for (const entry of pages) {
          await page.locator('[data-testid="sidebar"] .sidebar-item').nth(entry.index).click();
          await page.locator(`[data-testid="${entry.testId}"]`).waitFor({ state: 'visible' });
          await page.evaluate(
            () =>
              new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
          );
          await page.screenshot({
            path: path.join(outDir, `${entry.name}-${language.code}-${theme}-${width}.png`),
            caret: 'initial',
          });
        }

        await context.close();
        console.log(`Captured ${language.code} ${theme} ${width}px`);
      }
    }
  }
} finally {
  await browser.close();
}
