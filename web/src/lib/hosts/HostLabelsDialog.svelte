<!--
  The labels on one host: the key/value pairs a pool selects it by.

  Capacity and the reserve are not here. They were, and a host then had two
  ways to set the same two numbers -- this dialog and "Adjust" beside the slot
  bar -- which disagreed about what a good number was, because only one of
  them knew what a runner in this fleet asks for. Adjust owns the resources
  and its recommendations; this owns everything else.
-->
<script lang="ts">
  import { updateHost } from '$lib/api/client';
  import type { Host } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import LabelMapEditor from './LabelMapEditor.svelte';

  interface Props {
    open?: boolean;
    host: Host | null;
    onclose?: () => void;
  }

  let { open = $bindable(false), host, onclose }: Props = $props();

  let rows = $state<{ key: string; value: string }[]>([]);
  let saving = $state(false);

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
    rows = Object.entries(host.labels ?? {}).map(([key, value]) => ({ key, value }));
  });

  function close(): void {
    open = false;
    onclose?.();
  }

  async function save(): Promise<void> {
    if (!host?.id) return;
    saving = true;
    const labels: Record<string, string> = {};
    for (const row of rows) {
      const key = row.key.trim();
      if (key) labels[key] = row.value.trim();
    }
    try {
      await updateHost(host.id, { labels });
      await fleet.reconcile();
      toasts.success(
        `${host.name || host.id} updated`,
        'Pools match against the new labels on the next scheduling pass.',
      );
      close();
    } catch (cause) {
      // Nothing here is a per-field error the form could point at -- the map
      // is one setting -- so the refusal is said once, in the toast.
      toasts.fromError(cause, 'Those labels were not saved');
    } finally {
      saving = false;
    }
  }
</script>

<Dialog
  bind:open
  title="Labels on {host?.name || 'host'}"
  description="Pools choose hosts by these key/value pairs. Capacity and the reserve are under Adjust."
  onclose={close}
>
  <div class="form">
    <Field
      label="Labels"
      hint="A pool with a host selector runs only where every pair it names matches."
    >
      {#snippet children({ describedBy })}
        <LabelMapEditor bind:rows {describedBy} />
      {/snippet}
    </Field>
  </div>

  {#snippet footer()}
    <Button variant="ghost" onclick={close}>Cancel</Button>
    <Button variant="primary" loading={saving} onclick={save}>Save changes</Button>
  {/snippet}
</Dialog>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-2);
  }
</style>
