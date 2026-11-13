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

function pageRoot(page: Page) {
  return page.getByTestId('documents-page');
}

function detail(page: Page) {
  return page.getByTestId('documents-detail');
}

function row(page: Page, id: number) {
  return page.getByTestId(`document-row-${id}`);
}

type MockDebug = {
  documentsStateCalls: number;
  documentsSearchCalls: string[];
  documentsUploadCalls: number;
  documentsOpenCalls: number;
};

function mockDebug(page: Page): Promise<MockDebug> {
  return page.evaluate(() => (window as unknown as { __finglassMock: MockDebug }).__finglassMock);
}

test('the Documents reference screenshots', async ({ page }) => {
  await goto(page, 'documents-en-light', 1100, 720);
  await expect(pageRoot(page)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible();
  await expect(detail(page).getByRole('heading', { name: 'kassenbon.jpg' })).toBeVisible();
  await shot(page, 'documents-en-light');

  await goto(page, 'documents-de-dark', 1100, 720, 'dark');
  await expect(pageRoot(page)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Dokumente', exact: true })).toBeVisible();
  await expect(detail(page).getByRole('heading', { name: 'kassenbon.jpg' })).toBeVisible();
  await shot(page, 'documents-de-dark');

  await goto(page, 'documents-empty-en', 1100, 720);
  await expect(page.getByTestId('documents-empty')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'No documents' })).toBeVisible();
  await shot(page, 'documents-empty-en');
});

test('the page pulls its state on mount and debounces the search', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'documents-en-light');
  await expect(pageRoot(page)).toBeVisible();

  await expect.poll(async () => (await mockDebug(page)).documentsStateCalls).toBeGreaterThan(0);

  const search = page.getByLabel('Search documents by name or text content…');
  await search.fill('ba');
  await search.fill('bae');
  await search.fill('baeckerei');

  await page.waitForTimeout(100);
  expect((await mockDebug(page)).documentsSearchCalls).toEqual([]);

  await expect
    .poll(async () => (await mockDebug(page)).documentsSearchCalls)
    .toEqual(['baeckerei']);
  await expect(row(page, 1)).toBeVisible();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(1);

  await page.getByRole('button', { name: 'Clear search' }).click();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(3);

  await page.evaluate(() => {
    (document.activeElement as HTMLElement | null)?.blur();
    window.dispatchEvent(new CustomEvent('finglass:mock-menu-action', { detail: 'find' }));
  });
  await expect(search).toBeFocused();

  await expectClean(watcher);
});

test('the tabs filter the list and their labels carry the counts', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'documents-en-light');

  await expect(page.getByRole('radio', { name: 'All documents (3)' })).toBeVisible();
  await page.getByRole('radio', { name: 'Unlinked (2)' }).click();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(2);
  await expect(row(page, 2)).toHaveCount(0);

  await page.getByRole('radio', { name: 'Linked to transaction (1)' }).click();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(1);
  await expect(row(page, 2)).toBeVisible();

  await page.getByRole('radio', { name: 'All documents (3)' }).click();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(3);

  await expectClean(watcher);
});

test('a row opens the detail and Close hides it', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'documents-en-light');
  await expect(detail(page)).toBeVisible();

  await row(page, 1).click();
  await expect(detail(page).getByRole('heading', { name: 'baeckerei-beleg.png' })).toBeVisible();
  await detail(page).getByRole('button', { name: 'Close', exact: true }).click();
  await expect(detail(page)).toHaveCount(0);

  await row(page, 2).click();
  await expect(
    detail(page).getByRole('heading', { name: 'stadtwerke-rechnung.pdf' }),
  ).toBeVisible();
  await expect(detail(page).getByTestId('documents-pdf-preview')).toBeVisible();
  await expect(detail(page).locator('img')).toHaveCount(0);
  await expect(detail(page).getByRole('button', { name: 'Unlink', exact: true })).toBeVisible();

  await row(page, 3).click();
  await expect(detail(page).getByRole('heading', { name: 'kassenbon.jpg' })).toBeVisible();
  await expect(detail(page).locator('img')).toHaveCount(1);

  await expectClean(watcher);
});

