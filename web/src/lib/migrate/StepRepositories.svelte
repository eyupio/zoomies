<!--
  Step two: which repositories, and which of their workflow files.

  Every repository the installation can see is listed, including the ones with
  nothing to migrate. That is deliberate: "acme/docs has no workflows" and
  "acme/infra is already on self-hosted runners" are both answers an operator
  wants, and a list that silently omitted them would look like the scan had
  missed something. They are hidden by default all the same, with a count and a
  switch: in an organisation they are most of the list, and this step is for
  the ones that can move.

  The repositories that can move often have several workflow files, of which a
  release or a nightly is exactly the one to leave where it is, so a repository
  with more than one can be opened up and picked through file by file.

  An organisation is read a page at a time, because reading every workflow file
  in a thousand repositories is the most expensive thing Zoomies asks GitHub
  for. A page is appended to this list rather than replacing it, so a choice
  made on the way survives to the review.

  What can be ticked is "this repository asks for a rented runner", not "this
  repository would change under the mapping the server has guessed so far". The
  two are different, and the difference used to be a dead end: the labels are
  mapped on the *next* step, so a fleet whose pools the server could not match
  to any label offered an operator a page of checkboxes that all refused to
  tick, with nothing on screen saying why.

  Whatever cannot be ticked says so in its own words, and the two reasons that
  are the operator's to fix -- a repository the App may not read, and a scan
  that turned up nothing at all -- are said once at the top, where they cannot
  be mistaken for a broken control.
-->
<script lang="ts">
  import type { MigrationPlan, MigrationRepo, MigrationWorkflow } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import {
    AlertTriangle,
    ChevronDown,
    ChevronRight,
    ExternalLink,
    FolderGit2,
  } from '@lucide/svelte';

  interface Props {
    plan: MigrationPlan | null;
    /** Repository -> the workflow paths chosen in it. A repository with none is unchosen. */
    selection?: Record<string, string[]>;
    /** How many repositories the installation can see, of which this list is a page. */
    total?: number;
    /** Set when there is another page to read. */
    hasMore?: boolean;
    busy?: boolean;
    onloadmore?: () => void;
  }

  let {
    plan,
    selection = $bindable({}),
    total = 0,
    hasMore = false,
    busy = false,
    onloadmore,
  }: Props = $props();

  /** One workflow file that asks for a runner somebody else operates. */
  interface File {
    path: string;
    jobs: number;
    skips: number;
  }

  interface Row {
    repo: string;
    jobs: number;
    workflows: number;
    skipped: number;
    note: string;
    /** The files that could move, which are what a repository is chosen by. */
    files: File[];
    migratable: boolean;
    unreadable: boolean;
  }

  function rowOf(repo: MigrationRepo): Row {
    const workflows = repo.workflows ?? [];
    const jobs = workflows.reduce((n, w) => n + (w.rewrites ?? []).length, 0);
    const changed = workflows.filter((w) => (w.rewrites ?? []).length > 0).length;
    const skipped = workflows.reduce((n, w) => n + (w.skips ?? []).length, 0);
    const labels = repo.hosted_labels ?? [];

    // What this repository would get out of the migration, in one line. The
    // order matters: an unreadable repository explains itself first, then what
    // would change, then what was found but not yet mapped -- and "no
    // workflows" is a different answer from "workflows, but nothing to move".
    const note = repo.error
      ? repo.error
      : jobs > 0
        ? `${jobs} ${jobs === 1 ? 'job' : 'jobs'} in ${changed} ${changed === 1 ? 'file' : 'files'}`
        : labels.length > 0
          ? `Runs on ${labels.join(', ')}; choose what those become on the next step`
          : workflows.length === 0
            ? 'No workflows'
            : skipped > 0
              ? `Nothing to move; ${skipped} left alone`
              : 'Nothing to move';

    // On the labels a file asks for, not on what the provisional mapping
    // already rewrites: the mapping is made on the next step, so a file with
    // nothing to rewrite yet is still one the operator came here to choose.
    const files = workflows
      .filter((w: MigrationWorkflow) => (w.hosted_labels ?? []).length > 0)
      .map((w: MigrationWorkflow) => ({
        path: w.path ?? '',
        jobs: (w.rewrites ?? []).length,
        skips: (w.skips ?? []).length,
      }));

    return {
      repo: repo.repo ?? '',
      jobs,
      workflows: changed,
      skipped,
      note,
      files,
      migratable: !repo.error && labels.length > 0,
      unreadable: Boolean(repo.error),
    };
  }

  /**
   * Hiding what cannot move is the default.
   *
   * The rest are still worth being able to see -- a repository the App cannot
   * read, and one whose jobs already point somewhere deliberate, are both
   * answers -- but they are not what this step is for, and in an organisation
   * they are most of it.
   */
  let onlyMigratable = $state(true);

  /** Which repositories have their file list open. */
  let expanded = $state<Record<string, boolean>>({});

  const rows = $derived((plan?.repositories ?? []).map(rowOf));
  const migratable = $derived(rows.filter((r) => r.migratable));
  const unreadable = $derived(rows.filter((r) => r.unreadable));
  const missingPermissions = $derived(plan?.missing_permissions ?? []);
  const hidden = $derived(onlyMigratable ? rows.length - migratable.length : 0);
  const visible = $derived(onlyMigratable ? migratable : rows);

  const chosenIn = (repo: string): string[] => selection[repo] ?? [];
  const chosenCount = $derived(migratable.filter((r) => chosenIn(r.repo).length > 0).length);
  const allChosen = $derived(
    migratable.length > 0 &&
      migratable.every((r) => r.files.every((f) => chosenIn(r.repo).includes(f.path))),
  );
  const someChosen = $derived(migratable.some((r) => chosenIn(r.repo).length > 0));

  /** Replaces a repository's chosen files, dropping the key when none are left. */
  function set(repo: string, paths: string[]): void {
    const next = { ...selection };
    if (paths.length === 0) delete next[repo];
    else next[repo] = paths;
    selection = next;
  }

  function toggle(row: Row, on: boolean): void {
    set(row.repo, on ? row.files.map((f) => f.path) : []);
  }

  function toggleFile(row: Row, path: string, on: boolean): void {
    const current = chosenIn(row.repo);
    set(row.repo, on ? [...current, path] : current.filter((p) => p !== path));
  }

  function toggleAll(on: boolean): void {
    if (!on) {
      selection = {};
      return;
    }
    const next: Record<string, string[]> = { ...selection };
    for (const row of migratable) next[row.repo] = row.files.map((f) => f.path);
    selection = next;
  }

  function expand(repo: string): void {
    expanded = { ...expanded, [repo]: !expanded[repo] };
  }
