<!--
  The pages before sign in: the stage the dog stands on, and the appearance
  switch in the header. The frame and the split page are whole documents (a
  header, the page, a footer, as tall as the window), so they are reviewed on
  the real pages and by tests/first-run.spec.ts; what is here is what they are
  made of, in the states that break a layout: a headline with nothing in it to
  break on, and the band a page with no form beside it uses.
-->
<script lang="ts">
  import BandStats from '$lib/bands/BandStats.svelte';
  import type { BandStat } from '$lib/bands/stats';
  import Panel from '$lib/components/Panel.svelte';
  import BrandStage from '$lib/public/BrandStage.svelte';
  import ThemeSwitch from '$lib/public/ThemeSwitch.svelte';
  import type { ThemeChoice } from '$lib/state/theme.svelte';

  let choice = $state<ThemeChoice>('dark');
  /*
    The figures a band carries, in the states that break a strip: a link, a filter that
    is on, a warning, a figure whose answer has not arrived, and one that is a live age.
  */
  const FIGURES: BandStat[] = [
    { label: 'Jobs queued', value: '7', href: '#', tone: 'warning', hint: 'Open them.' },
    { label: 'Ready', value: '1,204', onselect: () => {}, active: true },
    { label: 'Paused', onselect: () => {} },
    { label: 'Oldest demand', since: new Date(Date.now() - 250_000).toISOString() },
  ];
  /** Unbroken, and longer than any column: a hostname, as a stage may be given one. */
  const UNBROKEN =
    'controller0123456789012345678901234567890123456789012345678901234567890123456789.example';
</script>

<Panel
  title="Public pages"
  description="The stage and the appearance switch the pages before sign in are made of."
>
  <div class="block">
    <h3>Appearance switch</h3>
    <div class="row">
      <ThemeSwitch value={choice} onchange={(next) => (choice = next)} />
      <span class="muted"
        >Chosen: {choice}. The page leaves the value out and uses the stored one.</span
      >
    </div>
  </div>

  <div class="block">
    <h3>Stage</h3>
    <div class="stages">
      <BrandStage
        eyebrow="Your infrastructure. Your control."
        headline={['A fresh runner.', 'For every job.']}
        lede="GitHub Actions runners on machines you own. Managed from one place."
      />
      <BrandStage
        eyebrow="First step"
        headline="First, an account."
        accent="Then the fleet."
        lede="Then: connect a GitHub App, add a host, create a pool, and point a workflow at it."
      />
    </div>
  </div>

  <div class="block">
    <h3>Stage, a headline with nothing to break on</h3>
    <div class="stages one">
      <BrandStage eyebrow="Unbroken" headline={UNBROKEN} lede={UNBROKEN} />
    </div>
  </div>

  <div class="block">
    <h3>Stage as a band</h3>
    <BrandStage
      wide
      headline="Fleet status"
      lede="How this fleet is doing right now, in plain words."
    />
  </div>

  <div class="block">
    <h3>Band, dog hidden or sized</h3>
    <div class="stages">
      <BrandStage
        wide
        art={false}
        headline="Fleet status"
        lede="The dog hidden, so the sentence has the whole band."
      />
    </div>
  </div>

  <div class="block">
    <h3>Band figures, in the band</h3>
    <BrandStage
      wide
      eyebrow="Right now"
      headline="12 runners are live."
      lede="Every one is working."
    >
      <BandStats stats={FIGURES} more={{ label: 'Manage queue', href: '#' }} variant="band" />
    </BrandStage>
  </div>

  <div class="block">
    <h3>Band figures, on their own (band off, or a phone)</h3>
    <BandStats stats={FIGURES} more={{ label: 'Manage queue', href: '#' }} variant="panel" />
  </div>
</Panel>

<style>
  .block + .block {
    margin-top: var(--z-space-6);
  }
  h3 {
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text-muted);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-4);
  }
  .muted {
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .stages {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
    gap: var(--z-space-4);
  }
  .stages.one {
    grid-template-columns: minmax(0, 24rem);
  }
</style>
