<!--
  One repository: what Kennel Club concluded about it, why, and how far it could
  see.

  Three sections, named for the three jobs a reader has: the findings, what the
  fleet knows about the repository, and its AI Context. Protection and agent
  guidance are kinds of finding, not separate destinations; their own addresses
  open the findings area they belong to, so a link written before the merge still
  lands on what it named.

  A repository is never shown as all clear on a read that was not whole. When
  something could not be read, the page says what and why in place of an empty
  list of findings, and a clean "Best in show" appears only when every check that
  is turned on ran against everything it needs.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { EyeOff, RotateCw, ShieldCheck, Trophy, Undo2 } from '@lucide/svelte';
  import {
    ApiError,
    getKennelRepository,
    recheckKennelRepository,
    unwaiveKennelFinding,
  } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { KennelFinding, KennelRepository, KennelWaived } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Tabs from '$lib/components/Tabs.svelte';
  import KennelShell from '$lib/kennel/KennelShell.svelte';
  import { kennelClub } from '$lib/state/kennel.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import Finding from '$lib/kennel/Finding.svelte';
  import { kennelTimeoutFixHref } from '$lib/kennel/pages';
  import { isProtectionCheck } from '$lib/kennel/protection';
  import RepositoryOverview from '$lib/kennel/RepositoryOverview.svelte';
  import RepositoryAgentGuidance from '$lib/kennel/RepositoryAgentGuidance.svelte';
  import RepositoryAiContext from '$lib/kennel/RepositoryAiContext.svelte';
  import RepositoryProtection from '$lib/kennel/RepositoryProtection.svelte';
  import TrackSwitch from '$lib/kennel/TrackSwitch.svelte';
  import WaiveDialog from '$lib/kennel/WaiveDialog.svelte';
  import {
    ERROR_WAIVER_SENTENCE,
    openFindingsText,
    timeoutFixTargets,
    scoreBasis,
    scoreText,
    waiverRole,
    worstSeverity,
  } from '$lib/kennel/words';
  import { capitalise, formatAbsolute } from '$lib/format';
  import { router } from '$lib/router';
  import { fleet } from '$lib/state/fleet.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import {
    kennelCoverageStatus,
    kennelStatus,
    kennelTrackingStatus,
    severityStatus,
  } from '$lib/status';

  const id = $derived(router.params.id ?? '');

  let repo = $state<KennelRepository | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let gone = $state(false);
  let reload = $state(0);
  let showing = '';
  // How many frames for this page's repository have been applied. A read is
  // answered for the moment it was made, so one that was already out when a frame
  // arrived must not replace what the frame set: it is the older of the two, and
  // nothing would ever put the newer back.
  let frames = 0;

  /**
   * The sections, by address: `/kennel/repositories/:id/:section?`. A path
   * segment and not a query, so back and forward move between them and a section
   * has a link of its own.
   *
   * Findings is the default for a repository with something open. A clean
   * repository opens on what the fleet knows, because there is nothing to read
   * in a findings list of zero. A section for a stage that has not shipped is not
   * here, and an address that names one gets the default.
   */
  const TABS = [
    { id: 'findings', label: 'Findings' },
    { id: 'fleet', label: 'Fleet activity' },
    { id: 'ai-context', label: 'AI Context' },
  ] as const;
  type TabId = (typeof TABS)[number]['id'];
  type AreaId = 'all' | 'workflow' | 'protection' | 'guidance';

  // Every address a repository page has ever answered to, plus the three areas
  // that used to be tabs of their own.
  const SEGMENTS = new Set([
    '',
    'ci',
    'workflow',
    'protection',
    'agent-guidance',
    'overview',
    'ai-context',
  ]);
  const requested = $derived(router.params.tab ?? '');
  const base = $derived(`/kennel/repositories/${encodeURIComponent(id)}`);

  const area = $derived<AreaId>(
    requested === 'protection'
      ? 'protection'
      : requested === 'agent-guidance'
        ? 'guidance'
        : requested === 'workflow'
          ? 'workflow'
          : 'all',
  );

  const tab = $derived<TabId>(
    requested === 'overview'
      ? 'fleet'
      : requested === 'ai-context'
        ? 'ai-context'
        : requested === '' && repo && repo.findings.length === 0
          ? 'fleet'
          : 'findings',
  );

  const tabs = $derived(
    TABS.map((entry) =>
      entry.id === 'findings' && repo
        ? { ...entry, badge: repo.findings.length || undefined }
        : entry,
    ),
  );

  function openTab(next: string): void {
    if (next === 'fleet') router.navigate(`${base}/overview`);
    else if (next === 'ai-context') router.navigate(`${base}/ai-context`);
    else router.navigate(`${base}/ci`);
  }

  function openArea(next: AreaId): void {
    if (next === 'all') router.navigate(base);
    else if (next === 'protection') router.navigate(`${base}/protection`);
    else if (next === 'guidance') router.navigate(`${base}/agent-guidance`);
    else router.navigate(`${base}/workflow`);
  }

  $effect(() => {
    if (requested && !SEGMENTS.has(requested)) router.navigate(base, { replace: true });
  });

  $effect(() => {
    void reload;
    // Turned on or off from the rail, here or in another tab: ask again.
    void kennelClub.epoch;
    const current = id;
    const asked = frames;
    // The page is not keyed, so a different repository is the same component
    // with a different id: what was showing must not outlive the change.
    if (untrack(() => showing) !== current) {
      showing = current;
      repo = null;
      error = null;
      gone = false;
    }
    const controller = new AbortController();
    loading = true;
    void getKennelRepository(current, controller.signal)
      .then((next) => {
        if (frames !== asked) return;
        repo = next;
        error = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        // Kennel Club was turned off while this page was open. What it showed is
        // not so now, and the page says why, as it would have on arrival; any other
        // failure keeps what is held, since a blip should not blank the page.
        if (cause instanceof ApiError && cause.isConflict) repo = null;
        error = cause;
      })
      .finally(() => (loading = false));
    return () => controller.abort();
  });

  $effect(() => {
    if (repo) router.setTitle(repo.name);
  });

  // Anything that changed this repository is announced in the shape this page
  // read it in, so a frame replaces what is held and is never merged into it.
  $effect(() =>
    events.subscribe('kennel.updated', (row) => {
      if (row.id === id) {
        frames += 1;
        repo = row;
        error = null;
      }
    }),
  );
  $effect(() =>
    events.subscribe('kennel.deleted', (row) => {
      if (row.id === id) gone = true;
    }),
  );
  $effect(() => events.subscribe('resync', () => (reload += 1)));
  let previous = '';
  $effect(() => {
    const next = fleet.connection;
    if (next === 'live' && previous && previous !== 'live') untrack(() => (reload += 1));
    previous = next;
  });

  /* -- recheck ---------------------------------------------------------------- */

  let rechecking = $state(false);

  async function recheck(): Promise<void> {
    if (!repo) return;
    rechecking = true;
    try {
      repo = await recheckKennelRepository(repo.id);
      toasts.success(
        'Recheck requested',
        'The reads happen when the request budget and the installation allow.',
      );
    } catch (cause) {
      // The cooldown and the "Kennel Club is off" answers carry their own
      // sentence, which says when to try again or where to turn it on.
      toasts.fromError(cause, 'That repository was not rechecked');
    } finally {
      rechecking = false;
    }
  }

  /* -- waiving and ending a waiver --------------------------------------------- */

  let waiveOpen = $state(false);
  let waiving = $state<KennelFinding | null>(null);

  function startWaive(finding: KennelFinding): void {
    waiving = finding;
    waiveOpen = true;
  }

  let endOpen = $state(false);
  let ending = $state<KennelWaived | null>(null);

  function startEnd(entry: KennelWaived): void {
    ending = entry;
    endOpen = true;
  }

  async function endWaiver(): Promise<boolean> {
    const entry = ending;
    if (!repo || !entry) return false;
    try {
      repo = await unwaiveKennelFinding(repo.id, entry.waiver.id);
      toasts.success('Waiver ended', 'The finding is open again.');
      return true;
    } catch (cause) {
      toasts.fromError(cause, 'That waiver was not ended');
      return false;
    }
  }

  /* -- what to say ------------------------------------------------------------- */

  // Not one of the four standings: those are what the evaluator concluded, and
  // nothing is evaluated for a repository Kennel Club has been told not to look at.
  const status = $derived(
    repo
      ? repo.tracking.tracked
        ? kennelStatus(repo.state, worstSeverity(repo.counts), prefs.quirkyStatus)
        : kennelTrackingStatus()
      : null,
  );

  /** Whether there is a reason this is not a clean read, to be said in place of "nothing wrong". */
  const incomplete = $derived(repo ? !repo.complete || repo.state === 'partial' : false);
  const unread = $derived((repo?.coverage ?? []).filter((source) => source.state !== 'ok'));

  /** Which kind of finding this is, for the area row and the old section addresses. */
  function areaOf(code: string): Exclude<AreaId, 'all'> {
    if (isProtectionCheck(code)) return 'protection';
    if (code.startsWith('guidance.')) return 'guidance';
    return 'workflow';
  }

  const areaCounts = $derived.by(() => {
    const counts = { all: 0, workflow: 0, protection: 0, guidance: 0 };
    for (const finding of repo?.findings ?? []) {
      counts.all += 1;
      counts[areaOf(finding.code)] += 1;
    }
    return counts;
  });

  const visibleFindings = $derived(
    area === 'all'
      ? (repo?.findings ?? [])
      : (repo?.findings ?? []).filter((finding) => areaOf(finding.code) === area),
  );

  const areas = $derived([
    { id: 'all' as const, label: 'All', count: areaCounts.all },
    { id: 'workflow' as const, label: 'Workflow', count: areaCounts.workflow },
    { id: 'protection' as const, label: 'Protection', count: areaCounts.protection },
    { id: 'guidance' as const, label: 'Agent guidance', count: areaCounts.guidance },
  ]);
