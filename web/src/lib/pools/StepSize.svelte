<!--
  Step five: how much machine one runner gets, and how much disk its cache may
  keep.

  It is a step of its own, after the hosts and before the count, because that
  is the order the decision is actually made in: these are the machines, this
  is what one runner of mine costs on them, and therefore this is how many
  there can be. Asked the other way round -- a maximum typed first, a size
  typed last -- the number that mattered was chosen before anything on screen
  could say what it would buy.

  There are two answers, and the first one is the usual one. A pool that names
  no size is given one slot's share of whichever host each runner lands on --
  charged against that host and applied as a real cgroup limit, so the books
  and the cgroups agree -- which is correct on every machine in an unequal
  fleet without anybody typing a number. A fixed size is for the pool whose
  jobs need a particular amount of machine wherever they run.

  What is not an answer is "no limit at all". A runner with no limit takes
  every core on the machine it lands on while the fleet charges it one slot's
  share, so the host reads as half committed, its daemon stops answering, and
  the creates queued behind it time out on a machine every page calls busy.
  Neither choice here does that.

  The fixed figures are sliders rather than boxes. A box invites 3000 MB as
  readily as 4096 and says nothing about whether any host can back it; a notch
  is a value somebody has a reason to choose, and the count underneath says
  what choosing it costs.
