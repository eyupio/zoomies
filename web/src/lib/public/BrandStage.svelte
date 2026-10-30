<!--
  The stage: the dark panel the dog stands on, on the pages before sign in.

  Zoomies Black in both themes, because the backdrop behind the dog is a glow
  drawn for a dark ground and the choice made in Appearance is only ever shown
  on one. What stands on it is brand and not content: the words are the
  product's pitch or a page's purpose in one line, never the thing a person came
  to do, which is why none of them is a heading. Every page that has a stage
  puts its own h1 beside it, in the column the person acts in.

  The dog is drawn at one size and shrinks to the room it is given, so a phone
  gets a smaller dog and not a different layout.

  A page with nothing to do beside it, like the status page, uses it wide: the
  words on the left and the dog on the right, as the page that quotes a price does,
  and the headline is that page's h1.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import LogoArt from '$lib/components/LogoArt.svelte';
  import { DEFAULT_LOGO_TREATMENT, type LogoTreatment } from '$lib/state/logo-treatment';
  import type { BandLogoArtSize } from '../bands/band-logo';

  interface Props {
    /** The small capitals above the headline. */
    eyebrow?: string;
    /**
     * The headline, as one string or as its lines. A line break is a line break;
     * the stage never wraps it for you.
     */
    headline: string | readonly string[];
    /** A line of the headline drawn in the brand's runner blue, as the hero does. */
    accent?: string;
    /** The sentence under the rule. Dropped on a phone, where the form is the point. */
    lede?: string;
    /** Words beside the dog, in a short band, for a page that has no form beside it. */
    wide?: boolean;
    /** The headline is the page's h1, when the stage is the top of the page. */
    heading?: boolean;
    /**
     * The backdrop behind the dog. A page that knows the operator's choice passes
     * it; one that has no preferences, like the status page, takes the default.
     */
    treatment?: LogoTreatment;
    /** Whether the dog is drawn at all. A band hides it when its operator asks. */
    art?: boolean;
    /**
     * Makes the dog a control: a band's dog opens the panel that changes it.
     * The stage has no opinion about what that panel does, so a public page
     * that draws a stage still imports no preference store.
     */
    artAction?: (anchor: HTMLElement) => void;
    /** The dog control's accessible name: "Band logo settings". */
    artActionLabel?: string;
    /** Whether the panel the dog opens is showing. */
    artExpanded?: boolean;
    /** How large the dog is drawn, when `wide`. The stage's own size otherwise. */
    artSize?: BandLogoArtSize;
    /** Anything else the stage carries, under the lede. */
    children?: Snippet;
    class?: string;
  }
  const {
    eyebrow,
    headline,
    accent,
    lede,
    wide = false,
    heading = false,
    treatment = DEFAULT_LOGO_TREATMENT,
    art = true,
    artAction,
    artActionLabel = 'Band logo settings',
    artExpanded = false,
    artSize,
    children,
    class: klass = '',
  }: Props = $props();
  const lines = $derived(typeof headline === 'string' ? headline.split('\n') : headline);
</script>

