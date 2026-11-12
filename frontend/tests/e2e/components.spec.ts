import { expect, test } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

const galleryMarker = 'FINGLASS_GALLERY_ONLY_7A';

test('the component gallery renders and behaves with a clean CSP', async ({ page }) => {
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

  const cspViolations = () =>
    page.evaluate(() => (window as unknown as { __cspViolations: string[] }).__cspViolations);

  await page.goto('/#gallery');

  await expect(page.locator(`[data-gallery="${galleryMarker}"]`)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Appearance' })).toBeVisible();

  // The gallery opens with its dialog in the Dialog section; Escape closes it.
  const dialogSection = page.locator('section', {
    has: page.getByRole('heading', { name: 'Dialog' }),
  });
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(await page.evaluate(() => Boolean(document.activeElement?.closest('dialog')))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).not.toBeVisible();

  // The close button also calls onclose and focus returns to the opener.
  const openButton = dialogSection.getByRole('button', { name: 'Open' });
  await openButton.click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click();
  await expect(page.getByRole('dialog')).not.toBeVisible();
  await expect(openButton).toBeFocused();

  // Escape closes the dropdown.
  const select = page.locator('[aria-haspopup="listbox"]').first();
  await select.click();
  await expect(page.getByRole('listbox')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('listbox')).not.toBeVisible();

  // A disabled button does not fire.
  const disabled = page.getByRole('button', { name: 'Export PDF', exact: true });
  await expect(disabled).toBeDisabled();
  await disabled.evaluate((element) => (element as HTMLButtonElement).click());
  await expect(page.getByTestId('disabled-count')).toHaveText('0');

  // The period arrows fire their callbacks.
  await page.getByRole('button', { name: 'Previous month' }).first().click();
  await page.getByRole('button', { name: 'Next month' }).first().click();
  await expect(page.getByTestId('period-count')).toHaveText('1 / 1');

  // The secret field never copies its value into storage or the URL.
  const secret = page.getByLabel('Passphrase');
  await expect(secret).toHaveAttribute('type', 'password');
  await expect(secret).toHaveAttribute('autocomplete', 'off');
  await expect(secret).toHaveAttribute('spellcheck', 'false');
  await secret.fill('gallery-secret-7A');
  const leaked = await page.evaluate(() => {
    const value = 'gallery-secret-7A';
    const storage = [...Object.entries(localStorage), ...Object.entries(sessionStorage)].flat();
    return {
      storage: storage.some((entry) => entry.includes(value)),
      url: `${location.href} ${location.hash} ${location.search}`.includes(value),
    };
  });
  expect(leaked).toEqual({ storage: false, url: false });

  // Tab reaches every enabled button on the page.
  const buttonCount = await page.evaluate(() => {
    const visible = (element: Element) => element.getClientRects().length > 0;
    const buttons = Array.from(document.querySelectorAll('button:not([disabled])')).filter(visible);
    buttons.forEach((button, index) => button.setAttribute('data-tab-probe', String(index)));
    return buttons.length;
  });
  expect(buttonCount).toBeGreaterThan(0);

  const seen = new Set<string>();
  for (let index = 0; index < buttonCount + 40; index += 1) {
    await page.keyboard.press('Tab');
    const probe = await page.evaluate(() => document.activeElement?.getAttribute('data-tab-probe'));
    if (probe !== null && probe !== undefined) {
      seen.add(probe);
    }
  }
  expect(seen.size).toBe(buttonCount);

  // Check the application before taking screenshots. Playwright itself
  // injects a temporary <style> into the page while capturing, which a strict
  // CSP reports; that is test tooling, not the application.
  expect(await cspViolations()).toEqual([]);
  expect(problems).toEqual([]);

  await mkdir(screenshotDir, { recursive: true });

  await page.emulateMedia({ colorScheme: 'light' });
  await page.evaluate(() => document.documentElement.removeAttribute('data-theme'));
  await page.screenshot({
    path: path.join(screenshotDir, 'gallery-light.png'),
    fullPage: true,
    caret: 'initial',
  });

  await page.emulateMedia({ colorScheme: 'dark' });
  await page.screenshot({
    path: path.join(screenshotDir, 'gallery-dark.png'),
    fullPage: true,
    caret: 'initial',
  });

  await page.emulateMedia({ colorScheme: 'light' });
  await page.evaluate(() => {
    document.documentElement.dataset.theme = 'dark';
  });
  await page.screenshot({
    path: path.join(screenshotDir, 'gallery-forced-dark.png'),
    fullPage: true,
    caret: 'initial',
  });
});
