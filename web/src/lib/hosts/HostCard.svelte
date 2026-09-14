<!--
  One host.

  A card rather than a table row: a host carries a set of facts that do not fit
  a grid -- capacity, health, backend capabilities and labels -- and there are
  usually few enough hosts that density is not the constraint. Everything an
  operator would act on is here, and the state is carried by a shape and a word
  as well as by a colour.
-->
<script lang="ts">
  import { navigate } from '$lib/router';
  import { CircleDashed, Gauge, Pencil, ServerCog, Trash2 } from '@lucide/svelte';
  import type { Host, Machine } from '$lib/api/types';
  import { formatMegabytes, formatNumber } from '$lib/format';
  import { hostStatus, throttled } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import DropdownMenu from '$lib/components/DropdownMenu.svelte';
  import type { MenuItem } from '$lib/components/DropdownMenu.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import UtilisationBar from '$lib/components/UtilisationBar.svelte';
  import BackendList from './BackendList.svelte';
  import ResourceBar from './ResourceBar.svelte';

  interface Props {
    host: Host;
    /**
     * The machine this host is, when Zoomies rented it. Handed down rather
     * than fetched here: the Hosts page already has the list for its own band,
     * and a card that fetched its own would be one request per host.
     */
    machine?: Machine | null;
    canOperate?: boolean;
    canAdmin?: boolean;
    oncordon: (host: Host, cordoned: boolean) => void;
    /** Lift the throttle the controller has this host on. */
    onthrottle: (host: Host) => void;
    oncapacity: (host: Host) => void;
    onedit: (host: Host) => void;
    ondelete: (host: Host) => void;
    class?: string;
  }

  let {
    host,
    machine = null,
    canOperate = false,
    canAdmin = false,
    oncordon,
    onthrottle,
    oncapacity,
    onedit,
    ondelete,
    class: className = '',
  }: Props = $props();

  const status = $derived(
    hostStatus({ healthy: host.healthy, cordoned: host.cordoned, throttle: host.throttle }),
  );
  // Whether the controller has stepped this host down. The slots line, the
  // bar and the menu all follow it; the sentence itself is the controller's.
  const isThrottled = $derived(throttled(host.throttle));

  // How this host's release stands to the controller's, and whether its agent
  // speaks a protocol the controller understands at all. Both come from the
  // controller: it is the side that knows its own version, and computing skew
  // here would give a different answer from the one the agent logs about
  // itself.
  const skew = $derived(host.version_skew ?? '');
  const skewLabel = $derived(
    skew === 'behind' ? 'Behind' : skew === 'ahead' ? 'Ahead' : skew ? 'Different build' : '',
  );
  const skewHint = $derived(
    skew === 'ahead'
      ? 'This agent is a later release than the controller, which is the direction nothing is tested in. Upgrade the controller first.'
      : skew === 'behind'
        ? 'This agent is an earlier release than the controller. It is placing work as normal; upgrade it when convenient.'
        : 'This agent is a build the controller cannot order against its own. Both are running; check which is which before reporting a bug.',
  );
  // The host's own count, not one derived from the cached runner list: the cache
  // holds a page of runners, so counting it would undercount a busy host.
  const active = $derived(host.active_runners ?? 0);
  const capacity = $derived(host.capacity ?? 0);
  // What the host takes right now: its capacity stepped down by the throttle,
  // and its capacity when there is none. Free is measured against it by the
  // controller, so the card never promises a slot the next pass would refuse.
  const effective = $derived(isThrottled ? (host.effective_capacity ?? capacity) : capacity);
  const free = $derived(host.free ?? Math.max(0, effective - active));
  const labels = $derived(Object.entries(host.labels ?? {}));
  // What this machine is, in the terms a pool asks in. The controller renders
  // the sentence; the kernel and architecture are the fallback for an agent too
  // old to report a distribution.
  const platform = $derived(host.platform_label || [host.os, host.arch].filter(Boolean).join('/'));

  // How much machine it is, which is the other half of the answer to "why is
  // this host full".
  const size = $derived.by(() => {
    const cpus = host.cpus ?? 0;
    if (cpus <= 0) return '';
    const memory = host.memory_mb ?? 0;
    return memory > 0
      ? `${formatNumber(cpus)} vCPU · ${formatNumber(Math.round(memory / 1024))} GB`
      : `${formatNumber(cpus)} vCPU`;
  });

  /**
   * The disk behind the work directory, where a runner's checkout and its
   * caches land.
   *
   * It is the resource that runs out first and says nothing when it does: a
   * host with slots free and no space starts a job that fails part-way
   * through, which reads as a flaky build rather than a full disk. Free is
   * what a runner may actually write to -- the filesystem's reserve is root's,
   * and a runner is not root.
   *
   * Zero total means the agent did not measure it, which is not a full disk,
   * so nothing is shown rather than "0 GB free".
   */
  const disk = $derived.by(() => {
    const total = host.disk_total_mb ?? 0;
    if (total <= 0) return '';
    const free = host.disk_free_mb ?? 0;
    const gb = (mb: number) => formatNumber(Math.round(mb / 1024));
    return `${gb(free)} GB free of ${gb(total)}`;
  });

  /**
   * What this host has already promised away, against what may be placed on it.
   *
   * Slots answer "will the fleet take another runner here"; this answers
   * "can the machine carry it", and they are different questions -- a host with
   * three slots free and no memory left takes nothing, and the slot bar above
   * cannot say why. The figures are the scheduler's own, as of its last pass:
   * a number here that disagreed with the one it placed against would be worse
   * than none, because it would be believed.
   */
  const resources = $derived.by(() => {
    if (!host.resources_known || !host.reserved_known) return null;
    const rows: { label: string; used: number; total: number; text: string; hint: string }[] = [];
    const cpuTotal = host.allocatable_cpus ?? 0;
    if (cpuTotal > 0) {
      const used = host.reserved_cpus ?? 0;
      rows.push({
        label: 'CPU',
        used,
        total: cpuTotal,
        text: `${round(used)} of ${round(cpuTotal)}`,
        hint: host.reserve_cpus
          ? `${formatNumber(host.reserve_cpus)} held back for the machine`
          : '',
      });
    }
    const memTotal = host.allocatable_memory_mb ?? 0;
    if (memTotal > 0) {
      const used = host.reserved_memory_mb ?? 0;
      rows.push({
        label: 'Memory',
        used,
        total: memTotal,
        text: `${formatMegabytes(used)} of ${formatMegabytes(memTotal)}`,
        hint: host.reserve_memory_mb
          ? `${formatMegabytes(host.reserve_memory_mb)} held back for the machine`
          : '',
      });
    }
    return rows.length > 0 ? rows : null;
  });

  /** One decimal at most: a CPU share of 3.75 is a fact, 3.7500000001 is not. */
  function round(n: number): string {
    return formatNumber(Math.round(n * 10) / 10);
  }

  /** Below this, the disk is the reason a job will fail rather than a detail. */
  const DISK_LOW = 0.1;
  const diskLow = $derived.by(() => {
    const total = host.disk_total_mb ?? 0;
    return total > 0 && (host.disk_free_mb ?? 0) / total < DISK_LOW;
  });

  /**
   * "Proxmox · pve-1 · 143": which provider, and which resource there.
   *
   * The resource identifier is the half that matters during an incident -- it
   * is what an operator types into the hypervisor's own console -- so it is on
   * the card rather than a click away.
   */
  const machineLabel = $derived.by(() => {
    if (!machine) return '';
    const parts = [machine.provider_kind || machine.provider_name || 'Provider'];
    if (machine.resource_zone) parts.push(machine.resource_zone);
    if (machine.resource_id) parts.push(machine.resource_id);
    return parts.join(' · ');
  });

  const actions = $derived<MenuItem[]>([
    {
      id: 'usage',
      label: 'View usage history',
      onSelect: () => navigate(`/usage?group_by=host&entity=${encodeURIComponent(host.id ?? '')}`),
    },
    {
      id: 'cordon',
      label: host.cordoned ? 'Uncordon this host' : 'Cordon this host',
      icon: ServerCog,
      disabled: !canOperate,
      onSelect: () => oncordon(host, !host.cordoned),
    },
    // Only while there is one to lift: an action that says "lift the
    // throttle" on a host that is not throttled is a question with no answer.
    ...(isThrottled
      ? [
          {
            id: 'throttle',
            label: 'Lift the throttle',
            icon: CircleDashed,
            disabled: !canOperate,
            onSelect: () => onthrottle(host),
          },
        ]
      : []),
    {
      id: 'capacity',
      label: 'Adjust capacity and reserve',
      icon: Gauge,
      disabled: !canOperate,
      onSelect: () => oncapacity(host),
    },
    {
      id: 'edit',
      label: 'Edit capacity and labels',
      icon: Pencil,
      disabled: !canOperate,
      onSelect: () => onedit(host),
    },
    {
      id: 'delete',
      // A machine-backed host is removed by deleting the machine, because the
      // resource outlives the row: forgetting the host here would leave a VM
      // running and on somebody's bill with nothing left that remembers it.
      label: machine ? 'Delete the machine instead' : 'Remove this host',
      icon: Trash2,
      danger: true,
      separated: true,
      disabled: !canAdmin || host.embedded === true,
      onSelect: () => {
        if (machine?.id) navigate(`/machines/${machine.id}`);
        else ondelete(host);
      },
    },
  ]);
