<!--
  Appearance.

  Three sections, by what a choice changes and where it is kept. Look is the
  palette, the logo and the words, and belongs to this browser. Layout is where
  things sit. Page bands are a table, a row for each page, because they are the
  same three questions asked thirteen times; as a grid of cards, each with its
  own copy of the controls, they were two thirds of a page that scrolled for
  four screens. Bands and Overview panels follow a signed-in account between
  browsers, with the local copy as the immediate fallback, and each section
  says which of the two it is under its heading, once, so that no row has to.

  A description is one sentence about what the control does. The page used to
  explain each choice at paragraph length beside it, and a settings page that is
  mostly prose is a page nobody reads.
-->
<script lang="ts">
  import { CLOCK_INTERVAL_MS } from '$lib/format';
  import { prefs } from '$lib/state/prefs.svelte';
  import type { GridView } from '$lib/state/prefs.svelte';
  import { STATUS_STYLE_OPTIONS, type StatusStyle } from '$lib/state/status-style';
  import { theme, THEME_OPTIONS } from '$lib/state/theme.svelte';
  import type { ThemeChoice } from '$lib/state/theme.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import Select from '$lib/components/Select.svelte';
  import { LOGO_TREATMENT_OPTIONS, type LogoTreatment } from '$lib/state/logo-treatment';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import { BAND_PAGES, BAND_PICTURE_PAGES, BAND_SIZES, type BandSize } from '$lib/bands/pages';
  import Button from '$lib/components/Button.svelte';
  import { OVERVIEW_PANELS, isDefaultLayout, setHidden } from '$lib/overview/panels';
  import { ALL_BAND_PAGES } from '$lib/bands/scope';
  import { visualStyle } from '$lib/state/ui-style.svelte';
  import VisualStylePicker from './VisualStylePicker.svelte';

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

  const logoDescription = $derived(
    LOGO_TREATMENT_OPTIONS.find((option) => option.value === prefs.logoTreatment)?.description,
  );

  /**
   * The cut every band shares, or null when they differ. The control above the
   * list says what "every page" would change, and shows no pressed answer when
   * there is not one to show.
   */
  const commonSize = $derived.by(() => {
    const first = prefs.bandSize(BAND_PAGES[0].key);
    return BAND_PAGES.every((page) => prefs.bandSize(page.key) === first) ? first : null;
  });

  const panelLayout = $derived(prefs.panelLayout('overview'));

  const customPicturePages = $derived(
    BAND_PICTURE_PAGES.filter((page) => prefs.bandPictureCustom(page.key)).length,
  );

  const seconds = Math.round(CLOCK_INTERVAL_MS / 1000);
</script>

<PageHeader
  title="Appearance"
  subtitle="How Zoomies looks in this browser, and the page layout your account carries with it."
/>

<section class="section" aria-labelledby="look-heading">
  <header class="section-head">
    <h2 id="look-heading">Look</h2>
    <p>Kept in this browser, including the sign-in page</p>
  </header>
  <div class="settings">
    <VisualStylePicker />
    <div class="setting">
      <div class="text">
        <p class="label" id="theme-label">Theme</p>
        <p class="description">
          Follow the operating system or hold one palette. Showing the {theme.resolved} palette now.
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
        <label class="label" for="logo-treatment">Logo style</label>
        <p class="description" id="logo-treatment-description">
          The backdrop behind the main Zoomies logo. The artwork and the paw icons stay as they are.
        </p>
        <p class="description" aria-live="polite">{logoDescription}</p>
      </div>
      <div class="logo-control">
        <div class="logo-preview" aria-label="Logo style preview">
          <Logo variant="lockup" size={71} label="Zoomies logo preview" />
        </div>
        <Select
          id="logo-treatment"
          options={LOGO_TREATMENT_OPTIONS}
          value={prefs.logoTreatment}
          describedBy="logo-treatment-description"
          onchange={(value) => (prefs.logoTreatment = value as LogoTreatment)}
        />
      </div>
    </div>

    <div class="setting">
      <div class="text">
        <p class="label">Zoomies vocabulary</p>
        <p class="description">
          Off uses plain status names and icons. Cute and Standard use Zoomies words on runners,
          workflows and the queue, with the animated dog or the black-and-white cocker spaniel.
        </p>
      </div>
      <Segmented
        options={STATUS_STYLE_OPTIONS}
        value={prefs.statusStyle}
        label="Zoomies vocabulary"
        onchange={(value) => (prefs.statusStyle = value as StatusStyle)}
      />
    </div>
  </div>
