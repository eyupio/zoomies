<!--
  Kennel Club: how the repositories this fleet serves measure up against what
  affects CI and the fleet.

  The states are written before the happy path, because most people meet this
  page off, or empty, or part-read, and a page that is only good when
  everything is on and read is a page that explains nothing the first time:

    off        what it does, what it reads and what it never does
    empty      on, with nothing looked at yet
    partial    the counts are a minimum, and the page says so above them
    attention  the repositories to open first, and the checks behind them
    best       the repositories with nothing open

  The answer comes first. A band says what needs attention and what is clean,
  the full counts follow as a compact strip, and the depth is below it:
  the checks with nothing open and the sources that were read are one press
  away, not another row of equal cards.

  It works with Kennel Club off, because GET /kennel answers 200 with
  enabled:false: the page needs a document to explain itself from, and "off" is
  a state it describes, not a failure it reports.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { Trophy } from '@lucide/svelte';
  import PageBand from '$lib/bands/PageBand.svelte';
  import { kennelLine } from '$lib/bands/lines';
  import type { BandStat } from '$lib/bands/stats';
  import { getKennelOverview, listKennelChecks } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { KennelCatalogueEntry, KennelOverview } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import ChecksTable from '$lib/kennel/ChecksTable.svelte';
  import KennelPageNav from '$lib/kennel/KennelPageNav.svelte';
  import KennelShell from '$lib/kennel/KennelShell.svelte';
  import KennelTurnOn from '$lib/kennel/KennelTurnOn.svelte';
  import { kennelListHref, type KennelListFilter } from '$lib/kennel/pages';
  import {
    coverageStatesText,
    countsAreAFloor,
    floorSentence,
    openFindingsText,
    worstCoverageState,
    worstSeverity,
  } from '$lib/kennel/words';
  import { capitalise, formatNumber } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { kennelClub } from '$lib/state/kennel.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import { kennelCoverageStatus, kennelStatus } from '$lib/status';

  let overview = $state<KennelOverview | null>(null);
  let catalogue = $state<KennelCatalogueEntry[]>([]);
  let loading = $state(true);
  let loaded = $state(false);
  let error = $state<unknown>(null);
  /** A refresh failed after a good load: the page keeps what it had and says so. */
  let stale = $state(false);
  let reload = $state(0);
  // How many summaries the stream has delivered. A read is answered for the moment
  // it was made, so one already out when a summary arrives is the older of the two
  // and must not replace it: nothing would ever put the newer one back.
  let summaries = 0;

  const canAdmin = $derived(session.can('admin'));
  const enabled = $derived(overview?.enabled === true);

  $effect(() => {
    void reload;
    // Turned on or off from the rail, here or in another tab: ask again, so the
    // page does not wait for a frame that a stream which is down cannot send.
    void kennelClub.epoch;
    const asked = summaries;
    const controller = new AbortController();
    loading = true;
    void getKennelOverview(controller.signal)
      .then((next) => {
        if (summaries !== asked) return;
        overview = next;
        error = null;
        stale = false;
        loaded = true;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        if (untrack(() => loaded)) stale = true;
        else error = cause;
      })
      .finally(() => (loading = false));
    return () => controller.abort();
  });

  // What is checked is what the Off page explains itself with. It is the
  // registry the evaluator runs, so the page and the checks cannot disagree.
  $effect(() => {
    if (!loaded || enabled) return;
    const controller = new AbortController();
    void listKennelChecks(controller.signal)
      .then((next) => (catalogue = next.items ?? []))
      .catch(() => {
        // The explanation stands without the list.
      });
    return () => controller.abort();
  });

  // The summary is computed and sent when it changes, since nothing writes a row
  // when an evaluation grows older. A frame replaces the document whole.
  $effect(() =>
    events.subscribe('kennel.summary', (next) => {
      summaries += 1;
      overview = next;
      error = null;
      stale = false;
      loaded = true;
    }),
  );

  // A page-local subscription does nothing across a reconnect on its own, so
  // this one asks again when the stream says it lost its place and when it comes
  // back after being down.
  $effect(() => events.subscribe('resync', () => (reload += 1)));
  let previous = '';
  $effect(() => {
    const next = fleet.connection;
    if (next === 'live' && previous && previous !== 'live') untrack(() => (reload += 1));
    previous = next;
  });

  const floor = $derived(overview ? countsAreAFloor(overview) : false);
  const floorText = $derived(overview ? floorSentence(overview) : '');
  const bestWord = $derived(kennelStatus('best_in_show', undefined, prefs.quirkyStatus).label);
  const firstAttention = $derived(overview?.attention[0]);

  const bandLine = $derived.by(() => {
    if (!loaded || !overview) return null;
    const first = firstAttention;
    return kennelLine({
      enabled: overview.enabled,
      repositories: overview.repositories,
      notTracked: overview.not_tracked,
      attention: overview.states.attention,
      clean: overview.states.best_in_show,
      first: first
        ? {
            name: first.name,
            findings: openFindingsText(first.counts),
            public: first.visibility === 'public',
          }
        : null,
      more: Math.max(0, overview.states.attention - overview.attention.length),
    });
  });
  // The band counts repositories, so its links land on the repositories list
  // rather than at the top of the page that also holds the summary.
  const toRepositories = (filter: KennelListFilter) =>
    `${kennelListHref(filter)}#kennel-repositories`;
  const bandStats = $derived.by<BandStat[]>(() => {
    if (!overview || !overview.enabled || overview.repositories === 0) return [];
    const partly = overview.states.partial + overview.states.pending;
    return [
      {
        label: bestWord,
        value: formatNumber(overview.states.best_in_show),
        hint: 'Nothing worse than a note is open.',
        href:
          overview.states.best_in_show > 0
            ? toRepositories({ state: 'best_in_show', everything: true })
            : undefined,
      },
      {
        label: 'Partly checked',
        value: formatNumber(partly),
        hint: 'Something could not be read, or not looked at yet.',
        href: partly > 0 ? toRepositories({ incomplete: true, everything: true }) : undefined,
      },
      {
        label: 'Repositories',
        value: formatNumber(overview.repositories),
        hint: overview.scope === 'served' ? 'This fleet has run jobs for.' : 'The App can see.',
        href: toRepositories({ everything: true }),
      },
    ];
  });
  const bandMore = $derived(
    overview && overview.enabled && overview.states.attention > 0
      ? {
          label: 'See all findings',
          href: toRepositories({ state: 'attention', everything: true }),
        }
      : undefined,
  );

  // A card opens the repositories it counts, and only when it counts some: a link
  // to an empty list is a click that finds nothing. "Partly checked" adds two
  // standings, which the list filters by as one; "Waived" counts findings, and opens
  // the repositories that have them, as "Errors" does for errors.
  const opens = (count: number, filter?: KennelListFilter) =>
    count > 0 ? { href: kennelListHref({ ...filter, everything: true }) } : {};

  const metrics = $derived.by(() => {
    if (!overview) return [];
    const { states, counts } = overview;
    const atLeast = floor ? 'At least. ' : '';
    return [
      {
        label: 'Repositories',
        value: formatNumber(overview.repositories),
        detail: overview.scope === 'served' ? 'This fleet has run jobs for' : 'The App can see',
        ...opens(overview.repositories),
      },
      // Only when there are some: a card of zero for a feature most fleets never use
      // is a number to learn to ignore. It opens the ones Kennel Club was told not to
      // look at, which the list leaves out unless asked.
      ...(overview.not_tracked > 0
        ? [
            {
              label: 'Not tracked',
              value: formatNumber(overview.not_tracked),
              detail: 'Kennel Club was told not to look at these',
              ...opens(overview.not_tracked, { notTracked: true }),
            },
          ]
        : []),
      {
        label: bestWord,
        value: formatNumber(states.best_in_show),
        detail: 'Nothing worse than a note is open',
        tone: 'success' as const,
        ...opens(states.best_in_show, { state: 'best_in_show' }),
      },
      {
        label: 'Need attention',
        value: formatNumber(states.attention),
        detail: `${atLeast}An error or a warning is open`,
        tone: states.attention > 0 ? ('danger' as const) : ('neutral' as const),
        ...opens(states.attention, { state: 'attention' }),
      },
      {
        label: 'Partly checked',
        value: formatNumber(states.partial + states.pending),
        detail: 'Something could not be read, or not looked at yet',
        ...opens(states.partial + states.pending, { incomplete: true }),
      },
      {
        label: 'Errors',
        value: formatNumber(counts.error),
        detail: `${atLeast}Open across every repository`,
        tone: counts.error > 0 ? ('danger' as const) : ('neutral' as const),
        ...opens(counts.error, { severity: 'error' }),
      },
      {
        label: 'Warnings',
        value: formatNumber(counts.warning),
        detail: `${atLeast}Open across every repository`,
        tone: counts.warning > 0 ? ('warning' as const) : ('neutral' as const),
        ...opens(counts.warning, { severity: 'warning' }),
      },
      {
        label: 'Waived',
        value: formatNumber(counts.waived),
        detail: 'Somebody decided these are acceptable',
        ...opens(counts.waived, { waived: true }),
      },
    ];
  });

  // What is affecting a repository now leads; a check with nothing open is still
  // checked, and folds away so the two are not read as equal.
  const affectedChecks = $derived(
    (overview?.checks ?? []).filter((check) => check.repositories > 0),
  );
  // A check somebody turned off has nothing open because it never ran. It is
  // kept in its own fold, so "nothing open" and "not running" cannot be read as
  // the same sentence.
  const quietChecks = $derived(
    (overview?.checks ?? []).filter((check) => check.repositories === 0 && !check.disabled),
  );
  const offChecks = $derived((overview?.checks ?? []).filter((check) => check.disabled));

  // The worst state across every source, for the confidence badge and the bar.
  const coverageStates = $derived.by(() => {
    const merged: Record<string, number> = {};
    for (const source of overview?.coverage ?? []) {
      for (const [state, count] of Object.entries(source.states)) {
        merged[state] = (merged[state] ?? 0) + count;
      }
    }
    return merged;
  });
  const coverageWorst = $derived(worstCoverageState(coverageStates));
  const coverageStatus = $derived(kennelCoverageStatus(coverageWorst ?? 'not_read'));
  const sourcesRead = $derived(
    (overview?.coverage ?? []).filter((source) => worstCoverageState(source.states) === 'ok')
      .length,
  );

  const navItems = $derived([
    { id: 'summary', label: 'Summary' },
    { id: 'attention', label: 'Attention', count: overview?.states.attention },
    { id: 'checks', label: 'Checks', count: affectedChecks.length },
    { id: 'confidence', label: 'Read confidence' },
  ]);
