<!--
  API tokens.

  A token is a credential for automation: the CLI, a Prometheus scrape, a
  deploy job. The plaintext exists once, in the response that creates it, and
  the panel is built around that fact -- the value is shown in a block that says
  it will not be shown again, and everything afterwards is metadata.
-->
<script lang="ts">
  import { KeyRound, Plus } from '@lucide/svelte';
  import { ApiError, createToken, listTokens, revokeToken } from '$lib/api/client';
  import type { APIToken, Role } from '$lib/api/types';
  import { toasts } from '$lib/state/toasts.svelte';
  import { ROLE_OPTIONS, roleLabel } from '$lib/roles';
  import { apiTokenStatus } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Select from '$lib/components/Select.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import OneTimeSecret from './OneTimeSecret.svelte';

  type Minted = APIToken & { token?: string };

  const EXPIRY_OPTIONS = [
    { value: '720h', label: '30 days' },
    { value: '2160h', label: '90 days' },
    { value: '8760h', label: 'A year' },
    { value: '', label: 'Never (not recommended)' },
  ];

  interface Props {
    /**
     * Bumped by the page's refresh button. Read inside the fetch effect, which
     * is what makes one press at the top of Settings re-read whichever panel is
     * open rather than only the tab the operator happens to be looking past.
     */
    reloadKey?: number;
  }

  let { reloadKey = 0 }: Props = $props();

  let tokens = $state<APIToken[]>([]);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let reload = $state(0);

  $effect(() => {
    void reload;
    void reloadKey;
    const controller = new AbortController();
    loading = true;
    void listTokens(controller.signal)
      .then((result) => {
        tokens = result.items ?? [];
        error = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        error = cause;
      })
      .finally(() => (loading = false));
    return () => controller.abort();
  });

  /* -- create -------------------------------------------------------------------- */

  let createOpen = $state(false);
  let name = $state('');
  let role = $state('viewer');
  let expiry = $state('2160h');
  let scopeText = $state('');
  let creating = $state(false);
  let errors = $state<Record<string, string>>({});
  let minted = $state<Minted | null>(null);

  function open(): void {
    name = '';
    role = 'viewer';
    expiry = '2160h';
    scopeText = '';
    errors = {};
    minted = null;
    createOpen = true;
  }

  const scopes = $derived(
    scopeText
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean),
  );

  async function mint(): Promise<void> {
    if (!name.trim()) return;
    creating = true;
    errors = {};
    try {
      minted = await createToken({
        name: name.trim(),
        role: role as Role,
        scopes: scopes.length > 0 ? scopes : undefined,
        expires_in: expiry || undefined,
      });
      reload += 1;
    } catch (cause) {
      if (cause instanceof ApiError) errors = cause.fieldErrors();
      toasts.fromError(cause, 'That token was not created');
    } finally {
      creating = false;
    }
  }

  /* -- revoke --------------------------------------------------------------------- */

  let revokeOpen = $state(false);
  let revoking = $state<APIToken | null>(null);

  async function revoke(): Promise<boolean> {
    const token = revoking;
    if (!token?.id) return false;
    try {
      await revokeToken(token.id);
      toasts.success(`${token.name ?? 'Token'} revoked`, 'Anything using it stops working now.');
      reload += 1;
      return true;
    } catch (cause) {
      toasts.fromError(cause, 'That token was not revoked');
      return false;
    }
  }
</script>

