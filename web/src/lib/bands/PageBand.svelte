<!--
  A page's band: its answer, said first, in the Overview hero's stage.

  One component for every page, so the bands cannot drift apart. Zoomies Black in both
  themes, neutral white and the brand's blue and no status colour, so a colour chosen
  for one theme is never wrong on the other. The grid or the list under it is the
  evidence; this is the headline over it.

  Four sizes, from Slim (the shortest) through Compact and Standard to Expanded. Each page
  has a default, and the operator's choice is per page: the same four sizes sit on the band,
  as Eli's panel sizes do, and in Appearance. Pressing one asks where it lands (this page,
  pages picked one at a time, or every page). The dog can be resized or removed, and asks
  the same question about where. Both choices follow the signed-in account, with this
  browser as the fallback.

  A band can carry figures (`stats`) in a strip along its lower edge, which is how a
  page gives up the panel it used to spend a row of its height on. They are never lost
  with the band: when it is switched off, or a phone is hiding it (the rows must be on
  a phone's first screen), the same figures stand alone as a plain panel. That choice is
  made here, not by each page, so a page cannot forget it.

  The small cross removes the band in place, after a confirmation that says where to
  bring it back, because a control that deletes something quietly is one nobody trusts.
  It is the one control that does not ask where it lands: it hides the band under it.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import {
    Minus,
    RectangleHorizontal,
    RectangleVertical,
    Square,
    X,
    type LucideIcon,
  } from '@lucide/svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import BrandStage from '$lib/public/BrandStage.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import BandApplyPanel from './BandApplyPanel.svelte';
  import BandStats from './BandStats.svelte';
  import type { BandLogoSize } from './band-logo';
  import { figuresPlacement } from './placement';
  import { BAND_PAGES, BAND_SIZES, type BandPage, type BandSize } from './pages';
  import { bandPageLabel } from './scope';
  import type { BandLine } from './lines';
  import type { BandMore, BandStat } from './stats';

  interface Props {
    page: BandPage;
    /** The sentence, or null while the figures it is made from have not arrived. */
    line: BandLine | null;
    /** Figures the band carries, so the page does not need a panel of its own for them. */
    stats?: readonly BandStat[];
    /** The way on to the fuller view, at the end of the strip. */
    more?: BandMore;
    eyebrow?: string;
    /** A selector for tests; defaults to `band-<page>`. */
    testid?: string;
    /** Anything the band offers beside its sentence, like the Overview's Review. */
    actions?: Snippet;
    /**
     * A picture drawn beside the words, where the dog stands when it is shown:
     * the Overview's punch card, the slot map. Every cut carries it, since the
     * figures give even Slim the height for seven rows of squares, and
     * Appearance can switch it off per page or for every page.
     */
    aside?: Snippet;
  }
  let {
    page,
    line,
    stats = [],
    more,
    eyebrow = 'Right now',
    testid,
    actions,
    aside,
  }: Props = $props();

  /*
    One glyph for each cut, read as a scale: a line, a short bar, a square
    and a tall bar. The words would take the width the band needs for its
    sentence, and the tooltip and accessible name carry the name.
  */
  const BAND_SIZE_ICONS: Record<BandSize, LucideIcon> = {
    slim: Minus,
    compact: RectangleHorizontal,
    standard: Square,
    expanded: RectangleVertical,
  };

  const shown = $derived(prefs.bandShown(page));
  const size = $derived(prefs.bandSize(page));
  const logoShown = $derived(prefs.bandLogoShown(page));
  const logoSize = $derived(prefs.bandLogoSizeFor(page));
  const name = $derived(BAND_PAGES.find((p) => p.key === page)?.label ?? page);
  const placement = $derived(
    figuresPlacement({ shown, hasLine: line !== null, hasStats: stats.length > 0 }),
  );
  let confirming = $state(false);

  /**
   * The change waiting for an answer about where it lands. It holds the control
   * it was opened from, because that is where focus goes back to when the panel
   * closes, and the cut, because the icon that opened it is the cut chosen.
   */
  type Pending =
    { kind: 'size'; size: BandSize; anchor: HTMLElement } | { kind: 'logo'; anchor: HTMLElement };
  let pending = $state<Pending | null>(null);

  function openSize(next: BandSize, anchor: HTMLElement): void {
    // The control that opens a panel also closes it: a second press on the size
    // already chosen puts the panel away rather than starting it again.
    if (pending?.kind === 'size' && pending.size === next && pending.anchor === anchor) {
      pending = null;
      return;
    }
    pending = { kind: 'size', size: next, anchor };
  }

  function openLogo(anchor: HTMLElement): void {
    if (pending?.kind === 'logo' && pending.anchor === anchor) {
      pending = null;
      return;
    }
    pending = { kind: 'logo', anchor };
  }

  function closePanel(): void {
    pending = null;
  }

  /** Apply what the panel asked for, then put the keyboard back where it was. */
  function apply(pages: BandPage[], logo: { shown: boolean; size: BandLogoSize } | null): void {
    const change = pending;
    if (!change) return;
    if (change.kind === 'size') {
      prefs.applyBandSize(pages, change.size);
      const cut = BAND_SIZES.find((option) => option.key === change.size)?.label ?? change.size;
      toasts.success(
        pages.length === 1
          ? `${bandPageLabel(pages[0] ?? page)} band set to ${cut}`
          : `${pages.length} bands set to ${cut}`,
        saveNote(),
      );
    } else if (logo) {
      prefs.applyBandLogo(pages, logo);
      const what = !logo.shown
        ? 'Dog hidden'
        : logo.size === 'auto'
          ? 'Dog follows the cut'
          : `${logo.size.charAt(0).toUpperCase()}${logo.size.slice(1)} dog`;
      toasts.success(
        pages.length === 1
          ? `${what} on ${bandPageLabel(pages[0] ?? page)}`
          : `${what} on ${pages.length} bands`,
        saveNote(),
      );
    }
    pending = null;
    if (document.contains(change.anchor)) change.anchor.focus();
  }

  /** Where the choice was kept, said honestly: the account when there is one. */
  function saveNote(): string {
    return prefs.accountSynced
      ? 'Saved to your account, so it holds in every browser you sign in to.'
      : 'Saved in this browser.';
  }

  function hide(): boolean {
    prefs.setBandShown(page, false);
    return true;
  }
</script>

{#if line && shown}
  <div class="band" data-size={size} data-testid={testid ?? `band-${page}`}>
    <BrandStage
      wide
      {eyebrow}
      headline={line.headline}
      accent={line.accent || undefined}
      lede={line.lede}
      treatment={prefs.logoTreatment}
      art={logoShown}
      artSize={logoSize === 'auto' ? undefined : logoSize}
      artAction={openLogo}
      artActionLabel={`${name} band logo settings`}
      artExpanded={pending?.kind === 'logo'}
      aside={prefs.bandPictureShown(page) ? aside : undefined}
    >
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
      {#if stats.length > 0}
        <div class="figures"><BandStats {stats} {more} variant="band" /></div>
      {/if}
    </BrandStage>
    <div class="tools">
      <div class="sizes" role="group" aria-label={`${name} band size`}>
        {#each BAND_SIZES as option (option.key)}
          {@const SizeIcon = BAND_SIZE_ICONS[option.key]}
          <Tooltip bare text={option.name} placement="bottom">
            <button
              type="button"
              class="size"
              aria-label={option.label}
              aria-pressed={size === option.key}
              aria-haspopup="dialog"
              aria-expanded={pending?.kind === 'size' && pending.size === option.key}
              onclick={(event) => openSize(option.key, event.currentTarget)}
            >
              <SizeIcon size={16} aria-hidden="true" />
            </button>
          </Tooltip>
        {/each}
      </div>
      <Tooltip bare text={`Hide the ${name} band`} placement="left">
        <button
          type="button"
          class="dismiss"
          aria-label={`Hide the ${name} band`}
          onclick={() => (confirming = true)}
        >
          <X size={14} aria-hidden="true" />
        </button>
      </Tooltip>
    </div>
  </div>
{/if}

{#if pending}
  <BandApplyPanel
    kind={pending.kind}
    {page}
    size={pending.kind === 'size' ? pending.size : undefined}
    anchor={pending.anchor}
    onclose={closePanel}
    onapply={apply}
  />
{/if}

<!--
  The figures on their own: always when the band is off or has no sentence yet, and on a
  phone when it is on, since a phone never draws the stage. Nothing when there are none.
-->
{#if placement !== 'none'}
  <div class="alone" class:phone-only={placement === 'phone-only'}>
    <BandStats {stats} {more} variant="panel" />
  </div>
{/if}

<ConfirmDialog
  bind:open={confirming}
  title={`Hide the ${name} band?`}
  description={`The band is the dark panel at the top of this page. ${
    stats.length > 0 ? 'Its figures stay on the page, in a plain panel. ' : ''
  }Nothing else changes.`}
  consequences={[
    `To bring it back, open Settings, then Appearance, and switch on ${name} under Page bands.`,
    prefs.accountSynced
      ? 'Saved to your account, so it holds in every browser you sign in to.'
      : 'Saved in this browser.',
  ]}
  confirmLabel="Hide the band"
  tone="default"
  onconfirm={hide}
/>

<style>
  .band {
    position: relative;
    margin-bottom: var(--z-space-4);
  }
  .actions {
    margin-top: var(--z-space-5);
  }
  .actions:empty {
    display: none;
  }
  .figures {
    margin-top: var(--z-space-5);
  }

  /* Slim: the least air, so the rows begin right under it. */
  .band[data-size='slim'] :global(.stage) {
    padding-block: var(--z-space-3);
    padding-inline: var(--z-space-8);
  }
  .band[data-size='slim'] :global(.art) {
    width: clamp(3rem, 7%, 5rem);
  }
  .band[data-size='slim'] :global(.headline) {
    font-size: var(--z-text-xl);
    line-height: var(--z-leading-xl);
  }
  .band[data-size='slim'] :global(.eyebrow) {
    margin-bottom: var(--z-space-2);
  }
  .band[data-size='slim'] :global(.rule) {
    margin-block: var(--z-space-2);
  }
  .band[data-size='slim'] :global(.lede) {
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .band[data-size='slim'] .actions,
  .band[data-size='slim'] .figures {
    margin-top: var(--z-space-3);
  }
  .band[data-size='slim'] .figures :global(.value) {
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
  }

  /* Compact: less air above and below, a smaller dog and a smaller headline. */
  .band[data-size='compact'] :global(.stage) {
    padding-block: var(--z-space-6);
    padding-inline: var(--z-space-10);
  }
  .band[data-size='compact'] :global(.art) {
    width: clamp(5rem, 14%, 8rem);
  }
  .band[data-size='compact'] :global(.headline) {
    font-size: var(--z-text-2xl);
    line-height: var(--z-leading-2xl);
  }
  .band[data-size='compact'] :global(.rule) {
    margin-block: var(--z-space-4);
  }
  .band[data-size='compact'] .actions,
  .band[data-size='compact'] .figures {
    margin-top: var(--z-space-4);
  }

  /* Standard and Expanded: the strip's figures are the size of the page's own
     tiles, and stay on one line while the cut around them grows. */
  .band[data-size='standard'] .figures :global(.value),
  .band[data-size='expanded'] .figures :global(.value) {
    font-size: var(--z-text-2xl);
    line-height: var(--z-leading-2xl);
  }

  /* Expanded: more air, a larger dog and a headline that can carry a quiet page. */
  .band[data-size='expanded'] :global(.stage) {
    padding-block: var(--z-space-12);
  }
  .band[data-size='expanded'] :global(.art) {
    width: clamp(10rem, 28%, 16rem);
  }
  .band[data-size='expanded'] :global(.headline) {
    font-size: calc(var(--z-text-3xl) * 1.2);
    line-height: calc(var(--z-leading-3xl) * 1.2);
  }
  .band[data-size='expanded'] :global(.rule) {
    margin-block: var(--z-space-8);
  }
  .band[data-size='expanded'] :global(.lede) {
    max-width: 36rem;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
  }
  .band[data-size='expanded'] .actions,
  .band[data-size='expanded'] .figures {
    margin-top: var(--z-space-6);
  }

  /* The band's own controls, as Eli's panel carries its sizes: faint at rest,
     plain when the band is looked at or one of them has focus. */
  .tools {
    position: absolute;
    inset-block-start: var(--z-space-3);
    inset-inline-end: var(--z-space-3);
    z-index: 1;
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  .sizes {
    display: flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  .size {
    display: grid;
    place-items: center;
    width: var(--z-space-6);
    height: var(--z-space-6);
    padding: 0;
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-sm);
    background: transparent;
    color: var(--z-panel-text-muted);
    opacity: 0.7;
    cursor: pointer;
    transition:
      opacity var(--z-motion-fast) var(--z-ease),
      background-color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  .band:hover .size,
  .band:focus-within .size {
    opacity: 1;
  }
  .size:hover {
    background: var(--z-panel-raised);
    color: var(--z-panel-text);
  }
  .size[aria-pressed='true'] {
    border-color: var(--z-brand-runner-blue);
    background: var(--z-panel-raised);
    color: var(--z-panel-text);
    opacity: 1;
  }
  .size:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .dismiss {
    display: grid;
    place-items: center;
    width: var(--z-space-6);
    height: var(--z-space-6);
    padding: 0;
    border: 0;
    border-radius: var(--z-radius-md);
    background: transparent;
    color: var(--z-panel-text-subtle);
    opacity: 0.6;
    cursor: pointer;
    transition:
      opacity var(--z-motion-fast) var(--z-ease),
      background-color var(--z-motion-fast) var(--z-ease);
  }
  .band:hover .dismiss,
  .dismiss:hover,
  .dismiss:focus-visible {
    opacity: 1;
  }
  .dismiss:hover {
    background: var(--z-panel-raised);
    color: var(--z-panel-text);
  }
  .dismiss:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  /* The targets are larger than the text and glyph, which is a fingertip's
     problem and not a mouse's. */
  @media (pointer: coarse) {
    .tools {
      inset-block-start: 0;
      inset-inline-end: 0;
    }
    .size,
    .dismiss {
      width: var(--z-control-touch);
      height: var(--z-control-touch);
    }
  }

  @media (max-width: 768px) {
    .band {
      display: none;
    }
  }
  @media (min-width: 769px) {
    .alone.phone-only {
      display: none;
    }
  }
</style>
