import { describe, expect, it } from 'vitest';
import { translate } from '../../src/lib/i18n';
import { createMockSettings, mockPalette } from '../../src/lib/settings.mock';

function fake() {
  return createMockSettings();
}

describe('settings.mock', () => {
  it('builds the reference page', () => {
    const state = fake().state();
    expect(state.section).toBe(0);
    expect(state.categoriesEmpty).toBe(false);
    expect(state.hasDuplicateColours).toBe(false);
    expect(state.categoryRows.map((row) => row.name)).toEqual([
      'Donations',
      'Food',
      'Health',
      'Own transfers',
    ]);
    expect(state.categoryRows.map((row) => row.budgetText)).toEqual([
      '',
      'Budget €300.00',
      'Budget €200.00',
      '',
    ]);
    expect(state.categoryRows.map((row) => row.usageText)).toEqual([
      '2 bookings',
      '5 bookings',
      '0 bookings',
      '1 booking',
    ]);
    expect(state.categoryRows.map((row) => row.taxRelevant)).toEqual([true, false, true, false]);
    expect(state.categoryRows.map((row) => row.transfer)).toEqual([false, false, false, true]);
    expect(state.primaryCurrency).toBe('EUR');
    expect(state.primaryCurrencyIndex).toBe(0);
    expect(state.currencyOptions[0]).toBe('EUR');
    expect(state.taxKeywords).toBe('arzt\napotheke\nhandwerker');
    expect(state.taxKeywordsCount).toBe('3 keywords');
    expect(state.categoryDialogSwatches).toHaveLength(mockPalette.length);
    expect(state.categoryDialogSwatches[0]).toEqual({
      hex: '#E76530',
      colour: '#E76530',
    });
  });

  it('builds the reference rules', () => {
    const state = fake().state();
    expect(state.ruleRows.map((row) => row.pattern)).toEqual([
      'Example Market',
      'Example Aid',
      'Apotheke|Arzt',
    ]);
    expect(state.ruleRows.map((row) => row.categoryText)).toEqual([
      'Food',
      'Donations · Tax',
      'Health · No tax',
    ]);
    expect(state.ruleRows.map((row) => row.detail)).toEqual([
      'Partner name only · Expenses only',
      '',
      'Purpose only',
    ]);
    expect(state.ruleRows.map((row) => row.prioText)).toEqual([
      'prio 60',
      'prio 45',
      'prio 20 · manual',
    ]);
    expect(state.ruleDialogCategories).toEqual(['Donations', 'Food', 'Health', 'Own transfers']);
    expect(state.ruleDialogFieldOptions).toEqual([
      'Partner and purpose',
      'Partner name only',
      'Purpose only',
    ]);
    expect(state.ruleDialogDirectionOptions).toEqual([
      'Expenses and income',
      'Expenses only',
      'Income only',
    ]);
    expect(state.ruleDialogTaxOptions).toEqual([
      'As the category',
      'Tax relevant',
      'Not tax relevant',
    ]);
  });

  it('takes its section from the navigation and speaks the shell language', () => {
    const settings = createMockSettings({ section: 2, language: 'de' });
    expect(settings.state().section).toBe(2);
    expect(settings.state().categoryRows[1]?.budgetText).toBe('Budget 300,00\u00a0€');
    expect(settings.state().categoryRows[1]?.usageText).toBe('5 Buchungen');

    settings.setLanguage('fr');
    expect(settings.state().categoryRows[1]?.usageText).toBe('5 écritures');
  });

  it('validates the category dialog', () => {
    const settings = fake();
    settings.openAddCategory();
    expect(settings.state().categoryDialogOpen).toBe(true);
    expect(settings.state().categoryDialogId).toBe(-1);

    settings.categoryChanged('  ', '#E76530');
    expect(settings.state().categoryDialogError).toBe(translate('en', 'ErrorNameRequired'));

    settings.categoryChanged('Food', '#E76530');
    expect(settings.state().categoryDialogError).toBe(translate('en', 'ErrorCategoryExists'));

    settings.categoryChanged('Neu', 'nope');
    expect(settings.state().categoryDialogError).toBe(translate('en', 'ErrorColorFormat'));

    settings.categoryChanged('Neu', '#4693F3');
    expect(settings.state().categoryDialogError).toBe('');

    const saved = settings.saveCategory('Neu', '#4693F3', 'abc', false, false);
    expect(saved.categoryDialogError).toBe(translate('en', 'BudgetNotANumber'));
    expect(saved.categoryDialogOpen).toBe(true);

    const added = settings.saveCategory(' Neu ', '#4693F3', '12.50', true, true);
    expect(added.categoryDialogOpen).toBe(false);
    const row = added.categoryRows.find((entry) => entry.name === 'Neu');
    expect(row?.budgetText).toBe('Budget €12.50');
    expect(row?.taxRelevant).toBe(true);
    expect(row?.transfer).toBe(true);
    expect(row?.usageText).toBe('0 bookings');
  });

  it('edits and deletes a category with its rules', () => {
    const settings = fake();
    const food = settings.state().categoryRows.find((row) => row.name === 'Food');
    expect(food).toBeDefined();
    settings.openEditCategory(food?.id ?? 0);
    const dialog = settings.state();
    expect(dialog.categoryDialogName).toBe('Food');
    expect(dialog.categoryDialogBudget).toBe('300.00');
    expect(dialog.categoryDialogColour).toBe('#E76530');

    const edited = settings.saveCategory('Food', '#4693F3', '400', false, false);
    expect(edited.categoryRows.find((row) => row.name === 'Food')?.budgetText).toBe(
      'Budget €400.00',
    );

    settings.openDeleteCategory(food?.id ?? 0);
    const confirm = settings.state();
    expect(confirm.categoryDeleteOpen).toBe(true);
    expect(confirm.categoryDeleteTitle).toBe('Delete category Food?');
    expect(confirm.categoryDeleteText).toContain('Used by 5 bookings.');

    const deleted = settings.confirmDeleteCategory();
    expect(deleted.categoryRows.map((row) => row.name)).toEqual([
      'Donations',
      'Health',
      'Own transfers',
    ]);
    expect(deleted.ruleRows.map((row) => row.pattern)).toEqual(['Example Aid', 'Apotheke|Arzt']);
  });

  it('adds the default categories once', () => {
    const settings = createMockSettings({ emptyCategories: true });
    expect(settings.state().categoriesEmpty).toBe(true);
    expect(settings.state().ruleRows).toEqual([]);

    const added = settings.addDefaultCategories();
    expect(added.categoriesEmpty).toBe(false);
    expect(added.categoryRows).toHaveLength(24);
    expect(added.messageText).toBe(translate('en', 'MessageDefaultCategoriesAdded', 24));
    expect(added.categoryRows.find((row) => row.name === 'Own transfers')?.transfer).toBe(true);

    const again = settings.addDefaultCategories();
    expect(again.messageText).toBe(translate('en', 'MessageDefaultCategoriesNone'));
  });

  it('fixes duplicate colours', () => {
    const settings = fake();
    const food = settings.state().categoryRows.find((row) => row.name === 'Food');
    settings.openEditCategory(food?.id ?? 0);
    settings.saveCategory('Food', '#D56A93', '300.00', false, false);

    const duplicates = settings.state();
    expect(duplicates.hasDuplicateColours).toBe(true);

    const fixed = settings.fixDuplicateColours();
    expect(fixed.hasDuplicateColours).toBe(false);
    expect(fixed.messageText).toBe(translate('en', 'MessageColoursFixed', 1));

    const distinct = settings.fixDuplicateColours();
    expect(distinct.messageText).toBe(translate('en', 'MessageColoursAlreadyDistinct'));
  });

  it('validates, adds, edits and deletes a rule', () => {
    const settings = fake();
    settings.openAddRule();
    const opened = settings.state();
    expect(opened.ruleDialogOpen).toBe(true);
    expect(opened.ruleDialogPriority).toBe('0');
    expect(opened.ruleDialogAutoApply).toBe(true);

    settings.ruleChanged('  ', '0');
    expect(settings.state().ruleDialogError).toBe(translate('en', 'ErrorPatternRequired'));

    settings.ruleChanged('(', '0');
    expect(settings.state().ruleDialogError).toContain('Invalid regular expression');

    settings.ruleChanged('Test', 'abc');
    expect(settings.state().ruleDialogError).toBe(translate('en', 'PriorityMustBeNumber'));

    const saved = settings.saveRule('Test', 1, '7', true, 1, 1, 1);
    expect(saved.ruleDialogOpen).toBe(false);
    const row = saved.ruleRows.find((entry) => entry.pattern === 'Test');
    expect(row?.categoryText).toBe('Food · Tax');
    expect(row?.detail).toBe('Partner name only · Expenses only');
    expect(row?.prioText).toBe('prio 7');

    settings.openEditRule(row?.id ?? 0);
    const edit = settings.state();
    expect(edit.ruleDialogPattern).toBe('Test');
    expect(edit.ruleDialogCategoryIndex).toBe(1);
    expect(edit.ruleDialogFieldIndex).toBe(1);
    expect(edit.ruleDialogDirectionIndex).toBe(1);
    expect(edit.ruleDialogTaxIndex).toBe(1);

    const edited = settings.saveRule('Test', 1, '8', false, 0, 0, 0);
    expect(edited.ruleRows.find((entry) => entry.pattern === 'Test')?.prioText).toBe(
      'prio 8 · manual',
    );

    settings.openDeleteRule(row?.id ?? 0);
    expect(settings.state().ruleDeleteTitle).toBe('Delete rule Test?');
    const deleted = settings.confirmDeleteRule();
    expect(deleted.ruleRows.some((entry) => entry.pattern === 'Test')).toBe(false);
  });

  it('saves and resets the tax keywords', () => {
    const settings = fake();
    const saved = settings.saveTaxKeywords('eins\n\nZwei\nzwei\n drei ');
    expect(saved.taxKeywords).toBe('eins\nZwei\ndrei');
    expect(saved.taxKeywordsCount).toBe('3 keywords');

    const reset = settings.resetTaxKeywords();
    expect(reset.taxKeywords.split('\n')).toHaveLength(23);
    expect(reset.taxKeywords.split('\n')[0]).toBe('Spende');
    expect(reset.taxKeywordsCount).toBe('23 keywords');
  });

  it('exports rules and previews, cancels and confirms an import', () => {
    const settings = fake();
    expect(settings.exportRules().messageText).toBe('3 rules exported');

    const preview = settings.importRules();
    expect(preview.ruleImportOpen).toBe(true);
    expect(preview.ruleImportNewRulesText).toBe('3 new rules');
    expect(preview.ruleImportCategoriesText).toBe('2 categories to create');
    expect(preview.ruleImportCategoriesList).toBe('Household, Salary');
    expect(preview.ruleImportDuplicatesText).toBe('1 duplicate skipped');
    expect(preview.ruleImportConflictsText).toBe('1 conflict skipped');
    expect(preview.ruleImportConflictsList).toBe(
      'Line 7: Example Market → Household (existing: Food)',
    );
    expect(preview.ruleImportErrorsText).toBe('1 invalid row');
    expect(preview.ruleImportErrorsList).toBe('Line 8: Expected 6 columns, found 2');
    expect(preview.ruleImportShowCategorize).toBe(true);
    expect(preview.ruleImportCategorizeText).toBe('Categorize 2 uncategorized bookings now');
    expect(preview.ruleImportConfirmEnabled).toBe(true);

    const cancelled = settings.cancelRuleImport();
    expect(cancelled.ruleImportOpen).toBe(false);
    expect(cancelled.categoryRows.some((row) => row.name === 'Household')).toBe(false);

    settings.importRules();
    const imported = settings.confirmRuleImport(true);
    expect(imported.ruleImportOpen).toBe(false);
    expect(imported.messageText).toBe(translate('en', 'MessageRulesImported', 3, 2, 2));
    expect(imported.categoryRows.some((row) => row.name === 'Household')).toBe(true);
    expect(imported.categoryRows.some((row) => row.name === 'Salary')).toBe(true);
    expect(imported.ruleRows).toHaveLength(6);
  });

  it('publishes every change to its subscribers', () => {
    const settings = fake();
    const seen: string[] = [];
    const stop = settings.subscribe((dto) => seen.push(dto.primaryCurrency));

    settings.setPrimaryCurrency('USD');
    expect(settings.state().primaryCurrency).toBe('USD');
    settings.dismissMessage();
    stop();
    settings.setPrimaryCurrency('GBP');

    expect(seen).toEqual(['USD', 'USD']);
    expect(settings.state().primaryCurrency).toBe('GBP');
  });
});
