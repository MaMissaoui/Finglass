<script lang="ts">
  import { iconUrl, type IconName } from '../icons';

  let {
    name,
    size = 20,
    label,
  }: {
    name: IconName;
    size?: number;
    label?: string;
  } = $props();

  const url = $derived(iconUrl(name));
</script>

{#if name === 'flag-gb'}
  <img
    class="flag"
    src={url}
    width={size}
    height={size}
    style:width={`${size}px`}
    style:height={`${size}px`}
    alt={label ?? ''}
    aria-hidden={label ? undefined : 'true'}
  />
{:else}
  <span
    class="icon"
    style:--icon-url={`url("${url}")`}
    style:--icon-size={`${size}px`}
    role={label ? 'img' : undefined}
    aria-label={label || undefined}
    aria-hidden={label ? undefined : 'true'}
  ></span>
{/if}

<style>
  .icon {
    display: inline-block;
    flex: none;
    width: var(--icon-size);
    height: var(--icon-size);
    background-color: currentColor;
    mask: var(--icon-url) center / contain no-repeat;
    -webkit-mask: var(--icon-url) center / contain no-repeat;
  }

  .flag {
    display: inline-block;
    flex: none;
    max-width: 100%;
    object-fit: contain;
  }
</style>
