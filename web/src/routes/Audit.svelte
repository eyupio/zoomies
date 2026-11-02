<!--
  Audit: who did what.

  Every mutating request in Zoomies writes a row here, with the before and after
  documents already redacted by the server -- no secret ever reaches this page,
  which is why it can show the whole diff rather than a summary.

  Opening a row shows that diff, and only the keys that actually changed. A
  settings change that touched one field should read as one line, not as two
  pages of JSON that happen to differ somewhere in the middle.
-->
<script lang="ts">
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { ScrollText, Search } from '@lucide/svelte';
  import { listAudit, listAuditActions } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { AuditEvent } from '$lib/api/types';
  import { NO_VALUE, formatAbsolute } from '$lib/format';
  import { registerSearch } from '$lib/keys';
  import { router } from '$lib/router';
  import { fleet } from '$lib/state/fleet.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import DataGrid from '$lib/components/DataGrid.svelte';
  import type { GridColumn, GridPage, GridQuery } from '$lib/components/DataGrid.svelte';
  import Drawer from '$lib/components/Drawer.svelte';
  import FilterBar from '$lib/components/FilterBar.svelte';
  import FilterChips from '$lib/components/FilterChips.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import { pickOne } from '$lib/components/chip-selection';
  import { auditEyebrow, auditKindChips } from '$lib/audit/kind-chips';
  import type { FilterChip } from '$lib/components/FilterBar.svelte';
  import Input from '$lib/components/Input.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Select from '$lib/components/Select.svelte';
  // The date range lives with the Jobs filters; it is the same control and the
  // same conversion from a calendar day to the instants the API takes.
  import DateRange, { endOfDay, startOfDay } from '$lib/jobs/DateRange.svelte';

  /* -- filters, in the URL under the API's own names -------------------------- */

  const search = $derived(router.param('q'));
  const action = $derived(router.param('action'));
  const targetKind = $derived(router.param('target_kind'));
  const actorId = $derived(router.param('actor_id'));
  const since = $derived(router.param('since'));
  const until = $derived(router.param('until'));

  const filters = $derived({ search, action, targetKind, actorId, since, until });
  const anyFilter = $derived(Boolean(search || action || targetKind || actorId || since || until));

  let searchField = $state<HTMLInputElement | null>(null);
  $effect(() => registerSearch(searchField));

  /* -- the filter menus ------------------------------------------------------- */

  let actions = $state<string[]>([]);
  $effect(() => {
    const controller = new AbortController();
    void listAuditActions(controller.signal)
      .then((result) => (actions = result.items ?? []))
      .catch(() => {
        // The menu simply stays empty; the grid reports the outage itself.
      });
    return () => controller.abort();
  });

  /**
   * Actors and target kinds are not endpoints of their own, so they are
   * gathered from the rows that have been through this page: the actor menu
   * says so rather than pretending to be exhaustive, and the kind chips grow
   * as the operator reads.
   */
  let actors = $state<Record<string, string>>({});
  let kinds = $state<string[]>([]);

  function remember(rows: AuditEvent[]): void {
    const nextActors = { ...actors };
    const nextKinds = [...kinds];
    for (const row of rows) {
      if (row.actor_id) nextActors[row.actor_id] = row.actor_name || row.actor_id;
      if (row.target_kind && !nextKinds.includes(row.target_kind)) nextKinds.push(row.target_kind);
    }
    actors = nextActors;
    kinds = nextKinds.sort((a, b) => a.localeCompare(b));
  }

  const actionOptions = $derived([
    { value: '', label: 'Any action' },
    ...actions.map((name) => ({ value: name, label: name })),
  ]);
  const actorOptions = $derived([
    { value: '', label: 'Anyone' },
    ...Object.entries(actors).map(([id, name]) => ({ value: id, label: name })),
  ]);

  const chips = $derived<FilterChip[]>(
    [
      search && {
        id: 'q',
        label: 'Text',
        value: search,
        onremove: () => router.setQuery({ q: null, offset: null }),
      },
      action && {
        id: 'action',
        label: 'Action',
        value: action,
        onremove: () => router.setQuery({ action: null, offset: null }),
      },
      targetKind && {
        id: 'target_kind',
        label: 'Thing',
        value: targetKind,
        onremove: () => router.setQuery({ target_kind: null, offset: null }),
      },
      actorId && {
        id: 'actor_id',
        label: 'Actor',
        value: actors[actorId] ?? actorId,
        onremove: () => router.setQuery({ actor_id: null, offset: null }),
      },
      since && {
        id: 'since',
        label: 'From',
        value: since,
        onremove: () => router.setQuery({ since: null, offset: null }),
      },
      until && {
        id: 'until',
        label: 'To',
        value: until,
        onremove: () => router.setQuery({ until: null, offset: null }),
      },
    ].filter((chip): chip is FilterChip => Boolean(chip)),
  );

  function clearFilters(): void {
    router.setQuery({
      q: null,
      action: null,
      target_kind: null,
      actor_id: null,
      since: null,
      until: null,
      offset: null,
    });
  }

  /* -- rows -------------------------------------------------------------------- */

  let liveKey = $state(0);
  $effect(() => events.subscribe('audit', () => (liveKey += 1)));
  // How many events the filters leave, from the page the grid last fetched,
  // for the card's eyebrow. Absent until the first page has said.
  let total = $state<number | undefined>();

  async function fetchAudit(query: GridQuery, signal: AbortSignal): Promise<GridPage<AuditEvent>> {
    const page = await listAudit(
      {
        q: search || undefined,
        action: action ? [action] : undefined,
        target_kind: targetKind ? [targetKind] : undefined,
        actor_id: actorId ? [actorId] : undefined,
        since: startOfDay(since),
        until: endOfDay(until),
        limit: query.limit,
        offset: query.offset,
        sort: query.sort,
        order: query.order,
      },
      signal,
    );
    if (!signal.aborted) total = page.total;
    return { items: page.items ?? [], total: page.total };
  }
  // A figure over an error would be the last filter's, read as this one's.
  async function fetchCounted(
    query: GridQuery,
    signal: AbortSignal,
  ): Promise<GridPage<AuditEvent>> {
    try {
      return await fetchAudit(query, signal);
    } catch (error) {
      if (!signal.aborted) total = undefined;
      throw error;
    }
  }

  /* -- the target of an event --------------------------------------------------
   * A pool or a runner that is still in the live cache gets a link; one that has
   * been deleted since gets its id and says so, rather than a link to a page
   * that would only say "not found".
   * ------------------------------------------------------------------------ */

  function targetHref(event: AuditEvent): string | null {
    const id = event.target_id;
    if (!id) return null;
    switch (event.target_kind) {
      case 'pool':
        return fleet.pool(id) ? `/pools/${id}` : null;
      case 'runner':
        return fleet.runner(id) ? `/runners/${id}` : null;
      case 'host':
        return fleet.host(id) ? '/hosts' : null;
      case 'installation':
        return '/installations';
      case 'user':
        return '/settings/users';
      case 'token':
        return '/settings/tokens';
      case 'settings':
        return '/settings/configuration';
      case 'join_token':
        return '/hosts';
      default:
        return null;
    }
  }

  const ACTOR_TONE: Record<string, 'accent' | 'neutral' | 'busy'> = {
    user: 'accent',
    token: 'busy',
    connection: 'busy',
    client: 'neutral',
    agent: 'neutral',
    system: 'neutral',
    webhook: 'neutral',
  };

  /* -- the diff ------------------------------------------------------------------ */

  interface Change {
    path: string;
    before: string | null;
    after: string | null;
  }

  function parse(raw: string | undefined): unknown {
    if (!raw) return undefined;
    try {
      return JSON.parse(raw) as unknown;
    } catch {
      return raw;
    }
  }

  /** Flatten an object into dot paths. Arrays stay whole: an index-by-index diff of labels is noise. */
  function flatten(value: unknown, prefix: string, into: Record<string, string>): void {
    if (value === null || value === undefined) {
      into[prefix] = 'null';
      return;
    }
    if (Array.isArray(value) || typeof value !== 'object') {
      into[prefix] = JSON.stringify(value) ?? String(value);
      return;
    }
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) {
      into[prefix] = '{}';
      return;
    }
    for (const [key, child] of entries) flatten(child, prefix ? `${prefix}.${key}` : key, into);
  }

  function changesFor(event: AuditEvent | null): Change[] {
    if (!event) return [];
    const before: Record<string, string> = {};
    const after: Record<string, string> = {};
    const parsedBefore = parse(event.before);
    const parsedAfter = parse(event.after);
    if (parsedBefore !== undefined) flatten(parsedBefore, '', before);
    if (parsedAfter !== undefined) flatten(parsedAfter, '', after);

    const paths = [...Object.keys(before), ...Object.keys(after).filter((k) => !(k in before))];
    const out: Change[] = [];
    for (const path of paths.sort((a, b) => a.localeCompare(b))) {
      const was = before[path] ?? null;
      const now = after[path] ?? null;
      if (was === now) continue;
      out.push({ path, before: was, after: now });
    }
    return out;
  }

  let selected = $state<AuditEvent | null>(null);
  let drawerOpen = $state(false);
  const changes = $derived(changesFor(selected));
  const unparsed = $derived(
    selected
      ? typeof parse(selected.before) === 'string' || typeof parse(selected.after) === 'string'
      : false,
  );

  function openRow(row: AuditEvent): void {
    selected = row;
    drawerOpen = true;
  }

  function rowId(row: AuditEvent): string {
    return row.id ?? '';
  }

  const columns = $derived<GridColumn<AuditEvent>[]>([
    {
      id: 'created_at',
      header: 'When',
      sortable: true,
      width: '10rem',
      hideable: false,
      value: (row) => formatAbsolute(row.created_at),
      cell: whenCell,
    },
    {
      id: 'actor',
      header: 'Actor',
      sortable: true,
      width: '13rem',
      value: (row) => row.actor_name ?? '',
      cell: actorCell,
    },
    {
      id: 'action',
      header: 'Action',
      sortable: true,
      width: '12rem',
      value: (row) => row.action ?? '',
      cell: actionCell,
    },
    { id: 'target', header: 'Target', value: (row) => row.target_id ?? '', cell: targetCell },
    {
      id: 'ip',
      header: 'From',
      priority: 'wide',
      width: '9rem',
      value: (row) => row.ip ?? '',
      cell: ipCell,
    },
  ]);
