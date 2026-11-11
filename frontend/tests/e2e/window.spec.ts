import { expect, test } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

test('the proof window renders, greets, ticks and keeps a clean CSP', async ({ page }) => {
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
    page.evaluate(
      () => (window as unknown as { __cspViolations: string[] }).__cspViolations,
    );

  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'Finglass' })).toBeVisible();
  await expect(page.getByText('0.4.0-dev')).toBeVisible();
  await expect(page.getByText('go1.27.4')).toBeVisible();
  await expect(page.getByTestId('tick-count')).toBeVisible();

  await page.getByLabel('Name').fill('Ada');
  await page.getByRole('button', { name: 'Greet' }).click();
  await expect(page.getByTestId('greeting')).toContainText('Hello, Ada');

  await expect
    .poll(
      async () => Number(await page.getByTestId('tick-count').innerText()),
      { timeout: 6000 },
    )
    .toBeGreaterThan(0);

  await page.getByRole('button', { name: 'DE' }).click();
  await expect(page.getByRole('button', { name: 'Begrüßen' })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Sprache' })).toBeVisible();
  await page.getByRole('button', { name: 'FR' }).click();
  await expect(page.getByRole('button', { name: 'Saluer' })).toBeVisible();
  await page.getByRole('button', { name: 'EN' }).click();
  await expect(page.getByRole('button', { name: 'Greet' })).toBeVisible();

  // Check the application before taking screenshots. Playwright itself
  // injects a temporary <style> into the page while capturing, which a strict
  // CSP reports; that is test tooling, not the application, so the evidence is
  // gathered across the real interactions first.
  expect(await cspViolations()).toEqual([]);
  expect(problems).toEqual([]);

  // Reload to a clean window for the reference screenshots.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Finglass' })).toBeVisible();
  await expect
    .poll(
      async () => Number(await page.getByTestId('tick-count').innerText()),
      { timeout: 6000 },
    )
    .toBeGreaterThan(0);
  expect(problems).toEqual([]);
  expect(await cspViolations()).toEqual([]);

  await mkdir(screenshotDir, { recursive: true });

  await page.emulateMedia({ colorScheme: 'light' });
  await page.screenshot({
    path: path.join(screenshotDir, 'window-light.png'),
    caret: 'initial',
  });

  await page.emulateMedia({ colorScheme: 'dark' });
  await page.screenshot({
    path: path.join(screenshotDir, 'window-dark.png'),
    caret: 'initial',
  });

  await page.emulateMedia({ colorScheme: 'light' });
  await page.getByLabel('Name').fill('Ada');
  await page.getByRole('button', { name: 'Greet' }).click();
  await expect(page.getByTestId('greeting')).toContainText('Hello, Ada');
  await page.screenshot({
    path: path.join(screenshotDir, 'window-greeting.png'),
    caret: 'initial',
  });
});
