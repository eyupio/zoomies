<!--
  Moving a repository's CI onto this fleet.

  Five steps, and the fourth one is the point: nothing is written until an
  operator has read the exact diff that is about to appear in somebody else's
  repository. The first three exist to make that diff correct, and the last
  reports what actually happened, repository by repository, with a link to
  every pull request.

  The wizard holds one plan at a time and re-fetches it whenever an answer that
  changes it changes. That is a round trip per edit, but the alternative --
  rewriting workflows in the browser -- would mean two implementations of the
  rewriting rules that could disagree, and the one that matters is the one on
  the server, because it is the one that commits.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { ExternalLink, GitPullRequest, RefreshCw } from '@lucide/svelte';
  import {
    ApiError,
    listInstallations,
    openMigrationPullRequests,
    planMigration,
  } from '$lib/api/client';
  import type {
    Installation,
    MigrationOutcome,
    MigrationPlan,
    MigrationRepo,
  } from '$lib/api/types';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import Wizard from '$lib/components/Wizard.svelte';
  import type { WizardStep } from '$lib/components/Wizard.svelte';
  import StepTarget from './StepTarget.svelte';
  import StepRepositories from './StepRepositories.svelte';
  import StepMapping from './StepMapping.svelte';
  import StepReview from './StepReview.svelte';
  import StepOutcome from './StepOutcome.svelte';

  interface Props {
    /** Preselected from the query string, so a link can point straight at one. */
    installationId?: string;
    oncancel?: () => void;
  }

  let { installationId = '', oncancel }: Props = $props();

  /**
   * Four steps, not five: the outcome is not a step.
   *
   * A results screen with a Back button invites an operator to walk back into a
   * review of work that is already open, and it would make the wizard's Next
   * mean "open the pull requests" on one step and "go forward" on the others.
   * Review is the last step, its button says exactly what it does, and the
   * results replace the wizard once there are any.
   */
  const STEPS: readonly WizardStep[] = [
    { id: 'target', title: 'Installation', description: 'Whose repositories.' },
    { id: 'repos', title: 'Repositories', description: 'Which ones to migrate.' },
    { id: 'mapping', title: 'Labels', description: 'What each GitHub label becomes.' },
    { id: 'review', title: 'Review', description: 'The exact change, before it is opened.' },
  ];

  let step = $state(0);
  let busy = $state(false);
  let failure = $state('');

  /* -- step one ------------------------------------------------------------ */

  let installations = $state<Installation[]>([]);
  let loadingInstallations = $state(true);
  let installationsError = $state('');
  /** Bumped by the error state's retry, which re-runs the fetch below. */
  let installationsAttempt = $state(0);
  // Seeded from the query string once, then owned by the wizard: navigating
  // back to step one must not snap the choice back to whatever the link said.
  let selected = $state(untrack(() => installationId));

  /* -- the plan ------------------------------------------------------------ */

  let plan = $state<MigrationPlan | null>(null);
  /**
   * Repository -> the workflow paths chosen in it.
   *
   * A repository is chosen when it has at least one path here, which is what
   * lets a repository with a dozen workflow files send only the two that should
   * move. The apply call carries the same shape, so what is committed is what
   * was ticked rather than "everything in that repository that happens to
   * match" -- including a workflow file somebody added since the scan.
   */
  let selection = $state<Record<string, string[]>>({});
  /** The cursor for the page of repositories after the ones on screen. */
  let nextCursor = $state('');
  /** How many repositories the installation can see, of which those are a page. */
  let totalRepos = $state(0);
  /** Hosted label -> the runs-on value replacing it. "" means "leave it alone". */
  let mapping = $state<Record<string, string>>({});
  /** True once the operator has edited the mapping, so a re-scan stops overwriting it. */
  let mappingEdited = false;

  /* -- step five ----------------------------------------------------------- */

  let outcome = $state<MigrationOutcome | null>(null);

  $effect(() => {
    void installationsAttempt;
    loadingInstallations = true;
    installationsError = '';
    void (async () => {
      try {
        const result = await listInstallations();
        installations = result.items ?? [];
        if (!selected && installations.length === 1) selected = installations[0]?.id ?? '';
      } catch (cause) {
        installationsError =
          cause instanceof ApiError ? cause.message : 'The installations could not be listed.';
      } finally {
        loadingInstallations = false;
      }
    })();
  });

  const target = $derived(installations.find((i) => i.id === selected));

  const chosen = $derived(Object.keys(selection));

  /**
   * The repositories the review step shows, narrowed to the chosen files.
   *
   * Narrowing here rather than in the review means the diffs, the counts and
   * the pull requests all come from one decision: a file nobody ticked is not
   * shown, not counted, and not committed.
   */
  const selectedChanged = $derived.by(() => {
    const out: MigrationRepo[] = [];
    for (const repo of plan?.repositories ?? []) {
      const paths = selection[repo.repo ?? ''] ?? [];
      if (paths.length === 0) continue;
      const workflows = (repo.workflows ?? []).filter((w) => paths.includes(w.path ?? ''));
      if (!workflows.some((w) => (w.rewrites ?? []).length > 0)) continue;
      out.push({ ...repo, workflows });
    }
    return out;
  });
  const blockedByPermissions = $derived((plan?.missing_permissions ?? []).length > 0);

  const canAdvance = $derived.by(() => {
    if (busy) return false;
    switch (step) {
      case 0:
        return selected !== '';
      case 1:
        return chosen.length > 0;
      case 2:
        return Object.values(mapping).some((v) => v !== '');
      case 3:
        return selectedChanged.length > 0 && !blockedByPermissions;
      default:
        return true;
    }
  });

  function report(cause: unknown, fallback: string): void {
    failure = cause instanceof ApiError ? cause.message : fallback;
  }

  /**
   * Ask the server what it would change.
   *
   * `repos` is left empty on the first scan so the operator sees the
   * organisation and chooses from it; every scan after that is narrowed to what
   * they chose, which is both faster and a smaller slice of the installation's
   * GitHub quota.
   *
   * `cursor` reads the next page of an organisation too large to read at once.
   * A page is merged into the plan rather than replacing it, because the point
   * of paging is to end up looking at one list of everything you have looked
   * at, with the choices you made on the way still ticked.
   */
  async function scan(repos: string[], cursor = ''): Promise<boolean> {
    busy = true;
    failure = '';
    try {
      const result = await planMigration({
        installation_id: selected,
        ...(repos.length > 0 ? { repos } : {}),
        ...(cursor !== '' ? { cursor } : {}),
        ...(mappingEdited ? { mapping } : {}),
      });
      const narrowed = repos.length > 0;
      plan = narrowed ? mergeNarrowed(plan, result) : mergePage(plan, result, cursor !== '');
      if (!narrowed) {
        nextCursor = result.next_cursor ?? '';
        totalRepos = result.total_repos ?? (plan?.repositories ?? []).length;
      }
      if (!mappingEdited) {
        // The server's proposal, plus an explicit blank for every label it
        // could not place, so the mapping step lists all of them.
        const next: Record<string, string> = {};
        for (const label of plan?.hosted_labels ?? []) next[label] = '';
        Object.assign(next, result.mapping ?? {});
        mapping = next;
      }
      if (!narrowed) chooseByDefault(result.repositories ?? []);
      return true;
    } catch (cause) {
      report(cause, 'The repositories could not be read.');
      return false;
    } finally {
      busy = false;
    }
  }

  /** A fresh scan, or the page after the one on screen appended to it. */
  function mergePage(
    current: MigrationPlan | null,
    page: MigrationPlan,
    append: boolean,
  ): MigrationPlan {
    if (!append || !current) return page;
    const seen = new Set((current.repositories ?? []).map((r) => r.repo));
    return {
      ...page,
      repositories: [
        ...(current.repositories ?? []),
        ...(page.repositories ?? []).filter((r) => !seen.has(r.repo)),
      ],
      hosted_labels: [
        ...new Set([...(current.hosted_labels ?? []), ...(page.hosted_labels ?? [])]),
      ].sort(),
    };
  }

  /**
   * A re-scan of the chosen repositories, folded back into the whole list.
   *
   * The repositories nobody chose keep the plan they were read with. Their diff
   * is stale under a mapping that has since changed, which is why the step that
   * shows a diff shows only the repositories that were chosen and re-read.
   */
  function mergeNarrowed(current: MigrationPlan | null, fresh: MigrationPlan): MigrationPlan {
    if (!current) return fresh;
    const byName = new Map((fresh.repositories ?? []).map((r) => [r.repo, r]));
    const kept = (current.repositories ?? []).map((r) => byName.get(r.repo) ?? r);
    for (const repo of fresh.repositories ?? []) {
      if (!kept.some((r) => r.repo === repo.repo)) kept.push(repo);
    }
    return { ...fresh, repositories: kept };
  }

  /**
   * Ticks every workflow file that asks for a rented runner, in the
   * repositories a scan has just brought in.
   *
   * On the labels, not on "would change under the mapping so far": the mapping
   * is chosen on the step after this one, so ticking on what the server's
   * provisional mapping already rewrites would hide from an operator the very
   * files they came here to map. Choices already made elsewhere in the list are
   * left alone.
   */
  function chooseByDefault(repos: readonly MigrationRepo[]): void {
    const next = { ...selection };
    for (const repo of repos) {
      const name = repo.repo ?? '';
      if (!name || name in next || repo.error) continue;
      const paths = (repo.workflows ?? [])
        .filter((w) => (w.hosted_labels ?? []).length > 0)
        .map((w) => w.path ?? '')
        .filter(Boolean);
      if (paths.length > 0) next[name] = paths;
    }
    selection = next;
  }

  /** The next page of the organisation, appended to what is on screen. */
  async function loadMore(): Promise<void> {
    if (nextCursor === '') return;
    await scan([], nextCursor);
  }

  async function next(): Promise<void> {
    switch (step) {
      case 0: {
        if (!(await scan([]))) step -= 1;
        return;
      }
      case 2: {
        // The mapping changed, so every diff downstream is stale. A scan
        // that failed leaves the old plan in place, and the review must not
        // show it as if it were the new mapping's -- the pull requests it
        // would open are built from the mapping, not from what is on screen.
        if (!(await scan(chosen))) step -= 1;
        return;
      }
      default:
        return;
    }
  }

  function onMappingChange(label: string, to: string): void {
    mappingEdited = true;
    mapping = { ...mapping, [label]: to };
  }

  async function rescan(): Promise<void> {
    await scan(chosen);
  }

  /** Back to the start, keeping the installation and nothing else. */
  function restart(): void {
    outcome = null;
    plan = null;
    selection = {};
    nextCursor = '';
    totalRepos = 0;
    mapping = {};
    mappingEdited = false;
    failure = '';
    step = 0;
  }

  /** The last step. This is the only thing here that writes anything. */
  async function open(): Promise<void> {
    busy = true;
    failure = '';
    try {
      const chosenMapping: Record<string, string> = {};
      for (const [label, to] of Object.entries(mapping)) {
        if (to !== '') chosenMapping[label] = to;
      }
      // The files, not just the repositories: a repository where only two of
      // its five workflows were ticked must get a pull request touching two.
      const workflows: Record<string, string[]> = {};
      for (const repo of selectedChanged) {
        workflows[repo.repo ?? ''] = (repo.workflows ?? [])
          .filter((w) => (w.rewrites ?? []).length > 0)
          .map((w) => w.path ?? '')
          .filter(Boolean);
      }
      const result = await openMigrationPullRequests({
        installation_id: selected,
        repos: selectedChanged.map((r) => r.repo ?? '').filter(Boolean),
        mapping: chosenMapping,
        workflows,
      });
      // Assigned only once the call has returned, because assigning it is what
      // swaps the wizard for the results.
      outcome = result;
      const opened = result.opened ?? 0;
      toasts.push({
        tone: opened > 0 ? 'success' : 'warning',
        title:
          opened > 0
            ? `Opened ${opened} pull ${opened === 1 ? 'request' : 'requests'}`
            : 'Nothing was opened',
        message:
          opened > 0
            ? 'Each one changes only the runs-on lines you reviewed.'
            : 'Every repository was skipped or failed. The results say why.',
      });
    } catch (cause) {
      report(cause, 'The pull requests could not be opened.');
    } finally {
      busy = false;
    }
  }
