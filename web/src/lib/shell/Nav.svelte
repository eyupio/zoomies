<!--
  The persistent navigation: a left sidebar on a desktop, a bar along the
  bottom edge on a phone.

  The order is fixed and matches docs/ui-guidelines.md, because muscle memory is
  the whole point of a persistent nav. On a desktop it collapses to icons and
  remembers that it did; the `g` shortcut letter is shown beside each entry so
  the keyboard route is discoverable rather than folklore.

  A phone gets a different component rather than the same one squeezed. Ten
  icon-only targets across a 412px screen were 40px apart and told apart only by
  a glyph, so the bar carries the four sections a fleet is watched with, each
  under its own word, and a "More" button opens the side menu holding all ten.
  That is also why the collapsed state is a desktop idea only: a bar along the
  bottom has nothing to collapse, and the pref leaking into it was what made the
  bar 56px wide with every entry piled into the corner.
-->
<script lang="ts">
  import { Menu, PanelLeftClose, PanelLeftOpen } from '@lucide/svelte';
  import { router } from '../router';
  import { prefs } from '../state/prefs.svelte';
  import { viewport } from '../state/viewport.svelte';
  import IconButton from '../components/IconButton.svelte';
  import Logo from '../components/Logo.svelte';
  import { SECTIONS, isCurrentSection } from './sections';

  interface Props {
    /** Whether the phone's side menu is open, for the More button's state. */
    menuOpen?: boolean;
    onmore?: () => void;
  }

  let { menuOpen = false, onmore }: Props = $props();

  const phone = $derived(viewport.phone);
  const collapsed = $derived(prefs.navCollapsed && !phone);
  const items = $derived(phone ? SECTIONS.filter((item) => item.primary) : SECTIONS);

  function isCurrent(path: string): boolean {
    return isCurrentSection(path, router.pathname);
  }

  /**
   * Whether the page being looked at is one of the six the bar does not carry.
   * The bar would otherwise show nothing marked at all on those pages, and a
   * navigation that cannot say where you are is not doing its job.
   */
  const inMenu = $derived(phone && !items.some((item) => isCurrent(item.path)));
</script>