</script>

<article class="card {className}" aria-labelledby="host-{host.id}-name">
  <header>
    <div class="identity">
      <h3 id="host-{host.id}-name" tabindex="-1">{host.name || host.id}</h3>
      <div class="badges">
        <Badge {status} size="sm" title={status.hint} />
        {#if host.incompatible}
          <Badge
            tone="danger"
            label="Incompatible"
            size="sm"
            dot={false}
            title={host.incompatible_reason ||
              'This agent speaks a protocol the controller does not, so no new runner is placed here.'}
          />
        {:else if skewLabel}
          <!-- neutral, not a status colour: a host on another release is not
               in a state, it is a fact worth knowing. The status palette is a
               fixed mapping and reusing one here would teach it a second
               meaning. -->
          <Badge tone="neutral" label={skewLabel} size="sm" dot={false} title={skewHint} />
        {/if}
        {#if !host.resources_known}
          <!-- Not a fault: an agent too old to measure its machine is placed by
               slots alone, exactly as every host was before it could. Saying so
               is what stops a card with every figure missing reading as broken. -->
          <Badge
            tone="neutral"
            label="Size unknown"
            size="sm"
            dot={false}
            title="This agent has not reported the machine's CPUs, memory or disk, so this host is placed by its slot count alone. Upgrade the agent and the figures appear on its next heartbeat."
          />
        {/if}
        {#if machine}
          <!-- Neutral, never the status palette. Which provider built this
               host and which resource it is are facts about it, not states of
               it, and the six status hues already mean something an operator
               has learned. -->
          <Badge
            tone="neutral"
            label={machineLabel}
            size="sm"
            dot={false}
            title="Zoomies created this host. Deleting the machine removes the resource too; removing the host here would leave it running, and on the bill."
          />
        {/if}
        {#if host.connection === 'tailcat'}
          <Badge
            tone="accent"
            label="Tailcat host"
            size="sm"
            dot={false}
            title="Private encrypted agent connection. No public host address or inbound port required. Health is shown separately."
          />
        {/if}
        {#if host.embedded}
          <Badge
            tone="accent"
            label="Embedded"
            size="sm"
            dot={false}
            title="This agent runs inside the controller process, so it cannot be removed."
          />
        {/if}
      </div>
    </div>
    <DropdownMenu items={actions} label="Actions for {host.name || host.id}" size="sm" />
  </header>

  <p class="meta">
    {#if platform}<span>{platform}</span>{/if}
    {#if size}<span class="tabular">{size}</span>{/if}
    {#if disk}<span
        class="tabular"
        class:low={diskLow}
        title="Disk on the filesystem holding the work directory">{disk}</span
      >{/if}
    {#if host.version}<span>agent {host.version}</span>{/if}
    {#if host.version_channel}<span class="mono">channel :{host.version_channel}</span>{/if}
    {#if host.address}<span class="mono">{host.address}</span>{/if}
  </p>

  <p class="health" class:bad={host.healthy === false}>
    {#if host.healthy === false}
      No heartbeat since <RelativeTime value={host.last_heartbeat} plain />. The agent is not
      reporting, so no runner will be placed here until it does.
    {:else}
      Agent connected · last heartbeat <RelativeTime value={host.last_heartbeat} plain />
    {/if}
  </p>

  {#if host.incompatible}
    <!-- The controller's own sentence, which already names both protocol
         versions and what happens to the work here. Appending one of our own
         produced a run-on paragraph saying the same thing twice. -->
    <p class="cordoned">
      {host.incompatible_reason ||
        'This agent speaks a protocol the controller does not, so no new runner is placed here. Its running work finishes and is drained as normal.'}
    </p>
  {:else if machine?.state === 'draining' || machine?.state === 'deleting'}
    <!-- Before the plain cordon sentence, because this host is cordoned and
         saying only that would leave an operator waiting for it to come back.
         It is not coming back: the machine under it is on its way out. -->
    <p class="cordoned">
      This host is a machine Zoomies created. It is draining because the pools it serves have had no
      queued work for long enough to stop paying for it; the machine is deleted when the last runner
      finishes.
      <a href="/machines/{machine.id}">Open the machine</a>
    </p>
  {:else if host.cordoned}
    <p class="cordoned">
      Cordoned. Its running work finishes; no new runner is placed here until it is uncordoned.
    </p>
  {:else if isThrottled && host.throttle_reason}
    <!-- The controller's sentence, whole: it already says what was taken,
         why, what the running jobs are doing and how it ends. It takes the
         pending colour because a throttle is the fleet attending to
         something, not the operator having stopped it. -->
    <p class="cordoned throttled">{host.throttle_reason}</p>
  {:else if host.admission_reason}
    <p class="cordoned">{host.admission_reason}. Running jobs continue.</p>
  {:else if host.usage_fresh && (host.usage?.cpu_percent ?? 0) >= 85}
    <p class="cordoned">CPU is busy. New runners start one at a time while pressure clears.</p>
  {/if}

  <p class="meta" aria-label="Current host usage">
    {#if host.usage_fresh && host.usage}
      {#if host.usage.cpu_percent !== undefined}
        <span class="tabular">CPU usage {round(host.usage.cpu_percent)}%</span>
      {/if}
      {#if host.usage.memory_available_mb !== undefined}
        <span class="tabular"
          >{formatMegabytes(host.usage.memory_available_mb)} memory available</span
        >
      {/if}
      {#if host.usage.load_average_1m !== undefined}
        <!-- The runnable queue, which is what catches a machine that has
             stopped keeping up while its CPU figure sits at 100. -->
        <span class="tabular" title="One-minute load average, against {host.cpus ?? '?'} CPUs"
          >load {round(host.usage.load_average_1m)}</span
        >
      {/if}
      <span>Measured <RelativeTime value={host.usage.sampled_at} plain /></span>
    {:else if isThrottled}
      <!-- A throttle outlives the sample that put it there: the rung stands
           on the last measurements and is lifted after ten minutes without a
           fresh one, so "cannot be throttled" would be untrue on this card. -->
      <span
        >Current usage unavailable. The throttle stands on its last measurements and lifts after ten
        minutes without a fresh one.</span
      >
    {:else}
      <span
        >Current usage unavailable. Placement uses configured capacity and reservations, and this
        host cannot be throttled.</span
      >
    {/if}
  </p>

  <div class="capacity">
    <UtilisationBar
      busy={active}
      live={effective}
      tone="capacity"
      label="Runner slots in use on {host.name || host.id}"
      showText={false}
    />
    <div class="capacity-line">
      <p class="capacity-text tabular">
        {#if isThrottled}
          <strong>{formatNumber(active)}</strong> of {formatNumber(effective)} slots in use
          <span class="muted">· throttled from {formatNumber(capacity)}</span>
        {:else}
          <strong>{formatNumber(active)}</strong> of {formatNumber(capacity)} slots in use
          <span class="muted">· {formatNumber(free)} free</span>
        {/if}
      </p>
      {#if canOperate}
        <Button size="sm" icon={Gauge} onclick={() => oncapacity(host)}>Adjust</Button>
      {/if}
    </div>
  </div>

  {#if resources}
    <section class="block" aria-label="Resources committed on {host.name || host.id}">
      <h4>Committed</h4>
      <div class="resources">
        {#each resources as row (row.label)}
          <ResourceBar
            label={row.label}
            used={row.used}
            total={row.total}
            text={row.text}
            hint={row.hint}
          />
        {/each}
      </div>
    </section>
  {/if}

  <section class="block" aria-label="Backends on {host.name || host.id}">
    <h4>Backends</h4>
    <BackendList backends={host.backend_info} kinds={host.backends} />
  </section>

  <section class="block" aria-label="Labels on {host.name || host.id}">
    <h4>Labels</h4>
    {#if labels.length === 0}
      <p class="none">None. Pools that select hosts by label will not choose this one.</p>
    {:else}
      <ul class="labels">
        {#each labels as [key, value] (key)}
          <li class="mono">{key}={value}</li>
        {/each}
      </ul>
    {/if}
  </section>
  {#if canOperate && !host.embedded && host.upgrade_note}
    <!--
      Last on the card, and folded away until it is asked for.

      A fleet upgraded in step is a fleet where every card carries this, and an
      open copy of the same instructions on each one is three times the card
      for a paragraph that does not differ between them. Kept anywhere above,
      even folded, it also pushes the figures an operator came to compare --
      slots, committed resources, backends -- down by a line on the hosts that
      have it and not on the hosts that do not, so nothing lines up across the
      row. The header badge is what says a host is behind; this is where the
      command lives when somebody wants it.
    -->
    <details class="upgrade">
      <summary>
        {#if host.upgrade_command}
          {host.upgrade_version
            ? `Update this agent to ${host.upgrade_version}`
            : 'Update this agent'}
        {:else}
          Agent version guidance
        {/if}
      </summary>
      <div class="upgrade-body">
        <p>{host.upgrade_note}</p>
        {#if host.upgrade_command}
          <p>
            Running runner containers stay in place. The host reports its new version on the next
            heartbeat.
          </p>
          <pre><code>{host.upgrade_command}</code></pre>
          <div class="upgrade-actions">
            <CopyButton value={host.upgrade_command} label="Copy the upgrade command" showLabel />
          </div>
        {/if}
      </div>
    </details>
  {/if}
</article>

<style>
  .upgrade {
    /* Against the foot of the card, so that a row of stretched cards puts
       every one of these on the same line rather than wherever its own
       content happened to end. */
    margin-top: auto;
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-pending-subtle);
  }
  .upgrade summary {
    padding: var(--z-space-2) var(--z-space-3);
    cursor: pointer;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .upgrade[open] summary {
    border-bottom: var(--z-border-width) solid var(--z-pending-border);
  }
  .upgrade-body {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    padding: var(--z-space-3);
  }
  .upgrade-body p {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .upgrade-actions {
    display: flex;
  }
  .upgrade pre {
    margin: 0;
    padding: var(--z-space-3);
    background: var(--z-surface-sunken);
    border-radius: var(--z-radius-sm);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    min-width: 0;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .identity {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  /* A disk this close to full is the reason the next job fails, not a detail:
     it takes the pending colour so it reads as something to attend to before
     it becomes an incident. */
  .meta .low {
    color: var(--z-pending);
    font-weight: var(--z-weight-medium);
  }
  .health {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .health.bad {
    color: var(--z-danger);
  }
  .cordoned {
    margin: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-draining-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-draining-subtle);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  /* A throttle is the fleet's doing and lifts itself, so it takes the
     pending tone the badge uses rather than the draining one a cordon has. */
  .cordoned.throttled {
    border-color: var(--z-pending-border);
    background: var(--z-pending-subtle);
  }
  .capacity {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
  }
  .capacity-line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .capacity-text {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .capacity-text strong {
    color: var(--z-text);
    font-weight: var(--z-weight-semibold);
  }
  .muted {
    color: var(--z-text-subtle);
  }
  .block {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    padding-top: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  h4 {
    margin: 0;
    font-size: var(--z-text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-muted);
    font-weight: var(--z-weight-medium);
  }
  .resources {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
  }
  .labels {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .labels li {
    padding: 0 var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
  }
  .none {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
</style>
