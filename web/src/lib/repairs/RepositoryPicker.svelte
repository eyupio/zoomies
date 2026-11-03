<script lang="ts">
  import { listEliRepairRepositories, ApiError } from '$lib/api/client';
  import Input from '$lib/components/Input.svelte';
  import Field from '$lib/components/Field.svelte';
  import Button from '$lib/components/Button.svelte';
  let {
    installation,
    selected = $bindable([]),
    multiple = true,
    id = 'repair-repository-search',
    disabled = false,
    selectingAll = $bindable(false),
  }: {
    installation: string;
    selected?: string[];
    multiple?: boolean;
    id?: string;
    disabled?: boolean;
    selectingAll?: boolean;
  } = $props();
  let query = $state('');
  let items = $state<string[]>([]);
  let total = $state(0);
  let offset = $state(0);
  let missing = $state<string[]>([]);
  let capped = $state(false);
  let error = $state('');
  let loading = $state(false);
  let selectionAbort: AbortController | undefined;
  $effect(() => {
    // A selection belongs to the connection and search that were visible when
    // it started. Changing either must not add rows from the old search.
    void installation;
    void query;
    return () => selectionAbort?.abort();
  });
  $effect(() => {
    const install = installation;
    const q = query;
    const page = offset;
    const abort = new AbortController();
    const timer = setTimeout(async () => {
      if (!install) {
        items = [];
        return;
      }
      loading = true;
      try {
        const result = await listEliRepairRepositories(
          { installation_id: install, q, offset: page },
          abort.signal,
        );
        if (abort.signal.aborted) return;
        items = result.items;
        total = result.total;
        missing = result.missing_permissions;
        capped = result.capped;
        error = '';
      } catch (cause) {
        if (!abort.signal.aborted)
          error = cause instanceof ApiError ? cause.message : 'Could not discover repositories.';
      } finally {
        if (!abort.signal.aborted) loading = false;
      }
    }, 200);
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  });
  function choose(repo: string, checked: boolean): void {
    selected = checked
      ? multiple
        ? [...new Set([...selected, repo])]
        : [repo]
      : selected.filter((value) => value !== repo);
  }
  async function selectAll(): Promise<void> {
    if (disabled || selectingAll || !installation) return;
    selectingAll = true;
    const controller = new AbortController();
    selectionAbort = controller;
    const repos: string[] = [];
    try {
      for (let page = 0; page < 10; page++) {
        const result = await listEliRepairRepositories(
          { installation_id: installation, q: query, offset: page * 50 },
          controller.signal,
        );
        if (controller.signal.aborted) return;
        repos.push(...result.items);
        if ((page + 1) * 50 >= result.total || result.items.length < 50) break;
      }
      selected = [...new Set([...selected, ...repos])];
      error = '';
    } catch (cause) {
      if (!controller.signal.aborted)
        error =
          cause instanceof ApiError ? cause.message : 'Could not select repositories. Try again.';
    } finally {
      if (selectionAbort === controller) selectingAll = false;
    }
  }
</script>

<div class="picker">
  <Field {id} label="Find repositories"
    ><Input
      {id}
      bind:value={query}
      oninput={() => (offset = 0)}
      placeholder="Search connected repositories"
      {disabled}
    /></Field
  >
  {#if error}<p role="alert">{error}</p>{/if}
  {#if missing.length}<p>
      App setup needs: {missing.join(', ')}.
      <a href="/installations">Review installation permissions</a>.
    </p>{/if}
  {#if installation}
    <fieldset aria-busy={loading || selectingAll} disabled={disabled || selectingAll}>
      <legend>{multiple ? 'Select repositories' : 'Select a repository'}</legend>
      {#each items as repo (repo)}<label
          ><input
            type="checkbox"
            checked={selected.includes(repo)}
            onchange={(e) => choose(repo, e.currentTarget.checked)}
          />{repo}</label
        >{/each}
      {#if !items.length && !loading}<p>No matching repositories.</p>{/if}
    </fieldset>
    <div class="paging">
      <Button
        size="sm"
        disabled={disabled || selectingAll || offset === 0 || loading}
        onclick={() => (offset = Math.max(0, offset - 50))}>Previous</Button
      ><span>{offset + (items.length ? 1 : 0)}–{offset + items.length} of {total}</span><Button
        size="sm"
        disabled={disabled || selectingAll || offset + items.length >= total || loading}
        onclick={() => (offset += 50)}>Next</Button
      >
    </div>
    {#if capped}<p>
        Discovery reached its 500-repository limit. Narrow the GitHub installation's repository
        selection to include the repositories you need.
      </p>{/if}
    {#if multiple}<p>
        {selected.length} selected <Button
          size="sm"
          variant="ghost"
          disabled={disabled || loading || selectingAll}
          onclick={() => (selected = [...new Set([...selected, ...items])])}
          >Select this page</Button
        ><Button
          size="sm"
          variant="ghost"
          disabled={disabled || loading || selectingAll || !total}
          loading={selectingAll}
          onclick={() => void selectAll()}>Select all matching repositories</Button
        >
        <Button
          size="sm"
          variant="ghost"
          disabled={disabled || selectingAll}
          onclick={() => (selected = [])}>Clear selection</Button
        >
      </p>{/if}
  {:else}<p>Choose a GitHub installation to discover its repositories.</p>{/if}
</div>

<style>
  .picker {
    display: grid;
    gap: var(--z-space-3);
  }
  fieldset {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    max-height: 18rem;
    overflow: auto;
  }
  label {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    padding: var(--z-space-2);
    overflow-wrap: anywhere;
  }
  p,
  .paging {
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .paging {
    display: flex;
    gap: var(--z-space-3);
    align-items: center;
  }
  a {
    color: var(--z-accent);
  }
</style>
