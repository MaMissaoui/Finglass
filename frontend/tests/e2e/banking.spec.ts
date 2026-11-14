import { expect, test, type Page } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';

const screenshotDir = process.env.FINGLASS_SCREENSHOTS
  ? path.resolve(process.env.FINGLASS_SCREENSHOTS)
  : path.resolve('test-results/screenshots');

type MockDebug = {
  accountsStateCalls: number;
  bankingStateCalls: number;
  bankingPromptCalls: number;
  bankingPromptId: () => number;
  bankingAnswerPin: (id: number, pin: string) => void;
  bankingAnswerTan: (id: number, tan: string) => void;
  bankingAnswerChoice: (id: number, choice: string) => void;
  bankingAnswerDecoupled: (id: number, confirmed: boolean) => void;
};

type Watcher = {
  problems: string[];
  consoleText: string[];
  csp: () => Promise<string[]>;
};

async function watch(page: Page): Promise<Watcher> {
  const problems: string[] = [];
  const consoleText: string[] = [];

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
    consoleText.push(message.text());
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
    consoleText,
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
  await page.evaluate(() => {
    const active = document.activeElement;
    if (active instanceof HTMLElement) {
      active.blur();
    }
  });
  await page.screenshot({
    path: path.join(screenshotDir, `${name}.png`),
    caret: 'initial',
    animations: 'disabled',
  });
}

async function expectClean(watcher: Watcher): Promise<void> {
  expect(await watcher.csp()).toEqual([]);
  expect(watcher.problems).toEqual([]);
}

function prompt(page: Page) {
  return page.getByTestId('banking-prompt');
}

function sparkasse(page: Page) {
  return page.getByTestId('bank-card-2');
}

async function mockDebug(page: Page): Promise<MockDebug> {
  const debug = await page.evaluate(
    () => (window as unknown as { __finglassMock?: unknown }).__finglassMock,
  );
  return debug as MockDebug;
}

async function currentPromptId(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      (window as unknown as { __finglassMock?: MockDebug }).__finglassMock?.bankingPromptId() ?? 0,
  );
}

async function assertSecretGone(page: Page, secret: string): Promise<void> {
  const seen = await page.evaluate(() => {
    const values = Array.from(document.querySelectorAll('input, textarea')).map(
      (field) => (field as HTMLInputElement | HTMLTextAreaElement).value,
    );
    const storage: Record<string, string> = {};
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index) ?? '';
      storage[`local:${key}`] = localStorage.getItem(key) ?? '';
    }
    for (let index = 0; index < sessionStorage.length; index += 1) {
      const key = sessionStorage.key(index) ?? '';
      storage[`session:${key}`] = sessionStorage.getItem(key) ?? '';
    }
    return {
      values,
      storage: JSON.stringify(storage),
      url: location.href,
      dom: document.documentElement.outerHTML,
    };
  });
  expect(seen.values.join('\u0000')).not.toContain(secret);
  expect(seen.storage).not.toContain(secret);
  expect(seen.url).not.toContain(secret);
  expect(seen.dom).not.toContain(secret);
}

async function startSync(page: Page): Promise<void> {
  await sparkasse(page).getByRole('button', { name: 'Sync', exact: true }).click();
  await expect(prompt(page)).toBeVisible();
}

async function answerPin(page: Page, pin: string): Promise<void> {
  const dialog = prompt(page);
  await dialog.getByRole('textbox', { name: 'PIN', exact: true }).fill(pin);
  await dialog.getByRole('button', { name: 'OK', exact: true }).click();
}

async function answerProductID(page: Page, productID: string): Promise<void> {
  const dialog = prompt(page);
  await dialog.getByRole('textbox', { name: 'Product id', exact: true }).fill(productID);
  await dialog.getByRole('button', { name: 'OK', exact: true }).click();
}

async function openTestConnection(page: Page): Promise<void> {
  await sparkasse(page)
    .locator('.bank-head')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await sparkasse(page).getByRole('menuitem', { name: 'Test connection', exact: true }).click();
  await expect(prompt(page)).toBeVisible();
}

