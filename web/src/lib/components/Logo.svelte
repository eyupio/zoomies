<!-- The original black artwork keeps its coat and facial markings in both
     themes. Decoration belongs to the backdrop, never to the artwork. -->
<script lang="ts">
  import { prefs } from '../state/prefs.svelte';
  import type { LogoTreatment } from '../state/logo-treatment';

  interface Props {
    variant?: 'mark' | 'wordmark' | 'full' | 'lockup';
    size?: number;
    label?: string;
    /** Appearance previews can show an option without changing the saved choice. */
    treatment?: LogoTreatment;
    class?: string;
  }
  const {
    variant = 'full',
    size = 24,
    label = 'Zoomies',
    treatment,
    class: klass = '',
  }: Props = $props();
  const chosen = $derived(treatment ?? prefs.logoTreatment);
  const asset = $derived(variant === 'lockup' ? 'logo' : size > 64 ? 'mark' : 'paw-swish');
  const edge = $derived(variant === 'lockup' ? Math.max(220, Math.round(size * 3.1)) : size);
  const src = $derived(
    `/brand/${asset}-black${asset === 'paw-swish' ? '.png' : '-transparent.svg'}`,
  );
  const large = $derived(variant === 'lockup' || size > 64);
</script>

<span
  class="logo {variant} {klass}"
  class:large
  style="--edge: {edge}px"
  data-treatment={large ? chosen : undefined}
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
>
  {#if variant !== 'wordmark'}
    <span class="art">
      {#if asset === 'paw-swish'}
        <img
          class="black"
          src="/brand/paw-swish-black.png"
          width={edge}
          height={edge}
          alt=""
          decoding="async"
        />
        <img
          class="white"
          src="/brand/paw-swish-white.png"
          width={edge}
          height={edge}
          alt=""
          decoding="async"
        />
      {:else}
        <img {src} width={edge} height={edge} alt="" decoding="async" />
      {/if}
    </span>
  {/if}
  {#if variant === 'full' || variant === 'wordmark'}
    <span class="wordmark" style="--word-h: {Math.round(size * 0.62)}px"></span>
  {/if}
</span>

<style>
  .logo {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    color: var(--z-text);
    line-height: 0;
    max-width: 100%;
  }
  .logo.lockup {
    display: flex;
    width: calc(var(--edge) * var(--z-logo-scene-scale));
    justify-content: center;
  }
  .art {
    position: relative;
    isolation: isolate;
    display: block;
    width: var(--edge);
    max-width: 100%;
    aspect-ratio: 1;
    flex: none;
  }
  .large .art::before {
    content: '';
    position: absolute;
    pointer-events: none;
    inset: -12%;
    background: var(--z-logo-starlight) center / contain no-repeat;
    mask-image: var(--z-logo-edge-fade);
    mask-composite: intersect;
  }
  .lockup .art {
    width: 100%;
  }
  .lockup .art::before {
    inset: 0;
  }
  .lockup .art img {
    padding: var(--z-logo-scene-padding);
    box-sizing: border-box;
  }
  .art img {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  [data-treatment='pearl'] .art::before {
    background-image: var(--z-logo-pearl);
  }
  [data-treatment='orbital'] .art::before {
    background-image: var(--z-logo-orbital);
  }
  .lockup[data-treatment='square'] .art::before,
  .lockup[data-treatment='circle'] .art::before {
    inset: var(--z-logo-scene-padding);
  }
  [data-treatment='square'] .art::before,
  [data-treatment='circle'] .art::before {
    inset: 0;
    background: var(--z-brand-white);
    mask-image: none;
    border: var(--z-border-width) solid var(--z-logo-inset-border);
    border-radius: var(--z-radius-md);
  }
  [data-treatment='circle'] .art::before {
    border-radius: var(--z-radius-full);
  }
  .large[data-treatment='square'] .art::before {
    border-radius: var(--z-logo-inset-radius);
  }
  .large[data-treatment='square'] .art::before,
  .large[data-treatment='circle'] .art::before {
    box-shadow: var(--z-logo-inset-shadow);
  }
  /* Paw icons retain their original theme variants and have no backdrop. */
  .art .white {
    display: none;
  }
  :global(:root[data-theme='dark']) .black {
    display: none;
  }
  :global(:root[data-theme='dark']) .white {
    display: block;
  }
  @media (prefers-color-scheme: dark) {
    :global(:root:not([data-theme='light'])) .black {
      display: none;
    }
    :global(:root:not([data-theme='light'])) .white {
      display: block;
    }
  }
  .wordmark {
    display: block;
    height: var(--word-h);
    aspect-ratio: 1128 / 226;
    background: currentColor;
    mask: url('/brand/wordmark-black-transparent.svg') left center / contain no-repeat;
  }
</style>