-->
<script lang="ts">
  import { Sparkles, TriangleAlert } from '@lucide/svelte';
  import type { PoolRoom as PoolRoomShape, Resources, Result } from '$lib/api/types';
  import { formatMegabytes, pluralise } from '$lib/format';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Select from '$lib/components/Select.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import Slider from '$lib/components/Slider.svelte';
  import PoolFit from './PoolFit.svelte';
  import PoolRoom from './PoolRoom.svelte';
  import {
    CACHE_NOTCHES,
    CPU_NOTCHES,
    DISK_NOTCHES,
    MEMORY_NOTCHES,
    cacheBytes,
    cacheGb,
    chargedSize,
    cpuLabel,
    gbLabel,
    memoryLabel,
    nearest,
    withValue,
  } from './sizing';
  import type { PoolDraft } from './PoolWizardForm.svelte';

  interface Props {
    draft: PoolDraft;
    errors: Record<string, string>;
    touch: (field: string) => void;
    /** The fleet's own default, which is what "recommended" means here. */
    defaults: Resources | null;
    /** What the controller makes of the size, asked as the sliders move. */
    verdict: Result<'validatePool'> | null;
    validating: boolean;
  }

  let { draft, errors, touch, defaults, verdict, validating }: Props = $props();

  /* -- the size ------------------------------------------------------------- */

  const cpus = $derived(Number(draft.cpus) || 0);
  const memoryMb = $derived(Number(draft.memory_mb) || 0);
  const diskGb = $derived(Number(draft.disk_gb) || 0);

  const defaultCpus = $derived(defaults?.cpus ?? 2);
  const defaultMemoryMb = $derived(defaults?.memory_mb ?? 4096);

  const cpuNotches = $derived(withValue(CPU_NOTCHES, cpus));
  const memoryNotches = $derived(withValue(MEMORY_NOTCHES, memoryMb));
  const diskNotches = $derived(withValue(DISK_NOTCHES, diskGb));

  const cpuMarks = $derived([
    { value: defaultCpus, label: "the fleet's default", recommended: true },
    { value: cpuNotches[0] ?? 0.25, label: cpuLabel(cpuNotches[0] ?? 0.25) },
    {
      value: cpuNotches[cpuNotches.length - 1] ?? 64,
      label: cpuLabel(cpuNotches[cpuNotches.length - 1] ?? 64),
    },
  ]);
  const memoryMarks = $derived([
    { value: defaultMemoryMb, label: "the fleet's default", recommended: true },
    { value: memoryNotches[0] ?? 512, label: memoryLabel(memoryNotches[0] ?? 512) },
    {
      value: memoryNotches[memoryNotches.length - 1] ?? 131072,
      label: memoryLabel(memoryNotches[memoryNotches.length - 1] ?? 131072),
    },
  ]);
  const diskMarks = $derived([
    { value: 0, label: 'no limit' },
    { value: 40, label: '40 GB' },
    {
      value: diskNotches[diskNotches.length - 1] ?? 640,
      label: gbLabel(diskNotches[diskNotches.length - 1] ?? 640),
    },
  ]);

  const charged = $derived(chargedSize(cpus, memoryMb, draft.docker_mode));
  const atDefaults = $derived(cpus === defaultCpus && memoryMb === defaultMemoryMb);

  /* The room is the controller's count, which knows what the fleet is actually
     charged. It is what both this step and the next one are read against. */
  const room = $derived<PoolRoomShape | null>(verdict?.room ?? null);
  const roomTotal = $derived(room?.runners ?? 0);

  function setCpus(value: number): void {
    draft.cpus = String(value);
    touch('resources.cpus');
  }
  function setMemory(value: number): void {
    draft.memory_mb = String(value);
    touch('resources.memory_mb');
  }
  function setDisk(value: number): void {
    draft.disk_gb = value > 0 ? String(value) : '';
    touch('resources.disk_gb');
  }
  function useDefaults(): void {
    setCpus(defaultCpus);
    setMemory(defaultMemoryMb);
  }

  /*
    What the automatic answer amounts to on this fleet, said in one line beside
    the choice. The range comes from the controller's own per-host figures, so
    the sentence beside the radio and the table under it can never disagree.
  */
  const automaticDescription = $derived.by(() => {
    const plain =
      'Each runner is given one slot\u2019s share of the machine it lands on. Correct on every host in an unequal fleet, and it follows a host that is resized.';
    const hosts = room?.hosts ?? [];
    if (hosts.length === 0) return plain;
    const charges = hosts.map((h) => h.charge_cpus ?? 0);
    const low = Math.min(...charges);
    const high = Math.max(...charges);
    const range = low === high ? cpuLabel(low) : `${cpuLabel(low)} to ${cpuLabel(high)}`;
    return `${plain} Today that is ${range} per runner across ${pluralise(hosts.length, 'host')}.`;
  });

  /* -- the cache ------------------------------------------------------------ */

  const cacheLimitGb = $derived(cacheGb(Number(draft.cache_size_limit) || 0));
  const cacheNotches = $derived(withValue(CACHE_NOTCHES, cacheLimitGb));
  /* The disk the smallest matching host can spare. A limit above it is not a
     limit: the disk fills first, and a host at or below its disk reserve takes
     no runner of any pool. */
  const spareGb = $derived(room?.disk_known ? Math.floor((room.smallest_disk_mb ?? 0) / 1024) : 0);
  const cacheMarks = $derived.by(() => {
    const marks: { value: number; label: string; recommended?: boolean }[] = [
      { value: 0, label: 'no limit' },
    ];
    if (spareGb > 0) {
      const fits = nearest(
        cacheNotches.filter((n) => n > 0 && n <= spareGb),
        spareGb / 2,
      );
      if (fits > 0) marks.push({ value: fits, label: 'fits every host', recommended: true });
    }
    const top = cacheNotches[cacheNotches.length - 1] ?? 500;
    if (!marks.some((m) => m.value === top)) marks.push({ value: top, label: gbLabel(top) });
    return marks;
  });
  const cacheAboveDisk = $derived(spareGb > 0 && cacheLimitGb > spareGb);
  /* The server refuses a limit on a named volume, because there is no
     directory to measure behind one. Saying so beside the slider beats finding
     out on the review step. */
  const cacheNeedsPath = $derived(cacheLimitGb > 0 && !draft.cache_source.trim().startsWith('/'));

  function setCacheLimit(gb: number): void {
    draft.cache_size_limit = gb > 0 ? String(cacheBytes(gb)) : '';
    touch('cache.size_limit');
  }
</script>