function connectionPanel(page: Page) {
  return sparkasse(page).getByTestId('connection-test-2');
}

async function runConnectionTest(
  page: Page,
  productID = 'INVENTED-PRODUCT-ID',
  pin = 'INVENTED-PIN-2468',
): Promise<void> {
  await openTestConnection(page);
  await expect(
    prompt(page).getByRole('heading', { name: 'FinTS product id', exact: true }),
  ).toBeVisible();
  await answerProductID(page, productID);
  await expect(
    prompt(page).getByRole('heading', { name: 'PIN for Sparkasse', exact: true }),
  ).toBeVisible();
  await expect(prompt(page).getByText('Remember PIN for syncs')).toHaveCount(0);
  await answerPin(page, pin);
  await expect(connectionPanel(page)).toBeVisible();
}

async function answerChoice(page: Page, label: string): Promise<void> {
  const dialog = prompt(page);
  await dialog.getByRole('option', { name: label, exact: true }).click();
  await dialog.getByRole('button', { name: 'OK', exact: true }).click();
}

async function answerTan(page: Page, tan: string): Promise<void> {
  const dialog = prompt(page);
  await dialog.getByRole('textbox', { name: 'TAN', exact: true }).fill(tan);
  await dialog.getByRole('button', { name: 'OK', exact: true }).click();
}

async function answerDecoupled(page: Page): Promise<void> {
  await prompt(page).getByRole('button', { name: 'I confirmed it' }).click();
}

