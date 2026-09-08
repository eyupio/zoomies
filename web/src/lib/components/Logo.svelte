<!--
  The Zoomies logo system.

  The v2.1 hierarchy is deliberate, and it has four rungs rather than three:

  * lockup is the unchanged primary full logo, never below 220px wide, and
    given real room on the screens where it is the only thing on the page;
  * mark at 128px and above is the original circular dog, the primary
    standalone mark, at the guide's minimum size for it;
  * mark from 48px is the secondary head/swish;
  * mark below 48px is the paw/swish, the official smallest-size shorthand.

  The circular dog is the rung this component used to skip, which left the one
  slot with room for it -- Settings -> About -- showing the head/swish instead.
  That matters beyond preference: the head/swish is a restored reconstruction
  (see ASSET_MANIFEST.json), so it was the only standalone mark in the product
  that is not approved source artwork.

  All artwork is the supplied white reverse on Zoomies Black. Nothing is
  recoloured, cropped or reconstructed in CSS.

  See docs/brand.md.
-->
<script lang="ts">
  interface Props {
    /** mark: the dog alone. wordmark: the word alone. full: side by side. lockup: stacked, for sign-in. */
    variant?: 'mark' | 'wordmark' | 'full' | 'lockup';
    /** The mark's edge length in pixels. The wordmark scales with it. */
    size?: number;
    /** Rendered as the accessible name. Set it to "" inside an already-labelled link. */
    label?: string;
    class?: string;
  }

  const { variant = 'full', size = 24, label = 'Zoomies', class: klass = '' }: Props = $props();

  const markSrc = $derived(
    size >= 128
      ? '/brand/mark-white.png'
      : size >= 48
        ? '/brand/head-swish-white.png'
        : '/brand/paw-swish-white.png',
  );
  /* Only the circular dog has a 2x copy; the others are drawn far larger than
     they are ever placed, so the browser already has pixels to spare. */
  const markSrcset = $derived(
    size >= 128 ? '/brand/mark-white.png 1x, /brand/mark-white@2x.png 2x' : undefined,
  );
  const wordmarkRatio = 975 / 250;
  const wordHeight = $derived(Math.round(size * 0.62));
  const lockupWidth = $derived(Math.max(220, Math.round(size * 3.1)));
</script>

<span
  class="logo {variant} {klass}"
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
>
  {#if variant === 'lockup'}
    <span class="lockup-frame" style="--lockup-width: {lockupWidth}px">
      <img
        class="primary"
        src="/brand/logo-white.png"
        width={lockupWidth}
        height={lockupWidth}
        alt=""
        decoding="async"
      />
    </span>
  {:else if variant !== 'wordmark'}
    <span class="chip" style="--chip: {size}px">
      <img src={markSrc} srcset={markSrcset} width={size} height={size} alt="" decoding="async" />
    </span>
  {/if}

  {#if variant !== 'mark' && variant !== 'lockup'}
    <span
      class="wordmark"
      style="--mark-src: url('/brand/wordmark-white.png'); --word-h: {wordHeight}px; --word-w: {Math.round(
        wordHeight * wordmarkRatio,
      )}px"
    ></span>
  {/if}
</span>

<style>
  .logo {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    color: var(--z-text);
    line-height: 0;
  }
  /*
    The frame is square and capped at the container rather than fixed, so asking
    for a bigger lockup on a sign-in card cannot push the card wider than the
    phone it is being read on. The artwork keeps its supplied padding -- the
    brand guide forbids cropping it -- so the frame is deliberately larger than
    the dog inside it.
  */
  .logo.lockup {
    display: flex;
    width: 100%;
    justify-content: center;
  }
  .lockup-frame {
    display: grid;
    width: min(100%, var(--lockup-width));
    aspect-ratio: 1;
    place-items: center;
    border-radius: var(--z-radius-lg);
    background: var(--z-brand-black);
  }
  .chip {
    display: inline-grid;
    width: var(--chip);
    height: var(--chip);
    place-items: center;
    border-radius: var(--z-radius-md);
    background: var(--z-mark-chip);
  }
  .primary,
  .chip img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  /* The wordmark is a mask over currentColor, so it inherits the theme's text
     colour rather than needing a light and a dark copy. */
  .wordmark {
    display: block;
    width: var(--word-w);
    height: var(--word-h);
    background-color: currentColor;
    -webkit-mask-image: var(--mark-src);
    mask-image: var(--mark-src);
    -webkit-mask-repeat: no-repeat;
    mask-repeat: no-repeat;
    -webkit-mask-size: contain;
    mask-size: contain;
    -webkit-mask-position: left center;
    mask-position: left center;
  }
</style>