</script>

{#snippet whenCell(row: AuditEvent)}
  <RelativeTime value={row.created_at} />
{/snippet}

{#snippet actorCell(row: AuditEvent)}
  <span class="actor">
    <span class="actor-name">{row.actor_name || 'unknown'}</span>
    {#if row.actor_kind}
      <Badge
        tone={ACTOR_TONE[row.actor_kind] ?? 'neutral'}
        label={row.actor_kind}
        size="sm"
        dot={false}
      />
    {/if}
  </span>
{/snippet}

{#snippet actionCell(row: AuditEvent)}
  <span class="mono">{row.action || NO_VALUE}</span>
{/snippet}

{#snippet targetCell(row: AuditEvent)}
  {@const href = targetHref(row)}
  <span class="target">
    {#if row.target_kind}<span class="kind">{row.target_kind}</span>{/if}
    {#if href}
      <a class="mono" {href} onclick={(event) => event.stopPropagation()}>{row.target_id}</a>
    {:else if row.target_id}
      <Tooltip bare text="This no longer exists, so there is nothing to open">
        <span class="mono gone">{row.target_id}</span>
      </Tooltip>
    {:else}
      <span class="none">{NO_VALUE}</span>
    {/if}
  </span>
{/snippet}

{#snippet ipCell(row: AuditEvent)}
  {#if row.ip}<span class="mono">{row.ip}</span>{:else}<span class="none">{NO_VALUE}</span>{/if}
{/snippet}

<PageHeader
  title="Audit"
  subtitle="Every change made through Zoomies, and who made it."
  onrefresh={() => {
    liveKey += 1;
  }}
/>

<div class="content">
  <FilterBar {chips} onclear={anyFilter ? clearFilters : undefined} class="in-effect" />

  <!--
    The list card: the log's tile, how many events the filters leave over the
    title, the search, the menus and the dates opposite, and the kinds of thing
    acted on as chips where the menu was. The grid is bare inside, so the page
    shows one frame; there is nothing between the header and the list to fold.
  -->
  <Panel title="All events" icon={ScrollText} eyebrow={auditEyebrow(total)} flush class="list-card">
    {#snippet actions()}
      <div class="search">
        <Input
          bind:element={searchField}
          value={search}
          type="search"
          size="sm"
          icon={Search}
          placeholder="Search actor, action or target"
          ariaLabel="Search the audit log"
          oninput={(event) =>
            router.setQuery({
              q: (event.currentTarget as HTMLInputElement).value || null,
              offset: null,
            })}
        />
      </div>

      <!--
        Sized: a Select is as wide as the space it is given, so bare in the
        card's wrapping row each would take a line of its own and the controls
        would stand in a column rather than read as one set of choices.
      -->
      <div class="field">
        <Select
          value={action}
          options={actionOptions}
          size="sm"
          ariaLabel="Filter by action"
          onchange={(value) => router.setQuery({ action: value || null, offset: null })}
        />
      </div>
      <div class="field">
        <Select
          value={actorId}
          options={actorOptions}
          size="sm"
          ariaLabel="Filter by actor"
          onchange={(value) => router.setQuery({ actor_id: value || null, offset: null })}
        />
      </div>
      <div class="dates">
        <DateRange
          {since}
          {until}
          label="Changed between"
          onchange={(next) =>
            router.setQuery({
              since: next.since || null,
              until: next.until || null,
              offset: null,
            })}
        />
      </div>
    {/snippet}
    {#snippet toolbar()}
      <FilterChips
        label="Filter by the kind of thing acted on"
        options={auditKindChips(kinds, targetKind)}
        selected={targetKind ? [targetKind] : []}
        onchange={(next) =>
          router.setQuery({ target_kind: pickOne(targetKind, next) || null, offset: null })}
      />
    {/snippet}
    <DataGrid
      gridId="audit"
      label="Audit log"
      {columns}
      fetcher={fetchCounted}
      {rowId}
      {filters}
      defaultSort="created_at"
      defaultOrder="desc"
      noun="events"
      bare
      {liveKey}
      onopen={openRow}
      onrows={remember}
      emptyTitle={anyFilter ? 'Nothing matches those filters' : 'Nothing recorded yet'}
      emptyDescription={anyFilter
        ? 'Try a wider date range, or clear a filter.'
        : 'Zoomies writes a row here whenever somebody changes something: a pool, a runner, a user, a setting. Reading things is not recorded.'}
    >
      {#snippet emptyAction()}
        {#if anyFilter}
          <Button onclick={clearFilters}>Clear filters</Button>
        {/if}
      {/snippet}
    </DataGrid>
  </Panel>
</div>

<Drawer
  bind:open={drawerOpen}
  title={selected?.action || 'Audit event'}
  description={selected
    ? `${selected.actor_name || 'unknown'} · ${formatAbsolute(selected.created_at)}`
    : undefined}
  width="lg"
  onclose={() => (selected = null)}
>
  {#if selected}
    <div class="detail">
      <dl class="facts">
        <dt>Actor</dt>
        <dd>
          {selected.actor_name || 'unknown'}
          {#if selected.actor_kind}<span class="muted">({selected.actor_kind})</span>{/if}
        </dd>

        <dt>Action</dt>
        <dd class="mono">{selected.action || NO_VALUE}</dd>

        <dt>Target</dt>
        <dd>
          {#if selected.target_kind}<span class="kind">{selected.target_kind}</span>{/if}
          {#if targetHref(selected)}
            <a class="mono" href={targetHref(selected)}>{selected.target_id}</a>
          {:else}
            <span class="mono">{selected.target_id || NO_VALUE}</span>
          {/if}
        </dd>

        <dt>From</dt>
        <dd class="mono">{selected.ip || 'not recorded'}</dd>

        <dt>When</dt>
        <dd>{formatAbsolute(selected.created_at)}</dd>
      </dl>

      <section aria-labelledby="diff-heading">
        <h3 id="diff-heading">What changed</h3>
        {#if unparsed}
          <p class="note">
            The recorded documents are not JSON, so they are shown as they were written.
          </p>
          {#if selected.before}<pre class="raw mono">{selected.before}</pre>{/if}
          {#if selected.after}<pre class="raw mono">{selected.after}</pre>{/if}
        {:else if changes.length === 0}
          <p class="note">
            No field differs. The action was recorded, but it left the stored values as they were.
          </p>
        {:else}
          <ul class="changes">
            {#each changes as change (change.path)}
              <li>
                <span class="path mono">{change.path || 'value'}</span>
                <span class="values">
                  {#if change.before !== null}
                    <span class="was mono"
                      ><span aria-hidden="true">−</span>
                      <span class="sr-only">was</span>
                      {change.before}</span
                    >
                  {/if}
                  {#if change.after !== null}
                    <span class="now mono"
                      ><span aria-hidden="true">+</span>
                      <span class="sr-only">now</span>
                      {change.after}</span
                    >
                  {/if}
                </span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  {/if}
</Drawer>

<style>
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  .search {
    width: 16rem;
    max-width: 100%;
  }
  .field {
    width: 11rem;
    max-width: 100%;
  }
  .dates {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
  }
  /*
    max-width, because min-width alone does not do it. An inline-flex box inside
    a block is as wide as its content's minimum, and a name that will not wrap
    has a minimum of its whole length, so the ellipsis below never had anything
    to cut and a long host name -- an agent is the actor of its own join --
    pushed a phone's card past the screen. Capped at its cell, the name shrinks.
  */
  .actor {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
    max-width: 100%;
  }
  .actor-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .target {
    display: inline-flex;
    align-items: baseline;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .kind {
    padding: 0 var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
  }
  .gone {
    color: var(--z-text-subtle);
    text-decoration: line-through;
  }
  .none {
    color: var(--z-text-subtle);
  }
  a {
    color: var(--z-accent);
  }
  .detail {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-5);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(0, 7rem) minmax(0, 1fr);
    gap: var(--z-space-2) var(--z-space-4);
    margin: 0;
    font-size: var(--z-text-base);
  }
  dt {
    color: var(--z-text-muted);
  }
  dd {
    margin: 0;
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .muted {
    color: var(--z-text-subtle);
  }
  h3 {
    margin: 0 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .note {
    margin: 0;
    max-width: 74ch;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
  .changes {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .changes li {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
  }
  .path {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .values {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
  }
  .was,
  .now {
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    overflow-wrap: anywhere;
  }
  .was {
    color: var(--z-danger);
  }
  .now {
    color: var(--z-idle);
  }
  .raw {
    margin: var(--z-space-2) 0 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-xs);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
