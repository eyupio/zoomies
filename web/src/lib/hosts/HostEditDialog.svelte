<!--
  Capacity, labels and the reserve for one host.

  Capacity is how many runners the scheduler may place here at once; the field
  says so, because the number on its own invites being set to the host's CPU
  count for reasons that are not quite right. Lowering it below what is already
  running is allowed and does not evict anything -- it simply stops more being
  placed -- and the hint says that too.

  The reserve is the other half of the same question: capacity is how many
  runners the fleet will put here, and the reserve is how much of the machine it
  must leave alone while doing it. Only the fields the host has actually
  reported are offered, because a reserve held back from a figure nobody has
  measured is a number the scheduler ignores and the page would still show.
-->
<script lang="ts">
  import { updateHost } from '$lib/api/client';
  import { ApiError } from '$lib/api/client';
  import type { Host } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import LabelMapEditor from './LabelMapEditor.svelte';

  interface Props {
    open?: boolean;
    host: Host | null;
    onclose?: () => void;
  }

  let { open = $bindable(false), host, onclose }: Props = $props();

  let capacity = $state('');
  let reserveCpus = $state('');
  let reserveMemory = $state('');
  let reserveDisk = $state('');
  let rows = $state<{ key: string; value: string }[]>([]);
  let saving = $state(false);
  let errors = $state<Record<string, string>>({});

  // Reload the form whenever a different host is opened, and only then: an SSE
  // update to the host must not overwrite what is being typed.
  let loadedFor = $state<string | null>(null);
  $effect(() => {
    if (!open || !host) {
      loadedFor = null;
      return;
    }
    if (loadedFor === host.id) return;
    loadedFor = host.id ?? null;
    capacity = String(host.capacity ?? 0);
    reserveCpus = String(host.reserve_cpus ?? 0);
    reserveMemory = String(host.reserve_memory_mb ?? 0);
    reserveDisk = String(host.reserve_disk_mb ?? 0);
    rows = Object.entries(host.labels ?? {}).map(([key, value]) => ({ key, value }));
    errors = {};
  });

  const active = $derived(host?.active_runners ?? 0);
  const parsed = $derived(Number(capacity));
  const capacityError = $derived(
    capacity.trim() === ''
      ? 'Give a number. Use 0 to stop new runners being placed here at all.'
      : !Number.isInteger(parsed) || parsed < 0
        ? 'Capacity is a whole number of runners, and cannot be negative.'
        : '',
  );
  /** The server's field error wins, because it knows something we did not. */
  const capacityFieldError = $derived(errors.capacity ?? capacityError);

  // What the host has measured about itself. A reserve is offered only against
  // a figure that exists; the server refuses the rest, and offering a field
  // whose every value is refused is a worse way to learn that.
  const knownCpus = $derived(host?.cpus ?? 0);
  const knownMemory = $derived(host?.memory_mb ?? 0);
  const knownDisk = $derived(host?.disk_total_mb ?? 0);
  const anyReserve = $derived(knownCpus > 0 || knownMemory > 0 || knownDisk > 0);

  /** A reserve is a whole number, not negative, and smaller than the machine. */
  function reserveError(value: string, of: number, unit: string): string {
    if (value.trim() === '') return '';
    const n = Number(value);
    if (!Number.isInteger(n) || n < 0)
      return `A reserve is a whole number of ${unit}, and cannot be negative.`;
    if (of > 0 && n >= of)
      return `This host has ${of} ${unit}; holding all of it back would leave nothing to place on.`;
    return '';
  }
  const cpuReserveError = $derived(
    errors.reserve_cpus ?? reserveError(reserveCpus, knownCpus, 'CPUs'),
  );
  const memoryReserveError = $derived(
    errors.reserve_memory_mb ?? reserveError(reserveMemory, knownMemory, 'MB'),
  );
  const diskReserveError = $derived(
    errors.reserve_disk_mb ?? reserveError(reserveDisk, knownDisk, 'MB'),
  );
  const anyError = $derived(
    Boolean(capacityError || cpuReserveError || memoryReserveError || diskReserveError),
  );

  async function save(): Promise<void> {
    if (!host?.id || anyError) return;
    saving = true;
    errors = {};
    const labels: Record<string, string> = {};
    for (const row of rows) {
      const key = row.key.trim();
      if (key) labels[key] = row.value.trim();
    }
    try {
      await updateHost(host.id, {
        capacity: parsed,
        labels,
        // Only what this host can honour: a field it has never reported is
        // left alone rather than sent as a zero it would have to refuse.
        ...(knownCpus > 0 ? { reserve_cpus: Number(reserveCpus || 0) } : {}),
        ...(knownMemory > 0 ? { reserve_memory_mb: Number(reserveMemory || 0) } : {}),
        ...(knownDisk > 0 ? { reserve_disk_mb: Number(reserveDisk || 0) } : {}),
      });
      await fleet.reconcile();
      toasts.success(`${host.name || host.id} updated`);
      open = false;
      onclose?.();
    } catch (cause) {
      if (cause instanceof ApiError) errors = cause.fieldErrors();
      toasts.fromError(cause, 'That host was not updated');
    } finally {
      saving = false;
    }
  }
