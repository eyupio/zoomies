<!--
  Appearance.

  Theme, navigation and phone layout belong to this browser, not the fleet.
  Column widths and order are the exception: a signed-in account carries them
  between browsers, with the local copy as the immediate fallback.

  Three choices, each a row: what it is and what it does on the left, the
  control on the right. The things that used to be explained at length here
  and could not be changed -- how often times refresh, that motion follows the
  operating system -- are one line at the foot, because a settings page that
  is mostly prose about settings it does not have is a page nobody reads.
-->
<script lang="ts">
  import { CLOCK_INTERVAL_MS } from '$lib/format';
  import { prefs } from '$lib/state/prefs.svelte';
  import type { GridView } from '$lib/state/prefs.svelte';
  import { theme, THEME_OPTIONS } from '$lib/state/theme.svelte';
  import type { ThemeChoice } from '$lib/state/theme.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Switch from '$lib/components/Switch.svelte';

  /*
    What a grid does with a row on a phone. Both layouts exist because they
    answer different questions: cards are how ten columns are read one-handed,
    rows are how a fleet is scanned for the one that is different. This is only
    the default -- each grid's own toggle, above its rows, overrides it there.
  */
  const GRID_VIEWS = [
    { value: 'rows', label: 'Rows', name: 'One line per row, scrolling sideways' },
    { value: 'cards', label: 'Cards', name: 'Each row is a card with a line per value' },
  ];

  const seconds = Math.round(CLOCK_INTERVAL_MS / 1000);
</script>

<PageHeader
  title="Appearance"
  subtitle="Theme and navigation stay in this browser. Table widths and column order follow your signed-in account."
/>

<div class="settings">
  <div class="setting">
    <div class="text">
      <p class="label" id="theme-label">Theme</p>
      <p class="description">
        Follow the operating system, or hold one palette whatever it says. Showing the {theme.resolved}
        palette now; both are measured for WCAG AA, so nothing becomes harder to read either way.
      </p>
    </div>
    <Segmented
      options={THEME_OPTIONS}
      value={theme.choice}
      label="Theme"
      onchange={(value) => theme.set(value as ThemeChoice)}
    />
  </div>

  <div class="setting">
    <div class="text">
      <p class="label">Collapse the navigation</p>
      <p class="description">
        Icons only, which gives a wide grid more room. The same thing the toggle at the foot of the
        sidebar does.
      </p>
    </div>
    <Switch
      label="Collapse the navigation"
      hideLabel
      checked={prefs.navCollapsed}
      onchange={(on) => (prefs.navCollapsed = on)}
    />
  </div>

  <div class="setting">
    <div class="text">
      <p class="label">Tables on a phone</p>
      <p class="description">
        Rows keeps the table a desktop shows, scrolling sideways to the columns that do not fit.
        Cards give every value a line of its own. Each grid carries the same choice above its rows,
        and a grid told there keeps it whatever this says.
      </p>
    </div>
    <Segmented
      options={GRID_VIEWS}
      value={prefs.gridView}
      label="Tables on a phone"
      onchange={(value) => (prefs.gridView = value as GridView)}
    />
  </div>
</div>

<p class="note">
  Relative times refresh every {seconds} seconds and carry the exact timestamp in their tooltip. Animation
  follows the operating system's reduced-motion setting. There is one density, tuned for a dense grid
  at 13px; how many rows a page shows is set at the foot of each grid and remembered per table. Drag a
  column heading to move it, or its right edge to resize it; both controls also work with arrow keys.
</p>

<style>
  .settings {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .setting {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-6);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .setting:last-child {
    border-bottom: 0;
  }
  .text {
    flex: 1 1 20rem;
    min-width: 0;
  }
  .label {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text);
  }
  .description {
    margin: var(--z-nudge-2) 0 0;
    max-width: 64ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  /* The control sits on the label's line, not the description's. */
  .setting > :global(:last-child) {
    flex: none;
    margin-top: var(--z-nudge-1);
  }
  .note {
    margin: var(--z-space-4) var(--z-space-1) 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  @media (max-width: 768px) {
    .setting {
      flex-direction: column;
      gap: var(--z-space-3);
    }
    /*
      The basis above is a width for the row layout. Down here the main axis
      is vertical, so the same declaration asks for 20rem of *height*, and the
      switch ends up a screenful below the sentence it belongs to. In a column
      the text is as tall as the text.
    */
    .text {
      flex: initial;
    }
    /* Nothing to sit on a line with, so nothing to nudge against. */
    .setting > :global(:last-child) {
      margin-top: 0;
    }
  }
</style>
