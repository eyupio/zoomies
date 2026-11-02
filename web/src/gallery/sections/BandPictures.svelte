<!--
  The pictures a band draws beside its sentence, in the states a healthy
  fleet never shows: a host no heartbeat has come from, a host holding more
  runners than its slots, a pool at its ceiling, a machine that needs review,
  a connection low on quota and one never read, a repository with an error
  open, a range with no work, and every list cut short with "and n more".

  Only the pictures that take their figures as props are here. The punch
  card, the queue trend and the in-flight rows fetch their own series, and
  their loading and failed states are reviewed on the real pages; what a
  square or a bar looks like in every kind is settled here once.
-->
<script lang="ts">
  import type {
    Host,
    Installation,
    KennelOverview,
    Machine,
    Pool,
    Provider,
    Runner,
    Stats,
    UsageRow,
  } from '$lib/api/types';
  import BandMachines from '$lib/bands/BandMachines.svelte';
  import BandPoolBars from '$lib/bands/BandPoolBars.svelte';
  import BandQuota from '$lib/bands/BandQuota.svelte';
  import BandRepositories from '$lib/bands/BandRepositories.svelte';
  import BandSlotMap from '$lib/bands/BandSlotMap.svelte';
  import BandUsageColumns from '$lib/bands/BandUsageColumns.svelte';
  import { MAX_ROWS } from '$lib/bands/slot-map';
  import Panel from '$lib/components/Panel.svelte';
  import BrandStage from '$lib/public/BrandStage.svelte';

  /*
    Fixtures built to the generated types, with no casts, so a shape that moves
    in the API fails the check here as it would in a unit test. Each is one
    state the fleet rarely reaches.
  */
  const host = (id: string, slots: number, extra: Partial<Host> = {}): Host => ({
    id,
    name: id,
    slots,
    capacity: slots,
    healthy: true,
    ...extra,
  });
  const runner = (host_id: string, state: Runner['state'], n: number): Runner => ({
    id: `${host_id}-${state}-${n}`,
    host_id,
    state,
  });

  const HOSTS: Host[] = [
    host('builder-1', 4),
    host('builder-2', 4, { cordoned: true }),
    // Throttled under running work: more runners than slots, every one shown.
    host('builder-3', 2),
    // No heartbeat: drawn dashed whatever it holds.
    host('builder-4', 3, { healthy: false }),
    host('a-very-long-hostname-that-the-row-cuts-short.internal.example', 2),
    ...Array.from({ length: MAX_ROWS }, (_, i) => host(`spare-${i}`, 1)),
  ];
  const RUNNERS: Runner[] = [
    runner('builder-1', 'busy', 1),
    runner('builder-1', 'busy', 2),
    runner('builder-1', 'idle', 3),
    runner('builder-1', 'provisioning', 4),
    runner('builder-2', 'draining', 1),
    runner('builder-3', 'busy', 1),
    runner('builder-3', 'busy', 2),
    runner('builder-3', 'busy', 3),
    runner('builder-4', 'busy', 1),
  ];

  const pool = (id: string, extra: Partial<Pool> = {}): Pool => ({
    id,
    name: id,
    enabled: true,
    min_runners: 1,
    max_runners: 8,
    priority: 0,
    ...extra,
  });
  const POOLS: Pool[] = [
    pool('linux-x64'),
    pool('linux-arm64', { max_runners: 4 }),
    pool('gpu', { max_runners: 2, min_runners: 0 }),
    pool('windows', { enabled: false }),
    ...Array.from({ length: 5 }, (_, i) => pool(`extra-${i}`, { max_runners: 2 })),
  ];
  const STATS: Stats = {
    pools: [
      { pool_id: 'linux-x64', live: 8, busy: 7, max: 8, queued: 5 },
      { pool_id: 'linux-arm64', live: 1, busy: 0, max: 4, queued: 0 },
      { pool_id: 'gpu', live: 2, busy: 2, max: 2, queued: 0 },
    ],
  };

  const provider = (id: string, extra: Partial<Provider> = {}): Provider => ({
    id,
    name: id,
    kind: 'proxmox',
    enabled: true,
    paused: false,
    credentials_configured: true,
    machines: {},
    owned: 0,
    max_machines: 4,
    ...extra,
  });
  const machine = (provider_id: string, state: Machine['state'], n: number): Machine => ({
    id: `${provider_id}-${n}`,
    provider_id,
    name: `m${n}`,
    state,
    labels: {},
    safe_to_delete: state !== 'ready',
    timeline: [],
    created_at: '2026-11-01T08:00:00Z',
    updated_at: '2026-11-01T08:00:00Z',
  });
  const PROVIDERS: Provider[] = [
    provider('pve-lab'),
    provider('pve-dc', { paused: true, max_machines: 20 }),
    provider('pve-old', { enabled: false, max_machines: 1 }),
  ];
  const MACHINES: Machine[] = [
    machine('pve-lab', 'ready', 1),
    machine('pve-lab', 'bootstrapping', 2),
    machine('pve-lab', 'failed', 3),
    machine('pve-dc', 'ready', 1),
    machine('pve-dc', 'ready', 2),
    machine('pve-dc', 'draining', 3),
    machine('pve-old', 'quarantined', 1),
  ];

  const installation = (id: string, extra: Partial<Installation> = {}): Installation => ({
    id,
    target: id,
    target_type: 'org',
    healthy: true,
    ...extra,
  });
  const INSTALLATIONS: Installation[] = [
    installation('acme'),
    installation('acme-labs'),
    installation('acme-legacy', { healthy: false }),
  ];
  const RATES = {
    acme: {
      limit: 5000,
      remaining: 4310,
      reset_at: new Date(Date.now() + 23 * 60_000).toISOString(),
    },
    'acme-labs': {
      limit: 5000,
      remaining: 120,
      reset_at: new Date(Date.now() + 9 * 60_000).toISOString(),
    },
  };

  const attention = (
    name: string,
    error: number,
    warning: number,
  ): KennelOverview['attention'][number] => ({
    id: name,
    name,
    visibility: 'private',
    state: 'attention',
    counts: { error, warning, info: 0, waived: 0 },
  });
  const OVERVIEW: KennelOverview = {
    enabled: true,
    scope: 'served',
    repositories: 60,
    not_tracked: 0,
    states: { pending: 4, partial: 6, attention: 5, best_in_show: 45 },
    counts: { error: 3, warning: 4, info: 0, waived: 0 },
    score: null,
    checks: [],
    attention: [
      attention('acme/api', 2, 3),
      attention('acme/site', 0, 1),
      attention('acme/widgets', 1, 0),
    ],
    coverage: [],
    unavailable: [],
    oldest_evaluation: null,
    disabled_checks: [],
  };

  const bucket = (hour: number, seconds: number, finished: number) => ({
    from: new Date(Date.UTC(2026, 10, 1, hour)).toISOString(),
    queued: finished,
    started: finished,
    succeeded: finished,
    failed: 0,
    cancelled: 0,
    unknown: 0,
    execution_seconds: seconds,
    allocated_seconds: 0,
    capacity_samples: 0,
    capacity_reached: 0,
  });
  const usageRow = (history: ReturnType<typeof bucket>[]): UsageRow => ({
    key: 'pool',
    jobs: 0,
    jobs_started: 0,
    jobs_completed: 0,
    job_execution_seconds: 0,
    allocated_runner_seconds: null,
    average_queue_wait_seconds: null,
    peak_concurrency: 0,
    history,
  });
  const USAGE: UsageRow[] = [
    usageRow(
      Array.from({ length: 24 }, (_, h) =>
        bucket(h, h > 8 && h < 19 ? (h - 8) * 900 : 0, h > 8 && h < 19 ? h - 8 : 0),
      ),
    ),
  ];
  const QUIET: UsageRow[] = [usageRow(Array.from({ length: 24 }, (_, h) => bucket(h, 0, 0)))];
