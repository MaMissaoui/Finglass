<script lang="ts">
  import PrimaryButton from '../../../lib/components/PrimaryButton.svelte';
  import TextButton from '../../../lib/components/TextButton.svelte';
  import { translate, type Language } from '../../../lib/i18n';

  let {
    language,
    onaddbank,
    onopensettings,
  }: {
    language: Language;
    onaddbank: () => void;
    onopensettings: () => void;
  } = $props();

  function t(key: string): string {
    return translate(language, key);
  }

  const steps = $derived([
    {
      title: t('StepAddBank'),
      hint: t('StepAddBankHint'),
    },
    {
      title: t('StepSync'),
      hint: t('StepSyncHint'),
    },
    {
      title: t('StepCategorize'),
      hint: t('StepCategorizeHint'),
    },
  ]);
</script>

<section class="welcome" data-testid="dashboard-first-run">
  <h2>{t('WelcomeTitle')}</h2>
  <p class="intro">{t('GettingStartedIntro')}</p>

  {#each steps as step, index (step.title)}
    <div class="step">
      <span class="number" aria-hidden="true">{index + 1}</span>
      <div class="step-text">
        <h3>{step.title}</h3>
        <p>{step.hint}</p>
        {#if index === 0}
          <div class="action primary-action">
            <PrimaryButton text={t('AddBank')} icon="add" onclick={onaddbank} />
          </div>
        {:else if index === 2}
          <div class="action">
            <TextButton text={t('OpenSettings')} onclick={onopensettings} />
          </div>
        {/if}
      </div>
    </div>
  {/each}
</section>

<style>
  .welcome {
    display: flex;
    flex-direction: column;
    gap: 16px;
    width: 640px;
    max-width: 100%;
    padding: 24px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-surface);
    background: var(--color-surface);
  }

  .welcome h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
  }

  .intro {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
  }

  .step {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .number {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 14px;
    background: var(--color-selected);
    color: var(--color-on-selected);
    font-size: var(--type-label-size);
    font-weight: var(--type-label-weight);
  }

  .step-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }

  .step-text h3 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-small-size);
    font-weight: var(--type-title-small-weight);
  }

  .step-text p {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .action {
    display: flex;
    justify-content: flex-start;
    padding-top: 4px;
  }

  .primary-action {
    width: 140px;
  }

  .primary-action :global(button) {
    width: 100%;
  }
</style>
