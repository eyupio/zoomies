<!--
  Proving a provider works by building one real machine on it.

  The preflight asks the hypervisor what it would refuse; this is the only way
  to learn that a machine actually clones, boots, installs its agent and joins
  the fleet. The machine goes through the same loop as any other, so each step
  shown here is a phase of its own timeline, and the one that fails is the one
  to go and look at. It is released as soon as it reports ready, and left in
  place when it fails so what it said is still there to read.

  It costs a machine for a few minutes and counts against the maximums, so it
  is a button an operator presses and never something that runs by itself.
-->
<script lang="ts">
  import { Check, CircleDashed, LoaderCircle, X } from '@lucide/svelte';
  import { listMachines, testProviderMachine } from '$lib/api/client';
  import type { Machine, Provider } from '$lib/api/types';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import { blocksAnotherTest, latestTest, stepStatusText, testProgress } from './testmachine';

  interface Props {
    provider: Pick<Provider, 'id' | 'name' | 'max_machines' | 'owned' | 'held'>;
    /** The page's own live list of this provider's machines, kept current by events. */
    machines: readonly Machine[];
    canOperate?: boolean;
    /**
     * Whether the controller is renting at all. When it is not, the notice above
     * this panel already gives the controller's own sentence for it, with the
     * way to turn it on, so the line under the button says only what the button
     * cannot do.
     */
    rentingOff?: boolean;
  }

  let { provider, machines, canOperate = false, rentingOff = false }: Props = $props();

  /** The test machine as last seen, which outlives its row once it is released. */
  let remembered = $state<Machine | null>(null);
  let starting = $state(false);

  // What the effect below is keyed on, and all it is keyed on. The page replaces
  // `provider` with a fresh object on every provider.updated frame, and an effect
  // that read `provider.id` itself ran again for each one: its cleanup aborted the
  // read in flight, and the run that followed found it had already been asked for.
  const providerId = $derived(provider.id ?? '');

  // The newest test the controller still has a row for, so the answer is still
  // here after a reload, and after the machine itself has been deleted.
  $effect(() => {
    const id = providerId;
    if (id === '') return;
    remembered = null;
    const controller = new AbortController();
    void listMachines({ provider: id, include_deleted: true, limit: 50 }, controller.signal)
      .then((page) => {
        const found = latestTest(page.items ?? []);
        if (found && !remembered) remembered = found;
      })
      .catch(() => {
        // The panel is still usable without history; a failed read is not worth a toast.
      });
    return () => controller.abort();
  });

  // The live row wins over the remembered one while the machine exists; once
  // the page's list drops it, the last thing seen stands for the result.
  const live = $derived(latestTest(machines));
  const shown = $derived.by(() => {
    if (
      live &&
      (!remembered || live.id === remembered.id || live.created_at >= remembered.created_at)
    )
      return live;
    return remembered;
  });
  $effect(() => {
    if (live) remembered = live;
  });

  const progress = $derived(shown ? testProgress(shown) : null);
  const running = $derived(progress?.outcome === 'running');
  // The controller holds the next test off until this one's machine is deleted,
  // which is after it has passed as well as while it runs.
  const inTheWay = $derived(shown ? blocksAnotherTest(shown) : false);
  const refusal = $derived(
    !canOperate
      ? 'Running a test needs the operator role.'
      : provider.held
        ? // The provider's own sentence, which names it and the switch that is
          // holding it. Renting off for the whole controller is said by the
          // notice above the panel, and repeating it here said one paragraph
          // twice on one screen, so this line then says what the button cannot
          // do and leaves the reason to the notice.
          rentingOff
          ? 'A test machine cannot start while renting is off.'
          : provider.held
        : (provider.max_machines ?? 0) <= (provider.owned ?? 0)
          ? 'There is no room under this provider’s maximum machines. Raise it in Settings first.'
          : '',
  );

  async function start(): Promise<void> {
    if (!provider.id || starting) return;
    starting = true;
    try {
      remembered = await testProviderMachine(provider.id);
    } catch (cause: unknown) {
      toasts.fromError(cause, 'The test machine was not started');
    } finally {
      starting = false;
    }
  }
</script>

<Panel
  title="Test machine"
  description="Builds one real machine, waits for it to join, then releases it"
>
  <p class="intro">
    The check asks the provider what it would refuse. This proves a machine really gets built,
    boots, installs its agent and joins the fleet, which is everything a job needs. It is paid for
    only until it reports ready.
  </p>

  {#if progress}
    <div class="result" data-testid="test-machine-result" aria-live="polite">
      <p class="headline">
        <Badge
          tone={progress.outcome === 'passed'
            ? 'idle'
            : progress.outcome === 'failed'
              ? 'danger'
              : 'pending'}
          label={progress.outcome === 'passed'
            ? 'Passed'
            : progress.outcome === 'failed'
              ? 'Failed'
              : 'Running'}
          size="sm"
        />
        {progress.summary}
      </p>
      <ol class="steps">
        {#each progress.steps as step (step.id)}
          <li class={step.status}>
            {#if step.status === 'done'}
              <Check size={14} aria-hidden="true" />
            {:else if step.status === 'active'}
              <LoaderCircle size={14} class="spin" aria-hidden="true" />
            {:else if step.status === 'failed'}
              <X size={14} aria-hidden="true" />
            {:else}
              <CircleDashed size={14} aria-hidden="true" />
            {/if}
            <span>{step.label}<span class="sr-only">, {stepStatusText(step.status)}</span></span>
          </li>
        {/each}
      </ol>
      {#if progress.problem}
        <p class="problem" role="alert">{progress.problem}</p>
      {/if}
    </div>
  {/if}

  <div class="actions">
    <Button
      variant="secondary"
      disabled={starting || inTheWay || refusal !== ''}
      onclick={() => void start()}
    >
      {starting
        ? 'Starting…'
        : running
          ? 'Test running…'
          : inTheWay
            ? 'Releasing the test machine…'
            : shown
              ? 'Run the test again'
              : 'Run a test machine'}
    </Button>
    {#if refusal && !inTheWay}
      <p class="note">{refusal}</p>
    {/if}
  </div>
</Panel>

<style>
  .intro,
  .note {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
  }
  .result {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    margin-top: var(--z-space-3);
  }
  .headline {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .steps {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    margin: 0;
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    list-style: none;
  }
  .steps li {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .steps li.done,
  .steps li.active {
    color: var(--z-text);
  }
  .steps li.failed {
    color: var(--z-danger);
  }
  .problem {
    margin: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-danger-subtle);
    font-size: var(--z-text-sm);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .actions {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--z-space-2);
    margin-top: var(--z-space-3);
  }
  :global(.spin) {
    animation: test-machine-spin 1s linear infinite;
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.spin) {
      animation: none;
    }
  }
  @keyframes test-machine-spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
