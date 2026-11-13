<script lang="ts">
  import { TextButton } from '../../lib/components';

  let {
    text,
    t,
    onClose,
  }: {
    text: string;
    t: (key: string) => string;
    onClose: () => void;
  } = $props();
</script>

<div class="banner" role="status" data-testid="banking-info">
  <p>{text}</p>
  <TextButton text={t('Close')} onclick={onClose} />
</div>

<style>
  .banner {
    position: fixed;
    z-index: 60;
    bottom: 16px;
    left: 50%;
    display: flex;
    align-items: center;
    gap: 12px;
    width: 520px;
    max-width: calc(100% - 32px);
    padding: 12px;
    transform: translateX(-50%);
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-panel);
    background: var(--color-surface);
    box-shadow: var(--shadow-menu);
  }

  p {
    flex: 1;
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.35;
    overflow-wrap: anywhere;
  }
</style>