test('the banking reference screenshots', async ({ page }) => {
  test.setTimeout(120_000);
  await goto(page, 'accounts-add-online-bank');
  await expect(
    page.getByTestId('accounts-dialog').getByRole('heading', { name: 'Add bank' }),
  ).toBeVisible();
  await page.getByRole('radio', { name: 'Online banking (FinTS)' }).click();
  await shot(page, 'accounts-add-online-bank-en');

  await goto(page, 'accounts-add-online-bank-search');
  await page.getByRole('radio', { name: 'Online banking (FinTS)' }).click();
  const dialog = page.getByTestId('accounts-dialog');
  await dialog.getByLabel('Search bank: name, BLZ, BIC or IBAN').fill('hypo');
  await expect(dialog.getByText('Rheintal Hypobank')).toBeVisible();
  await shot(page, 'accounts-add-online-bank-search-en');

  await goto(page, 'accounts-add-online-bank-central-note');
  await page.getByRole('radio', { name: 'Online banking (FinTS)' }).click();
  const central = page.getByTestId('accounts-dialog');
  await central.getByLabel('Search bank: name, BLZ, BIC or IBAN').fill('hypo');
  await central.getByRole('button', { name: /Rheintal Hypobank/u }).click();
  await central.getByLabel('Search bank: name, BLZ, BIC or IBAN').fill('');
  await expect(central.getByLabel('BLZ (8 digits)')).toHaveValue('70020270');
  await expect(
    central.getByText("This bank's online banking uses bank code 70020270"),
  ).toBeVisible();
  await central.evaluate((dialog) => {
    dialog.scrollTop = 0;
  });
  await shot(page, 'accounts-add-online-bank-central-note-en');

  await goto(page, 'accounts-add-online-bank-no-match-de-dark', 1100, 720, 'dark');
  await page.getByRole('radio', { name: 'Online-Banking (FinTS)' }).click();
  const german = page.getByTestId('accounts-dialog');
  await german.getByLabel('Bank suchen: Name, BLZ, BIC oder IBAN').fill('zzzzzz');
  await expect(german.getByText('Keine Bank gefunden – Daten unten eingeben')).toBeVisible();
  await shot(page, 'accounts-add-online-bank-no-match-de-dark');

  await goto(page, 'accounts-bank-menu-en-light', 1100, 900);
  await sparkasse(page).locator('.bank-head').getByRole('button', { name: 'More' }).click();
  await expect(sparkasse(page).getByRole('menu')).toBeVisible();
  await shot(page, 'accounts-bank-menu-en-light');

  await goto(page, 'accounts-bank-menu-de-dark', 1100, 900, 'dark');
  await sparkasse(page).locator('.bank-head').getByRole('button', { name: 'Mehr' }).click();
  await expect(sparkasse(page).getByRole('menu')).toBeVisible();
  await shot(page, 'accounts-bank-menu-de-dark');

  await goto(page, 'accounts', 1100, 900);
  await openTestConnection(page);
  await expect(
    prompt(page).getByRole('heading', { name: 'FinTS product id', exact: true }),
  ).toBeVisible();
  await shot(page, 'accounts-test-connection-product-id-en');

  await answerProductID(page, 'INVENTED-PRODUCT-ID');
  await answerPin(page, 'INVENTED-PIN-2468');
  await expect(connectionPanel(page)).toContainText('Connection works.');
  await shot(page, 'accounts-test-connection-go-en-light');

  await goto(page, 'accounts-test-connection-en-dark', 1100, 900, 'dark');
  await runConnectionTest(page);
  await expect(connectionPanel(page)).toContainText('Connection works.');
  await shot(page, 'accounts-test-connection-go-en-dark');

  await goto(page, 'accounts-test-connection-nogo-en-light', 1100, 900);
  await runConnectionTest(page);
  await expect(connectionPanel(page)).toContainText('9942');
  await shot(page, 'accounts-test-connection-nogo-en-light');

  await goto(page, 'accounts-test-connection-nogo-en-dark', 1100, 900, 'dark');
  await runConnectionTest(page);
  await expect(connectionPanel(page)).toContainText('9942');
  await shot(page, 'accounts-test-connection-nogo-en-dark');

  await goto(page, 'accounts-connection');
  await expect(
    page.getByTestId('accounts-dialog').getByRole('heading', {
      name: 'Online banking for Sparkasse',
    }),
  ).toBeVisible();
  await shot(page, 'accounts-connection-en');

  await goto(page, 'accounts-history');
  await expect(
    page.getByTestId('accounts-dialog').getByRole('heading', {
      name: 'Fetch history for Sparkasse',
    }),
  ).toBeVisible();
  await shot(page, 'accounts-history-en');

  await goto(page, 'accounts-sync-all');
  await expect(page.getByRole('button', { name: 'Add bank', exact: true })).toBeDisabled();
  await shot(page, 'accounts-sync-all-en');

  await goto(page, 'accounts-syncing');
  await expect(sparkasse(page).getByText('Connecting…')).toBeVisible();
  await shot(page, 'accounts-syncing-en');

  await goto(page, 'accounts-pin');
  await expect(prompt(page).getByRole('heading', { name: 'PIN for Sparkasse' })).toBeVisible();
  await shot(page, 'accounts-pin-en');

  await goto(page, 'accounts-pin-remember');
  await expect(prompt(page).getByText('Remember PIN for syncs')).toBeVisible();
  await shot(page, 'accounts-pin-remember-en');

  await goto(page, 'accounts-tan-method');
  await expect(prompt(page).getByRole('heading', { name: 'Choose TAN procedure' })).toBeVisible();
  await shot(page, 'accounts-tan-method-en');

  await goto(page, 'accounts-tan-photo');
  await expect(prompt(page).getByRole('heading', { name: 'TAN required' })).toBeVisible();
  await shot(page, 'accounts-tan-photo-en');

  await goto(page, 'accounts-decoupled');
  await expect(
    prompt(page).getByRole('heading', { name: 'Confirm in your banking app' }),
  ).toBeVisible();
  await shot(page, 'accounts-decoupled-en');
});

test('the page pulls the banking state and prompt on mount', async ({ page }) => {
  await goto(page, 'accounts');
  await expect(page.getByText("Online sync isn't available in Finglass yet.")).toBeVisible();
  const debug = await mockDebug(page);
  expect(debug.bankingStateCalls).toBeGreaterThanOrEqual(1);
  expect(debug.bankingPromptCalls).toBeGreaterThanOrEqual(1);
});

