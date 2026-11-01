<!--
  A titled section: a heading, an optional line of context, an optional action
  opposite it, and a body.

  The Overview is five of these and a runner's page is another five, and for a
  while they were two components with the same markup and diverging CSS -- one
  had learnt to wrap its header, the other to let its body fill a bounded
  height, and neither knew what the other had learnt. Keeping the shape of a
  section in one place is what stops a dashboard of five different card
  designs.

  A panel can carry an icon in a tile beside its title. The Overview's cards
  all do, so the page has one head and not a tile drawn by each panel in its
  own way; a panel without one is unchanged.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { LucideIcon } from '@lucide/svelte';

  interface Props {
    title: string;
    /** Small capitals above the title: a count or a scope. "11 known, 9 live". */
    eyebrow?: string;
    /** Drawn in a tinted tile before the title, as the Overview's cards are. */
    icon?: LucideIcon;
    /** One line under the heading, when the heading cannot say it all. */
    description?: string;
    /** A count, a badge, or a link out. Sits opposite the heading. */
    actions?: Snippet;
    /** A row between the head and the body: a list card's filter chips. */
    toolbar?: Snippet;
    /** Drop the body padding, for lists that draw their own full-width rows. */
    flush?: boolean;
    /**
     * Scroll the body inside whatever height the layout gives the panel,
     * instead of growing the panel to fit. Only meaningful when something
     * outside has bounded that height; in normal flow it changes nothing.
     */
    scroll?: boolean;
    /**
     * Let the panel itself shrink inside a bounded parent, so a column of
     * panels divides the height between them rather than overflowing it.
     */
    fill?: boolean;
    class?: string;
    children: Snippet;
  }

  let {
    title,
    eyebrow,
    icon: Icon,
    description,
    actions,
    toolbar,
    flush = false,
    scroll = false,
    fill = false,
    class: className = '',
    children,
  }: Props = $props();

  const headingId = $props.id();
</script>

<section class="panel {className}" class:fill aria-labelledby={headingId}>
  <header>
    {#if Icon}<span class="tile" aria-hidden="true"><Icon size={18} /></span>{/if}
    <div class="titles">
      {#if eyebrow}<p class="eyebrow">{eyebrow}</p>{/if}
      <h2 id={headingId}>{title}</h2>
      {#if description}<p class="description">{description}</p>{/if}
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </header>
  {#if toolbar}<div class="toolbar">{@render toolbar()}</div>{/if}
  <div class="body" class:flush class:scroll>{@render children()}</div>
</section>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-xl);
    background: var(--z-surface);
  }
  .panel.fill {
    /* A flex child will not shrink below its content unless told it may. */
    min-height: 0;
  }
  header {
    display: flex;
    /* Wraps rather than squeezing: a panel whose actions carry a switch as
       well as a count has more than a phone's width of header. */
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-4);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .titles {
    /* The titles keep a readable width; actions that would squeeze them
       below it wrap under the heading instead, since the header wraps. */
    flex: 1 1 14rem;
    min-width: 0;
  }
  .tile {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    align-self: flex-start;
    width: var(--z-space-10);
    height: var(--z-space-10);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
    color: var(--z-text);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .eyebrow {
    margin: 0 0 var(--z-space-1);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
    color: var(--z-text-muted);
  }
  .toolbar {
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .titles .description {
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    flex: none;
    max-width: 100%;
  }
  /* On a phone the actions wrap too: a panel whose actions are a range and
     a select has more than a phone's width of them, and a row that cannot
     wrap widens the page instead. Only there -- a select is as wide as its
     container, so a row allowed to wrap on a desktop would put it on a line
     of its own and make every such panel a line taller. */
  @media (max-width: 768px) {
    .actions {
      flex-wrap: wrap;
    }
  }
  .body {
    flex: 1;
    min-width: 0;
    padding: var(--z-space-5);
  }
  .panel.fill .body {
    min-height: 0;
  }
  .body.flush {
    padding: 0;
  }
  .body.scroll {
    /* A flex child will not shrink below its content unless told it may. */
    min-height: 0;
    overflow-y: auto;
    /* Rows scrolled to the bottom edge stay inside the panel's corners. */
    border-bottom-left-radius: var(--z-radius-xl);
    border-bottom-right-radius: var(--z-radius-xl);
  }
</style>
