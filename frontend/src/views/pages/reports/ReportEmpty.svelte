<script lang="ts">
  let { title, hint }: { title: string; hint: string } = $props();
</script>

<div class="empty" data-testid="reports-empty">
  <div class="mark" aria-hidden="true">
    <span class="stroke tall"></span>
    <span class="stroke short"></span>
  </div>
  <p class="title">{title}</p>
  <p class="hint">{hint}</p>
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 40px;
    text-align: center;
  }

  .mark {
    position: relative;
    width: 44px;
    height: 44px;
    border: 2px solid var(--color-muted-text);
    border-radius: var(--radius-panel);
  }

  .stroke {
    position: absolute;
    display: block;
    border-radius: var(--radius-hairline);
    background: var(--color-muted-text);
  }

  .stroke.tall {
    top: 12px;
    left: 12px;
    width: 8px;
    height: 18px;
  }

  .stroke.short {
    top: 18px;
    left: 23px;
    width: 8px;
    height: 12px;
  }

  .title {
    margin: 0;
    color: var(--color-text);
    font-size: var(--type-title-large-size);
    font-weight: var(--type-title-large-weight);
    line-height: 1.3;
    overflow-wrap: anywhere;
  }

  .hint {
    margin: 0;
    color: var(--color-muted-text);
    font-size: var(--type-body-size);
    font-weight: var(--type-body-weight);
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
</style>
