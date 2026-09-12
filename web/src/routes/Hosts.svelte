<!--
  Hosts: where runners can go.

  The list comes from the live fleet cache, so a host that stops sending
  heartbeats goes unhealthy here without anything being pressed. Everything that
  keeps a host from taking work -- cordoned, unhealthy, no capacity left, a
  backend that is not available -- is said in words on the card, because that is
  the question this page exists to answer.

  Two things on this page do not arrive over the stream, though, and that is
  what refreshing is for here: a host enrolled a moment ago, which the
  controller announces only once it has heard from the agent, and the join
  tokens, which move when somebody mints or spends one rather than when the
  fleet does.
-->
<script lang="ts">
  import { Plus, Server } from '@lucide/svelte';
  import { cordonHost, listJoinTokens } from '$lib/api/client';
  import type { Host, JoinToken } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import HostCard from '$lib/hosts/HostCard.svelte';
  import HostCapacityDialog from '$lib/hosts/HostCapacityDialog.svelte';
  import HostDeleteDialog from '$lib/hosts/HostDeleteDialog.svelte';
  import HostEditDialog from '$lib/hosts/HostEditDialog.svelte';
  import JoinTokenList from '$lib/hosts/JoinTokenList.svelte';

  const canOperate = $derived(session.can('operator'));
  const canAdmin = $derived(session.can('admin'));

  const hosts = $derived(
    [...fleet.hosts].sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '')),
  );
  const healthy = $derived(hosts.filter((h) => h.healthy !== false).length);
  const capacity = $derived(hosts.reduce((sum, h) => sum + (h.capacity ?? 0), 0));
  const inUse = $derived(hosts.reduce((sum, h) => sum + (h.active_runners ?? 0), 0));

  /* -- join tokens ------------------------------------------------------------
   * Admin only, and fetched here rather than kept in the fleet cache: they
   * change when somebody mints one, not when the fleet moves.
   * ---------------------------------------------------------------------- */

  let tokens = $state<JoinToken[]>([]);
  let tokensLoading = $state(false);
  let tokensError = $state<unknown>(null);
  let tokensReload = $state(0);

  $effect(() => {
    if (!canAdmin) return;
    void tokensReload;
    const controller = new AbortController();
    tokensLoading = true;
    void listJoinTokens(controller.signal)
      .then((result) => {
        tokens = result.items ?? [];
        tokensError = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        tokensError = cause;
      })
      .finally(() => (tokensLoading = false));
    return () => controller.abort();
  });

  /* -- actions ----------------------------------------------------------------- */

  let editing = $state<Host | null>(null);
  let editOpen = $state(false);
  let sizing = $state<Host | null>(null);
  let sizeOpen = $state(false);
  let deleting = $state<Host | null>(null);
  let deleteOpen = $state(false);

  /**
   * Fetch both halves of this page again: the fleet, and the tokens beside it.
   * The token list reports its own progress in place, so the button follows the
   * reconcile -- the slower and more interesting of the two.
   */
  async function refreshPage(): Promise<void> {
    tokensReload += 1;
    await fleet.reconcile();
  }

  async function cordon(host: Host, cordoned: boolean): Promise<void> {
    if (!host.id) return;
    const name = host.name || host.id;
    const result = await fleet.optimistic(
      host.id,
      { cordoned },
      () => cordonHost(host.id ?? '', { cordoned }),
      cordoned ? `${name} was not cordoned` : `${name} was not uncordoned`,
    );
    if (result === undefined) return;
    if (cordoned) {
      toasts.info(
        `${name} cordoned`,
        'Its runners keep going and finish their jobs. No new runner will be placed here.',
      );
    } else {
      toasts.success(`${name} uncordoned`, 'The scheduler may place runners here again.');
    }
  }

  function edit(host: Host): void {
    editing = host;
    editOpen = true;
  }

  function size(host: Host): void {
    sizing = host;
    sizeOpen = true;
  }

  function remove(host: Host): void {
    deleting = host;
    deleteOpen = true;
  }