test('the scripted sync walks through every prompt', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await startSync(page);
  const pinID = await currentPromptId(page);
  expect(pinID).toBeGreaterThan(0);
  await expect(prompt(page).getByRole('heading', { name: 'PIN for Sparkasse' })).toBeVisible();
  await answerPin(page, 'S3cret-PIN-9931');
  await assertSecretGone(page, 'S3cret-PIN-9931');

  await expect(prompt(page).getByRole('heading', { name: 'Choose TAN procedure' })).toBeVisible();
  const methodID = await currentPromptId(page);
  expect(methodID).toBeGreaterThan(pinID);

  // A stale answer from the replaced PIN prompt changes nothing.
  await page.evaluate(
    (id) =>
      (window as unknown as { __finglassMock: MockDebug }).__finglassMock.bankingAnswerPin(
        id,
        'stale-pin',
      ),
    pinID,
  );
  await expect(prompt(page).getByRole('heading', { name: 'Choose TAN procedure' })).toBeVisible();
  expect(await currentPromptId(page)).toBe(methodID);

  await answerChoice(page, 'chipTAN optisch');
  await expect(prompt(page).getByRole('heading', { name: 'Choose TAN medium' })).toBeVisible();
  await answerChoice(page, 'Mein Smartphone');

  const tan = prompt(page).getByRole('textbox', { name: 'TAN', exact: true });
  await expect(tan).toBeVisible();
  const image = prompt(page).locator('img.challenge');
  await expect(image).toHaveAttribute('src', /^data:image\/png;base64,/u);
  await answerTan(page, '7F3K-TAN-8812');
  await assertSecretGone(page, '7F3K-TAN-8812');

  await expect(
    prompt(page).getByRole('heading', { name: 'Confirm in your banking app' }),
  ).toBeVisible();
  await answerDecoupled(page);

  await expect(prompt(page)).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('Synced 2 accounts');
  await expect(sparkasse(page).getByText('Syncing…')).toHaveCount(0);

  const consoleText = watcher.consoleText.join('\n');
  expect(consoleText).not.toContain('S3cret-PIN-9931');
  expect(consoleText).not.toContain('7F3K-TAN-8812');
  await assertSecretGone(page, 'S3cret-PIN-9931');
  await assertSecretGone(page, '7F3K-TAN-8812');
  await expectClean(watcher);
});

test('the connection test walks product id and PIN then saves the trace', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts');

  await openTestConnection(page);
  const productIDField = prompt(page).getByRole('textbox', { name: 'Product id', exact: true });
  await expect(productIDField).toHaveAttribute('type', 'password');
  await expect(productIDField).toHaveAttribute('autocomplete', 'off');
  await productIDField.fill('INVENTED-PRODUCT-ID');
  await prompt(page).getByRole('button', { name: 'OK', exact: true }).click();

  await expect(
    prompt(page).getByRole('heading', { name: 'PIN for Sparkasse', exact: true }),
  ).toBeVisible();
  await assertSecretGone(page, 'INVENTED-PRODUCT-ID');
  await expect(prompt(page).getByText('Remember PIN for syncs')).toHaveCount(0);
  const pinField = prompt(page).getByRole('textbox', { name: 'PIN', exact: true });
  await expect(pinField).toHaveAttribute('type', 'password');
  await pinField.fill('INVENTED-PIN-2468');
  await prompt(page).getByRole('button', { name: 'OK', exact: true }).click();

  await expect(prompt(page)).toHaveCount(0);
  const panel = connectionPanel(page);
  await expect(panel).toContainText('Connection works.');
  await expect(panel).toContainText('1 account checked');
  await expect(panel).toContainText('1 account skipped');
  await expect(panel).toContainText('Runs a read-only check against your bank.');
  await assertSecretGone(page, 'INVENTED-PIN-2468');

  await panel.getByRole('button', { name: 'Save redacted trace…', exact: true }).click();
  await expect(page.getByText('The redacted trace was saved.')).toBeVisible();
  await expectClean(watcher);
});

test('a refused login shows the NO-GO panel with the bank return code', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts-test-connection-nogo-en-light');

  await runConnectionTest(page);
  const panel = connectionPanel(page);
  await expect(panel).toContainText('The login data is invalid.');
  await expect(panel.locator('.code-chip')).toHaveText(['9942']);
  await expect(panel).not.toContainText('Connection works.');
  await expect(panel.getByRole('button', { name: 'Save redacted trace…' })).toBeVisible();
  await expectClean(watcher);
});