</script>

<KennelShell current="overview">
  <PageHeader
    title="Kennel Club"
    subtitle="How the repositories this fleet serves measure up against what affects CI and the fleet."
    onrefresh={() => {
      reload += 1;
    }}
  >
    {#snippet meta()}
      {#if enabled && overview?.oldest_evaluation}
        <p class="summary">
          Oldest evaluation <RelativeTime value={overview.oldest_evaluation} />
        </p>
      {/if}
    {/snippet}
  </PageHeader>

  <section id="summary" class="anchor" aria-label="Summary">
    <PageBand
      page="kennel"
      line={bandLine}
      stats={bandStats}
      more={bandMore}
      eyebrow={overview?.enabled && overview.states.attention > 0 ? 'Needs attention' : 'Right now'}
    />
  </section>

  <div class="content">
    <LoadingBoundary
      class="sections"
      loading={loading && !loaded}
      error={loaded ? null : error}
      onretry={() => (reload += 1)}
    >
      {#snippet skeleton()}
        <div class="tiles">
          {#each [0, 1, 2, 3] as tile (tile)}
            <div class="tile-skeleton">
              <Skeleton width="50%" height="0.875rem" />
              <Skeleton width="30%" height="1.75rem" />
            </div>
          {/each}
        </div>
        <Skeleton lines={5} />
      {/snippet}

      {#if overview}
        {#if stale}
          <div class="notice" role="status">
            <p>
              The last refresh did not get through, so this is what was last received.
              <Button size="sm" variant="ghost" onclick={() => (reload += 1)}>Try again</Button>
            </p>
          </div>
        {/if}

        {#if !overview.enabled}
          <Panel title="Kennel Club is off">
            <div class="explain">
              <p>
                Kennel Club looks at the repositories your fleet has run jobs for and says which of
                them put the fleet at risk, or make it work harder than it should: a public
                repository whose pull requests run on a machine that keeps state between jobs, or a
                label no pool serves.
              </p>
              <h3>What it reads</h3>
              <ul>
                <li>
                  GitHub's own record of each repository: its name and whether it is public. Nothing
                  more than the App already has permission to see.
                </li>
                <li>
                  For a public repository this fleet has run jobs for, how each recent workflow run
                  was started and from where. Private repositories cost no request beyond the shared
                  list.
                </li>
                <li>Zoomies' own record of its jobs and pools.</li>
              </ul>
              <h3>What it never does</h3>
              <ul>
                <li>Read file contents unless their checks are explicitly enabled.</li>
                <li>Change repository files without a reviewed pull request.</li>
                <li>
                  Spend more than its share of GitHub's request limit, or read at all while an
                  installation is held for a rate limit.
                </li>
              </ul>
              {#if canAdmin}
                <p><KennelTurnOn /></p>
              {:else}
                <p class="note">
                  An administrator can turn it on, with the switch beside this page.
                </p>
              {/if}
            </div>
          </Panel>

          {#if catalogue.length > 0}
            <Panel title="What it checks" flush>
              <ChecksTable rows={catalogue} label="What Kennel Club checks" />
            </Panel>
          {/if}
        {:else if overview.repositories === 0}
          <!-- Not tracked is a choice, not an absence: say so, and where they are. -->
          {#if overview.not_tracked > 0}
            <EmptyState
              icon={Trophy}
              title="Kennel Club is not looking at any repository"
              description="{overview.not_tracked === 1
                ? 'The one repository it has'
                : `All ${overview.not_tracked} repositories it has`} been told not to look at."
            >
              <Button href={kennelListHref({ notTracked: true, everything: true })}>
                See {overview.not_tracked === 1 ? 'it' : 'them'}
              </Button>
            </EmptyState>
          {:else}
            <EmptyState
              icon={Trophy}
              title="Nothing to look at yet"
              description="Kennel Club looks at repositories your fleet has run jobs for. None yet."
            />
          {/if}
          <Panel title="What it checks" flush>
            <ChecksTable rows={overview.checks} label="What Kennel Club checks" />
          </Panel>
        {:else}
          {#if floorText}
            <p class="floor" role="note">{floorText}</p>
          {/if}

          <KennelPageNav items={navItems} />

          <section class="all-counts" aria-labelledby="counts-heading">
            <h2 id="counts-heading" class="section-title">All counts</h2>
            <MetricGrid items={metrics} compact />
          </section>

          {#each overview.unavailable as note (note.installation_id)}
            {@const status = kennelCoverageStatus(note.state)}
            <div class="notice" role="note">
              <p>
                <Badge {status} size="sm" />
                <strong>{note.target}</strong>: {note.reason}
                <span class="since">since <RelativeTime value={note.since} plain /></span>
              </p>
            </div>
          {/each}

          <div class="overview-grid">
            <div class="stack">
              <div id="attention" class="anchor">
                <Panel
                  title="Needs attention"
                  description="The repositories with the worst findings open, worst first."
                  flush
                >
                  {#snippet actions()}
                    <a href={kennelListHref({ state: 'attention', everything: true })}>See all</a>
                  {/snippet}
                  {#if overview.attention.length === 0}
                    <EmptyState
                      compact
                      title="Nothing needs attention"
                      description="No repository has an error or a warning open."
                    />
                  {:else}
                    <ul class="rows">
                      {#each overview.attention as row (row.id)}
                        <li>
                          <a href="/kennel/repositories/{encodeURIComponent(row.id)}/ci">
                            <span class="name">{row.name}</span>
                            <Badge
                              status={kennelStatus(
                                row.state,
                                worstSeverity(row.counts),
                                prefs.quirkyStatus,
                              )}
                              size="sm"
                            />
                            {#if row.visibility}
                              <Badge
                                tone="neutral"
                                label={capitalise(row.visibility)}
                                size="sm"
                                dot={false}
                              />
                            {/if}
                            <span class="counts">{openFindingsText(row.counts)}</span>
                          </a>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </Panel>
              </div>

              <div id="checks" class="anchor">
                <Panel
                  title="What is open by check"
                  description="Which checks are affecting a repository right now. Everything else is quiet."
                  flush
                >
                  {#snippet actions()}
                    <Badge
                      tone="neutral"
                      label="{affectedChecks.length} affected of {overview?.checks.length ?? 0}"
                      size="sm"
                      dot={false}
                    />
                  {/snippet}
                  {#if affectedChecks.length === 0}
                    <EmptyState
                      compact
                      title="No check is affecting a repository"
                      description="Every check that is turned on ran, and nothing is open."
                    />
                  {:else}
                    <ChecksTable
                      rows={affectedChecks}
                      label="Checks and the repositories that have each open"
                    />
                  {/if}
                  {#if quietChecks.length > 0}
                    <details class="quiet-checks">
                      <summary>
                        Show {quietChecks.length} checks with nothing open
                      </summary>
                      <ChecksTable rows={quietChecks} label="Checks with nothing open" />
                    </details>
                  {/if}
                  {#if offChecks.length > 0}
                    <details class="quiet-checks">
                      <summary>
                        Show {offChecks.length} checks turned off
                      </summary>
                      <ChecksTable rows={offChecks} label="Checks turned off" />
                    </details>
                  {/if}
                  <div class="panel-foot">
                    A check with nothing open is still checked. The number is repositories, not
                    findings.
                  </div>
                </Panel>
              </div>
            </div>

            <div class="stack">
              <div id="confidence" class="anchor">
                <Panel
                  title="What Kennel Club can see"
                  description="Each source of facts, and how far it could be read for the repositories above."
                >
                  {#snippet actions()}
                    <Badge status={coverageStatus} size="sm" />
                  {/snippet}
                  {#if overview.coverage.length === 0}
                    <p class="note">Nothing has been read yet.</p>
                  {:else}
                    <p class="coverage-summary">
                      {sourcesRead} of {overview.coverage.length} sources read
                      {coverageWorst === 'ok' ? 'in full' : 'for these repositories'}.
                    </p>
                    <div class="coverage-bar" aria-hidden="true">
                      {#each overview.coverage as source (source.source)}
                        {@const worst = worstCoverageState(source.states)}
                        <span data-state={worst ?? 'not_read'}></span>
                      {/each}
                    </div>
                    <ul class="coverage">
                      {#each overview.coverage as source (source.source)}
                        {@const worst = worstCoverageState(source.states)}
                        <li>
                          <div class="head">
                            <strong>{source.label}</strong>
                            <Badge status={kennelCoverageStatus(worst ?? 'not_read')} size="sm" />
                          </div>
                          <p>{coverageStatesText(source.states)}</p>
                          {#if source.permission && worst !== 'ok'}
                            <p class="permission">
                              Needs the App's <strong>{source.permission}</strong> permission.
                              {#if canAdmin}<a href="/installations">Open installations</a>{/if}
                            </p>
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </Panel>
              </div>

              <Panel title="Scope" description="What the numbers above cover.">
                <dl class="scope">
                  <div>
                    <dt>Scope</dt>
                    <dd>
                      {overview.scope === 'served'
                        ? 'Repositories this fleet has run jobs for'
                        : 'Every repository the App can see'}
                    </dd>
                  </div>
                  <div>
                    <dt>Repositories</dt>
                    <dd>{formatNumber(overview.repositories)} tracked</dd>
                  </div>
                  {#if overview.not_tracked > 0}
                    <div>
                      <dt>Not tracked</dt>
                      <dd>{formatNumber(overview.not_tracked)}</dd>
                    </div>
                  {/if}
                  <div>
                    <dt>List</dt>
                    <dd><a href="/kennel/repositories?active=all">Open repositories</a></dd>
                  </div>
                </dl>
              </Panel>
            </div>
          </div>
        {/if}
      {/if}
    </LoadingBoundary>
  </div>
</KennelShell>

<style>
  /* LoadingBoundary puts what it holds in a div of its own, so the gap has to be on that div. */
  .content :global(.sections) {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  .summary {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(155px, 1fr));
    gap: var(--z-space-3);
  }
  .tile-skeleton {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .floor {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .notice {
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-md);
    background: var(--z-accent-subtle);
    color: var(--z-text);
    font-size: var(--z-text-sm);
  }
  .notice p {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
  }
  .since {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .explain {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    max-width: 70ch;
  }
  .explain p,
  .explain ul {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
  }
  .explain ul {
    padding-left: var(--z-space-5);
    list-style: disc;
  }
  .explain h3 {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .note {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-subtle);
  }

  /* In-page targets clear the sticky bar above them. */
  .anchor {
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-space-4));
  }

  .section-title {
    margin: 0 0 var(--z-space-2);
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
  }
  .overview-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.55fr) minmax(18rem, 0.9fr);
    gap: var(--z-space-4);
    align-items: start;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .rows li + li {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .rows a {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-5);
    color: var(--z-text);
    text-decoration: none;
  }
  .rows a:hover {
    background: var(--z-surface-hover);
  }
  .name {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    overflow-wrap: anywhere;
  }
  .counts {
    margin-left: auto;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .quiet-checks {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .quiet-checks summary {
    padding: var(--z-space-3) var(--z-space-5);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    cursor: pointer;
  }
  .quiet-checks summary:hover {
    color: var(--z-text);
  }
  .panel-foot {
    padding: var(--z-space-3) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
  .coverage-summary {
    margin: 0 0 var(--z-space-2);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .coverage-bar {
    display: flex;
    gap: var(--z-space-1);
    margin-bottom: var(--z-space-4);
  }
  .coverage-bar span {
    flex: 1;
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: var(--z-border-strong);
  }
  .coverage-bar span[data-state='ok'] {
    background: var(--z-idle);
  }
  .coverage-bar span[data-state='partial'] {
    background: var(--z-pending);
  }
  .coverage-bar span[data-state='held'] {
    background: var(--z-draining);
  }
  .coverage-bar span[data-state='denied'],
  .coverage-bar span[data-state='unavailable'] {
    background: var(--z-neutral);
  }
  .coverage-bar span[data-state='error'] {
    background: var(--z-danger);
  }
  .coverage {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .coverage li {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .coverage .head {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    flex-wrap: wrap;
  }
  .coverage p {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .permission {
    color: var(--z-text);
  }
  .scope {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    margin: 0;
  }
  .scope > div {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .scope dt {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .scope dd {
    margin: 0;
    font-size: var(--z-text-xs);
    text-align: right;
  }
  @media (max-width: 980px) {
    .overview-grid {
      grid-template-columns: 1fr;
    }
  }
  @media (max-width: 768px) {
    .counts {
      margin-left: 0;
      flex-basis: 100%;
    }
  }
</style>