</script>

<Panel
  title="Band pictures"
  description="What a band draws beside its sentence, in the states a healthy fleet never shows. Each stands on the stage, as it does on its page."
>
  <div class="block">
    <h3>Slot map, by occupancy (Hosts)</h3>
    <BrandStage
      wide
      art={false}
      headline="12 hosts are connected."
      lede="Every host has checked in."
    >
      {#snippet aside()}<BandSlotMap hosts={HOSTS} runners={RUNNERS} detail="occupancy" />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Slot map, by runner state (Runners)</h3>
    <BrandStage
      wide
      art={false}
      headline="9 runners are live."
      lede="2 are starting up and 1 is draining."
    >
      {#snippet aside()}<BandSlotMap
          hosts={HOSTS}
          runners={RUNNERS}
          detail="state"
          title="Runners by host"
        />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Pool bars: one at its ceiling, a disabled pool left out, more than fit</h3>
    <BrandStage wide art={false} headline="8 pools are enabled." lede="1 is at its ceiling.">
      {#snippet aside()}<BandPoolBars pools={POOLS} stats={STATS} />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Machines: one needing review, a paused provider, a ceiling past the squares drawn</h3>
    <BrandStage wide art={false} headline="7 machines are owned." lede="1 needs review.">
      {#snippet aside()}<BandMachines providers={PROVIDERS} machines={MACHINES} />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Quota: one low, one not yet read, one not answering</h3>
    <BrandStage wide art={false} headline="3 connections to GitHub." lede="1 is not answering.">
      {#snippet aside()}<BandQuota installations={INSTALLATIONS} rates={RATES} />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Repositories: errors, warnings, clean, not yet evaluated, and more than fit</h3>
    <BrandStage wide art={false} headline="5 repositories need attention." lede="45 are clean.">
      {#snippet aside()}<BandRepositories overview={OVERVIEW} to="/kennel/repositories" />{/snippet}
    </BrandStage>
  </div>

  <div class="block">
    <h3>Usage columns: a working day, and a range with no work</h3>
    <div class="stages">
      <BrandStage wide art={false} headline="60 jobs queued today." lede="12.4 execution hours.">
        {#snippet aside()}<BandUsageColumns rows={USAGE} />{/snippet}
      </BrandStage>
      <BrandStage wide art={false} headline="No jobs queued today." lede="Nothing ran.">
        {#snippet aside()}<BandUsageColumns rows={QUIET} />{/snippet}
      </BrandStage>
    </div>
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
  .stages {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr));
    gap: var(--z-space-4);
  }
</style>
