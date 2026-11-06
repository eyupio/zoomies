<!--
  The states an operator actually meets: loading, empty, refused, failed, and
  the status vocabulary itself. These are the components pages use, not page
  imitations, so a change to a Skeleton or an ErrorState is reviewed here
  before it reaches a route.

  A disconnected stream that keeps showing last-known data is a page-level
  state, not a component one: web/tests/live.spec.ts drives it against the real
  binary, and the Overview captures record both a first-load failure and a
  reconnecting stream.
-->
<script lang="ts">
  import { Inbox, TriangleAlert } from '@lucide/svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import StateCell from '$lib/components/StateCell.svelte';
  import Toaster from '$lib/components/Toaster.svelte';
  import { ApiError } from '$lib/api/client';
  import { toasts } from '$lib/state/toasts.svelte';
  import {
    accountStatus,
    ELSEWHERE,
    HOSTED,
    hostStatus,
    installationStatus,
    machineStatus,
    OOM_KILLED,
    poolStatus,
    QUEUE_EXPEDITED,
    QUEUE_PAUSED,
    QUEUE_REMOVED,
    RUN_FAILING,
    runnerStatuses,
    RUNNER_LOST,
    UNMATCHED,
  } from '$lib/status';

  const runnerStates = runnerStatuses(false);

  const jobStates = [
    RUN_FAILING,
    UNMATCHED,
    QUEUE_PAUSED,
    QUEUE_EXPEDITED,
    QUEUE_REMOVED,
    ELSEWHERE,
    HOSTED,
    OOM_KILLED,
    RUNNER_LOST,
  ];

  const otherStates = [
    poolStatus({ enabled: true }),
    poolStatus({ enabled: false }),
    hostStatus({ healthy: true, cordoned: false }),
    hostStatus({ healthy: false, cordoned: false }),
    hostStatus({ healthy: true, cordoned: true }),
    machineStatus('creating'),
    machineStatus('ready'),
    machineStatus('ready', true),
    machineStatus('failed'),
    installationStatus(true),
    installationStatus(false),
    installationStatus(undefined),
    accountStatus(false),
    accountStatus(true),
  ];

  /*
    The errors are the real ApiError the client throws, so the heading, the
    server's own sentence and the offline icon are exercised rather than a
    label standing in for them.
  */
  const offline = new ApiError({
    status: 0,
    code: 'internal',
    message: 'The controller did not answer. It may be restarting, or this address may be wrong.',
  });
  const forbidden = new ApiError({
    status: 403,
    code: 'forbidden',
    message: 'Only administrators can delete a pool.',
  });
  const missing = new ApiError({
    status: 404,
    code: 'not_found',
    message: 'No runner with that id exists.',
  });
  const failed = new ApiError({
    status: 500,
    code: 'internal',
    message: 'The scheduler could not place this job.',
    detail: 'No host in the fleet answers the pool selector linux-x64 and size large.',
  });
</script>

<Panel
  title="Status vocabulary"
  description="Colour and shape together. The shapes are what survive greyscale."
>
  <div class="legend" data-gallery="runner-states">
    {#each runnerStates as status (status.key)}
      <StateCell {status} />
    {/each}
  </div>
  <div class="legend" data-gallery="other-states">
    {#each jobStates as status (status.key)}
      <Badge {status} icon />
    {/each}
  </div>
  <div class="legend">
    {#each otherStates as status, i (i)}
      <Badge {status} />
    {/each}
  </div>
</Panel>

<Panel
  title="Loading"
  description="Skeletons are the only loading affordance for content. Spinners are for actions."
>
  <div class="columns">
    <div class="column">
      <h3>Text and rows</h3>
      <Skeleton width="60%" height="1.25rem" />
      <Skeleton lines={3} />
      <Skeleton width="40%" height="2.5rem" radius="md" />
    </div>
    <div class="column">
      <h3>A panel while its fetch is out</h3>
      <div class="card">
        <Skeleton width="30%" height="0.875rem" />
        <Skeleton width="55%" height="1.75rem" />
        <Skeleton lines={2} />
      </div>
    </div>
  </div>
</Panel>

<Panel
  title="Empty states"
  description="Never a blank table: what this is, and the action that fills it."
>
  <div class="columns">
    <div class="column">
      <EmptyState
        title="No pools yet"
        description="A pool declares the labels a workflow asks for and how many runners may serve it."
      >
        <Button variant="primary">Create a pool</Button>
      </EmptyState>
    </div>
    <div class="column">
      <div class="card">
        <EmptyState compact icon={Inbox} title="No runners match these filters">
          <Button variant="ghost">Clear filters</Button>
        </EmptyState>
      </div>
    </div>
  </div>
</Panel>

<Panel title="Errors" description="The server's own sentence where there is one, and a way back.">
  <div class="columns">
    <div class="column">
      <ErrorState error={offline} onretry={() => {}} />
      <ErrorState error={forbidden} onretry={() => {}} />
    </div>
    <div class="column">
      <ErrorState error={missing} onretry={() => {}} />
      <ErrorState error={failed} onretry={() => {}} retryLabel="Try the placement again" />
      <ErrorState
        error={new Error('The response was not JSON.')}
        title="The controller answered with something unexpected"
        onretry={() => {}}
        compact
      />
    </div>
  </div>
</Panel>

<Panel
  title="Loading boundary"
  description="The four states of remote data in one place: loading, failed, empty, ready."
>
  <div class="columns">
    <div class="column">
      <h3>Loading</h3>
      <LoadingBoundary loading>
        <p>Never shown while the fetch is out.</p>
      </LoadingBoundary>
    </div>
    <div class="column">
      <h3>Failed</h3>
      <LoadingBoundary error={offline} onretry={() => {}}>
        <p>Never shown after a failure.</p>
      </LoadingBoundary>
    </div>
    <div class="column">
      <h3>Empty</h3>
      <LoadingBoundary empty>
        {#snippet emptyState()}
          <EmptyState
            compact
            icon={TriangleAlert}
            title="No problems today"
            description="Nothing needs an operator right now."
          />
        {/snippet}
        <p>Shown when the request succeeded and returned nothing.</p>
      </LoadingBoundary>
    </div>
    <div class="column">
      <h3>Ready</h3>
      <LoadingBoundary>
        <p class="ready">Two runners are idle, one is busy.</p>
      </LoadingBoundary>
    </div>
  </div>
</Panel>

<Panel
  title="Toasts"
  description="Polite for successes, assertive for refusals. Errors stay until they are read."
>
  <div class="row">
    <Button
      onclick={() => toasts.success('Pool created', 'linux-x64 is paused until you resume it.')}
    >
      Success
    </Button>
    <Button onclick={() => toasts.info('Copied the runner id')}>Information</Button>
    <Button
      onclick={() =>
        toasts.warning('Retention applied to two of three copies', 'The third remote refused.')}
    >
      Warning
    </Button>
    <Button
      variant="danger"
      onclick={() =>
        toasts.error('The drain was refused', 'Only administrators can drain runners.', {
          label: 'Open your account',
          run: () => {},
        })}
    >
      Error with an action
    </Button>
  </div>
</Panel>

<!-- The gallery is its own document, so nothing else has mounted the region. -->
<Toaster />

<style>
  .legend {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
    padding: var(--z-space-2) 0;
  }
  .legend + .legend {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .columns {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(18rem, 100%), 1fr));
    gap: var(--z-space-4) var(--z-space-6);
    align-items: start;
  }
  .column {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
  }
  .ready {
    margin: 0;
    color: var(--z-text);
  }
</style>
