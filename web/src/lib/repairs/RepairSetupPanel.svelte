<!--
  Repair settings: the GitHub account behind a person's repairs, the repository
  policies that say when Eli may help, and the form that links an account.

  It is the part of the old repairs page that was about setting things up, on a
  page of its own, so the hub is about repairs and this is about whether they can
  happen. Its behaviour is the old behaviour: the account card is for whoever is
  signed in, and the policies and the link form are for an administrator. The
  repair page's blockers open a wizard for the failure in hand instead
  (RepairSetup), so somebody fixing what is missing stays on the failure.
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { Link, Settings2 } from '@lucide/svelte';
  import {
    ApiError,
    getEliRepairSettings,
    listInstallations,
    listUsers,
    listAssistantProviders,
    setEliRepairPolicy,
    setEliIdentity,
    setEliRepairConsent,
  } from '$lib/api/client';
  import type { Schemas, Installation, User, AssistantProvider } from '$lib/api/types';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import RepositoryPicker from '$lib/repairs/RepositoryPicker.svelte';
  import { heldLink, heldLinkMessage, type HeldLink } from '$lib/repairs/identity';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Select from '$lib/components/Select.svelte';
  import Switch from '$lib/components/Switch.svelte';

  let {
    initialRepo = '',
  }: {
    /** A repository to have ready in the forms, as when somebody arrives from a failure. */
    initialRepo?: string;
  } = $props();

  let identities = $state<Schemas['EliIdentity'][]>([]);
  let policies = $state<Schemas['EliRepairPolicy'][]>([]);
  let installations = $state<Installation[]>([]);
  let users = $state<User[]>([]);
  let providers = $state<AssistantProvider[]>([]);
  let selectedRepos = $state<string[]>([]);
  let linkRepos = $state<string[]>([]);
  let mode = $state('ask');
  let bulkResults = $state<string[]>([]);
  let installation = $state('');
  let provider = $state('');
  let enabled = $state(true);
  let workflows = $state(false);
  let limit = $state('5');
  let user = $state('');
  let login = $state('');
  let linkInstallation = $state('');
  // A GitHub account one link may not serve two Zoomies accounts: after an
  // instance move or a re-created account, the old account's row is the thing
  // in the way, and this is the refusal the move button answers.
  let linkHeld = $state<HeldLink | null>(null);
  let busy = $state(false);
  let error = $state('');
  /** Which reads have answered, so a repository handed in is prepared once both have. */
  let heard = $state({ settings: false, choices: false });
  let prepared = false;
  const admin = $derived(session.can('admin'));
  const ownIdentity = $derived(identities.find((i) => i.user_id === session.identity?.id));
  const installationOptions = $derived(
    installations.map((i) => ({ value: i.id ?? '', label: i.target ?? i.id ?? 'Installation' })),
  );

  async function refresh(): Promise<void> {
    try {
      const settings = await getEliRepairSettings();
      identities = settings.identities;
      policies = settings.policies;
      error = '';
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Could not read PR repairs.';
    } finally {
      heard.settings = true;
    }
  }

  /** What the page header's Refresh means here: read everything again. */
  export async function reload(): Promise<void> {
    await Promise.all([refresh(), adminChoices()]);
  }

  async function adminChoices(): Promise<void> {
    if (!admin) return;
    try {
      const [i, u, p] = await Promise.all([
        listInstallations(),
        listUsers(),
        listAssistantProviders(undefined, 'installation'),
      ]);
      installations = i.items ?? [];
      if (!installation && installations.length === 1) installation = installations[0]?.id ?? '';
      if (!linkInstallation && installations.length === 1)
        linkInstallation = installations[0]?.id ?? '';
      users = u.items ?? [];
      providers = (p.items ?? []).filter((p) => !p.owner_id);
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Could not read repair settings.';
    } finally {
      heard.choices = true;
    }
  }

  // Read when the page opens and again if the person turns out to be an administrator.
  $effect(() => {
    void adminChoices();
  });

  onMount(() => void refresh());

  // A repository handed in is made ready in the forms once the policies and the
  // installations are known: its own policy to edit if it has one, and otherwise
  // the repository selected under the installation that serves it.
  $effect(() => {
    if (prepared || !initialRepo || !admin || !heard.settings || !heard.choices) return;
    prepared = true;
    untrack(() => prepare(initialRepo));
  });

  function prepare(repo: string): void {
    const policy = policies.find((item) => item.repo.toLowerCase() === repo.toLowerCase());
    if (policy) editPolicy(policy);
    else {
      selectedRepos = [repo];
      const match =
        installations.find((item) => item.target?.toLowerCase() === repo.toLowerCase()) ??
        installations.find(
          (item) => item.target?.toLowerCase() === repo.split('/')[0]?.toLowerCase(),
        );
      installation = match?.id ?? '';
    }
    linkRepos = [repo];
    linkInstallation = installation;
    user = session.identity?.id ?? '';
  }

  async function act(action: () => Promise<unknown>, success: string): Promise<void> {
    if (busy) return;
    busy = true;
    try {
      await action();
      toasts.success(success);
      await refresh();
      await adminChoices();
    } catch (cause) {
      toasts.error(
        'Could not complete that request',
        cause instanceof ApiError ? cause.message : 'Please try again.',
      );
    } finally {
      busy = false;
    }
  }

  /**
   * A verification the server could not write. The held-link refusal becomes
   * the card and its move button; everything else keeps the toast, because a
   * name it cannot act on is just text.
   */
  async function linkIdentity(relink = false): Promise<void> {
    if (busy) return;
    busy = true;
    linkHeld = null;
    try {
      await setEliIdentity({
        user_id: user,
        github_login: login.trim(),
        repo: linkRepos[0] ?? '',
        installation_id: linkInstallation,
        relink,
      });
      toasts.success(relink ? 'GitHub account link moved' : 'GitHub account linked');
      await refresh();
      await adminChoices();
    } catch (cause) {
      const held = heldLink(cause as { code?: string; detail?: string }, login, identities, users);
      if (held) {
        linkHeld = held;
        toasts.error('GitHub account already linked', heldLinkMessage(held));
      } else {
        toasts.error(
          'Could not complete that request',
          cause instanceof ApiError ? cause.message : 'Please try again.',
        );
      }
    } finally {
      busy = false;
    }
  }

  /** Moving the link is the same verification again, with the confirmation sent. */
  const moveHeld = (): Promise<void> => linkIdentity(true);
  function editPolicy(p: Schemas['EliRepairPolicy']): void {
    selectedRepos = [p.repo];
    installation = p.installation_id;
    provider = p.provider_id;
    enabled = p.enabled;
    mode = p.automatic ? 'automatic' : p.ask_first ? 'ask' : 'manual';
    workflows = p.allow_workflows;
    limit = String(p.daily_limit);
  }
  async function savePolicies(): Promise<void> {
    bulkResults = [];
    const targets = [...selectedRepos];
    const settings = {
      installation_id: installation,
      provider_id: mode === 'automatic' ? provider : '',
      enabled,
      automatic: mode === 'automatic',
      ask_first: mode === 'ask',
      allow_workflows: workflows,
      daily_limit: Number(limit),
    };
    for (const repo of targets) {
      try {
        await setEliRepairPolicy({ repo, ...settings });
        bulkResults = [...bulkResults, `${repo}: saved`];
      } catch (cause) {
        bulkResults = [
          ...bulkResults,
          `${repo}: ${cause instanceof ApiError ? cause.message : 'Could not save; try again.'}`,
        ];
      }
    }
  }
