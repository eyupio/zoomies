<!--
  Step two: which repositories, and which of their workflow files.

  Two things an organisation of any size makes true immediately. Most of its
  repositories have nothing to move -- no workflows, or workflows already on
  somebody's runners -- and the ones that do have several workflow files, of
  which a release or a nightly is often exactly the one to leave on GitHub.
  So the list hides the repositories with nothing to move by default, says how
  many it hid, and lets a repository be opened up and picked through file by
  file.

  The scan reads a page of the organisation at a time, because reading every
  workflow file in a thousand repositories is the most expensive thing Zoomies
  asks GitHub for. "Read the next page" appends to this list rather than
  replacing it, so a selection survives paging and the operator ends up looking
  at one list of everything they have looked at.
-->
<script lang="ts">
  import type { MigrationPlan, MigrationRepo, MigrationWorkflow } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import { ChevronDown, ChevronRight, FolderGit2 } from '@lucide/svelte';

  interface Props {
    plan: MigrationPlan | null;
    /** Repository -> the workflow paths chosen in it. A repository with none is unchosen. */
    selection?: Record<string, string[]>;
    /** How many repositories the installation has, of which this list is a page. */
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

  interface File {
    path: string;
    jobs: number;
    skips: number;
  }

  interface Row {
    repo: string;
    jobs: number;
    skipped: number;
    note: string;
    /** The workflow files this repository would get a change in. */
    files: File[];
    migratable: boolean;
  }

  function rowOf(repo: MigrationRepo): Row {
    const workflows = repo.workflows ?? [];
    const jobs = workflows.reduce((n, w) => n + (w.rewrites ?? []).length, 0);
    const skipped = workflows.reduce((n, w) => n + (w.skips ?? []).length, 0);
    const files = workflows
      .filter((w: MigrationWorkflow) => (w.rewrites ?? []).length > 0)
      .map((w: MigrationWorkflow) => ({
        path: w.path ?? '',
        jobs: (w.rewrites ?? []).length,
        skips: (w.skips ?? []).length,
      }));

    // What this repository would get out of the migration, in one line. The
    // order matters: an unreadable repository explains itself first, and
    // "no workflows" is a different answer from "workflows, but nothing to
    // move".
    const note = repo.error
      ? repo.error
      : jobs > 0
        ? `${jobs} ${jobs === 1 ? 'job' : 'jobs'} in ${files.length} ${files.length === 1 ? 'file' : 'files'}`
        : workflows.length === 0
          ? 'No workflows'
          : skipped > 0
            ? `Nothing to move; ${skipped} left alone`
            : 'Nothing to move';

    return { repo: repo.repo ?? '', jobs, skipped, note, files, migratable: jobs > 0 };
  }

  /**
   * Hiding the repositories with nothing to move is the default.
   *
   * They are still worth being able to see -- "acme/docs has no workflows" and
   * "acme/infra is already self-hosted" are both answers, and a list that
   * silently omitted them would look like the scan had missed something -- but
   * they are not what this step is for, and in an organisation they are most of
   * it.
   */
  let onlyMigratable = $state(true);

  /** Which repositories have their file list open. */
  let expanded = $state<Record<string, boolean>>({});

  const rows = $derived((plan?.repositories ?? []).map(rowOf));
  const migratable = $derived(rows.filter((r) => r.migratable));
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

  function toggleRepo(row: Row, on: boolean): void {
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
    {rows.length === 1 ? 'repository has' : 'repositories have'} jobs on a GitHub-hosted runner that a
    pool here could take{total > rows.length ? `, of the ${total} this installation can see` : ''}.
    A repository with several workflow files can be opened up and picked through one file at a time.
  </p>

  <div class="controls">
    <Checkbox
      checked={allChosen}
      indeterminate={!allChosen && someChosen}
      label="Select every file that would change"
      onchange={toggleAll}
      disabled={migratable.length === 0}
    />
    <Switch
      checked={onlyMigratable}
      label="Only repositories with something to move"
      onchange={(on) => (onlyMigratable = on)}
    />
  </div>

  {#if visible.length === 0}
    <p class="note">
      Nothing on this page has a job to move. {hasMore
        ? 'There are more repositories to read.'
        : 'Turn the filter off to see what was looked at.'}
    </p>
  {/if}

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
              ? `${row.note} — ${chosen.length} chosen`
              : row.note}
            onchange={(on) => toggleRepo(row, on)}
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
                  description="{file.jobs} {file.jobs === 1 ? 'job' : 'jobs'}{file.skips > 0
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
        ? `. ${hidden} with nothing to move ${hidden === 1 ? 'is' : 'are'} hidden`
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
  .note {
    margin: 0 0 var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-4);
    border: 1px solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    background: var(--z-pending-subtle);
    color: var(--z-pending);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
  .controls {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-4);
    flex-wrap: wrap;
    padding-bottom: var(--z-space-2);
    border-bottom: 1px solid var(--z-border);
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
    border-bottom: 1px solid var(--z-border);
  }
  .repos > li.inert {
    opacity: 0.6;
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
</style>
