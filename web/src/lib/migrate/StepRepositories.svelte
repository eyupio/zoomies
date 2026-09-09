<!--
  Step two: which repositories.

  Every repository the installation can see is listed, including the ones with
  nothing to migrate. That is deliberate: "acme/docs has no workflows",
  "acme/infra is already on Zoomies" and "acme/legacy is archived" are all
  answers an operator wants, and a list that silently omitted them would look
  like the scan had missed something.

  Only the repositories a pull request could actually be opened against are
  ticked, and the rest cannot be ticked at all.
-->
<script lang="ts">
  import type { MigrationPlan, MigrationRepo } from '$lib/api/types';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import { FolderGit2 } from '@lucide/svelte';
  import { isMigratable, noteFor } from './eligibility';

  interface Props {
    plan: MigrationPlan | null;
    chosen?: string[];
  }

  let { plan, chosen = $bindable([]) }: Props = $props();

  interface Row {
    repo: string;
    note: string;
    migratable: boolean;
  }

  function rowOf(repo: MigrationRepo): Row {
    return {
      repo: repo.repo ?? '',
      note: noteFor(repo),
      migratable: isMigratable(repo),
    };
  }

  const rows = $derived((plan?.repositories ?? []).map(rowOf));
  const migratable = $derived(rows.filter((r) => r.migratable));
  const blocked = $derived(
    (plan?.repositories ?? []).filter((r) => r.archived || (r.on_zoomies && !isMigratable(r))),
  );
  const allChosen = $derived(
    migratable.length > 0 && migratable.every((r) => chosen.includes(r.repo)),
  );
  const someChosen = $derived(migratable.some((r) => chosen.includes(r.repo)));

  function toggle(repo: string, on: boolean): void {
    chosen = on ? [...chosen, repo] : chosen.filter((r) => r !== repo);
  }

  function toggleAll(on: boolean): void {
    chosen = on ? migratable.map((r) => r.repo) : [];
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
    pool here could take. The rest are listed so you can see they were looked at.
    {#if blocked.length > 0}
      {blocked.length}
      {blocked.length === 1 ? 'is' : 'are'} archived or already on Zoomies, so
      {blocked.length === 1 ? 'it cannot be' : 'they cannot be'} chosen.
    {/if}
  </p>

  {#if plan?.truncated}
    <p class="note">
      This is the first page of the organisation. Migrate these, then run the wizard again for the
      rest — a batch of pull requests nobody can review is not progress.
    </p>
  {/if}

  <div class="head">
    <Checkbox
      checked={allChosen}
      indeterminate={!allChosen && someChosen}
      label="Select every repository that would change"
      onchange={toggleAll}
      disabled={migratable.length === 0}
    />
  </div>

  <ul class="repos">
    {#each rows as row (row.repo)}
      <li class:inert={!row.migratable}>
        <Checkbox
          checked={chosen.includes(row.repo)}
          disabled={!row.migratable}
          label={row.repo}
          description={row.note}
          onchange={(on) => toggle(row.repo, on)}
        />
      </li>
    {/each}
  </ul>
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
  .head {
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
  .repos li {
    padding: var(--z-space-2) 0;
    border-bottom: 1px solid var(--z-border);
  }
  .repos li.inert {
    opacity: 0.6;
  }
</style>