</script>

<Dialog
  bind:open
  title="Edit {host?.name || 'host'}"
  description="These take effect on the scheduler's next pass."
  {onclose}
>
  <div class="form">
    <Field
      label="Capacity"
      error={capacityFieldError}
      hint="How many runners may exist on this host at once. It is running {pluralise(
        active,
        'runner',
      )} now; setting a lower number does not stop them, it only prevents more."
    >
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={capacity}
          {id}
          {describedBy}
          invalid={invalid || Boolean(capacityFieldError)}
          type="number"
          min={0}
          step={1}
        />
      {/snippet}
    </Field>

    {#if anyReserve}
      <fieldset class="reserve">
        <legend>Held back for the machine</legend>
        <p class="note">
          What the scheduler leaves alone: the room this host needs to be a working machine rather
          than a pool of capacity. Memory and disk have floors of 512 MB and 2 GB even when these
          are zero.
        </p>
        {#if knownCpus > 0}
          <Field label="CPUs" error={cpuReserveError} hint="Of {knownCpus} on this host.">
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={reserveCpus}
                {id}
                {describedBy}
                invalid={invalid || Boolean(cpuReserveError)}
                type="number"
                min={0}
                step={1}
              />
            {/snippet}
          </Field>
        {/if}
        {#if knownMemory > 0}
          <Field
            label="Memory (MB)"
            error={memoryReserveError}
            hint="Of {knownMemory} MB on this host."
          >
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={reserveMemory}
                {id}
                {describedBy}
                invalid={invalid || Boolean(memoryReserveError)}
                type="number"
                min={0}
                step={1}
              />
            {/snippet}
          </Field>
        {/if}
        {#if knownDisk > 0}
          <Field
            label="Disk (MB)"
            error={diskReserveError}
            hint="Of {knownDisk} MB on the work directory's filesystem."
          >
            {#snippet children({ id, describedBy, invalid })}
              <Input
                bind:value={reserveDisk}
                {id}
                {describedBy}
                invalid={invalid || Boolean(diskReserveError)}
                type="number"
                min={0}
                step={1}
              />
            {/snippet}
          </Field>
        {/if}
      </fieldset>
    {/if}

    <Field label="Labels" hint="Pools choose hosts by these key/value pairs.">
      {#snippet children({ describedBy })}
        <LabelMapEditor bind:rows {describedBy} />
      {/snippet}
    </Field>
  </div>

  {#snippet footer()}
    <Button
      variant="ghost"
      onclick={() => {
        open = false;
        onclose?.();
      }}
    >
      Cancel
    </Button>
    <Button variant="primary" loading={saving} disabled={anyError} onclick={save}>
      Save changes
    </Button>
  {/snippet}
</Dialog>

<style>
  .reserve {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
  }
  legend {
    padding: 0 var(--z-space-1);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .note {
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-2);
  }
</style>