</script>

<PageHeader
  title="Hosts"
  subtitle="The machines that run runners, and how much room each one has left."
  onrefresh={refreshPage}
>
  {#snippet meta()}
    {#if fleet.loaded && hosts.length > 0}
      <p class="summary">
        {pluralise(hosts.length, 'host')} · {healthy} healthy · {inUse} of {capacity} runner slots in
        use
      </p>
    {/if}
  {/snippet}
  {#if canAdmin}
    <Button variant="primary" icon={Plus} href="/hosts/new">Add a host</Button>
  {/if}
</PageHeader>

<div class="content">
  <!--
    A failed reconcile once the hosts are on screen is not an error state: the
    cache still holds every host and the stream keeps updating them. Only a
    first load that never landed gets one, as the Overview does.
  -->
  <LoadingBoundary
    loading={fleet.loading && !fleet.loaded}
    error={fleet.loaded ? null : fleet.error}
    empty={fleet.loaded && hosts.length === 0}
    onretry={() => void fleet.reconcile()}
  >
    {#snippet skeleton()}
      <div class="grid">
        {#each [0, 1, 2] as card (card)}
          <div class="card-skeleton">
            <Skeleton width="45%" height="1.25rem" />
            <Skeleton lines={3} />
            <Skeleton height="var(--z-space-2)" radius="full" />
          </div>
        {/each}
      </div>
    {/snippet}

    {#snippet emptyState()}
      <EmptyState
        icon={Server}
        title="No hosts yet"
        description="A host is a machine running the Zoomies agent, and it is where runners are created. The controller can run one itself, or you can enrol another."
      >
        {#if canAdmin}
          <Button variant="primary" icon={Plus} href="/hosts/new">Add a host</Button>
        {:else}
          <p class="need-admin">An administrator can enrol one.</p>
        {/if}
      </EmptyState>
    {/snippet}

    <div class="grid">
      {#each hosts as host (host.id)}
        <HostCard
          {host}
          {canOperate}
          {canAdmin}
          oncordon={(target, next) => void cordon(target, next)}
          oncapacity={size}
          onedit={edit}
          ondelete={remove}
        />
      {/each}
    </div>
  </LoadingBoundary>

  {#if canAdmin}
    <section class="panel" aria-labelledby="join-tokens-heading">
      <!--
        No button of its own. "Add a host" is already the page's primary
        action, in the header, and two of the same button on one screen is a
        question about which one is the real one rather than a convenience.
      -->
      <header>
        <div>
          <h2 id="join-tokens-heading">Join tokens</h2>
          <p>
            Each one enrols a single host, then is spent. Adding a host mints one; revoke any that
            were minted and never used.
          </p>
        </div>
      </header>
      <div class="panel-body">
        <LoadingBoundary
          loading={tokensLoading && tokens.length === 0}
          error={tokensError}
          onretry={() => (tokensReload += 1)}
        >
          {#snippet skeleton()}
            <Skeleton lines={3} />
          {/snippet}
          <JoinTokenList {tokens} onrevoked={() => (tokensReload += 1)} />
        </LoadingBoundary>
      </div>
    </section>
  {/if}
</div>

<HostCapacityDialog bind:open={sizeOpen} host={sizing} onclose={() => (sizing = null)} />

<HostEditDialog bind:open={editOpen} host={editing} onclose={() => (editing = null)} />
<HostDeleteDialog bind:open={deleteOpen} host={deleting} onclose={() => (deleting = null)} />

<style>
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-6);
  }
  .summary {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: var(--z-space-4);
    align-items: start;
  }
  .card-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .panel {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .panel header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-4);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .panel header p {
    margin: var(--z-space-1) 0 0;
    max-width: 70ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .panel-body {
    padding: var(--z-space-3) 0;
  }
  .need-admin {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  @media (max-width: 768px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }
</style>
