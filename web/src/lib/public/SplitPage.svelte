<!--
  A page before sign in that has something to do: the stage on one side, the
  thing to do on the other.

  Sign in, the first account and the consent screen are the same shape, and
  drawing it three times is how they came to differ in everything but the brand.
  The stage says what this is and the column beside it is where the person acts,
  so the column is the page's own and the stage is borrowed. On a phone the stage
  becomes a strip above the column, so the first field is still on the screen.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import BrandStage from './BrandStage.svelte';
  import PublicFrame from './PublicFrame.svelte';

  interface Props {
    eyebrow?: string;
    headline: string | readonly string[];
    accent?: string;
    lede?: string;
    /** The footer's build and its note, as PublicFrame takes them. */
    version?: string;
    note?: string;
    /** The column the person acts in. */
    children: Snippet;
  }
  const { eyebrow, headline, accent, lede, version, note, children }: Props = $props();
</script>

<PublicFrame {version} {note}>
  <div class="split">
    <BrandStage {eyebrow} {headline} {accent} {lede} treatment={prefs.logoTreatment} />
    <div class="side">{@render children()}</div>
  </div>
</PublicFrame>

<style>
  /* The stage is as tall as the column wants to be, and the column is centred
     beside it, as the portal's sign-in is. */
  .split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--z-space-8);
    align-items: stretch;
    min-height: 40rem;
  }
  /* The column is a card with the stage's radius and a hairline edge, on the
     page's own ground: the same pairing of words with a dark panel that the
     band's pages make, so the two halves read as one composition and not as a
     panel beside an unframed form. */
  .side {
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    padding: var(--z-space-12);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-xl);
    background: var(--z-surface);
  }
  /* The touch size for what the person fills in, whichever page this is: a
     public page is most often met on a phone, by somebody in a hurry. */
  .side :global(input) {
    min-height: var(--z-space-12);
    font-size: var(--z-control-font-touch);
  }
  .side :global(.btn) {
    min-height: var(--z-space-12);
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .side :global(.icon-btn) {
    width: var(--z-control-touch);
    height: var(--z-control-touch);
  }
  .side :global(.has-trailing input) {
    padding-right: calc(var(--z-control-touch) + var(--z-space-2));
  }
  @media (max-width: 800px) {
    .split {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--z-space-4);
      min-height: 0;
    }
    .side {
      padding: var(--z-space-6) var(--z-space-5);
    }
  }
</style>