</script>

<section class="setup" aria-label="Repair settings">
  {#if error}<p role="alert" class="error">{error}</p>{/if}
  <div class="card" id="access">
    <h2>Your GitHub account</h2>
    {#if ownIdentity}<p>
        Linked to <strong>@{ownIdentity.github_login}</strong>. Comment
        <code>/eli fix this PR</code>
        or <code>/zoomies fix this issue</code> on a PR. Eli uses your personal provider: your default,
        or the first usable one you own.
      </p>
    {:else}<p>
        Ask an administrator to verify and link your GitHub account. You also need a personal
        provider and repository write access.
      </p>{/if}
    {#if ownIdentity}
      <Switch
        checked={ownIdentity.confirmed}
        label="Allow this GitHub account to request repairs using my provider"
        description="Enable only if this is your GitHub account. You can revoke access here at any time."
        disabled={busy}
        onchange={(value) =>
          void act(
            () =>
              setEliRepairConsent({ github_user_id: ownIdentity.github_user_id, enabled: value }),
            value ? 'Personal repair access enabled' : 'Personal repair access disabled',
          )}
      />
    {/if}
  </div>
  {#if admin}
    <details class="card" id="repositories" open>
      <summary><Settings2 size={16} /> Repairs in these repositories</summary>
      <p>
        Explicit requests use the requester's provider. Automatic repairs use the installation
        provider selected here, within the daily budget.
      </p>
      {#if policies.length === 0}
        <p class="lead">No repository has repairs turned on yet. Choose one below to start.</p>
      {/if}
      {#if policies.length}<div class="policy-list">
          {#each policies as p (p.repo)}<Button
              size="sm"
              variant="secondary"
              onclick={() => editPolicy(p)}>{p.repo}{p.enabled ? '' : ' (disabled)'}</Button
            >{/each}
        </div>{/if}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void act(savePolicies, 'Repository policies processed');
        }}
      >
        <div class="fields">
          <Field id="policy-installation" label="GitHub installation"
            ><Select
              id="policy-installation"
              bind:value={installation}
              onchange={() => (selectedRepos = [])}
              options={installationOptions}
              placeholder="Choose an installation"
              required
            /></Field
          >
          {#if mode === 'automatic'}<Field id="policy-provider" label="Installation provider"
              ><Select
                id="policy-provider"
                bind:value={provider}
                options={providers.map((p) => ({
                  value: p.id,
                  label: p.enabled ? p.name : `${p.name} (disabled)`,
                }))}
                placeholder="Choose a provider"
                required
              /></Field
            >{/if}
        </div>
        <Switch bind:checked={enabled} label="Allow PR repairs in this repository" />
        <Field id="repair-mode" label="When should Eli help?"
          ><Select
            id="repair-mode"
            bind:value={mode}
            options={[
              { value: 'ask', label: 'Ask me first (recommended)' },
              { value: 'manual', label: 'Manual only' },
              { value: 'automatic', label: 'Automatic' },
            ]}
          /></Field
        >
        <p>
          Ask me first offers help without spending model tokens. Automatic repairs use the
          installation provider and publish one commit per failed head.
        </p>
        {#key installation}<RepositoryPicker {installation} bind:selected={selectedRepos} />{/key}
        <details class="advanced">
          <summary>Advanced</summary>
          <Switch
            bind:checked={workflows}
            label="Allow edits to workflow files"
            description="Off by default. Allows changes to .github/workflows files."
          />
          <div id="allowance">
            <Field
              id="policy-limit"
              label="Tries per 24 hours"
              hint="Includes explicit requests and automatic tries. Personal accounts have an additional limit of 10."
              ><Input
                id="policy-limit"
                bind:value={limit}
                type="number"
                min={1}
                max={50}
                required
              /></Field
            >
          </div>
        </details>
        {#if selectedRepos.length === 0}
          <p class="hint">Choose at least one repository above.</p>
        {/if}
        <Button type="submit" disabled={busy || !selectedRepos.length}
          >Save in {selectedRepos.length}
          {selectedRepos.length === 1 ? 'repository' : 'repositories'}</Button
        >
        {#each bulkResults as result (result)}<p role="status">{result}</p>{/each}
      </form>
    </details>
    <details class="card" id="link-account">
      <summary><Link size={16} /> Link a GitHub account</summary>
      <p>
        Verify that the selected person owns this GitHub account before linking it. Eli checks its
        numeric identity and repository write permission.
      </p>
      {#if linkHeld}
        <div class="held card">
          <p>{heldLinkMessage(linkHeld)}</p>
          <Button size="sm" disabled={busy || !user} onclick={() => void moveHeld()}
            >Move the link to {users.find((u) => u.id === user)?.username ?? 'this account'}</Button
          >
        </div>
      {/if}
      <form
        onsubmit={(e) => {
          e.preventDefault();
          void linkIdentity();
        }}
      >
        <div class="fields">
          <Field id="link-user" label="Zoomies user"
            ><Select
              id="link-user"
              bind:value={user}
              options={users
                .filter((u) => !u.disabled)
                .map((u) => ({ value: u.id ?? '', label: u.username ?? u.id ?? 'User' }))}
              placeholder="Choose a user"
              required
            /></Field
          ><Field id="link-login" label="GitHub username"
            ><Input id="link-login" bind:value={login} placeholder="octocat" required /></Field
          >
        </div>
        <div class="fields">
          <Field id="link-installation" label="GitHub installation"
            ><Select
              id="link-installation"
              bind:value={linkInstallation}
              onchange={() => (linkRepos = [])}
              options={installationOptions}
              placeholder="Choose an installation"
              required
            /></Field
          >
        </div>
        <RepositoryPicker
          installation={linkInstallation}
          bind:selected={linkRepos}
          multiple={false}
          id="verify-repository-search"
        />
        <Button type="submit" disabled={busy || !linkRepos.length}>Verify and link account</Button>
      </form>
      {#each identities as i (i.user_id)}<div class="identity">
          <span
            >{users.find((u) => u.id === i.user_id)?.username ?? i.user_id} → @{i.github_login}</span
          ><Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onclick={() =>
              void act(
                () =>
                  setEliIdentity({
                    user_id: i.user_id,
                    github_login: '',
                    repo: '',
                    installation_id: '',
                  }),
                'Account link removed',
              )}>Unlink</Button
          >
        </div>{/each}
    </details>
  {/if}
</section>

<style>
  .setup {
    display: grid;
    gap: var(--z-space-4);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
  }
  p {
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    margin: var(--z-space-2) 0 var(--z-space-4);
    line-height: var(--z-leading-lg);
  }
  .card {
    padding: var(--z-space-4);
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
  }
  /* The held-link card is the move press's frame: it sits beside the fields
     it acts on, and the danger edge is what makes "own link" and "held link"
     read apart when both rows are on the page at once. */
  .held.card {
    border-color: var(--z-danger);
  }
  .held.card p {
    margin: var(--z-space-1) 0 var(--z-space-3);
  }
  summary {
    display: flex;
    gap: var(--z-space-3);
    align-items: center;
    cursor: pointer;
    font-weight: var(--z-weight-semibold);
    font-size: var(--z-text-sm);
  }
  form {
    display: grid;
    gap: var(--z-space-4);
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-4);
  }
  .lead {
    color: var(--z-text);
  }
  .hint {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .advanced {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
  }
  .advanced > summary {
    padding: var(--z-space-3) var(--z-space-4);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    cursor: pointer;
  }
  .advanced > :global(:not(summary)) {
    margin: 0 var(--z-space-4) var(--z-space-4);
  }
  .policy-list {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-bottom: var(--z-space-4);
  }
  .identity {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-sm);
    flex-wrap: wrap;
  }
  .error {
    color: var(--z-danger);
  }
  @media (max-width: 640px) {
    .fields {
      grid-template-columns: 1fr;
    }
  }
</style>
