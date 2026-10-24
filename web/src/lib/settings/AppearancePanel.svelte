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
  import { STATUS_STYLE_OPTIONS, type StatusStyle } from '$lib/state/status-style';
  import { theme, THEME_OPTIONS } from '$lib/state/theme.svelte';
  import type { ThemeChoice } from '$lib/state/theme.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import Select from '$lib/components/Select.svelte';
  import { LOGO_TREATMENT_OPTIONS, type LogoTreatment } from '$lib/state/logo-treatment';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import { BAND_PAGES, BAND_SIZES, type BandSize } from '$lib/bands/pages';
  import { BAND_LOGO_SIZE_OPTIONS, type BandLogoSize } from '$lib/bands/band-logo';
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

  const seconds = Math.round(CLOCK_INTERVAL_MS / 1000);
</script>

<PageHeader
  title="Appearance"
  subtitle="Visual style, theme, logo style and navigation stay in this browser. Table widths and column order follow your signed-in account."
/>

<div class="settings">
  <VisualStylePicker />
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

  <div class="setting logo-setting">
    <div class="text">
      <label class="label" for="logo-treatment">Logo style</label>
      <p class="description" id="logo-treatment-description">
        Orbital haze is the default. This choice applies to the main Zoomies logo, including sign-in
        on a phone, and stays in this browser after you sign out. Paw icons keep their original
        appearance. The black logo artwork is the same in every style.
      </p>
      <div class="logo-select">
        <Select
          id="logo-treatment"
          options={LOGO_TREATMENT_OPTIONS}
          value={prefs.logoTreatment}
          describedBy="logo-treatment-description"
          onchange={(value) => (prefs.logoTreatment = value as LogoTreatment)}
        />
      </div>
      <p class="description" aria-live="polite">{logoDescription}</p>
    </div>
    <div class="logo-preview" aria-label="Logo style preview">
      <Logo variant="lockup" size={71} label="Zoomies logo preview" />
    </div>
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

  <div class="setting bands-setting">
    <div class="text">
      <p class="label" id="bands-label">Page bands</p>
      <p class="description">
        The dark band at the top of a page that says its answer in a sentence, with the page's key
        figures in a strip along its lower edge. Four sizes, from Slim (the shortest) through
        Compact and Standard to Expanded (the tallest); the same four sizes are on the band itself.
        Switch off the ones you do not want, and their figures stay on the page in a plain panel. A
        phone never shows the bands, and the choices stay in this browser.
      </p>
      <div class="bands" role="group" aria-labelledby="bands-label">
        {#each BAND_PAGES as page (page.key)}
          <div class="band-row">
            <Switch
              label={page.label}
              size="sm"
              checked={prefs.bandShown(page.key)}
              onchange={(on) => prefs.setBandShown(page.key, on)}
            />
            <Segmented
              options={BAND_SIZES.map((size) => ({ value: size.key, label: size.label }))}
              value={prefs.bandSize(page.key)}
              label={`${page.label} band size`}
              disabled={!prefs.bandShown(page.key)}
              onchange={(value) => prefs.setBandSize(page.key, value as BandSize)}
            />
          </div>
        {/each}
      </div>
    </div>
  </div>

  <div class="setting">
    <div class="text">
      <p class="label">Logo in the band</p>
      <p class="description">
        The dog beside a band's headline, and how large it is drawn. Auto follows the band's size,
        from Slim to Expanded; the three named sizes pin it whichever the cut. Hiding it gives the
        sentence the whole band. The backdrop follows the Logo style above.
      </p>
    </div>
    <div class="band-logo-controls">
      <Switch
        label="Show the logo in the band"
        hideLabel
        checked={prefs.bandLogo}
        onchange={(on) => (prefs.bandLogo = on)}
      />
      <Segmented
        options={BAND_LOGO_SIZE_OPTIONS}
        value={prefs.bandLogoSize}
        label="Logo size in the band"
        disabled={!prefs.bandLogo}
        onchange={(value) => (prefs.bandLogoSize = value as BandLogoSize)}
      />
    </div>
  </div>

  <div class="setting">
    <div class="text">
      <p class="label">Zoomies vocabulary</p>
      <p class="description">
        Off, the default, uses plain status names and icons. Cute keeps the original animated dog.
        Standard uses the black-and-white cocker spaniel. Both dog styles use Zoomies words on
        runners, workflows and the queue.
      </p>
    </div>
    <Segmented
      options={STATUS_STYLE_OPTIONS}
      value={prefs.statusStyle}
      label="Zoomies vocabulary"
      onchange={(value) => (prefs.statusStyle = value as StatusStyle)}
    />
  </div>

  <div class="setting">
    <div class="text">
      <p class="label">Tables on a phone</p>
      <p class="description">
        Rows keeps the table a desktop shows, scrolling sideways to the columns that do not fit.
        Cards give every value a line of its own. With no saved choice, Mission Control grids start
        as Cards and Classic as Rows. Each grid carries the same choice above its rows, and a grid
        told there keeps it whatever this says.
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
</div>

<p class="note">
  Relative times refresh every {seconds} seconds and carry the exact timestamp in their tooltip. Animation
  follows the operating system's reduced-motion setting. Classic uses compact type; Mission Control gives
  it more room. How many rows a page shows is set at the foot of each grid and remembered per table. Drag
  a column heading to move it, or its right edge to resize it; both controls also work with arrow keys.
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
  /* This row has no control beside its text, so its one child is the text, not the control
     the rule above is for: it keeps its width and does not size itself to its contents. */
  .setting.bands-setting > .text {
    flex: 1 1 20rem;
    margin-top: 0;
  }
  .bands {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(19rem, 100%), 1fr));
    min-width: 0;
    gap: var(--z-space-3) var(--z-space-5);
    margin-top: var(--z-space-4);
  }
  .band-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-4);
    min-width: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .band-logo-controls {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--z-space-3);
    flex: none;
  }
  .logo-setting {
    align-items: center;
  }
  .logo-select {
    max-width: var(--z-width-dialog-sm);
    margin-block: var(--z-space-4);
  }
  .logo-preview {
    display: grid;
    place-items: center;
    padding: var(--z-space-2);
    overflow: hidden;
    border-radius: var(--z-radius-lg);
    background: var(--z-panel-bg);
  }
  @media (max-width: 768px) {
    .logo-preview {
      align-self: center;
      padding: var(--z-space-2);
      max-width: 100%;
    }
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
