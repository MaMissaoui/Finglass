<script lang="ts">
  import { bankLogoUrl } from '../icons';
  import { monogramColour } from '../theme';

  let {
    logo = '',
    letters = '',
    colourIndex = 0,
    size = 24,
  }: {
    logo?: string;
    letters?: string;
    colourIndex?: number;
    size?: number;
  } = $props();

  const logoUrl = $derived(logo ? bankLogoUrl(logo) : undefined);
  const showLogo = $derived(logoUrl !== undefined);
  const radius = $derived(showLogo ? size * 0.25 : size / 2);
</script>

{#if showLogo || letters}
  <span
    class="badge"
    class:logo={showLogo}
    style:--badge-size={`${size}px`}
    style:--badge-radius={`${radius}px`}
    style:--badge-background={showLogo ? '#ffffff' : monogramColour(colourIndex)}
    aria-hidden="true"
  >
    {#if showLogo}
      <img src={logoUrl} alt="" />
    {:else}
      <span class="letters">{letters}</span>
    {/if}
  </span>
{/if}

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--badge-size);
    height: var(--badge-size);
    border: 0;
    border-radius: var(--badge-radius);
    background: var(--badge-background);
    overflow: hidden;
  }

  .badge.logo {
    border: 1px solid var(--color-divider);
  }

  img {
    width: 80%;
    height: 80%;
    object-fit: contain;
  }

  .letters {
    color: #ffffff;
    font-family: var(--font-sans);
    font-size: calc(var(--badge-size) * 0.42);
    font-weight: var(--type-title-medium-weight);
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