test('cancel at the product id prompt shows the cancelled panel', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'accounts');

  await openTestConnection(page);
  await expect(
    prompt(page).getByRole('heading', { name: 'FinTS product id', exact: true }),
  ).toBeVisible();
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);

  const panel = connectionPanel(page);
  await expect(panel).toContainText('Connection test cancelled.');
  await expect(panel.getByRole('button', { name: 'Save redacted trace…' })).toHaveCount(0);
  await expectClean(watcher);
});

test('cancel closes the prompt and ends the sync', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await startSync(page);
  await prompt(page).getByRole('textbox', { name: 'PIN', exact: true }).fill('cancel-pin-4711');
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);
  await expect(sparkasse(page).getByText('Connecting…')).toHaveCount(0);
  await expect(sparkasse(page).getByRole('button', { name: 'Sync', exact: true })).toBeVisible();
  await assertSecretGone(page, 'cancel-pin-4711');

  // A fresh sync starts at the PIN prompt again, with an empty field.
  await startSync(page);
  await expect(prompt(page).getByRole('textbox', { name: 'PIN', exact: true })).toHaveValue('');
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);
  await expectClean(watcher);
});

test('a TAN cancel clears the field and ends the sync', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await startSync(page);
  await answerPin(page, 'pin-2468');
  await answerChoice(page, 'chipTAN optisch');
  await answerChoice(page, 'Mein Smartphone');

  const tan = prompt(page).getByRole('textbox', { name: 'TAN', exact: true });
  await expect(tan).toHaveAttribute('type', 'password');
  await tan.fill('cancel-tan-1357');
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);
  await expect(sparkasse(page).getByText('Connecting…')).toHaveCount(0);
  await assertSecretGone(page, 'cancel-tan-1357');
  await expectClean(watcher);
});

test('Sync all shows its progress and can be cancelled', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await page.getByRole('button', { name: 'Sync all', exact: true }).click();
  await expect(page.getByText('1/1 · Sparkasse')).toBeVisible();
  await expect(prompt(page).getByRole('heading', { name: 'PIN for Sparkasse' })).toBeVisible();
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);
  await expect(page.getByText('1/1 · Sparkasse')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Sync all', exact: true })).toBeVisible();
  await expectClean(watcher);
});

test('Remember PIN is only offered when the prompt allows it', async ({ page }) => {
  await goto(page, 'accounts-pin');
  await expect(prompt(page).getByText('Remember PIN for syncs')).toHaveCount(0);
  await expect(prompt(page).getByRole('textbox', { name: 'PIN', exact: true })).toHaveAttribute(
    'type',
    'password',
  );

  await goto(page, 'accounts-pin-remember');
  await expect(prompt(page).getByText('Remember PIN for syncs')).toBeVisible();
  await expect(
    prompt(page).getByText("Kept in your system's secure credential store"),
  ).toBeVisible();

  // The box is only one with the prompt, and cancelling clears it for the next.
  await prompt(page).getByRole('checkbox', { name: 'Remember PIN for syncs' }).check();
  await prompt(page).getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(prompt(page)).toHaveCount(0);
});