</script>

{#if rows.length === 0}
  <EmptyState
    icon={FolderGit2}
    title="No repositories"
    description="This installation can see no repositories. Check the App is installed on the account, and that it was given access to the repositories you expect."
  />
{:else}
  <p class="lede">
    {migratable.length} of {rows.length}
    {rows.length === 1 ? 'repository has' : 'repositories have'} jobs on a rented runner — GitHub's own,
    or a vendor's — that a pool here could take. The rest are hidden, and the switch below shows them
    again so you can see they were looked at. A repository with several workflow files can be opened up
    and picked through one file at a time.
  </p>

  {#if unreadable.length > 0}
    <div class="problem" role="alert">
      <p class="problem-title">
        <AlertTriangle size={15} aria-hidden="true" />
        {unreadable.length}
        {unreadable.length === 1 ? 'repository could' : 'repositories could'} not be read
      </p>
      <p>
        Their workflows were never looked at, so they cannot be migrated from here whatever they
        contain. The first one says: <span class="mono">{unreadable[0]?.note}</span>
      </p>
      {#if missingPermissions.length > 0}
        <ul>
          {#each missingPermissions as item (item)}
            <li>{item}</li>
          {/each}
        </ul>
      {/if}
      {#if plan?.permission_hint}<p>{plan.permission_hint}.</p>{/if}
      {#if plan?.settings_url}
        <p>
          <a href={plan.settings_url} target="_blank" rel="noopener noreferrer">
            Review the App's permissions on GitHub
            <ExternalLink size={12} aria-hidden="true" />
          </a>
        </p>
      {/if}
    </div>
  {/if}

  {#if migratable.length === 0}
    <div class="problem" role="status">
      <p class="problem-title">
        <AlertTriangle size={15} aria-hidden="true" />
        There is nothing here to migrate
      </p>
      <p>
        Nothing can be ticked because no repository on this page asks for a runner somebody else
        operates. Turn the filter off and each row says which of the three reasons applies to it:
        the App cannot read it, it has no workflows, or its jobs already point somewhere deliberate
        — a self-hosted fleet, or a label this controller does not recognise as rented.{hasMore
          ? ' There are more repositories to read.'
          : ''}
      </p>
    </div>
  {/if}

  <div class="controls">
    <Checkbox
      checked={allChosen}
      indeterminate={!allChosen && someChosen}
      label="Select every file that could move"
      onchange={toggleAll}
      disabled={migratable.length === 0}
    />
    <Switch
      checked={onlyMigratable}
      label="Only repositories with something to move"
      onchange={(on) => (onlyMigratable = on)}
    />
  </div>

  <ul class="repos">
    {#each visible as row (row.repo)}
      {@const chosen = chosenIn(row.repo)}
      <li class:inert={!row.migratable}>
        <div class="repo">
          <Checkbox
            checked={chosen.length > 0 && chosen.length === row.files.length}
            indeterminate={chosen.length > 0 && chosen.length < row.files.length}
            disabled={!row.migratable}
            label={row.repo}
            description={chosen.length > 0 && chosen.length < row.files.length
              ? `${row.note} — ${chosen.length} of ${row.files.length} files chosen`
              : row.note}
            onchange={(on) => toggle(row, on)}
          />
          {#if row.files.length > 1}
            <button
              type="button"
              class="disclose"
              aria-expanded={expanded[row.repo] ?? false}
              onclick={() => expand(row.repo)}
            >
              {#if expanded[row.repo]}
                <ChevronDown size={13} aria-hidden="true" />
              {:else}
                <ChevronRight size={13} aria-hidden="true" />
              {/if}
              {row.files.length} files
            </button>
          {/if}
        </div>

        {#if row.files.length > 1 && expanded[row.repo]}
          <ul class="files">
            {#each row.files as file (file.path)}
              <li>
                <Checkbox
                  checked={chosen.includes(file.path)}
                  label={file.path}
                  description="{file.jobs} {file.jobs === 1 ? 'job' : 'jobs'} to move{file.skips > 0
                    ? `, ${file.skips} left alone`
                    : ''}"
                  onchange={(on) => toggleFile(row, file.path, on)}
                />
              </li>
            {/each}
          </ul>
        {/if}
      </li>
    {/each}
  </ul>

  <p class="footer">
    <span>
      {chosenCount}
      {chosenCount === 1 ? 'repository' : 'repositories'} chosen{hidden > 0
        ? `. ${hidden} that cannot move ${hidden === 1 ? 'is' : 'are'} hidden`
        : ''}{hasMore && total > rows.length ? `. ${total - rows.length} not read yet` : ''}.
    </span>
    {#if hasMore}
      <Button size="sm" variant="secondary" onclick={onloadmore} disabled={busy} loading={busy}>
        Read the next page
      </Button>
    {/if}
  </p>
{/if}

<style>
  .lede {
    margin: 0 0 var(--z-space-3);
    max-width: 70ch;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
  .problem {
    margin: 0 0 var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    background: var(--z-pending-subtle);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text);
  }
  .problem p {
    margin: 0 0 var(--z-space-2);
    max-width: 78ch;
  }
  .problem p:last-child {
    margin-bottom: 0;
  }
  .problem-title {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    font-weight: var(--z-weight-semibold);
    color: var(--z-pending);
  }
  .problem ul {
    margin: 0 0 var(--z-space-2);
    padding-left: var(--z-space-5);
  }
  .problem a {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    color: var(--z-accent);
    font-weight: var(--z-weight-medium);
  }
  .mono {
    font-family: var(--z-font-mono);
    overflow-wrap: anywhere;
  }
  .controls {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-4);
    flex-wrap: wrap;
    padding-bottom: var(--z-space-2);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .repo {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .disclose {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    padding: 0;
    border: 0;
    background: none;
    cursor: pointer;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .disclose:hover {
    color: var(--z-accent);
  }
  .files {
    margin: var(--z-space-2) 0 0 var(--z-space-6);
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
  }
  .footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    flex-wrap: wrap;
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .repos {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: 26rem;
    overflow-y: auto;
  }
  .repos > li {
    padding: var(--z-space-2) 0;
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .repos > li.inert {
    opacity: 0.6;
  }
</style>
