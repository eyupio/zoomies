<!--
  Whether the hosts could hold more runners, from what this fleet measured of itself.

  A host's slots are its machine less the reserve, divided by what one runner
  asks, and for a pool that leaves its size to its hosts that ask is the fleet's
  default. A fleet whose runners use a fraction of it queues jobs for want of a
  slot while its processors idle, and nobody can see that from a pool's form: the
  figure that decides it is a fleet setting, and the evidence is in the host
  samples and the job history. So the controller works it out
  (GET /pools/performance) and this says what it found, host by host.

  It changes a fleet setting, which resizes every pool that follows the default,
  so it is the administrator's, it names those pools, and it needs a tick before
  anything is written. The pool's maximum is put in the form and saved with the
  pool like any other field: this never writes a pool behind a person's back.
  Nothing is shown when there is nothing to say, a failed read included, because
  a card that says "no change" on every pool would be noise on the one page
  people open to change something else. The reason is in the API and the MCP tool
  for the person who wants it.
-->
<script lang="ts">
  import { Gauge } from '@lucide/svelte';
  import { getPoolPerformance, updateSettings } from '$lib/api/client';
  import type { Result } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import { cpuLabel, memoryLabel } from './sizing';
  import type { PoolDraft } from './draft';

  interface Props {
    draft: PoolDraft;
    /** The pool being edited; left out for one that does not exist yet. */
    poolId?: string;
  }

  let { draft, poolId }: Props = $props();

  type Performance = Result<'getPoolPerformance'>;

  let result = $state<Performance | null>(null);
  let busy = $state(false);
  let applied = $state(false);
  let error = $state('');
  let consent = $state(false);

  const admin = $derived(session.can('admin'));
  const raise = $derived(result?.verdict === 'raise' ? result : null);
  const followers = $derived(result?.followers ?? []);
  const maxBefore = $derived(Number(draft.max_runners) || 0);

  $effect(() => {
    const controller = new AbortController();
    getPoolPerformance({ pool_id: poolId }, controller.signal)
      .then((answer) => {
        result = answer;
      })
      .catch(() => {
        // Advice that could not be worked out is no advice, and no error.
      });
    return () => controller.abort();
  });

  async function apply() {
    if (!raise || busy || (followers.length > 0 && !consent)) return;
    busy = true;
    error = '';
    try {
      await updateSettings({
        'runners.default_cpus': raise.recommended.cpus,
        'runners.default_memory_mb': raise.recommended.memory_mb,
      });
      // The pool's maximum is a field of this form, saved with the pool; it is
      // only ever raised, so a maximum somebody chose higher is left alone.
      if (raise.max_runners && raise.max_runners > maxBefore) {
        draft.max_runners = String(raise.max_runners);
      }
      applied = true;
      toasts.success(
        'Runner size updated',
        'Hosts re-balance their slots within a few minutes. New slots fill as running jobs finish.',
      );
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'The settings could not be saved.';
    } finally {
      busy = false;
    }
  }
</script>

{#if raise}
  <section class="perf" aria-label="Performance recommendation">
    <p class="title">
      <Gauge size={15} aria-hidden="true" />
      Your hosts could hold {raise.slots_after} runners, not {raise.slots_before}
    </p>
    <p class="body">{raise.reason}</p>
    <p class="ask">
      One runner would be given <strong>{cpuLabel(raise.recommended.cpus)}</strong> and
      <strong>{memoryLabel(raise.recommended.memory_mb)}</strong>, from
      {cpuLabel(raise.current.cpus)} and {memoryLabel(raise.current.memory_mb)}. Elastic CPU can
      still lend a build more while its host has any idle.
    </p>

    <table class="hosts">
      <caption class="visually-hidden">Slots on each host, before and after</caption>
      <thead>
        <tr
          ><th scope="col">Host</th><th scope="col">Slots now</th><th scope="col">Slots after</th
          ></tr
        >
      </thead>
      <tbody>
        {#each raise.hosts as host (host.host_id)}
          <tr>
            <th scope="row">{host.host_name}</th>
            <td>{host.before}</td>
            <td>
              {host.after}{#if host.limited_by === 'disk'}<span class="limit">
                  (limited by disk)</span
                >{/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if (raise.cautions ?? []).length > 0}
      <ul class="cautions">
        {#each raise.cautions ?? [] as caution (caution)}<li>{caution}</li>{/each}
      </ul>
    {/if}

    {#if applied}
      <p class="done" role="status">
        Saved. The pool's maximum is now {draft.max_runners} in this form; save the pool to keep it.
      </p>
    {:else if admin}
      {#if followers.length > 0}
        <Checkbox
          bind:checked={consent}
          label="Resize {pluralise(followers.length, 'other pool')} too"
          description="{followers.join(
            ', ',
          )} follow the fleet's default size, so they are resized the same way."
        />
      {/if}
      <div class="actions">
        <Button onclick={() => void apply()} disabled={busy || (followers.length > 0 && !consent)}>
          {busy ? 'Applying…' : 'Apply this size'}
        </Button>
        <span class="note"
          >Changes <code>runners.default_cpus</code> and <code>runners.default_memory_mb</code>
          for the whole fleet{raise.max_runners && raise.max_runners > maxBefore
            ? `, and sets this pool's maximum to ${raise.max_runners} in the form`
            : ''}.</span
        >
      </div>
      {#if error}<p role="alert" class="error">{error}</p>{/if}
    {:else}
      <p class="note">An administrator can apply this. You can still save this pool.</p>
    {/if}
  </section>
{/if}

<style>
  .perf {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }
  .title {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .body,
  .ask,
  .note,
  .done,
  .cautions li {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
    overflow-wrap: anywhere;
  }
  .ask strong {
    color: var(--z-text);
  }
  .hosts {
    border-collapse: collapse;
    font-size: var(--z-text-xs);
  }
  .hosts th,
  .hosts td {
    padding: var(--z-space-1) var(--z-space-3) var(--z-space-1) 0;
    text-align: left;
    font-weight: var(--z-weight-regular);
    color: var(--z-text);
  }
  .hosts thead th {
    color: var(--z-text-muted);
  }
  .limit {
    color: var(--z-text-muted);
  }
  .cautions {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    margin: 0;
    padding-left: var(--z-space-4);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }
  code {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    color: var(--z-text);
  }
  .error {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-danger);
  }
  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