<div class="stage {klass}" class:wide data-art-size={art ? artSize : undefined}>
  {#if art}
    {#if artAction}
      <button
        type="button"
        class="art art-button"
        aria-label={artActionLabel}
        aria-haspopup="dialog"
        aria-expanded={artExpanded}
        onclick={(event) => artAction(event.currentTarget)}
      >
        <LogoArt variant="mark" size={240} label="" {treatment} />
      </button>
    {:else}
      <div class="art"><LogoArt variant="mark" size={240} label="" {treatment} /></div>
    {/if}
  {/if}
  <div class="words">
    {#if eyebrow}<p class="eyebrow">{eyebrow}</p>{/if}
    {#snippet words()}
      {#each lines as line, i (i)}{#if i > 0}<br />{/if}{line}{/each}{#if accent}<br /><span
          class="accent">{accent}</span
        >{/if}
    {/snippet}
    {#if heading}
      <h1 class="headline">{@render words()}</h1>
    {:else}
      <p class="headline">{@render words()}</p>
    {/if}
    <div class="rule" aria-hidden="true"></div>
    {#if lede}<p class="lede">{lede}</p>{/if}
    {@render children?.()}
  </div>
</div>

<style>
  .stage {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    gap: var(--z-space-8);
    box-sizing: border-box;
    min-width: 0;
    padding: var(--z-space-12);
    border: var(--z-border-width) solid var(--z-panel-border);
    border-radius: var(--z-radius-xl);
    background: var(--z-panel-bg);
    color: var(--z-panel-text);
  }
  .art {
    display: flex;
    justify-content: center;
    /* The dog's own glow is drawn past its box, so the box needs room to spare. */
    padding-block: var(--z-space-6);
    min-width: 0;
  }
  .art :global(.logo) {
    width: clamp(8rem, 62%, 15rem);
  }
  /* In a band the dog is a control: it draws the ring itself when looked at or
     focused, rather than leaving the whole stage to look interactive. */
  .art-button {
    padding: 0;
    border: 0;
    border-radius: var(--z-radius-lg);
    background: transparent;
    color: inherit;
    cursor: pointer;
  }
  .art-button:hover {
    box-shadow: 0 0 0 var(--z-border-width) var(--z-panel-border);
  }
  .art-button:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .words {
    min-width: 0;
  }
  .eyebrow {
    margin: 0 0 var(--z-space-4);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
    color: var(--z-panel-text-muted);
  }
  .headline {
    margin: 0;
    font-size: var(--z-text-3xl);
    line-height: var(--z-leading-3xl);
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-tight);
    text-wrap: balance;
    overflow-wrap: anywhere;
  }
  .accent {
    color: var(--z-brand-runner-blue);
  }
  .rule {
    width: var(--z-space-10);
    height: var(--z-border-width-thick);
    margin-block: var(--z-space-6);
    background: var(--z-brand-runner-blue);
  }
  .lede {
    margin: 0;
    max-width: 28rem;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-lg);
    color: var(--z-panel-text-muted);
    text-wrap: pretty;
    overflow-wrap: anywhere;
  }
  /* Wide: words and dog side by side in a band, the dog at the right. */
  .wide {
    flex-direction: row-reverse;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-8);
    padding-block: var(--z-space-10);
  }
  .wide .art {
    flex: none;
    width: clamp(8rem, 24%, 13rem);
    padding-block: 0;
  }
  .wide .art :global(.logo) {
    width: 100%;
  }
  /* The dog's size is the operator's choice, separate from the cut, so a
     compact band can still carry a large dog and a standard band a small one. */
  .stage.wide[data-art-size='small'] .art {
    width: clamp(4rem, 12%, 6rem);
  }
  .stage.wide[data-art-size='medium'] .art {
    width: clamp(6rem, 18%, 10rem);
  }
  .stage.wide[data-art-size='large'] .art {
    width: clamp(8rem, 24%, 13rem);
  }
  @media (max-width: 800px) {
    .stage,
    .wide {
      flex-direction: row;
      align-items: center;
      justify-content: flex-start;
      gap: var(--z-space-5);
      padding: var(--z-space-6);
    }
    /* The dog beside the words, small, so the form is still on the first screen. */
    .art,
    .wide .art {
      flex: none;
      width: 5.5rem;
      padding-block: 0;
    }
    .art :global(.logo) {
      width: 100%;
    }
    .headline {
      font-size: var(--z-text-xl);
      line-height: var(--z-leading-xl);
    }
    .rule {
      display: none;
    }
    .lede {
      display: none;
    }
    /* A band keeps its sentence on a phone: it is all the stage says. */
    .wide .lede {
      display: block;
      font-size: var(--z-text-sm);
      line-height: var(--z-leading-sm);
    }
    .eyebrow {
      margin-bottom: var(--z-space-2);
    }
  }
</style>