test('link, unlink, reprocess and delete call their methods', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'documents-en-light');
  await expect(detail(page)).toBeVisible();

  await detail(page).getByRole('button', { name: 'Link', exact: true }).click();
  await expect(page.getByText('Document linked', { exact: true })).toBeVisible();
  await expect(detail(page).getByRole('button', { name: 'Unlink', exact: true })).toBeVisible();
  await expect(detail(page).getByText('REWE Markt GmbH (2026-03-13)')).toBeVisible();

  await page.getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(page.getByText('Document linked', { exact: true })).toHaveCount(0);

  await detail(page).getByRole('button', { name: 'Unlink', exact: true }).click();
  await expect(detail(page).getByRole('button', { name: 'Link', exact: true })).toBeVisible();

  await detail(page).getByRole('button', { name: 'Re-extract text', exact: true }).click();
  await expect(page.getByRole('status').getByText('Text extracted', { exact: true })).toBeVisible();

  await detail(page).getByRole('button', { name: 'Delete', exact: true }).click();
  const dialog = page.getByTestId('documents-delete-dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading', { name: 'Delete document?' })).toBeVisible();
  await expect(
    dialog.getByText('kassenbon.jpg and its stored copy will be deleted.'),
  ).toBeVisible();

  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(detail(page)).toBeVisible();

  await detail(page).getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click();

  await expect(page.getByText('Document deleted', { exact: true })).toBeVisible();
  await expect(detail(page)).toHaveCount(0);
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(2);
  await expect(row(page, 3)).toHaveCount(0);

  await expectClean(watcher);
});

test('upload and open call their argument-less methods and show the mock result', async ({
  page,
}) => {
  const watcher = await watch(page);
  await goto(page, 'documents-en-light');

  await page.getByRole('button', { name: 'Upload', exact: true }).click();
  await expect(
    page.getByText('Uploaded 1 file, text extracted from 1', { exact: true }),
  ).toBeVisible();
  await expect(page.locator('[data-testid^="document-row-"]')).toHaveCount(4);
  expect((await mockDebug(page)).documentsUploadCalls).toBe(1);

  await detail(page).getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.getByText('Open preview: kassenbon.jpg', { exact: true })).toBeVisible();
  expect((await mockDebug(page)).documentsOpenCalls).toBe(1);

  await expectClean(watcher);
});

test('the candidate search narrows the link picker', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'documents-candidates-en');
  await expect(detail(page)).toBeVisible();

  const search = detail(page).getByLabel('Find a transaction to link…');
  await expect(search).toHaveValue('rewe');
  const candidates = detail(page).getByTestId('documents-candidates');
  await expect(candidates.getByText('Beispiel Energie AG')).toHaveCount(0);
  await expect(candidates.getByText('REWE Markt GmbH')).toBeVisible();

  await search.fill('energie');
  await expect(candidates.getByText('Beispiel Energie AG')).toBeVisible();
  await expect(candidates.getByText('REWE Markt GmbH')).toHaveCount(0);

  await expectClean(watcher);
});

test('the Documents page renders every scenario without a CSP or console error', async ({
  page,
}) => {
  const watcher = await watch(page);

  const scenarios = [
    'documents',
    'documents-en-light',
    'documents-de-light',
    'documents-de-dark',
    'documents-empty-en',
    'documents-linked-en-light',
    'documents-candidates-en',
    'documents-uploading-en',
    'documents-message-en',
    'documents-error-en',
    'documents-ocr-unavailable-en',
    'documents-loading-en',
    'documents-no-match-en',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario, 1100, 720, scenario.endsWith('dark') ? 'dark' : 'light');
    await expect(pageRoot(page)).toBeVisible();
  }

  await goto(page, 'documents-uploading-en');
  await expect(page.getByTestId('documents-upload-progress')).toBeVisible();
  await expect(page.getByText('Uploading 1 of 2…', { exact: true })).toBeVisible();

  await goto(page, 'documents-ocr-unavailable-en');
  await expect(page.getByText(/Tesseract/)).toBeVisible();

  await goto(page, 'documents-linked-en-light');
  await expect(detail(page).getByRole('button', { name: 'Unlink', exact: true })).toBeVisible();

  await goto(page, 'documents-no-match-en');
  await expect(page.getByRole('heading', { name: 'No matches' })).toBeVisible();

  await expectClean(watcher);
});
