<!--
  The material half of the theme: every surface, border, text tone and accent
  step, plus the status tones, in one place. The values come from the tokens;
  nothing here sets a colour.

  This is the page to open when a panel "looks off" in one style: the problem
  is either a token whose light and dark values disagree, or a component that
  quietly wrote its own colour instead of reading one, and comparing the
  swatches beside the component above says which.
-->
<script lang="ts">
  import Badge from '$lib/components/Badge.svelte';
  import MetricTile from '$lib/components/MetricTile.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Facts from '$lib/components/Facts.svelte';
  import Terminal from '$lib/components/Terminal.svelte';
  import { runnerStatus } from '$lib/status';
  import UtilisationBar from '$lib/components/UtilisationBar.svelte';
  import { toneTokens, type StatusTone } from '$lib/status';

  const surfaces = [
    { token: '--z-bg', note: 'Page ground' },
    { token: '--z-surface', note: 'Panels' },
    { token: '--z-surface-raised', note: 'Raised above a panel' },
    { token: '--z-surface-sunken', note: 'Code and bounded detail' },
    { token: '--z-surface-hover', note: 'Hovered rows and cells' },
  ];
  const edges = [
    { token: '--z-border', note: 'Quiet grouping' },
    { token: '--z-border-strong', note: 'Control edges' },
  ];
  const inks = [
    { token: '--z-text', note: 'Primary reading' },
    { token: '--z-text-muted', note: 'Metadata and help' },
    { token: '--z-text-subtle', note: 'Timestamps and placeholders' },
  ];
  const accents = [
    { token: '--z-accent', note: 'Interactive accent' },
    { token: '--z-accent-hover', note: 'Hover' },
    { token: '--z-accent-active', note: 'Press' },
    { token: '--z-accent-subtle', note: 'Subtle background' },
    { token: '--z-accent-border', note: 'Subtle border' },
    { token: '--z-accent-contrast', note: 'Text on a filled accent' },
  ];
  const tones: StatusTone[] = ['idle', 'busy', 'pending', 'draining', 'danger', 'neutral'];
</script>

<Panel
  title="Surfaces and text"
  description="Every neutral and interaction value, read from the active tokens."
>
  <div class="swatches" data-gallery="surfaces">
    {#each surfaces as surface (surface.token)}
      <div class="swatch">
        <span class="chip" style="background: var({surface.token})"></span>
        <code>{surface.token}</code>
        <span class="note">{surface.note}</span>
      </div>
    {/each}
    {#each edges as edge (edge.token)}
      <div class="swatch">
        <span class="chip" style="background: var({edge.token})"></span>
        <code>{edge.token}</code>
        <span class="note">{edge.note}</span>
      </div>
    {/each}
    {#each inks as ink (ink.token)}
      <div class="swatch">
        <span class="chip" style="color: var({ink.token})">Aa</span>
        <code>{ink.token}</code>
        <span class="note">{ink.note}</span>
      </div>
    {/each}
    {#each accents as accent (accent.token)}
      <div class="swatch">
        <span class="chip" style="background: var({accent.token})"></span>
        <code>{accent.token}</code>
        <span class="note">{accent.note}</span>
      </div>
    {/each}
  </div>
</Panel>

<Panel
  title="Status tones"
  description="Each tone's foreground, subtle fill and border, in the fixed meaning it carries everywhere."
>
  <div class="swatches" data-gallery="tones">
    {#each tones as tone (tone)}
      {@const tokens = toneTokens(tone)}
      <div class="swatch">
        <span
          class="chip"
          style="background: {tokens.subtle}; border-color: {tokens.border}; color: {tokens.colour}"
          >{tone}</span
        >
        <code>--z-{tone}</code>
        <div class="row">
          <Badge {tone} label={tone} dot={false} />
          <Badge {tone} label={tone} />
        </div>
      </div>
    {/each}
  </div>
</Panel>

<Panel
  title="Terminal and facts"
  description="A command the operator runs, in the window they will run it in, on the dark stage."
>
  <div class="stage-parts">
    <Terminal
      title="sam@lab-vm-2"
      command={[
        'sudo sh join.sh --controller https://controller.example --token 0123456789012345678901234567890123456789012345678901234567890123456789',
        'zoomies agent join https://controller.example --token 0123456789',
      ]}
      copyLabel="Copy the command"
    >
      <p>The token in it is shown once, only its hash is stored, and works once.</p>
    </Terminal>
    <Terminal size="sm" title="What a workflow writes" command={'runs-on: zoomies-linux-x64'} />
    <Facts
      label="Version selected for this host"
      items={[
        { term: 'Controller build', value: 'v1.4.2 (3480089)', mono: true },
        { term: 'Host install channel', value: ':stable', mono: true },
        {
          term: 'Address',
          value:
            'controller0123456789012345678901234567890123456789012345678901234567890123456789.example',
          mono: true,
        },
        { term: 'State', value: 'Joined', status: runnerStatus('idle') },
      ]}
    />
  </div>
</Panel>

<Panel title="Type and density" description="The sizes the plan names, in the style's own tokens.">
  <div class="type">
    <p class="page-heading">Overview, 28px desktop and 24px on a phone</p>
    <p class="panel-heading">A panel heading, 16px sentence case</p>
    <p class="body">
      Body and controls, 14px in Classic and 15px in Mission Control, at a line height of about 1.5.
    </p>
    <p class="identifier">run_demo00 · pool_demolinux · mach_demo02</p>
    <p class="meta">Nonessential metadata at 12px, never shrinking a required action label.</p>
  </div>
  <div class="tiles">
    <MetricTile
      label="Running now"
      value={4}
      unit="jobs"
      tone="busy"
      hint="Started in the last five minutes"
    />
    <MetricTile label="Queued" value={1} unit="job" tone="pending" />
    <MetricTile
      label="Queue wait"
      value="14"
      unit="s"
      delta={-6}
      goodWhen="down"
      sparkline={[22, 19, 20, 17, 15, 14]}
    />
    <MetricTile label="Median run" value="1" unit="min 42 s" loading />
  </div>
  <div class="bars">
    <UtilisationBar busy={3} live={5} min={1} max={8} label="Runner utilisation" />
    <UtilisationBar busy={4} live={4} max={8} tone="capacity" label="Host slots" />
    <UtilisationBar busy={8} live={8} max={8} tone="capacity" label="At ceiling" />
  </div>
</Panel>

<style>
  .stage-parts {
    display: grid;
    gap: var(--z-space-4);
  }
  .swatches {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(11rem, 1fr));
    gap: var(--z-space-4);
  }
  .swatch {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .chip {
    display: flex;
    align-items: center;
    justify-content: center;
    height: var(--z-space-10);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-sm);
    font-weight: var(--z-weight-semibold);
  }
  code {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .note {
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .type {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    max-width: 80ch;
  }
  .type p {
    margin: 0;
  }
  .page-heading {
    font-size: var(--z-text-2xl);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .panel-heading {
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .body {
    font-size: var(--z-text-base);
    color: var(--z-text);
  }
  .identifier {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .meta {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
    gap: var(--z-space-4);
    margin-top: var(--z-space-5);
  }
  .bars {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    margin-top: var(--z-space-5);
    max-width: 40rem;
  }
</style>
