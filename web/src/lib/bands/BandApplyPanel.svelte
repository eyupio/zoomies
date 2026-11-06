<!--
  The question every band control now asks before it changes anything: where
  should this land?

  It exists because "this page" was the wrong answer often enough to be worth
  a step. The common case is still one press: the panel opens on This page and
  the primary button names that page, so an operator who meant only what they
  are looking at pays nothing for the choice. The other two answers are there
  for the person who has just set the same cut on eight bands by hand.

  The page list shows what each page holds now, so a change is a difference the
  operator can see rather than a number they have to remember.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import Popover from '$lib/components/Popover.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { BAND_PAGES, BAND_SIZES, type BandPage, type BandSize } from './pages';
  import {
    BAND_SCOPE_OPTIONS,
    bandPageLabel,
    bandPagesSentence,
    pagesForScope,
    type BandScope,
  } from './scope';

  interface Props {
    /** The page whose band opened the panel. */
    page: BandPage;
    /** The cut chosen on the band. */
    size: BandSize;
    /** The control the panel points at. Null while it is closed. */
    anchor: HTMLElement | null;
    onclose: () => void;
    onapply: (pages: BandPage[]) => void;
  }

  let { page, size, anchor, onclose, onapply }: Props = $props();

  // This page is the answer most of the time, so it starts chosen and the
  // panel's primary button is a single press. Read once, when the panel opens:
  // the page it was opened from does not change under it.
  let scope = $state<BandScope>('page');
  let selected = $state<BandPage[]>(untrack(() => [page]));

  const sizeOption = $derived(BAND_SIZES.find((option) => option.key === size) ?? BAND_SIZES[0]);
  const pages = $derived(pagesForScope(scope, page, selected));
  const count = $derived(pages.length);
  const offCount = $derived(pages.filter((entry) => !prefs.bandShown(entry)).length);

  /** The current answer for a page, in the words the rest of the panel uses. */
  function pageState(entry: BandPage): string {
    const bandOff = prefs.bandShown(entry) ? '' : ', band off';
    const cut = BAND_SIZES.find((option) => option.key === prefs.bandSize(entry));
    return `${cut?.label ?? prefs.bandSize(entry)}${bandOff}`;
  }

  const title = $derived(`Apply ${sizeOption.label}`);
  const hint = $derived(sizeOption.name);

  const applyLabel = $derived.by(() => {
    if (count === 0) return 'Apply';
    if (scope === 'page') return `Apply to ${bandPageLabel(page)}`;
    return `Apply to ${count} ${count === 1 ? 'page' : 'pages'}`;
  });

  const target = $derived.by(() => {
    if (count === 0) return 'Choose at least one page.';
    const which = count === 1 ? bandPagesSentence(pages) : `${count} bands`;
    return `${which} will take the ${sizeOption.label} cut.`;
  });

  const orderedPages = $derived.by(() => {
    const origin = BAND_PAGES.find((entry) => entry.key === page);
    return origin ? [origin, ...BAND_PAGES.filter((entry) => entry.key !== page)] : [...BAND_PAGES];
  });

  function togglePage(entry: BandPage, on: boolean): void {
    if (on) {
      if (!selected.includes(entry)) selected = [...selected, entry];
      return;
    }
    selected = selected.filter((chosen) => chosen !== entry);
  }

  function apply(): void {
    if (count === 0) return;
    onapply(pages);
  }
</script>

<Popover
  open={anchor !== null}
  {anchor}
  label={`${sizeOption.label} band size, where to apply`}
  {onclose}
>
  <div class="panel">
    <header class="head">
      <p class="eyebrow">Band size</p>
      <h2 class="title">{title}</h2>
      <p class="hint">{hint}</p>
    </header>

    <RadioGroup
      name="band-size-scope"
      legend="Where to apply"
      options={BAND_SCOPE_OPTIONS}
      value={scope}
      onchange={(value) => (scope = value as BandScope)}
    />

    {#if scope === 'selected'}
      <div class="picker" role="group" aria-label="Pages to change">
        <div class="picker-head">
          <p class="eyebrow">{selected.length} of {BAND_PAGES.length} chosen</p>
          <div class="picker-actions">
            <button
              type="button"
              class="link"
              onclick={() => (selected = [...pagesForScope('all', page, [])])}
            >
              All
            </button>
            <span class="dot" aria-hidden="true">·</span>
            <button type="button" class="link" onclick={() => (selected = [])}>None</button>
          </div>
        </div>
        <div class="pages">
          {#each orderedPages as entry (entry.key)}
            <Checkbox
              class="page"
              label={entry.key === page ? `${entry.label} (this page)` : entry.label}
              description={pageState(entry.key)}
              checked={selected.includes(entry.key)}
              onchange={(on) => togglePage(entry.key, on)}
            />
          {/each}
        </div>
      </div>
    {/if}

    {#if offCount > 0}
      <p class="aside">
        {offCount === 1
          ? 'One of these bands is switched off'
          : `${offCount} of these bands are switched off`}; the cut is kept for when it is switched
        back on.
      </p>
    {/if}
  </div>

  {#snippet footer()}
    <p class="target" aria-live="polite">{target}</p>
    <Button variant="ghost" size="sm" onclick={onclose}>Cancel</Button>
    <Button variant="primary" size="sm" disabled={count === 0} onclick={apply}>{applyLabel}</Button>
  {/snippet}
</Popover>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }
  .head .eyebrow {
    margin: 0 0 var(--z-space-1);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-subtle);
  }
  .title {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .hint {
    margin: var(--z-space-1) 0 0;
    max-width: 40ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .picker {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    overflow: hidden;
  }
  .picker-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2);
    padding: var(--z-space-2) var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
  }
  .picker-head .eyebrow {
    margin: 0;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-subtle);
  }
  .picker-actions {
    display: flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .link:hover {
    color: var(--z-accent-hover);
  }
  .link:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
    border-radius: var(--z-radius-sm);
  }
  .dot {
    color: var(--z-text-subtle);
  }
  /* The list is the only part that scrolls: a dozen rows plus the two
     questions above would otherwise put Apply below the fold on a laptop. */
  .pages {
    display: flex;
    flex-direction: column;
    max-height: 13rem;
    overflow-y: auto;
  }
  .pages :global(.page) {
    align-items: center;
    padding: var(--z-space-2) var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .pages :global(.page:first-child) {
    border-top: 0;
  }
  .pages :global(.page input) {
    margin-top: 0;
  }
  .pages :global(.page label) {
    font-size: var(--z-text-sm);
  }
  .pages :global(.page p) {
    font-size: var(--z-text-2xs);
  }
  .aside {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  .target {
    flex: 1 1 12rem;
    min-width: 0;
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
</style>