test('the add-online-bank dialog searches and stores a login', async ({ page }) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await page.getByRole('button', { name: 'Add bank', exact: true }).click();
  const dialog = page.getByTestId('accounts-dialog');
  await expect(dialog.getByRole('heading', { name: 'Add bank' })).toBeVisible();
  await page.getByRole('radio', { name: 'Online banking (FinTS)' }).click();

  await dialog.getByLabel('Search bank: name, BLZ, BIC or IBAN').fill('hypo');
  await dialog.getByRole('button', { name: /Rheintal Hypobank/u }).click();
  await expect(dialog.getByLabel('Bank name')).toHaveValue('Rheintal Hypobank');
  await expect(dialog.getByLabel('BLZ (8 digits)')).toHaveValue('70020270');
  await expect(dialog.getByLabel('BIC (optional)')).toHaveValue('HYVEDEMM488');

  await dialog.getByLabel('User ID (Anmeldename / Legitimations-ID)').fill('invented-user');
  await dialog.getByLabel('FinTS server URL').fill('https://fints.example/pintan');
  await dialog.getByRole('button', { name: 'Add', exact: true }).click();

  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status')).toContainText('Added Rheintal Hypobank');
  const card = page.getByTestId('bank-card-3');
  await expect(card).toContainText('Rheintal Hypobank');
  await expect(card.getByText('BLZ 70020270 · BIC HYVEDEMM488')).toBeVisible();

  await card.locator('.bank-head').getByRole('button', { name: 'More' }).click();
  await expect(card.getByRole('menu')).toBeVisible();
  await expect(card.getByRole('menuitemcheckbox', { name: 'Include in Sync all' })).toBeVisible();
  await expectClean(watcher);
});

test('the add-online-bank dialog maps a typed branch BLZ and keeps filled fields', async ({
  page,
}) => {
  const watcher = await watch(page);
  await goto(page, 'banking-walkthrough');

  await page.getByRole('button', { name: 'Add bank', exact: true }).click();
  const dialog = page.getByTestId('accounts-dialog');
  await page.getByRole('radio', { name: 'Online banking (FinTS)' }).click();

  await dialog.getByLabel('BLZ (8 digits)').fill('10020890');
  await expect(dialog.getByLabel('Bank name')).toHaveValue('Rheintal Hypobank');
  await expect(dialog.getByLabel('BLZ (8 digits)')).toHaveValue('70020270');
  await expect(dialog.getByLabel('BIC (optional)')).toHaveValue('HYVEDEMM488');
  await expect(
    dialog.getByText("This bank's online banking uses bank code 70020270"),
  ).toBeVisible();

  await dialog.getByLabel('Bank name').fill('My bank');
  await dialog.getByLabel('BIC (optional)').fill('MYBIC');
  await dialog.getByLabel('BLZ (8 digits)').fill('10020890');
  await expect(dialog.getByLabel('Bank name')).toHaveValue('My bank');
  await expect(dialog.getByLabel('BIC (optional)')).toHaveValue('MYBIC');

  await dialog.getByLabel('BLZ (8 digits)').fill('1002');
  await expect(dialog.getByLabel('BLZ (8 digits)')).toHaveValue('1002');
  await expect(dialog.getByLabel('Bank name')).toHaveValue('My bank');
  await expect(dialog.getByLabel('BIC (optional)')).toHaveValue('MYBIC');
  await expect(dialog.getByText("This bank's online banking uses bank code")).toHaveCount(0);

  await expectClean(watcher);
});

test('every banking scenario renders without a CSP or console error', async ({ page }) => {
  const watcher = await watch(page);

  const scenarios = [
    'accounts-add-online-bank',
    'accounts-add-online-bank-search',
    'accounts-add-online-bank-central-note',
    'accounts-add-online-bank-no-match-de-dark',
    'accounts-bank-menu-en-light',
    'accounts-bank-menu-de-dark',
    'accounts-connection',
    'accounts-history',
    'accounts-sync-all',
    'accounts-syncing',
    'accounts-pin',
    'accounts-pin-remember',
    'accounts-tan-method',
    'accounts-tan-medium',
    'accounts-tan-photo',
    'accounts-tan-text',
    'accounts-decoupled',
    'accounts-test-connection-en-dark',
    'accounts-test-connection-nogo-en-light',
    'accounts-test-connection-nogo-en-dark',
    'banking-walkthrough',
  ];
  for (const scenario of scenarios) {
    await goto(page, scenario);
    await expect(page.locator('#app > *').first()).toBeVisible();
  }

  // Every dialog closes with Escape again.
  await goto(page, 'accounts-connection');
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('accounts-dialog')).toHaveCount(0);

  await goto(page, 'accounts-history');
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('accounts-dialog')).toHaveCount(0);

  await goto(page, 'accounts-pin');
  await page.keyboard.press('Escape');
  await expect(prompt(page)).toHaveCount(0);

  await expectClean(watcher);
});
