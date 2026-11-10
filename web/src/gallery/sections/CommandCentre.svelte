<!--
  The command centre's deck in the states a healthy fleet rarely shows: a lane
  in each of its states, and a tile for each kind of slot, including a job
  whose name is four hundred characters long and must not widen its cell.

  The lanes are built by the page's own `lanes()` from fixtures, so what is
  reviewed here is what the page would draw from the same fleet.
-->
<script lang="ts">
  import type { Host, Job, Runner } from '$lib/api/types';
  import HostLane from '$lib/command/HostLane.svelte';
  import RunnerTile from '$lib/command/RunnerTile.svelte';
  import { lanes, type Cell } from '$lib/command/lanes';
  import { hostHref, runnerHref } from '$lib/command/links';
  import Panel from '$lib/components/Panel.svelte';

  const minutesAgo = (n: number) => new Date(Date.now() - n * 60_000).toISOString();

  const host = (id: string, slots: number, extra: Partial<Host> = {}): Host => ({
    id,
    name: id,
    slots,
    capacity: slots,
    healthy: true,
    usage_fresh: true,
    usage: { cpu_percent: 38 },
    ...extra,
  });
  const job = (name: string, extra: Partial<Job> = {}): Job => ({
    id: `job-${name.slice(0, 12)}`,
    job_name: name,
    repo: 'eyupio/zoomies',
    head_branch: 'main',
    started_at: minutesAgo(4),
    ...extra,
  });
  const runner = (
    id: string,
    host_id: string,
    state: Runner['state'],
    extra: Partial<Runner> = {},
  ): Runner => ({
    id,
    host_id,
    state,
    pool_name: 'linux-x64',
    created_at: minutesAgo(9),
    allocated_cpus: 2,
    ...extra,
  });

  const LONG = `build ${'and test the whole matrix again '.repeat(12)}`.slice(0, 400);

  const HOSTS: Host[] = [
    host('lane-ok', 4),
    host('lane-full', 2),
    host('lane-throttled', 2, { throttle: { level: 1, reason: 'CPU pressure' } }),
    host('lane-unreachable', 2, { healthy: false, usage_fresh: false }),
    host('lane-cordoned', 2, { cordoned: true }),
    host('lane-wide', 6),
  ];
  const RUNNERS: Runner[] = [
    runner('ok-1', 'lane-ok', 'busy', { current_job: job('test (ubuntu)'), cpu_percent: 140 }),
    runner('ok-2', 'lane-ok', 'idle'),
    runner('full-1', 'lane-full', 'busy', { current_job: job('lint'), cpu_percent: 60 }),
    runner('full-2', 'lane-full', 'busy', { current_job: job('build'), cpu_percent: 190 }),
    runner('thr-1', 'lane-throttled', 'busy', { current_job: job('e2e'), cpu_percent: 200 }),
    runner('unr-1', 'lane-unreachable', 'busy', { current_job: job('deploy') }),
    runner('cor-1', 'lane-cordoned', 'draining', { current_job: job('release') }),
    runner('wide-1', 'lane-wide', 'busy', { current_job: job('unit'), cpu_percent: 80 }),
    runner('wide-2', 'lane-wide', 'registering'),
  ];
  const QUEUED: Job[] = [
    job('waiting one', { state: 'queued', pool_name: 'linux-x64', started_at: null }),
    job('waiting two', { state: 'queued', pool_name: 'linux-x64', started_at: null }),
  ];
  const LANES = lanes(HOSTS, RUNNERS, QUEUED);

  const tile = (kind: Cell['kind'], r?: Runner): Cell =>
    r?.id
      ? { key: `tile:${r.id}`, kind, href: runnerHref(r.id), runner: r }
      : { key: `tile:free`, kind, href: hostHref('lane-ok') };

  const TILES: [string, Cell][] = [
    [
      'Busy',
      tile(
        'runner',
        runner('t-busy', 'x', 'busy', {
          current_job: job('test (ubuntu-latest, 22)', { head_branch: 'fix/flaky-cache' }),
          cpu_percent: 130,
        }),
      ),
    ],
    ['Idle', tile('runner', runner('t-idle', 'x', 'idle', { cpu_percent: 2 }))],
    ['Provisioning', tile('starting', runner('t-prov', 'x', 'provisioning'))],
    ['Registering', tile('starting', runner('t-reg', 'x', 'registering'))],
    [
      'Draining',
      tile('runner', runner('t-drain', 'x', 'draining', { current_job: job('release') })),
    ],
    ['Failed', tile('runner', runner('t-fail', 'x', 'failed'))],
    [
      'A four-hundred-character job name',
      tile(
        'runner',
        runner('t-long', 'x', 'busy', {
          current_job: job(LONG, { repo: `eyupio/${'a-very-long-repository-'.repeat(4)}` }),
          cpu_percent: 50,
        }),
      ),
    ],
    ['Free slot', tile('free')],
  ];
</script>

<Panel
  title="Command centre"
  description="The deck's lanes in each state, and a tile for each kind of slot. Built by the page's own lanes() from fixtures."
>
  <div class="block">
    <h3>Lanes: room to spare, full with jobs waiting, throttled, not answering, cordoned, wide</h3>
    <div class="deck">
      {#each LANES as lane (lane.host.id)}
        <HostLane {lane} />
      {/each}
    </div>
  </div>

  <div class="block">
    <h3>Tiles</h3>
    <div class="tiles">
      {#each TILES as [label, cell] (label)}
        <figure>
          <figcaption>{label}</figcaption>
          <RunnerTile {cell} hostName="lane-ok" />
        </figure>
      {/each}
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
  .deck {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr));
    gap: var(--z-space-4);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 13rem), 1fr));
    gap: var(--z-space-3);
  }
  figure {
    display: grid;
    gap: var(--z-space-1);
    min-width: 0;
    margin: 0;
  }
  figcaption {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
</style>
