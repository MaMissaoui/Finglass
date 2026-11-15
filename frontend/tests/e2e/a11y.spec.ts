import { AxeBuilder } from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';
import type { Result } from 'axe-core';

const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

type Theme = 'light' | 'dark';

type Scenario = {
  name: string;
  scenario: string;
  language?: 'en' | 'de';
  size?: [number, number];
  after?: (page: Page) => Promise<void>;
};

async function goto(
  page: Page,
  scenario: string,
  width = 1440,
  height = 900,
  colorScheme: Theme = 'light',
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

function formatViolations(violations: Result[]): string {
  return violations
    .map((violation) => {
      const targets = violation.nodes.map((node) => node.target.join(' ')).join('\n    ');
      return `${violation.id} (${violation.impact ?? 'unknown'})\n    ${targets}`;
    })
    .join('\n');
}

async function expectAccessible(
  page: Page,
  label: string,
  theme: Theme,
  forceTheme = true,
): Promise<void> {
  // A scenario may pin its own theme in the DTO. Set the requested theme on the
  // root so both palettes are checked for every state. The System cases keep
  // the root free of `data-theme`, so `prefers-color-scheme` decides.
  if (forceTheme) {
    await page.evaluate((next: string) => {
      document.documentElement.dataset.theme = next;
    }, theme);
  } else {
    expect(
      await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
    ).toBeNull();
  }
  await page.waitForTimeout(50);

  const results = await new AxeBuilder({ page }).withTags(tags).analyze();
  const blocking = results.violations.filter(
    (violation) => violation.impact === 'serious' || violation.impact === 'critical',
  );
  expect(
    blocking,
    `${label} (${theme}) has serious or critical accessibility violations:\n${formatViolations(results.violations)}`,
  ).toEqual([]);
}

async function openBankMenu(page: Page, name: 'More' | 'Mehr'): Promise<void> {
  await page
    .getByTestId('bank-card-2')
    .locator('.bank-head')
    .getByRole('button', { name, exact: true })
    .click();
  await expect(page.getByTestId('bank-card-2').getByRole('menu')).toBeVisible();
}

async function openTestConnection(page: Page): Promise<void> {
  await page
    .getByTestId('bank-card-2')
    .locator('.bank-head')
    .getByRole('button', { name: 'More', exact: true })
    .click();
  await page
    .getByTestId('bank-card-2')
    .getByRole('menuitem', { name: 'Test connection', exact: true })
    .click();
  await expect(page.getByTestId('banking-prompt')).toBeVisible();
}

async function runConnectionTest(page: Page, go: boolean): Promise<void> {
  await openTestConnection(page);
  const prompt = page.getByTestId('banking-prompt');
  await prompt
    .getByRole('textbox', { name: 'Product id', exact: true })
    .fill('INVENTED-PRODUCT-ID');
  await prompt.getByRole('button', { name: 'OK', exact: true }).click();
  await prompt.getByRole('textbox', { name: 'PIN', exact: true }).fill('INVENTED-PIN-2468');
  await prompt.getByRole('button', { name: 'OK', exact: true }).click();
  const panel = page.getByTestId('bank-card-2').getByTestId('connection-test-2');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText(go ? 'Connection works.' : '9942');
}

async function openSettingsSection(page: Page, name: string): Promise<void> {
  const button = page.getByRole('button', { name, exact: true });
  await button.click();
  await expect(button).toHaveAttribute('aria-current', 'true');
  if (!['Security', 'Audit log'].includes(name)) {
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  }
}

const scenarios: Scenario[] = [
  {
    name: 'unlock create',
    scenario: '',
    size: [560, 760],
    after: async (page) => {
      await expect(
        page.getByRole('heading', { name: 'Protect your data with a passphrase' }),
      ).toBeVisible();
    },
  },
  {
    name: 'unlock',
    scenario: 'restored',
    size: [560, 760],
    after: async (page) => {
      await expect(page.getByRole('heading', { name: 'Enter your passphrase' })).toBeVisible();
    },
  },
  {
    name: 'unlock wrong passphrase',
    scenario: 'restored',
    size: [560, 760],
    after: async (page) => {
      await page.getByRole('textbox', { name: 'Passphrase' }).fill('not the passphrase');
      await page.getByRole('button', { name: 'Unlock' }).click();
      await expect(page.getByTestId('unlock-error')).toHaveText('Wrong passphrase.');
    },
  },
  {
    name: 'migration welcome',
    scenario: 'migration-welcome',
    size: [560, 800],
    after: async (page) => {
      await expect(page.getByRole('heading', { name: 'FinDesk found on this Mac' })).toBeVisible();
    },
  },
  {
    name: 'dashboard',
    scenario: 'dashboard-en-light',
    after: async (page) => {
      await expect(page.getByTestId('dashboard-page')).toBeVisible();
    },
  },
  {
    name: 'accounts with bank menu',
    scenario: 'accounts-bank-menu-en-light',
    after: async (page) => {
      await expect(page.getByTestId('accounts-page')).toBeVisible();
      await openBankMenu(page, 'More');
    },
  },
  {
    name: 'test connection GO',
    scenario: 'accounts',
    after: async (page) => {
      await runConnectionTest(page, true);
    },
  },
  {
    name: 'test connection NO-GO',
    scenario: 'accounts-test-connection-nogo-en-light',
    after: async (page) => {
      await runConnectionTest(page, false);
    },
  },
  {
    name: 'transactions list',
    scenario: 'transactions-en-light',
    after: async (page) => {
      await expect(page.getByTestId('transactions-page')).toBeVisible();
    },
  },
  {
    name: 'transaction detail',
    scenario: 'transactions-detail-en',
    after: async (page) => {
      await expect(page.getByTestId('transaction-detail')).toBeVisible();
    },
  },
  {
    name: 'add transaction dialog',
    scenario: 'transactions-add-en',
    after: async (page) => {
      await expect(page.getByTestId('add-transaction-dialog')).toBeVisible();
    },
  },
  {
    name: 'documents list with detail',
    scenario: 'documents-en-light',
    after: async (page) => {
      await expect(page.getByTestId('documents-page')).toBeVisible();
      await expect(page.getByTestId('documents-detail')).toBeVisible();
    },
  },
  {
    name: 'reports summary',
    scenario: 'reports-summary-en-light',
    after: async (page) => {
      await expect(page.getByTestId('reports-page')).toBeVisible();
    },
  },
  {
    name: 'reports tax',
    scenario: 'reports-tax-en-light',
    after: async (page) => {
      await expect(page.getByTestId('reports-page')).toBeVisible();
    },
  },
  {
    name: 'reports category',
    scenario: 'reports-category-en-light',
    after: async (page) => {
      await expect(page.getByTestId('reports-page')).toBeVisible();
    },
  },
];

for (const section of [
  'Appearance',
  'Profiles',
  'Categories',
  'Auto-categorization rules',
  'Security',
  'Backup & restore',
  'Import from FinDesk',
  'Audit log',
  'About Finglass',
]) {
  scenarios.push({
    name: `settings ${section}`,
    scenario: 'settings',
    after: async (page) => {
      await expect(page.getByTestId('settings-page')).toBeVisible();
      await openSettingsSection(page, section);
    },
  });
}

for (const scenario of scenarios) {
  for (const theme of ['light', 'dark'] as const) {
    test(`${scenario.name} has no serious accessibility violations (${theme})`, async ({
      page,
    }) => {
      test.setTimeout(60_000);
      const [width, height] = scenario.size ?? [1440, 900];
      await goto(page, scenario.scenario, width, height, theme);
      await scenario.after?.(page);
      await expectAccessible(page, scenario.name, theme);
    });
  }
}

const germanScenarios: Scenario[] = [
  {
    name: 'dashboard (German)',
    scenario: 'dashboard-de-light',
    language: 'de',
    after: async (page) => {
      await expect(page.getByTestId('dashboard-page')).toBeVisible();
    },
  },
  {
    name: 'accounts with bank menu (German)',
    scenario: 'accounts-de-light',
    language: 'de',
    after: async (page) => {
      await expect(page.getByTestId('accounts-page')).toBeVisible();
      await openBankMenu(page, 'Mehr');
    },
  },
];

for (const scenario of germanScenarios) {
  test(`${scenario.name} has no serious accessibility violations (light)`, async ({ page }) => {
    test.setTimeout(60_000);
    const [width, height] = scenario.size ?? [1440, 900];
    await goto(page, scenario.scenario, width, height, 'light');
    await scenario.after?.(page);
    await expectAccessible(page, scenario.name, 'light');
  });
}

// The System theme follows the OS. These cases run with a dark OS colour
// scheme and never write `data-theme`, so the dark tokens are the ones the app
// really shows with "System".
const systemScenarios: Scenario[] = [
  {
    name: 'accounts status lines (System)',
    scenario: 'accounts-status-no-server-system-en',
    after: async (page) => {
      await expect(page.getByTestId('accounts-page')).toBeVisible();
      await expect(page.getByTestId('bank-status-2')).toBeVisible();
    },
  },
  {
    name: 'test connection result (System)',
    scenario: 'accounts-test-connection-nogo-system-en',
    after: async (page) => {
      await runConnectionTest(page, false);
    },
  },
  {
    name: 'dashboard (System)',
    scenario: 'dashboard-system-en',
    after: async (page) => {
      await expect(page.getByTestId('dashboard-page')).toBeVisible();
    },
  },
];

for (const scenario of systemScenarios) {
  test(`${scenario.name} has no serious accessibility violations (dark OS)`, async ({ page }) => {
    test.setTimeout(60_000);
    const [width, height] = scenario.size ?? [1440, 900];
    await goto(page, scenario.scenario, width, height, 'dark');
    await scenario.after?.(page);
    await expectAccessible(page, scenario.name, 'dark', false);
  });
}