</script>

<KennelShell current="repositories">
  <PageHeader
    title={repo?.name ?? 'Repository'}
    subtitle={repo ? undefined : 'What Kennel Club concluded about one repository.'}
    onrefresh={() => {
      reload += 1;
    }}
  >
    {#snippet meta()}
      {#if repo && status}
        <p class="meta">
          <Badge {status} size="sm" />
          {#if repo.visibility}
            <Badge tone="neutral" label={capitalise(repo.visibility)} size="sm" dot={false} />
          {/if}
          <span>
            {#if !repo.tracking.tracked && repo.tracking.since}
              Since <RelativeTime value={repo.tracking.since} />
            {:else if repo.evaluated_at}
              Evaluated <RelativeTime value={repo.evaluated_at} />
            {:else}
              Not evaluated yet
            {/if}
          </span>
        </p>
      {/if}
      {#if repo && repo.score}
        <p class="score">
          <span class="score-value">Score {scoreText(repo.score)}</span>
          <span class="score-basis">{scoreBasis(repo.score)}</span>
        </p>
      {/if}
      {#if repo}
        <!-- A constant, because a snippet does not keep what the branch learned about `repo`. -->
        {@const current = repo}
        <TrackSwitch repo={current} onchange={(next) => (repo = next)} />
      {/if}
    {/snippet}
    {#if repo && repo.tracking.tracked && session.can('operator')}
      <Button icon={RotateCw} loading={rechecking} onclick={() => void recheck()}>Recheck</Button>
    {/if}
  </PageHeader>

  <div class="content">
    {#if gone}
      <EmptyState
        icon={Trophy}
        title="Kennel Club has let this repository go"
        description="It has not had a job served by this fleet for long enough, so Kennel Club let it go. It comes back if one runs again."
      >
        <Button href="/kennel/repositories">All repositories</Button>
      </EmptyState>
    {:else if loading && !repo}
      <Skeleton lines={8} />
    {:else if error && !repo}
      <ErrorState {error} title="That repository could not be read" onretry={() => (reload += 1)} />
    {:else if repo}
      <!-- A constant, because a snippet does not keep what the branch learned about `repo`. -->
      {@const current = repo}
      <!-- AI Context is its own thing and goes on whether or not Kennel Club is looking. -->
      {#if !current.tracking.tracked && tab !== 'ai-context'}
        <div class="notice" role="note" data-testid="not-tracked">
          <p>
            <strong>Kennel Club is not looking at this repository.</strong>
            {current.tracking.by || 'Somebody'} stopped it{current.tracking.since
              ? ` on ${formatAbsolute(current.tracking.since)}`
              : ''}.
          </p>
          <blockquote class="why">{current.tracking.reason}</blockquote>
          <p>
            Nothing is read from GitHub for it and nothing is evaluated, so it has no findings and
            raises no problem. Its waivers are kept, and do nothing until it is tracked again.
          </p>
        </div>
      {/if}
      <Tabs {tabs} value={tab} label="Repository sections" onchange={openTab}>
        {#if tab === 'fleet'}
          <RepositoryOverview repo={current} refreshKey={reload} />
        {:else if tab === 'ai-context'}
          <RepositoryAiContext repo={current} />
        {:else if !current.tracking.tracked}
          <EmptyState
            compact
            icon={EyeOff}
            title="Nothing to show for a repository that is not tracked"
            description="Having no findings is not an all clear. Track it again to have it read from GitHub and evaluated."
          />
        {:else}
          {#if incomplete}
            <div class="notice" role="note">
              <p>
                <strong>This is not an all clear.</strong>
                {#if unread.length > 0 || current.skipped.length > 0}
                  Some of what Kennel Club needs could not be read, so the checks that depend on it
                  did not run. What it could read is below.
                {:else}
                  Every check that could run did, but not against everything it needs.
                {/if}
              </p>
            </div>
          {/if}

          <div class="areas" role="group" aria-label="Finding area">
            {#each areas as entry (entry.id)}
              <button
                type="button"
                class="area"
                class:current={area === entry.id}
                aria-pressed={area === entry.id}
                onclick={() => openArea(entry.id)}
              >
                {entry.label}
                <span class="count">{entry.count}</span>
              </button>
            {/each}
          </div>

          {#if area === 'protection'}
            <RepositoryProtection repo={current} />
          {:else if area === 'guidance'}
            <RepositoryAgentGuidance repo={current} />
          {:else}
            <div class="findings-layout">
              <section class="findings" aria-labelledby="findings-heading">
                <h2 id="findings-heading">
                  Open findings
                  <span class="count">{openFindingsText(current.counts)}</span>
                </h2>
                {#if visibleFindings.length > 0}
                  {#each visibleFindings as finding (finding.code + '\u0000' + finding.subject)}
                    <Finding {finding} files={current.files} repository={current.name}>
                      {#snippet actions()}
                        {#if session.can('operator')}
                          {#if !session.visitor}
                            {#each timeoutFixTargets(finding, current.files) as target (target.blob + ':' + target.job)}
                              <Button
                                size="sm"
                                variant="secondary"
                                href={kennelTimeoutFixHref(current.id, target.blob, target.job)}
                                ariaLabel="Preview a timeout fix: {target.label}"
                              >
                                Preview a fix
                              </Button>
                            {/each}
                          {/if}
                          {#if session.can(waiverRole(finding.severity))}
                            <Button
                              size="sm"
                              icon={ShieldCheck}
                              ariaLabel="Waive: {finding.title}"
                              onclick={() => startWaive(finding)}
                            >
                              Waive
                            </Button>
                          {:else}
                            <!-- Said, not hidden or greyed: a sentence reads on a phone and to a screen reader. -->
                            <span class="refusal">{ERROR_WAIVER_SENTENCE}</span>
                          {/if}
                        {/if}
                      {/snippet}
                    </Finding>
                  {/each}
                {:else if current.state === 'pending'}
                  <EmptyState
                    compact
                    title="Not looked at yet"
                    description="Kennel Club has not evaluated this repository. It will, on its next pass, or when you press Recheck."
                  />
                {:else if incomplete}
                  <EmptyState
                    compact
                    title="Nothing open, and not an all clear"
                    description="Nothing was found in what could be read. The sources that could not be read are listed beside these findings."
                  />
                {:else}
                  <EmptyState
                    compact
                    icon={Trophy}
                    title={area === 'workflow'
                      ? 'No workflow findings'
                      : kennelStatus('best_in_show', undefined, prefs.quirkyStatus).label}
                    description={area === 'workflow'
                      ? 'Every workflow check that could run did, and nothing is open in this area.'
                      : 'Every check that is turned on ran against everything it needs, and nothing is open.'}
                  />
                {/if}
              </section>

              <aside class="findings-aside">
                <Panel
                  title="What Kennel Club could see"
                  description="Each source of facts about this repository, and whether it could be read."
                >
                  <ul class="coverage">
                    {#each current.coverage as source (source.source)}
                      <li>
                        <div class="head">
                          <strong>{source.label}</strong>
                          <Badge status={kennelCoverageStatus(source.state)} size="sm" />
                        </div>
                        <p>{source.reason}</p>
                        {#if source.permission && source.state !== 'ok'}
                          <p class="permission">
                            Needs the App's <strong>{source.permission}</strong> permission.
                          </p>
                        {/if}
                      </li>
                    {/each}
                  </ul>
                  {#if current.skipped.length > 0}
                    <details class="aside-fold">
                      <summary>{current.skipped.length} checks did not run</summary>
                      <ul class="skipped">
                        {#each current.skipped as skip (skip.code)}
                          <li><code>{skip.code}</code>: {skip.reason}</li>
                        {/each}
                      </ul>
                    </details>
                  {/if}
                  {#if current.disabled.length > 0}
                    <details class="aside-fold">
                      <summary>{current.disabled.length} checks turned off</summary>
                      <p class="note">
                        Turned off in Settings, and so not counted as a gap:
                        {current.disabled.join(', ')}.
                      </p>
                    </details>
                  {/if}
                </Panel>
              </aside>
            </div>

            {#if current.waived.length > 0 || current.lapsed.length > 0}
              <details class="waived" open={current.waived.length > 0}>
                <summary>
                  Waived
                  <span class="count">{current.waived.length}</span>
                </summary>
                {#each current.waived as entry (entry.waiver.id)}
                  <div class="waiver">
                    <p class="what">
                      <Badge status={severityStatus(entry.finding.severity)} size="sm" />
                      <strong>{entry.finding.title}</strong>
                    </p>
                    <p class="reason">{entry.waiver.reason}</p>
                    <p class="by">
                      Waived by {entry.waiver.by}
                      <RelativeTime value={entry.waiver.at} plain />, until
                      {formatAbsolute(entry.waiver.expires_at)}.
                    </p>
                    <!-- Any operator may end any waiver: ending one only makes Kennel Club stricter. -->
                    {#if session.can('operator')}
                      <div class="end">
                        <Button
                          size="sm"
                          icon={Undo2}
                          ariaLabel="End the waiver: {entry.finding.title}"
                          onclick={() => startEnd(entry)}
                        >
                          End waiver
                        </Button>
                      </div>
                    {/if}
                  </div>
                {/each}
                {#each current.lapsed as waiver (waiver.id)}
                  <div class="waiver lapsed">
                    <p class="what"><strong>{waiver.code}</strong> is open again</p>
                    <p class="by">
                      A waiver by {waiver.by} no longer covers it: it ended, or the finding got worse.
                    </p>
                  </div>
                {/each}
              </details>
            {/if}

            <WaiveDialog
              bind:open={waiveOpen}
              repositoryId={current.id}
              finding={waiving}
              onwaived={(next) => (repo = next)}
            />

            <ConfirmDialog
              bind:open={endOpen}
              title="End waiver"
              name={ending?.finding.title}
              description="The finding is open again straight away."
              consequences={[
                'It counts against this repository again.',
                'To waive it again, somebody will have to give a reason.',
              ]}
              confirmLabel="End waiver"
              tone="default"
              onconfirm={endWaiver}
            />

            <p class="next">
              {#if current.next_due_at}
                Next read from GitHub <RelativeTime value={current.next_due_at} />.
              {:else}
                Due to be read now.
              {/if}
            </p>
          {/if}
        {/if}
      </Tabs>
    {/if}
  </div>
</KennelShell>

<style>
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-5);
  }
  .score {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .score-value {
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .notice {
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-md);
    background: var(--z-accent-subtle);
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .notice p {
    margin: 0;
  }
  .notice .why + p {
    margin-top: var(--z-space-2);
  }
  /* The reason is somebody's own sentence, set apart from the words around it. */
  .why {
    margin: var(--z-space-2) 0 0;
    padding-left: var(--z-space-3);
    border-left: var(--z-border-width-thick) solid var(--z-accent-border);
    overflow-wrap: anywhere;
    white-space: pre-line;
  }
  .areas {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .area {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-height: var(--z-space-8);
    padding: 0 var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-full);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-family: inherit;
    font-size: var(--z-text-xs);
    cursor: pointer;
  }
  .area:hover {
    border-color: var(--z-accent-border);
    color: var(--z-text);
  }
  .area.current {
    border-color: var(--z-accent-border);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
    font-weight: var(--z-weight-medium);
  }
  .area .count {
    color: var(--z-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .area.current .count {
    color: var(--z-accent);
  }
  .findings-layout {
    display: grid;
    grid-template-columns: minmax(0, 1.55fr) minmax(18rem, 0.9fr);
    gap: var(--z-space-4);
    align-items: start;
  }
  .findings {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    min-width: 0;
  }
  .findings-aside {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }
  h2 {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: var(--z-space-3);
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .count {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-normal);
    color: var(--z-text-muted);
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
  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .coverage p,
  .note {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .permission {
    color: var(--z-text);
  }
  .aside-fold {
    margin-top: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .aside-fold summary {
    padding-top: var(--z-space-3);
    color: var(--z-accent);
    font-size: var(--z-text-xs);
    cursor: pointer;
  }
  .skipped {
    margin: 0;
    padding-left: var(--z-space-5);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .note {
    margin-top: var(--z-space-3);
  }
  .waived {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .waived summary {
    cursor: pointer;
    padding: var(--z-space-3) var(--z-space-5);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .waiver {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    padding: var(--z-space-3) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .waiver p {
    margin: 0;
    font-size: var(--z-text-sm);
    overflow-wrap: anywhere;
  }
  .what {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .reason {
    color: var(--z-text);
  }
  .by {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .end {
    margin-top: var(--z-space-1);
  }
  .refusal {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .next {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  @media (pointer: coarse) {
    .area {
      min-height: var(--z-control-touch);
    }
  }
  @media (max-width: 980px) {
    .findings-layout {
      grid-template-columns: 1fr;
    }
  }
</style>
