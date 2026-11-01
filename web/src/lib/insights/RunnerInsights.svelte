<script lang="ts">
  import { fleet } from '$lib/state/fleet.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import LifecycleFlow from './LifecycleFlow.svelte';
  import ProvisioningPulse from './ProvisioningPulse.svelte';
  import StateBreakdown from './StateBreakdown.svelte';
  import { poolSignals, finite, queueTone } from './signals';
  import { runnerStatus } from '$lib/status';
  /**
   * The context above a runner list: the provisioning strip (unless the page carries it
   * elsewhere, with `pulse` off), and the lifecycle
   * explanation behind a disclosure. The grid answers "what are my runners doing", and
   * the Overview and Queue pages carry the figures the old tiles repeated.
   */
  let {
    poolId = '',
    pulse = true,
    folded = false,
  }: { poolId?: string; pulse?: boolean; folded?: boolean } = $props();
  const signal = $derived(poolSignals(fleet.pools, fleet.stats).find((p) => p.pool.id === poolId));
  const r = $derived(fleet.stats?.runners);
  // The one figure an operator looks for first: how much work is waiting for a runner.
  const queued = $derived(poolId ? signal?.queued : finite(fleet.stats?.fleet?.queued_jobs));
  const queuedTone = $derived(queueTone(queued, session.meta?.queue_warning_threshold));
  const busy = $derived(poolId ? signal?.busy : finite(r?.busy));
  const idle = $derived(poolId ? signal?.idle : finite(r?.idle));
  const scope = $derived(poolId ? (signal?.pool.name ?? 'Selected pool') : 'Fleet');
  const suffix = $derived(poolId ? `&pool_id=${encodeURIComponent(poolId)}` : '');
  const segments = $derived(
    poolId
      ? [
          {
            label: runnerStatus('busy', prefs.quirkyStatus).label,
            value: busy ?? 0,
            tone: 'busy' as const,
            href: `/runners?state=busy${suffix}`,
            hint: runnerStatus('busy', prefs.quirkyStatus).hint,
          },
          {
            label: runnerStatus('idle', prefs.quirkyStatus).label,
            value: idle ?? 0,
            tone: 'idle' as const,
            href: `/runners?state=idle${suffix}`,
            hint: runnerStatus('idle', prefs.quirkyStatus).hint,
          },
          {
            label: 'Other live',
            value: Math.max(0, (signal?.live ?? 0) - (busy ?? 0) - (idle ?? 0)),
            tone: 'pending' as const,
            href: `/runners?pool_id=${encodeURIComponent(poolId)}`,
            hint: 'Starting up, or draining after its last job.',
          },
        ]
      : [
          {
            label: runnerStatus('provisioning', prefs.quirkyStatus).label,
            value: r?.provisioning ?? 0,
            tone: 'pending' as const,
            href: '/runners?state=provisioning',
            hint: runnerStatus('provisioning', prefs.quirkyStatus).hint,
          },
          {
            label: runnerStatus('registering', prefs.quirkyStatus).label,
            value: r?.registering ?? 0,
            tone: 'accent' as const,
            href: '/runners?state=registering',
            hint: runnerStatus('registering', prefs.quirkyStatus).hint,
          },
          {
            label: runnerStatus('idle', prefs.quirkyStatus).label,
            value: r?.idle ?? 0,
            tone: 'idle' as const,
            href: '/runners?state=idle',
            hint: runnerStatus('idle', prefs.quirkyStatus).hint,
          },
          {
            label: runnerStatus('busy', prefs.quirkyStatus).label,
            value: r?.busy ?? 0,
            tone: 'busy' as const,
            href: '/runners?state=busy',
            hint: runnerStatus('busy', prefs.quirkyStatus).hint,
          },
          {
            label: runnerStatus('draining', prefs.quirkyStatus).label,
            value: r?.draining ?? 0,
            tone: 'draining' as const,
            href: '/runners?state=draining',
            hint: runnerStatus('draining', prefs.quirkyStatus).hint,
          },
        ],
  );
</script>

{#snippet scopeRow()}
  <div class="scope">
    <span
      >{scope} context · {fleet.connection === 'live'
        ? 'live snapshot'
        : 'last known snapshot'}</span
    ><a href={`/usage?group_by=pool${poolId ? `&entity=${encodeURIComponent(poolId)}` : ''}`}
      >Explore usage history</a
    >
  </div>
{/snippet}

{#snippet lifecycle()}
  <ChartPanel
    title="Runner lifecycle"
    description={poolId
      ? 'How this pool\u2019s live runners divide up right now. Failed and removed records are excluded from this bar.'
      : 'The states a runner passes through, in the order the controller allows, with how many are at each step right now. Failed and removed records are excluded from the bar.'}
  >
    {#if !poolId && r}<LifecycleFlow runners={r} />{/if}
    <StateBreakdown {segments} label="Live runner states" noun="live runners" />
  </ChartPanel>
  <p class="note">
    Job queue depth is waiting GitHub work, including jobs whose provisioning demand is held; it is
    not a count of runner creation requests. Search, host and state filters do not change this {poolId
      ? 'pool'
      : 'fleet'} context.
  </p>
{/snippet}

{#if folded}
  <!-- The Runners page puts this inside a disclosure of its own, under its
       list, so it is drawn as plain content: no section chrome and no
       disclosure inside a disclosure. -->
  {@render scopeRow()}
  {#if fleet.stats}{@render lifecycle()}{/if}
{:else}
  <section aria-label="Runner operational context" class="context">
    {@render scopeRow()}
    <!-- The Runners list carries these figures in its band instead (and on its own, with
         the band off), so it asks for the context without them. -->
    {#if pulse}
      <ProvisioningPulse
        {poolId}
        queued={queued ?? null}
        {queuedTone}
        queuedHref={`/jobs?state=queued${suffix}`}
      />
    {/if}
    {#if fleet.stats}
      <details class="how">
        <summary>How runners move through their states</summary>
        {@render lifecycle()}
      </details>
    {/if}
  </section>
{/if}

<style>
  .context {
    margin-bottom: var(--z-space-5);
  }
  .scope {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    margin: var(--z-space-3) 0;
    color: var(--z-text-muted);
  }
  a {
    color: var(--z-accent);
  }
  .how {
    margin: var(--z-space-3) 0 0;
  }
  .how summary {
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
    color: var(--z-accent);
    font-size: var(--z-text-sm);
    cursor: pointer;
  }
  .how[open] summary {
    margin-bottom: var(--z-space-3);
  }
  .note {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    margin: var(--z-space-3) 0 0;
    max-width: 110ch;
  }
</style>