</section>

<section class="section" aria-labelledby="layout-heading">
  <header class="section-head">
    <h2 id="layout-heading">Layout</h2>
    <p>Navigation and tables stay in this browser; Overview panels follow your account</p>
  </header>
  <div class="settings">
    <div class="setting">
      <div class="text">
        <p class="label">Collapse the navigation</p>
        <p class="description">
          Icons only, which gives a wide grid more room. The toggle at the foot of the sidebar does
          the same.
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
          Rows scroll sideways to the columns that do not fit; Cards give every value a line of its
          own. A grid set from its own toggle keeps that choice.
        </p>
      </div>
      <Segmented
        options={GRID_VIEWS}
        value={prefs.gridViewChosen
          ? prefs.gridView
          : visualStyle.choice === 'mission-control'
            ? 'cards'
            : 'rows'}
        label="Tables on a phone"
        onchange={(value) => (prefs.gridView = value as GridView)}
      />
    </div>

    <!--
      The same choice the Overview's Arrange mode offers, from here: which
      panels the page draws. The order is the page's own to change, where the
      cards can be seen moving, so it is only read here.
    -->
    <div class="setting stacked">
      <div class="text">
        <p class="label" id="panels-label">Overview panels</p>
        <p class="description">
          Which panels the Overview draws. Change their order on the Overview itself, with Arrange.
        </p>
      </div>
      <div class="panel-group" role="group" aria-labelledby="panels-label">
        <div class="panels">
          {#each panelLayout.order as id (id)}
            {@const panel = OVERVIEW_PANELS.find((p) => p.id === id)}
            {#if panel}
              <!--
              The switch is named in full for a screen reader, which meets it
              out of this group's context; the eye has the heading, and seven
              lines ending "on the Overview" were six too many.
            -->
              <div class="panel">
                <Switch
                  id="overview-panel-{id}"
                  label="{panel.title} on the Overview"
                  hideLabel
                  size="sm"
                  checked={!panelLayout.hidden.includes(id)}
                  onchange={(on) =>
                    prefs.setPanelLayout('overview', setHidden(panelLayout, id, !on))}
                />
                <label for="overview-panel-{id}">{panel.title}</label>
              </div>
            {/if}
          {/each}
        </div>
        <div>
          <Button
            variant="secondary"
            size="sm"
            disabled={isDefaultLayout(panelLayout)}
            reason={isDefaultLayout(panelLayout) ? 'The layout is the default.' : undefined}
            onclick={() => prefs.resetPanelLayout('overview')}>Reset layout</Button
          >
        </div>
      </div>
    </div>
  </div>
</section>

<section class="section" aria-labelledby="bands-label">
  <header class="section-head">
    <h2 id="bands-label">Page bands</h2>
    <p>
      The band at the top of a page that says its answer in a sentence. Follows your account; a
      phone never shows one
    </p>
  </header>
  <div class="settings bands" role="group" aria-labelledby="bands-label">
    <div class="band-row all-row">
      <div class="band-name">
        <span class="all-label">All pages</span>
        <span class="band-hint">Sets every row below at once</span>
      </div>
      <Segmented
        options={BAND_SIZES.map((size) => ({ value: size.key, label: size.label }))}
        value={commonSize ?? ''}
        label="Every page's band size"
        onchange={(value) => prefs.applyBandSize([...ALL_BAND_PAGES], value as BandSize)}
      />
      <div class="band-picture">
        <Switch
          label="Show the picture in the band"
          hideLabel
          size="sm"
          checked={prefs.bandPicture}
          onchange={(on) => (prefs.bandPicture = on)}
        />
        <span class="band-hint">
          {#if customPicturePages > 0}
            {customPicturePages === 1
              ? 'One page has its own picture setting'
              : `${customPicturePages} pages have their own picture setting`}; this switch puts
            every page back in step
          {:else}
            The picture beside the sentence
          {/if}
        </span>
      </div>
    </div>
    <div class="band-row band-head" aria-hidden="true">
      <span>Page</span>
      <span>Size</span>
      <span>Picture</span>
    </div>
    {#each BAND_PAGES as page (page.key)}
      <div class="band-row">
        <div class="band-name">
          <Switch
            label={page.label}
            size="sm"
            checked={prefs.bandShown(page.key)}
            onchange={(on) => prefs.setBandShown(page.key, on)}
          />
        </div>
        <Segmented
          options={BAND_SIZES.map((size) => ({ value: size.key, label: size.label }))}
          value={prefs.bandSize(page.key)}
          label={`${page.label} band size`}
          disabled={!prefs.bandShown(page.key)}
          onchange={(value) => prefs.setBandSize(page.key, value as BandSize)}
        />
        <div class="band-picture">
          {#if 'picture' in page}
            <Switch
              label={`${page.label} band picture`}
              hideLabel
              size="sm"
              checked={prefs.bandPictureShown(page.key)}
              disabled={!prefs.bandShown(page.key)}
              onchange={(on) => prefs.setBandPictureShown(page.key, on)}
            />
            <span class="band-hint capital">{page.picture}</span>
            {#if prefs.bandPictureCustom(page.key)}
              <button
                type="button"
                class="reset"
                onclick={() => prefs.clearBandPictureOverride(page.key)}
              >
                Use the default
              </button>
            {/if}
          {:else}
            <span class="band-hint">No picture on this page</span>
          {/if}
        </div>
      </div>
    {/each}
  </div>
</section>

<p class="note">
  Relative times refresh every {seconds} seconds and carry the exact timestamp in their tooltip. Animation
  follows the operating system's reduced-motion setting. Rows per page are set at the foot of each grid.
  Drag a column heading to move it, or its right edge to resize it; both also work with arrow keys.
</p>

<style>
  .section + .section {
    margin-top: var(--z-space-8);
  }
  .section-head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-1) var(--z-space-3);
    margin: 0 var(--z-space-1) var(--z-space-3);
  }
  .section-head h2 {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .section-head p {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
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
  .setting.stacked {
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .text {
    flex: 1 1 20rem;
    min-width: 0;
  }
  .stacked .text {
    flex: initial;
  }
  .label {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    font-weight: var(--z-weight-medium);
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
  .setting:not(.stacked) > :global(:last-child) {
    flex: none;
    margin-top: var(--z-nudge-1);
  }
  .logo-control {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
  }
  /*
    A thumbnail, in a box that holds its place. The lockup fits the width it is
    given, and at its own it was the tallest thing in the section: a preview is
    for telling five backdrops apart, and it does that at this size.
  */
  .logo-preview {
    flex: none;
    width: 7rem;
    overflow: hidden;
    border-radius: var(--z-radius-md);
    background: var(--z-panel-bg);
  }
  /* Two columns of names: seven switches in one are a tall, thin list beside
     an empty half of the card. */
  .panel-group {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    width: 100%;
  }
  .panels {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(14rem, 100%), 1fr));
    gap: var(--z-space-2) var(--z-space-6);
    width: 100%;
  }
  .panel {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .panel label {
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
    cursor: pointer;
  }
  /*
    One set of columns for every row, so the switches, the sizes and the
    pictures each sit in a column of their own and a difference between two
    pages is a difference on one line.
  */
  .bands {
    display: grid;
    grid-template-columns: minmax(9rem, 1fr) auto minmax(0, 17rem);
  }
  .band-row {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: subgrid;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-5);
    min-height: var(--z-space-10);
    padding: var(--z-space-1) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .band-row:last-child {
    border-bottom: 0;
  }
  /* The one row that changes all the others. It shows no pressed size while
     the pages disagree, which is the honest reading. */
  .all-row {
    padding-block: var(--z-space-3);
    border-radius: var(--z-radius-md) var(--z-radius-md) 0 0;
    background: var(--z-surface-sunken);
  }
  .band-head {
    min-height: var(--z-space-8);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  .band-name {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .all-label {
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .band-picture {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .band-hint {
    min-width: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .band-hint.capital::first-letter {
    text-transform: uppercase;
  }
  .reset {
    flex: none;
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-xs);
    text-decoration: underline;
    text-underline-offset: var(--z-nudge-2);
    cursor: pointer;
  }
  .reset:hover {
    color: var(--z-accent-hover);
  }
  .reset:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
    border-radius: var(--z-radius-sm);
  }
  .note {
    margin: var(--z-space-4) var(--z-space-1) 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  @media (max-width: 900px) {
    /* No room for three columns: the name and its size share a line, and the
       picture takes the one below, under the size it belongs with. */
    .bands {
      grid-template-columns: minmax(0, 1fr);
    }
    .band-row {
      padding-block: var(--z-space-3);
    }
    .band-head {
      display: none;
    }
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
    .setting:not(.stacked) > :global(:last-child) {
      margin-top: 0;
    }
    .logo-control {
      flex-wrap: wrap;
    }
  }
</style>