<nav class="nav" class:collapsed class:phone aria-label="Sections">
  {#if !phone}
    <div class="brand">
      <a href="/" class="mark" aria-label="Zoomies, go to the overview">
        <Logo variant="mark" size={32} label="" />
        {#if !collapsed}<span class="brand-name">Zoomies</span>{/if}
      </a>
      {#if !collapsed}
        <p class="descriptor">Self-hosted Git runners</p>
      {/if}
    </div>
  {/if}

  <ul>
    {#each items as item (item.path)}
      {@const current = isCurrent(item.path)}
      <li>
        <a
          href={item.path}
          aria-current={current ? 'page' : undefined}
          class:current
          title={collapsed ? item.label : undefined}
        >
          <item.icon size={phone ? 18 : 16} aria-hidden="true" />
          {#if collapsed}
            <span class="sr-only">{item.label}</span>
          {:else}
            <span class="label">{item.label}</span>
            {#if !phone}<kbd aria-hidden="true">g {item.key}</kbd>{/if}
          {/if}
        </a>
      </li>
    {/each}

    {#if phone}
      <li>
        <!--
          A button, not a link: it opens the menu over this page rather than
          going anywhere, and it says so to a screen reader.
        -->
        <button
          type="button"
          class="more"
          class:current={menuOpen || inMenu}
          aria-haspopup="dialog"
          aria-expanded={menuOpen}
          onclick={() => onmore?.()}
        >
          <Menu size={18} aria-hidden="true" />
          <span class="label">More</span>
        </button>
      </li>
    {/if}
  </ul>

  {#if !phone}
    <div class="foot">
      <IconButton
        icon={collapsed ? PanelLeftOpen : PanelLeftClose}
        label={collapsed ? 'Expand the navigation' : 'Collapse the navigation'}
        size="sm"
        onclick={() => prefs.toggleNav()}
      />
    </div>
  {/if}
</nav>

<style>
  .nav {
    position: sticky;
    top: 0;
    display: flex;
    flex-direction: column;
    width: var(--z-nav-width);
    height: 100vh;
    padding: var(--z-space-3);
    border-right: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
    z-index: var(--z-layer-nav);
    transition: width var(--z-motion-base) var(--z-ease);
  }
  .nav.collapsed {
    width: var(--z-nav-width-collapsed);
    padding-inline: var(--z-space-2);
  }
  /*
    The masthead is too small for the detailed dog artwork to read cleanly, so
    it uses the paw/swish shorthand at 32px and sets the product name as
    ordinary interface text. It does not imitate or crop the wordmark.
  */
  .brand {
    padding: var(--z-space-2) var(--z-space-2) var(--z-space-3);
    margin-bottom: var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .collapsed .brand {
    display: flex;
    justify-content: center;
    padding-inline: 0;
  }
  .mark {
    display: inline-flex;
    align-items: center;
    color: var(--z-text);
    text-decoration: none;
    border-radius: var(--z-radius-md);
  }
  .mark:hover {
    /* The mark is fixed artwork; the text carries the hover state. */
    color: var(--z-accent);
  }
  .brand-name {
    margin-left: var(--z-space-3);
    font-size: var(--z-text-xl);
    line-height: 1;
    font-weight: var(--z-weight-semibold);
    letter-spacing: -0.02em;
  }
  /*
    Set in Inter, not taken from the artwork: the descriptor line inside the
    full logo is not separated or cropped for compact UI.
  */
  .descriptor {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-2xs);
    line-height: 1.4;
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
    color: var(--z-text-subtle);
  }
  ul {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  a,
  .more {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    width: 100%;
    height: var(--z-space-8);
    padding: 0 var(--z-space-2);
    border: 0;
    border-radius: var(--z-radius-md);
    background: none;
    color: var(--z-text-muted);
    text-decoration: none;
    font-family: inherit;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  .collapsed a {
    justify-content: center;
    padding: 0;
  }
  a:hover,
  .more:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  a.current,
  .more.current {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  kbd {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    opacity: 0;
    transition: opacity var(--z-motion-fast) var(--z-ease);
  }
  a:hover kbd,
  a:focus-visible kbd {
    opacity: 1;
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    padding-top: var(--z-space-2);
  }
  .collapsed .foot {
    justify-content: center;
  }
  /*
    The phone bar. Every selector here names `.phone` as well, so that a rule
    of the sidebar's that happens to be written against two classes cannot
    outrank the layout it is meant to replace -- a media query adds no
    specificity of its own, and that is exactly how `.nav.collapsed`'s 56px
    used to win here.
  */
  @media (max-width: 768px) {
    .nav.phone {
      position: fixed;
      inset-block: auto 0;
      inset-inline: 0;
      flex-direction: row;
      align-items: center;
      width: 100%;
      height: auto;
      /* The home-indicator gap on an iPhone: index.html asks for
         viewport-fit=cover, so the inset has to be honoured somewhere. */
      padding: var(--z-space-1) var(--z-space-1)
        calc(var(--z-space-1) + env(safe-area-inset-bottom, 0px));
      border-right: 0;
      border-top: var(--z-border-width) solid var(--z-border);
    }
    .phone ul {
      flex-direction: row;
      gap: var(--z-nudge-2);
      width: 100%;
    }
    /*
      Equal shares of the width, and no more: the entries are what an operator
      aims a thumb at, so they are the same size as each other whatever their
      word is, and a long one ellipses rather than stealing room from its
      neighbours.
    */
    .phone li {
      flex: 1 1 0;
      min-width: 0;
    }
    /*
      Centred, both ways. Without this the icon sat against the top edge of the
      highlight pill and the rest of the pill was empty space below it.
    */
    .phone a,
    .phone .more {
      flex-direction: column;
      justify-content: center;
      gap: var(--z-nudge-2);
      height: var(--z-space-12);
      padding: 0 var(--z-nudge-2);
    }
    /*
      The word under the icon, rather than the visually-hidden label the
      collapsed sidebar uses. Five entries fit their names across a phone,
      and a named target is one an operator can hit without learning a glyph.
    */
    .phone .label {
      flex: none;
      max-width: 100%;
      font-size: var(--z-text-2xs);
      line-height: 1;
    }
  }
</style>
