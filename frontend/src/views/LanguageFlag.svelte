<script lang="ts">
  import { Icon } from '../lib/components';

  let { language }: { language: string } = $props();
</script>

<span class="flag" aria-hidden="true">
  {#if language === 'de'}
    <span class="de"><i class="black"></i><i class="red"></i><i class="gold"></i></span>
  {:else if language === 'fr'}
    <span class="fr"><i class="blue"></i><i class="white"></i><i class="red"></i></span>
  {:else}
    <Icon name="flag-gb" size={20} />
  {/if}
</span>

<style>
  .flag {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 14px;
    border: 1px solid var(--color-divider);
    border-radius: var(--radius-hairline);
    overflow: hidden;
    background: transparent;
  }

  .de,
  .fr {
    display: flex;
    width: 100%;
    height: 100%;
  }

  .de {
    flex-direction: column;
  }

  .fr {
    flex-direction: row;
  }

  i {
    display: block;
    flex: 1;
  }

  .de .black {
    background: #000000;
  }

  .de .red {
    background: #dd0000;
  }

  .de .gold {
    background: #ffce00;
  }

  .fr .blue {
    background: #002395;
  }

  .fr .white {
    background: #ffffff;
  }

  .fr .red {
    background: #ed2939;
  }
</style>