<fieldset class="group">
  <legend>What one runner gets</legend>
  <p class="hint">
    Whichever you choose, the fleet holds this much room for the runner on the host it lands on and
    the backend applies it as a real limit. There is no “unlimited” here.
  </p>

  <RadioGroup
    name="pool-sizing"
    bind:value={draft.sizing}
    options={[
      {
        value: 'automatic',
        label: 'One share of each host',
        description: automaticDescription,
      },
      {
        value: 'fixed',
        label: 'A fixed size on every host',
        description:
          'The same CPU and memory wherever a runner lands. For jobs that need a particular amount of machine, and for a pool whose hosts you do not want to share evenly.',
      },
    ]}
    onchange={() => {
      touch('resources.cpus');
      touch('resources.memory_mb');
    }}
  />

  {#if draft.sizing === 'automatic'}
    <div class="shares">
      {#if !room || (room.hosts ?? []).length === 0}
        <p class="shares-empty">
          The share is worked out per host, once a host has reported what machine it is.
        </p>
      {:else}
        <p class="shares-title">What each host would give one runner</p>
        <ul>
          {#each room.hosts ?? [] as host (host.host_id)}
            <li>
              <span class="shares-host">{host.host}</span>
              <span class="shares-value"
                >{cpuLabel(host.charge_cpus ?? 0)} and {memoryLabel(host.charge_memory_mb ?? 0)}</span
              >
              <span class="shares-room">{pluralise(host.room ?? 0, 'runner')}</span>
            </li>
          {/each}
        </ul>
        <p class="shares-note">
          Straight from the controller, so it is the figure a runner is actually created with. It
          moves on its own when a host is resized or its slot count changes.
        </p>
      {/if}
    </div>
  {/if}

  {#if draft.sizing === 'fixed'}
  <div class="lead">
    <p class="echo">
      One runner asks for <strong>{cpuLabel(cpus)}</strong> and
      <strong>{memoryLabel(memoryMb)}</strong>{charged.pair
        ? `, and is charged ${cpuLabel(charged.cpus)} and ${memoryLabel(charged.memoryMb)} on a host — a docker-in-docker slot is two containers, and the backend gives the sidecar the same limits`
        : ''}.
    </p>
    <Button
      variant="secondary"
      size="sm"
      icon={Sparkles}
      disabled={atDefaults}
      onclick={useDefaults}
    >
      Use the fleet's default
    </Button>
  </div>

  <Field
    label="CPU per runner"
    error={errors['resources.cpus']}
    hint="Becomes the container's CPU quota, and the cores the scheduler holds for it on a host."
  >
    {#snippet children({ id, describedBy })}
      <Slider
        {id}
        values={cpuNotches}
        value={cpus}
        label="CPU per runner"
        valuetext={cpuLabel}
        marks={cpuMarks}
        {describedBy}
        onchange={setCpus}
      />
    {/snippet}
  </Field>

  <Field
    label="Memory per runner"
    error={errors['resources.memory_mb']}
    hint="The container's memory limit. A job that goes past it is killed, so this is the figure to raise when a build dies without a message."
  >
    {#snippet children({ id, describedBy })}
      <Slider
        {id}
        values={memoryNotches}
        value={memoryMb}
        label="Memory per runner"
        valuetext={memoryLabel}
        marks={memoryMarks}
        {describedBy}
        onchange={setMemory}
      />
    {/snippet}
  </Field>

  <Field
    label="Disk per runner"
    error={errors['resources.disk_gb']}
    hint="Advisory, and charged against the host's free disk so the fleet does not promise the same space twice. No limit is the usual answer: what keeps a host from filling up is its own disk reserve."
  >
    {#snippet children({ id, describedBy })}
      <Slider
        {id}
        values={diskNotches}
        value={diskGb}
        label="Disk per runner"
        valuetext={gbLabel}
        marks={diskMarks}
        {describedBy}
        onchange={setDisk}
      />
    {/snippet}
  </Field>

  {/if}

  <!--
    Both answers belong here, under the sliders, while there is still a reason
    to move them: a size is the one setting on this form that costs a host
    quietly. The fit says which machines this size has just put out of reach,
    in the fleet's own words; the room says how many runners the rest can hold.

    They are shown for both answers. An automatic pool can still be refused by
    a host -- for its backend, its platform, or a share that falls under what a
    runner needs to be a runner -- and that is exactly as worth knowing.
  -->
  <PoolFit {verdict} {validating} />
  <PoolRoom {room} {cpus} {memoryMb} {validating} />
</fieldset>

<fieldset class="group">
  <legend>Performance cache</legend>
  <p class="hint">
    Mounted at <code>/opt/zoomies-cache</code> and kept between runners. This is disposable build acceleration
    — dependencies and build outputs — not persistent workflow storage, and it may be evicted at any time.
  </p>

  <Checkbox
    bind:checked={draft.cache_enabled}
    label="Keep a cache between runners"
    description="Faster builds, at the price of state one job can leave for the next within the boundary below."
    onchange={() => touch('cache.enabled')}
  />

  {#if draft.cache_enabled}
    <div class="pair">
      <Field
        label="Isolation scope"
        error={errors['cache.scope']}
        hint="Pool: one cache all this pool's jobs share. Repository: a cache per repository, which is only as private as the pool's labels."
      >
        {#snippet children({ id, describedBy, invalid })}
          <Select
            bind:value={draft.cache_scope}
            options={[
              { value: 'pool', label: 'Pool' },
              { value: 'repository', label: 'Repository' },
            ]}
            {id}
            {describedBy}
            {invalid}
          />
        {/snippet}
      </Field>

      <Field
        label="Host path or volume prefix"
        error={errors['cache.source']}
        hint="An absolute path on the host, or a named-volume prefix. A size limit needs the path: there is nothing to measure inside a volume."
      >
        {#snippet children({ id, describedBy, invalid })}
          <Input
            bind:value={draft.cache_source}
            {id}
            {describedBy}
            {invalid}
            mono
            placeholder="/var/lib/zoomies-cache"
            autocomplete="off"
            onblur={() => touch('cache.source')}
          />
        {/snippet}
      </Field>
    </div>

    <Field
      label="Cache size limit"
      error={errors['cache.size_limit']}
      hint="Kept by evicting whole entries, least recently used first, between one runner and the next."
    >
      {#snippet children({ id, describedBy })}
        <Slider
          {id}
          values={cacheNotches}
          value={cacheLimitGb}
          label="Cache size limit"
          valuetext={gbLabel}
          marks={cacheMarks}
          tone={cacheAboveDisk ? 'warning' : 'accent'}
          {describedBy}
          onchange={setCacheLimit}
        />
      {/snippet}
    </Field>

    {#if cacheAboveDisk}
      <div class="callout" role="status">
        <TriangleAlert size={16} aria-hidden="true" />
        <div>
          <p class="callout-title">More cache than the smallest host can spare</p>
          <p>
            {room?.smallest_disk_host} has {formatMegabytes(room?.smallest_disk_mb ?? 0)} free behind
            its work directory, and this cache may grow to {gbLabel(cacheLimitGb)}. Eviction happens
            between one runner and the next, so the disk fills first — and a host at or below its
            disk reserve takes no runner of any pool, not just this one.
          </p>
        </div>
      </div>
    {:else if cacheNeedsPath}
      <div class="callout" role="status">
        <TriangleAlert size={16} aria-hidden="true" />
        <div>
          <p class="callout-title">A size limit needs a host path</p>
          <p>
            The limit is kept by evicting entries from a directory on the host, and there is nothing
            to measure inside a named volume. Give the source above an absolute path, or leave the
            limit at no limit.
          </p>
        </div>
      </div>
    {:else if spareGb > 0 && cacheLimitGb === 0}
      <p class="echo">
        With no limit the cache grows until the disk does. The smallest host this pool reaches has
        {formatMegabytes(room?.smallest_disk_mb ?? 0)} free.
      </p>
    {/if}

    {#if draft.cache_scope === 'repository'}
      <Field
        label="Cache repository (owner/name)"
        error={errors['cache.repository']}
        hint="Leave it empty when the pool's installation is scoped to a single repository — it already says which one."
      >
        {#snippet children({ id, describedBy, invalid })}
          <Input
            bind:value={draft.cache_repository}
            {id}
            {describedBy}
            {invalid}
            placeholder="acme/widgets"
            autocomplete="off"
            onblur={() => touch('cache.repository')}
          />
        {/snippet}
      </Field>
    {/if}

    {#if roomTotal > 0 && draft.cache_scope === 'pool'}
      <p class="echo">
        One cache per host, shared by every runner of this pool on it — up to {pluralise(
          roomTotal,
          'runner',
        )} across the fleet.
      </p>
    {/if}
  {/if}
</fieldset>

<style>
  .group {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    margin: 0;
    padding: 0;
    border: 0;
  }
  legend {
    padding: 0 0 var(--z-space-1);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .hint {
    margin: 0;
    max-width: 70ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  .hint code {
    font-family: var(--z-font-mono);
  }
  .lead {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }
  .echo {
    margin: 0;
    max-width: 70ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .pair {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-4);
    align-items: start;
  }
  .shares {
    margin: var(--z-space-4) 0 0;
    padding: var(--z-space-3) var(--z-space-4);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }

  .shares-title,
  .shares-empty {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }

  .shares-title {
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }

  .shares ul {
    display: grid;
    gap: var(--z-space-1);
    margin: var(--z-space-2) 0 0;
    padding: 0;
    list-style: none;
  }

  .shares li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    gap: var(--z-space-3);
    align-items: baseline;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }

  .shares-host {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--z-text);
  }

  .shares-value {
    font-variant-numeric: tabular-nums;
    color: var(--z-text);
  }

  .shares-room {
    min-width: 8ch;
    text-align: end;
    font-variant-numeric: tabular-nums;
    color: var(--z-text-muted);
  }

  .shares-note {
    margin: var(--z-space-2) 0 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }

  .callout {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    background: var(--z-pending-subtle);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .callout p {
    margin: 0;
    max-width: 70ch;
  }
  .callout-title {
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  @media (max-width: 768px) {
    .pair {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
