<script lang="ts">
  import type { Problem, Settings } from '$lib/api/types';
  import { getSettings, updateSettings } from '$lib/api/client';
  import { session } from '$lib/state/session.svelte';
  import Button from '$lib/components/Button.svelte';
  import PoolWarnings from './PoolWarnings.svelte';
  import { isStartupFinding, startupFix } from './startupStability';

  let { warnings, onfixed }: { warnings: readonly Problem[]; onfixed: () => void } = $props();
  const relevant = $derived(warnings.filter(isStartupFinding));
  const admin = $derived(session.can('admin'));
  let settings = $state<Settings | null>(null);
  let error = $state('');
  let message = $state('');
  let busy = $state(false);
  let reload = $state(0);
  const fix = $derived(settings ? startupFix(settings, relevant) : null);
  const changes = $derived(Object.entries(fix?.changes ?? {}));

  $effect(() => {
    if (!admin) return;
    void reload;
    const controller = new AbortController();
    getSettings(controller.signal)
      .then((result) => {
        settings = result;
        error = '';
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        error =
          cause instanceof Error ? cause.message : 'Could not check which settings can be changed.';
      });
    return () => controller.abort();
  });

  async function apply() {
    if (!fix || changes.length === 0 || busy) return;
    busy = true;
    error = '';
    try {
      // Re-read before writing so a setting fixed elsewhere is not reset and a
      // newly pinned value remains untouched. PATCH validates the batch too.
      const current = await getSettings();
      const active = relevant.filter((p) => current.findings?.some((f) => f.code === p.code));
      const next = startupFix(current, active);
      settings = Object.keys(next.changes).length ? await updateSettings(next.changes) : current;
      message = settings.pending_restart?.some((key) => key === 'agent.bootstrap_cpu_grace')
        ? 'Recommendations saved. Restart the embedded agent/controller to apply startup grace. Standalone agents need the same setting on their own hosts.'
        : 'Editable recommendations applied. Check any remaining warnings below.';
      onfixed();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'The settings could not be saved.';
    } finally {
      busy = false;
    }
  }
</script>

{#if relevant.length || message}
  <section class="stability" aria-label="Automatic pool startup stability">
    <h3>Automatic pool startup stability</h3>
    {#if relevant.length}
      <PoolWarnings warnings={relevant} bare />
      {#if admin}
        {#if changes.length}
          <p>This one-time fix changes fleet settings for all pools:</p>
          <ul>
            {#each changes as [key, value] (key)}<li>
                <code>{key}</code> → {String(value)}
              </li>{/each}
          </ul>
          <Button onclick={() => void apply()} disabled={busy}
            >{busy ? 'Applying…' : 'Apply recommended settings once'}</Button
          >
        {/if}
        {#if fix?.blocked.length}<p>
            Change these environment-pinned or read-only settings at their source: {fix.blocked.join(
              ', ',
            )}.
          </p>{/if}
        {#if fix?.pending.length}<p>
            Already saved; awaiting restart: {fix.pending.join(', ')}.
          </p>{/if}
      {:else}
        <p>
          An administrator can apply the recommended fleet settings. You can still save this pool.
        </p>
      {/if}
    {/if}
    {#if error}<p role="alert">{error}</p>
      <Button variant="secondary" onclick={() => reload++}>Retry settings check</Button>{/if}
    {#if message}<p role="status">{message}</p>{/if}
  </section>
{/if}

<style>
  .stability {
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    padding: var(--z-space-4);
  }
  h3 {
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-base);
  }
  p,
  li {
    font-size: var(--z-text-base);
    color: var(--z-text-muted);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
</style>
