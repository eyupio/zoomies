<!-- A deliberately small path for the setting changed most often on a host. -->
<script lang="ts">
  import { ApiError, updateHost } from '$lib/api/client';
  import type { Host } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';

  interface Props {
    open?: boolean;
    host: Host | null;
    onclose?: () => void;
  }

  let { open = $bindable(false), host, onclose }: Props = $props();
  let capacity = $state('');
  let saving = $state(false);
  let serverError = $state('');
  let loadedFor = $state<string | null>(null);

  $effect(() => {
    if (!open || !host) {
      loadedFor = null;
      return;
    }
    if (loadedFor === host.id) return;
    loadedFor = host.id ?? null;
    capacity = String(host.capacity ?? 0);
    serverError = '';
  });

  const parsed = $derived(Number(capacity));
  const error = $derived(
    serverError ||
      (capacity.trim() === ''
        ? 'Give a number. Use 0 to pause new runner placement on this host.'
        : !Number.isInteger(parsed) || parsed < 0
          ? 'Runner capacity must be a whole number of zero or more.'
          : ''),
  );

  function close(): void {
    open = false;
    onclose?.();
  }

  async function save(): Promise<void> {
    if (!host?.id || error) return;
    saving = true;
    serverError = '';
    try {
      await updateHost(host.id, { capacity: parsed });
      await fleet.reconcile();
      toasts.success(`${host.name || host.id} runner capacity set to ${parsed}`);
      close();
    } catch (cause) {
      if (cause instanceof ApiError) serverError = cause.fieldErrors().capacity ?? '';
      toasts.fromError(cause, 'That host capacity was not changed');
    } finally {
      saving = false;
    }
  }
</script>

<Dialog
  bind:open
  size="sm"
  title="Runner capacity"
  description="Change only how many runners {host?.name || 'this host'} may hold."
  onclose={close}
>
  <form
    id="host-capacity-form"
    onsubmit={(event) => {
      event.preventDefault();
      void save();
    }}
  >
    <Field
      label="Maximum runners on this host"
      {error}
      hint="It is running {pluralise(
        host?.active_runners ?? 0,
        'runner',
      )} now. Lowering this does not interrupt them; it only stops new placement until there is room."
    >
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={capacity}
          {id}
          {describedBy}
          {invalid}
          type="number"
          min={0}
          step={1}
          mono
        />
      {/snippet}
    </Field>
  </form>

  {#snippet footer()}
    <Button variant="ghost" onclick={close}>Cancel</Button>
    <Button
      variant="primary"
      type="submit"
      form="host-capacity-form"
      loading={saving}
      disabled={Boolean(error)}
    >
      Save capacity
    </Button>
  {/snippet}
</Dialog>
