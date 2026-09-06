<!--
  A titled section: a heading, an optional line of context, an optional action
  opposite it, and a body.

  The Overview is five of these and a runner's page is another five, and for a
  while they were two components with the same markup and diverging CSS -- one
  had learnt to wrap its header, the other to let its body fill a bounded
  height, and neither knew what the other had learnt. Keeping the shape of a
  section in one place is what stops a dashboard of five different card
  designs.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    /** One line under the heading, when the heading cannot say it all. */
    description?: string;
    /** A count, a badge, or a link out. Sits opposite the heading. */
    actions?: Snippet;
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
    description,
    actions,
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
    <div class="titles">
      <h2 id={headingId}>{title}</h2>
      {#if description}<p>{description}</p>{/if}
    </div>
    {#if actions}<div class="actions">{@render actions()}</div>{/if}
  </header>
  <div class="body" class:flush class:scroll>{@render children()}</div>
</section>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
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
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .titles p {
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
    border-bottom-left-radius: var(--z-radius-md);
    border-bottom-right-radius: var(--z-radius-md);
  }
</style>