<div class="panel">
  <header>
    <div>
      <h2>API tokens</h2>
      <p>
        Bearer credentials for the CLI and for automation. Zoomies keeps only the hash, so a token
        that is lost has to be revoked and replaced.
      </p>
    </div>
    <Button variant="primary" icon={Plus} onclick={open}>Create a token</Button>
  </header>

  <LoadingBoundary
    {loading}
    {error}
    empty={!loading && !error && tokens.length === 0}
    onretry={() => (reload += 1)}
  >
    {#snippet skeleton()}
      <div class="pad"><Skeleton lines={3} /></div>
    {/snippet}

    {#snippet emptyState()}
      <EmptyState
        icon={KeyRound}
        title="No API tokens"
        description="A token lets the zoomies CLI or a script talk to this controller without a browser session."
      >
        <Button variant="primary" icon={Plus} onclick={open}>Create a token</Button>
      </EmptyState>
    {/snippet}

    <div class="scroll">
      <!--
      Every role is spelled out rather than left to the table's own display
      type. The rows become cards on a phone, which means `display` stops
      being `table-row`, and a browser drops the implicit row and cell roles
      the moment it does -- leaving a screen reader a run of loose text with
      nothing saying which value belongs to which record.
    -->
      <!-- svelte-ignore a11y_no_redundant_roles -->
      <table role="table">
        <caption class="sr-only">API tokens</caption>
        <!-- svelte-ignore a11y_no_redundant_roles -->
        <thead role="rowgroup">
          <!-- svelte-ignore a11y_no_redundant_roles -->
          <tr role="row">
            <th role="columnheader" scope="col">Name</th>
            <th role="columnheader" scope="col">Prefix</th>
            <th role="columnheader" scope="col">Role</th>
            <th role="columnheader" scope="col">Scopes</th>
            <th role="columnheader" scope="col">Last used</th>
            <th role="columnheader" scope="col">Expires</th>
            <th role="columnheader" scope="col"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <!-- svelte-ignore a11y_no_redundant_roles -->
        <tbody role="rowgroup">
          {#each tokens as token (token.id)}
            {@const state = apiTokenStatus(token)}
            <tr role="row" class:revoked={token.revoked}>
              <td role="cell" data-label="Name" class="name">{token.name}</td>
              <td role="cell" data-label="Prefix" class="mono">{token.prefix ?? '--'}</td>
              <td role="cell" data-label="Role">{roleLabel(token.role)}</td>
              <td role="cell" data-label="Scopes" class="scopes mono">
                {#if (token.scopes ?? []).length === 0}
                  <span class="muted">Whatever the role allows</span>
                {:else}
                  {(token.scopes ?? []).join(' ')}
                {/if}
              </td>
              <td role="cell" data-label="Last used">
                {#if token.last_used_at}
                  <RelativeTime value={token.last_used_at} />
                {:else}
                  <span class="muted">Never used</span>
                {/if}
              </td>
              <td role="cell" data-label="Expires">
                <Badge status={state} size="sm" />
                {#if token.expires_at && !token.revoked}
                  <span class="second"><RelativeTime value={token.expires_at} plain /></span>
                {/if}
              </td>
              <td role="cell" data-label="Actions" class="actions">
                {#if !token.revoked}
                  <Button
                    size="sm"
                    variant="ghost"
                    onclick={() => {
                      revoking = token;
                      revokeOpen = true;
                    }}
                  >
                    Revoke
                  </Button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </LoadingBoundary>
</div>

<Dialog
  bind:open={createOpen}
  title="Create an API token"
  description={minted
    ? 'Copy it now. This is the only time it exists in plain text.'
    : 'It carries a role, and optionally narrower scopes within that role.'}
  size="md"
>
  {#if minted}
    <div class="form">
      <OneTimeSecret
        what="API token"
        value={minted.token ?? ''}
        copyLabel="Copy the token"
        note={minted.expires_at
          ? undefined
          : 'It never expires, so revoke it when it is done with.'}
      />
      <p class="usage">
        Use it as <span class="mono">Authorization: Bearer &lt;token&gt;</span>, or give it to the
        CLI as <span class="mono">ZOOMIES_TOKEN</span>.
      </p>
    </div>
  {:else}
    <div class="form">
      <Field label="Name" hint="What is using it: prometheus, ci-deploy." error={errors.name}>
        {#snippet children({ id, describedBy, invalid })}
          <Input bind:value={name} {id} {describedBy} {invalid} autocomplete="off" />
        {/snippet}
      </Field>

      <RadioGroup bind:value={role} name="token-role" legend="Role" options={ROLE_OPTIONS} />

      <Field
        label="Expires"
        hint="A token that never expires is one more thing to remember. Prefer a date."
        error={errors.expires_in}
      >
        {#snippet children({ id, describedBy, invalid })}
          <Select bind:value={expiry} options={EXPIRY_OPTIONS} {id} {describedBy} {invalid} />
        {/snippet}
      </Field>

      <Field
        label="Scopes"
        hint="Optional. Space-separated, e.g. pools:read runners:write. Empty means everything the role allows."
        error={errors.scopes}
      >
        {#snippet children({ id, describedBy, invalid })}
          <Input bind:value={scopeText} {id} {describedBy} {invalid} mono autocomplete="off" />
        {/snippet}
      </Field>
    </div>
  {/if}

  {#snippet footer()}
    {#if minted}
      <Button variant="primary" onclick={() => (createOpen = false)}>Done</Button>
    {:else}
      <Button variant="ghost" onclick={() => (createOpen = false)}>Cancel</Button>
      <Button variant="primary" loading={creating} disabled={!name.trim()} onclick={mint}>
        Create token
      </Button>
    {/if}
  {/snippet}
</Dialog>

<ConfirmDialog
  bind:open={revokeOpen}
  title="Revoke token"
  name={revoking?.name}
  description="{revoking?.name ?? 'This token'} stops working immediately."
  consequences={['Anything still using it will start getting 401 responses.']}
  confirmLabel="Revoke"
  onconfirm={revoke}
  oncancel={() => (revoking = null)}
/>

<style>
  .panel {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  header {
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
  header p {
    margin: var(--z-space-1) 0 0;
    max-width: 74ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .pad {
    padding: var(--z-space-5);
  }
  .scroll {
    overflow-x: auto;
    /* The table is wider than a phone and scrolls inside this box, but a
       mobile browser still counts what it clips towards the page's width,
       grows the layout viewport to fit, and the fixed bottom navigation grows
       with it -- so the whole page scrolls sideways. Paint containment says
       what is clipped here stays here. */
    contain: paint;
  }
  table {
    width: 100%;
    /* The frame decides the width and the columns divide it, so the list never
       scrolls sideways -- see "Tables fit the window" in the UI guidelines. */
    table-layout: fixed;
    border-collapse: separate;
    border-spacing: 0;
    font-size: var(--z-text-sm);
  }
  th {
    /* A heading has nowhere to wrap when it is one word, and one cut to
       "Last sig..." names nothing, so it breaks instead. */
    overflow-wrap: anywhere;
    padding: var(--z-space-2) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-align: left;
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
  }
  td {
    overflow: hidden;
    overflow-wrap: anywhere;
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-text);
    vertical-align: top;
  }
  tbody tr:last-child td {
    border-bottom: 0;
  }
  tr.revoked td {
    color: var(--z-text-subtle);
  }
  .name {
    font-weight: var(--z-weight-medium);
  }
  .scopes {
    font-size: var(--z-text-xs);
    max-width: 20rem;
    overflow-wrap: anywhere;
  }
  .second {
    display: block;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  .muted {
    color: var(--z-text-subtle);
  }
  .actions {
    text-align: right;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-2);
  }
  .usage {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
  /*
    On a phone this is 7 columns in 360 pixels, and no amount of scrolling
    makes that a table -- the same problem, and the same answer, as the usage
    report and the grids: each row becomes a card and each cell carries its own
    heading. Nothing is dropped and nothing is truncated; the list reads down
    instead of across.
  */
  @media (max-width: 768px) {
    .scroll {
      overflow-x: visible;
      /*
        Nothing is clipped here any more -- the cards fit -- and the paint
        containment that kept a wide table from growing the layout viewport
        would now cut off a menu opened from the last row of a card.
      */
      contain: none;
    }
    table {
      display: block;
    }
    thead {
      display: none;
    }
    tbody {
      display: flex;
      flex-direction: column;
      gap: var(--z-space-3);
      padding: var(--z-space-3);
    }
    tbody tr {
      display: block;
      border: var(--z-border-width) solid var(--z-border);
      border-radius: var(--z-radius-md);
      background: var(--z-surface);
    }
    tbody td {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: var(--z-space-4);
      padding: var(--z-space-2) var(--z-space-3);
      border: 0;
      text-align: right;
      overflow-wrap: anywhere;
    }
    tbody td::before {
      content: attr(data-label);
      flex: none;
      color: var(--z-text-muted);
      font-size: var(--z-text-2xs);
      font-weight: var(--z-weight-medium);
      text-transform: uppercase;
      letter-spacing: var(--z-tracking-wide);
      text-align: left;
    }
  }
</style>
