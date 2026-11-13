<script lang="ts">
  import { Checkbox, PrimaryButton, TextButton } from '../../../lib/components';
  import type { ReapplyGroupRowDTO } from '../../../lib/api';

  let {
    summary,
    warning,
    groups,
    confirmText,
    confirmEnabled,
    t,
    onToggle,
    onConfirm,
    onCancel,
  }: {
    summary: string;
    warning: string;
    groups: ReapplyGroupRowDTO[];
    confirmText: string;
    confirmEnabled: boolean;
    t: (key: string) => string;
    onToggle: (from: number, to: number) => void;
    onConfirm: () => void;
    onCancel: () => void;
  } = $props();
</script>

<div class="scrim" data-testid="reapply-rules-dialog">
  <div class="modal" role="dialog" aria-modal="true" aria-label={t('ReapplyRulesTitle')}>
    <h2>{t('ReapplyRulesTitle')}</h2>
    <p class="summary">{summary}</p>
    <p class="warning">{warning}</p>
    {#each groups as group (`${group.fromId}:${group.toId}`)}
      <div class="group">
        <Checkbox
          text=""
          checked={group.ticked}
          ontoggled={() => onToggle(group.fromId, group.toId)}
        />
        <div class="group-text">
          <span class="group-title">{group.title}</span>
          <span class="group-detail">{group.detail}</span>
        </div>
      </div>
    {/each}
    <div class="actions">
      <TextButton text={t('Cancel')} onclick={onCancel} />
      <PrimaryButton text={confirmText} enabled={confirmEnabled} onclick={onConfirm} />
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-scrim);
  }

  .modal {
    display: flex;
    width: min(560px, 100%);
    max-height: 100%;
    flex-direction: column;
    gap: 16px;
    padding: 24px;
    overflow-y: auto;
    border-radius: var(--radius-dialog);
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: var(--shadow-dialog);
  }

  h2 {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.25;
  }

  .summary {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
  }

  .warning {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
    line-height: 1.4;
  }

  .group {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 0;
  }

  .group-text {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 2px;
  }

  .group-title {
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
  }

  .group-detail {
    color: var(--color-muted-text);
    font-size: var(--type-body-small-size);
    font-weight: var(--type-body-small-weight);
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
  }
</style>