</script>

{#if loadingInstallations}
  <div class="loading">
    <Skeleton width="220px" height="1.5rem" />
    <Skeleton width="100%" height="12rem" />
  </div>
{:else if installationsError}
  <ErrorState
    title="GitHub is not reachable"
    description={installationsError}
    onretry={() => (installationsAttempt += 1)}
  />
{:else if installations.length === 0}
  <ErrorState
    title="No installation to migrate"
    description="A migration reads and writes repositories through a GitHub App installation, and this controller has none. Connect one on the Installations page first."
  />
{:else if outcome}
  <StepOutcome {outcome} />
  <p class="summary">
    <Button variant="secondary" onclick={restart}>Migrate more repositories</Button>
    {#if target?.web_url}
      <a href={target.web_url} target="_blank" rel="noopener noreferrer">
        Open {target.target} on GitHub
        <ExternalLink size={12} aria-hidden="true" />
      </a>
    {/if}
  </p>
{:else}
  <Wizard
    steps={STEPS}
    bind:current={step}
    {canAdvance}
    {busy}
    finishLabel="Open the pull requests"
    onnext={next}
    onfinish={open}
    {oncancel}
  >
    {#snippet children(_s: WizardStep, index: number)}
      {#if failure}
        <p class="failure" role="alert">{failure}</p>
      {/if}

      {#if index === 0}
        <StepTarget {installations} bind:selected />
      {:else if index === 1}
        <StepRepositories
          {plan}
          bind:selection
          total={totalRepos}
          hasMore={nextCursor !== ''}
          {busy}
          onloadmore={loadMore}
        />
      {:else if index === 2}
        <StepMapping {plan} {mapping} onchange={onMappingChange} />
      {:else}
        <StepReview
          {plan}
          repos={selectedChanged}
          {busy}
          target={target?.target ?? ''}
          onrescan={rescan}
        />
      {/if}
    {/snippet}
  </Wizard>

  {#if step === 3 && plan}
    <p class="summary">
      <GitPullRequest size={14} aria-hidden="true" />
      {selectedChanged.length}
      {selectedChanged.length === 1 ? 'pull request' : 'pull requests'}, one per repository, each on
      its own branch.
      <Button size="sm" variant="ghost" icon={RefreshCw} onclick={rescan} disabled={busy}>
        Re-read from GitHub
      </Button>
    </p>
  {/if}
{/if}

<style>
  .loading {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  .failure {
    margin: 0 0 var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-md);
    background: var(--z-danger-subtle);
    color: var(--z-danger);
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
  }
  .summary {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: var(--z-space-4) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .summary a {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    color: var(--z-accent);
  }
</style>
