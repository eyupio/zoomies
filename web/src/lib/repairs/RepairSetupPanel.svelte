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
  import { onMount, tick, untrack } from 'svelte';
  import { Link } from '@lucide/svelte';
  import {
    ApiError,
    admitEliWatchRepository,
    getEliRepairSettings,
    listEliWatchRepositories,
    listEliWatches,
    listInstallations,
    listUsers,
    listAssistantProviders,
    setEliRepairPolicy,
    setEliIdentity,
    setEliRepairConsent,
  } from '$lib/api/client';
  import type { Schemas, Installation, User, AssistantProvider } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import RepositoryPicker from '$lib/repairs/RepositoryPicker.svelte';
  import WatchedRepositories from '$lib/repairs/WatchedRepositories.svelte';
  import {
    pauseRepository,
    pauseWatch,
    resumeRepository,
    resumeWatch,
    stopAllWatches,
    stopWatch,
  } from '$lib/repairs/watchActions';
  import {
    BEHAVIOURS,
    behaviourOf,
    behaviourLabel,
    capabilitiesFor,
    pauseWatchCopy,
    stopAllCopy,
    stopWatchCopy,
    watchingState,
    type Behaviour,
    type RepositoryWatch,
    type Watch,
    type WatchControlCopy,
  } from '$lib/repairs/watches';
  import { heldLink, heldLinkMessage, type HeldLink } from '$lib/repairs/identity';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
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
  let selectingAll = $state(false);
  let linkRepos = $state<string[]>([]);
  let mode = $state('automatic');
  /** What Eli should do in the chosen repositories: the three behaviours, and nothing else in front. */
  let behaviour = $state<Behaviour>('babysit');
  /** What each repository got in the last save, so a partial result can say which part failed. */
  type SaveResult = { repo: string; saved: boolean; error?: string; watching?: string };
  type PolicySettings = Omit<Schemas['EliRepairPolicy'], 'repo'>;
  let bulkResults = $state<SaveResult[]>([]);
  let lastSettings = $state<PolicySettings | null>(null);
  let editing = $state('');
  let policySearch = $state('');
  let installation = $state('');
  let provider = $state('');
  let enabled = $state(true);
  let workflows = $state(false);
  let user = $state('');
  let login = $state('');
  let linkInstallation = $state('');
  // A GitHub account one link may not serve two Zoomies accounts: after an
  // instance move or a re-created account, the old account's row is the thing
  // in the way, and this is the refusal the move button answers.
  let linkHeld = $state<HeldLink | null>(null);
  let busy = $state(false);
  let settingsError = $state('');
  let choicesError = $state('');
  const error = $derived([settingsError, choicesError].filter(Boolean).join(' '));
  /** What is being watched now, so saving and its result are on the same page. */
  let decisions = $state<RepositoryWatch[]>([]);
  let watches = $state<Watch[]>([]);
  let watchLoaded = $state(false);
  let watchFailed = $state(false);
  let watchError = $state('');
  let watchBusy = $state<string[]>([]);
  let confirming = $state<{ copy: WatchControlCopy; run: () => Promise<string> } | null>(null);
  let confirmOpen = $state(false);
  let confirmBusy = $state(false);
  let confirmError = $state('');
  /** Which reads have answered, so a repository handed in is prepared once both have. */
  let heard = $state({ settings: false, choices: false });
  let prepared = false;
  const admin = $derived(session.can('admin'));
  const ownIdentity = $derived(identities.find((i) => i.user_id === session.identity?.id));
  const usableProviders = $derived(providers.filter((p) => p.enabled));
  const selectedProvider = $derived(providers.find((p) => p.id === provider));
  const failedRepos = $derived(bulkResults.filter((r) => !r.saved).map((r) => r.repo));
  const filteredPolicies = $derived(
    policies.filter((p) => p.repo.toLowerCase().includes(policySearch.trim().toLowerCase())),
  );
  const enabledPolicies = $derived(policies.filter((p) => p.enabled).length);
  const installationOptions = $derived(
    installations.map((i) => ({ value: i.id ?? '', label: i.target ?? i.id ?? 'Installation' })),
  );

  async function refresh(): Promise<void> {
    try {
      const settings = await getEliRepairSettings();
      identities = settings.identities;
      policies = settings.policies;
      settingsError = '';
    } catch (cause) {
      settingsError = cause instanceof ApiError ? cause.message : 'Could not read PR repairs.';
    } finally {
      heard.settings = true;
    }
  }

  /** What the page header's Refresh means here: read everything again. */
  export async function reload(): Promise<void> {
    await Promise.all([refresh(), adminChoices(), readWatching()]);
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
      choicesError = '';
      if (!editing && !provider) {
        const available = providers.filter((item) => item.enabled);
        provider =
          available.find((item) => item.is_default)?.id ??
          (available.length === 1 ? available[0]!.id : '');
      }
    } catch (cause) {
      choicesError = cause instanceof ApiError ? cause.message : 'Could not read repair settings.';
    } finally {
      heard.choices = true;
    }
  }

  // Read when the page opens and again if the person turns out to be an administrator.
  $effect(() => {
    void adminChoices();
  });

  // What is being watched is part of this page, so it is read as soon as an
  // administrator can read it, not only when a save happens.
  $effect(() => {
    if (admin) void readWatching();
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
  async function focusPolicyForm(): Promise<void> {
    await tick();
    document.getElementById('policy-form-heading')?.focus();
  }
  function editPolicy(p: Schemas['EliRepairPolicy'], focus = false): void {
    editing = p.repo;
    bulkResults = [];
    lastSettings = null;
    selectedRepos = [p.repo];
    installation = p.installation_id;
    provider = p.provider_id;
    enabled = p.enabled;
    mode = p.automatic ? 'automatic' : p.ask_first ? 'ask' : 'manual';
    workflows = p.allow_workflows;
    // What the repository already does, so opening it for editing does not
    // quietly propose a different behaviour than the one it is on.
    const decided = decisions.find((d) => d.repo.toLowerCase() === p.repo.toLowerCase());
    behaviour = (decided && behaviourOf(decided.checks, decided.conflicts)) || 'babysit';
    if (focus) void focusPolicyForm();
  }
  function startNewPolicy(): void {
    editing = '';
    selectedRepos = [];
    mode = 'automatic';
    behaviour = 'babysit';
    enabled = true;
    workflows = false;
    provider =
      usableProviders.find((p) => p.is_default)?.id ??
      (usableProviders.length === 1 ? usableProviders[0]!.id : '');
    bulkResults = [];
    lastSettings = null;
    void focusPolicyForm();
  }
  async function savePolicies(
    targets = [...selectedRepos],
    settings: PolicySettings = {
      installation_id: installation,
      provider_id: mode === 'automatic' ? provider : '',
      enabled,
      automatic: mode === 'automatic',
      ask_first: mode === 'ask',
      allow_workflows: workflows,
    },
  ): Promise<void> {
    if (busy) return;
    busy = true;
    bulkResults = [];
    lastSettings = settings;
    const watching = settings.automatic && settings.enabled;
    for (const repo of targets) {
      try {
        await setEliRepairPolicy({ repo, ...settings });
      } catch (cause) {
        bulkResults = [
          ...bulkResults,
          {
            repo,
            saved: false,
            error: cause instanceof ApiError ? cause.message : 'Could not save; try again.',
          },
        ];
        continue;
      }
      // The policy is what repairs are allowed to be. Actually following the
      // repository's pull requests is a second, explicit decision, so a save
      // that turns automatic repairs on also admits them and says what came of
      // it. An admission the controller refused leaves the policy saved and
      // names the reason, rather than reporting a half-done job as done.
      if (!watching) {
        bulkResults = [...bulkResults, { repo, saved: true }];
        continue;
      }
      try {
        const { report } = await admitEliWatchRepository({
          repo,
          installation_id: settings.installation_id,
          capabilities: capabilitiesFor(behaviour),
        });
        bulkResults = [...bulkResults, { repo, saved: true, watching: admitWords(report) }];
      } catch (cause) {
        bulkResults = [
          ...bulkResults,
          {
            repo,
            saved: true,
            watching:
              'Saved, but Eli is not watching this repository: ' +
              (cause instanceof ApiError ? cause.message : 'the request was refused.'),
          },
        ];
      }
    }
    const saved = bulkResults.filter((result) => result.saved).length;
    const admitted = bulkResults.filter(
      (result) => result.watching && !result.watching.startsWith('Saved, but'),
    ).length;
    if (saved === targets.length)
      toasts.success(
        admitted
          ? `Saved repair settings in ${saved === 1 ? 'one repository' : `${saved} repositories`}, watching ${admitted}`
          : `Saved repair settings in ${saved} ${saved === 1 ? 'repository' : 'repositories'}`,
      );
    else
      toasts.error(
        saved ? `Saved ${saved} of ${targets.length} repositories` : 'No repository settings saved',
        'Review the failed repositories below, then retry.',
      );
    await reload();
    busy = false;
  }
  const modeLabel = (p: Schemas['EliRepairPolicy']): string =>
    !p.enabled
      ? 'Paused'
      : p.automatic
        ? 'Fix failed checks automatically'
        : p.ask_first
          ? 'Offer a fix for failed checks'
          : 'Manual requests only';

  /** The mode, and what the repository is actually following when that is known. */
  function policyWords(
    p: Schemas['EliRepairPolicy'],
    decided: RepositoryWatch | undefined,
  ): string {
    if (!decided) return modeLabel(p);
    const behaviour = behaviourOf(decided.checks, decided.conflicts);
    return behaviour ? `${modeLabel(p)} · ${behaviourLabel(behaviour)}` : modeLabel(p);
  }

  /** One repository's line in the save's results: what saved and what the admission did. */
  function resultWords(result: SaveResult): string {
    if (!result.saved) return result.error ?? 'Could not save.';
    return result.watching ? `saved · ${result.watching}` : 'saved';
  }

  const watching = $derived(
    watchingState({
      loaded: watchLoaded,
      failed: watchFailed,
      repositories: decisions.length,
      watches: watches.length,
    }),
  );

  /**
   * The two watch reads are one answer, as they are on the hub: a failure in
   * either means this page cannot say what is being followed, so neither half is
   * trusted and the section says so.
   */
  async function readWatching(): Promise<void> {
    try {
      const [d, w] = await Promise.all([
        listEliWatchRepositories(),
        listEliWatches({ limit: 100 }),
      ]);
      decisions = d.items ?? [];
      watches = w.items ?? [];
      watchFailed = false;
      watchError = '';
    } catch (cause) {
      watchFailed = true;
      watchError =
        cause instanceof ApiError ? cause.message : 'Could not read what Eli is watching.';
    } finally {
      watchLoaded = true;
    }
  }

  /** What one repository admission did, in the words the person needs to judge it. */
  function admitWords(report: Schemas['EliRepositoryWatchReport']): string {
    const parts: string[] = [];
    if (report.admitted.length)
      parts.push(`watching ${pluralise(report.admitted.length, 'open pull request')}`);
    if (report.existing) parts.push(`${report.existing} already watched`);
    if (report.excluded.length)
      parts.push(`${pluralise(report.excluded.length, 'pull request')} left alone`);
    if (report.capped) parts.push('the list stopped at the page limit, so more may exist');
    return parts.length ? parts.join(', ') : 'nothing open to watch yet';
  }

  async function changeWatch(id: string, action: () => Promise<string>): Promise<void> {
    if (watchBusy.includes(id)) return;
    watchBusy = [...watchBusy, id];
    try {
      if (!(await action())) await readWatching();
    } finally {
      watchBusy = watchBusy.filter((busyId) => busyId !== id);
    }
  }

  function confirmControl(copy: WatchControlCopy, run: () => Promise<string>): void {
    confirming = { copy, run };
    confirmError = '';
    confirmOpen = true;
  }

  async function runConfirmed(): Promise<boolean> {
    if (!confirming || confirmBusy) return false;
    confirmBusy = true;
    const failure = await confirming.run();
    confirmBusy = false;
    if (failure) {
      confirmError = failure;
      return false;
    }
    confirmError = '';
    await readWatching();
    return true;
  }
</script>

<section class="setup" aria-label="Repair settings">
  {#if error}<p role="alert" class="error">{error}</p>{/if}
  {#if admin}
    <section class="card" id="repositories" aria-labelledby="repository-repairs-heading">
      <h2 id="repository-repairs-heading">Choose repositories</h2>
      <p>
        Enable failed-check repairs for a few repositories or all the repositories you select. No
        personal GitHub link is needed for automatic repairs.
      </p>
      <form
        onsubmit={(event) => {
          event.preventDefault();
          void savePolicies();
        }}
      >
        <h3 id="policy-form-heading" tabindex="-1">
          {editing ? `Editing ${editing}` : 'Add repository repairs'}
        </h3>
        {#if editing}
          <Button disabled={busy} onclick={startNewPolicy}>Add repositories instead</Button>
        {/if}
        {#if installations.length > 1}
          <Field id="policy-installation" label="GitHub connection">
            <Select
              id="policy-installation"
              bind:value={installation}
              onchange={() => (selectedRepos = [])}
              options={installationOptions}
              placeholder="Choose a connection"
              required
              disabled={busy || !!editing}
            />
          </Field>
        {:else if installations.length === 1}
          <p>Connected through {installations[0]?.target}.</p>
        {:else if heard.choices && !choicesError}
          <p><a href="/installations">Connect GitHub</a> to choose repositories.</p>
        {/if}
        {#if !editing}
          {#key installation}<RepositoryPicker
              {installation}
              bind:selected={selectedRepos}
              bind:selectingAll
              disabled={busy}
            />{/key}
        {/if}
        <RadioGroup
          name="repair-behaviour"
          legend="What should Eli do in these repositories?"
          bind:value={behaviour}
          disabled={busy}
          options={BEHAVIOURS.map((choice) => ({
            value: choice.value,
            label: choice.label,
            description: choice.description,
          }))}
        />
        {#if mode === 'automatic' && enabled}
          {#if usableProviders.length === 1 && selectedProvider?.enabled}
            <p>
              Using <strong>{selectedProvider.name}</strong> ({selectedProvider.model}), paid by the
              installation. <a href="/settings/assistant">Manage model providers</a>.
            </p>
          {:else if usableProviders.length}
            <Field id="policy-provider" label="Team model provider">
              <Select
                id="policy-provider"
                bind:value={provider}
                disabled={busy}
                options={usableProviders.map((p) => ({ value: p.id, label: p.name }))}
                placeholder="Choose a provider"
                required
              />
            </Field>
          {:else}
            <p role="status">
              Add an enabled installation model provider to turn on automatic repairs. <a
                href="/settings/assistant">Set up a model provider</a
              >, then refresh this page.
            </p>
          {/if}
          <p>
            Eli reads source and failed-job evidence, publishes one fix per failing commit, then
            checks CI. It never merges. Earlier failures are not picked up automatically.
          </p>
        {:else if mode === 'ask'}
          <p>
            Eli offers help when checks fail. Reviewing an offer uses no model tokens. Accepting it
            uses your personal provider.
          </p>
        {:else if mode === 'manual'}
          <p>
            A person requests each repair using their own provider. Personal repair setup is below.
          </p>
        {/if}
        <details class="advanced">
          <summary>When Eli acts, and what it may edit</summary>
          <Field id="repair-mode" label="Repair mode">
            <Select
              id="repair-mode"
              bind:value={mode}
              disabled={busy}
              options={[
                { value: 'automatic', label: 'Repair without asking, and keep watching' },
                { value: 'ask', label: 'Offer a fix for failed checks' },
                { value: 'manual', label: 'Manual requests only' },
              ]}
            />
          </Field>
          <p class="hint">
            Only the first can follow pull requests. The other two keep their one-attempt behaviour:
            an offer waits for a person, and a manual request is made for one failure at a time.
          </p>
          <Switch
            bind:checked={enabled}
            disabled={busy}
            label="Allow PR repairs in these repositories"
            description="Turn off to pause repairs while keeping these settings."
          />
          <Switch
            bind:checked={workflows}
            disabled={busy}
            label="Allow edits to workflow files"
            description="Off by default. Allows changes to .github/workflows files."
          />
        </details>
        {#if !selectedRepos.length}<p class="hint">Choose at least one repository above.</p>{/if}
        <Button
          type="submit"
          disabled={busy ||
            selectingAll ||
            !!error ||
            !selectedRepos.length ||
            (enabled && mode === 'automatic' && !selectedProvider?.enabled)}
          loading={busy}
          >Save in {selectedRepos.length}
          {selectedRepos.length === 1 ? 'repository' : 'repositories'}</Button
        >
        {#if bulkResults.length}
          <div aria-live="polite">
            {#each bulkResults as result (result.repo)}<p class:error={!result.saved}>
                {result.repo}: {resultWords(result)}
              </p>{/each}
          </div>
        {/if}
        {#if failedRepos.length && lastSettings}
          <Button disabled={busy} onclick={() => void savePolicies([...failedRepos], lastSettings!)}
            >Retry {failedRepos.length} failed {failedRepos.length === 1
              ? 'repository'
              : 'repositories'}</Button
          >
        {/if}
      </form>
      {#if policies.length}
        <h3>Configured repositories</h3>
        <Field id="policy-search" label="Filter configured repositories">
          <Input id="policy-search" type="search" bind:value={policySearch} />
        </Field>
        <p role="status">
          Showing {filteredPolicies.length} of {pluralise(
            policies.length,
            'configured repository',
            'configured repositories',
          )}.
          {enabledPolicies} enabled, {policies.length - enabledPolicies} paused.
        </p>
        <p>
          Pausing stops new fixes. Work already running stops before publication unless it is
          already publishing. Pushed commits keep being checked.
        </p>
        <div class="policy-list">
          {#each filteredPolicies as p (p.repo)}
            {@const decided = decisions.find((d) => d.repo.toLowerCase() === p.repo.toLowerCase())}
            <div class="policy-row">
              <div>
                <strong>{p.repo}</strong>
                <p>{policyWords(p, decided)}</p>
              </div>
              <Button
                size="sm"
                disabled={busy || selectingAll}
                onclick={() => editPolicy(p, true)}
                ariaLabel="Edit repair settings for {p.repo}">Edit</Button
              >
              <Button
                size="sm"
                disabled={busy}
                onclick={() =>
                  void act(
                    () => setEliRepairPolicy({ ...p, enabled: !p.enabled }),
                    p.enabled ? `Repairs paused on ${p.repo}` : `Repairs enabled on ${p.repo}`,
                  )}
                ariaLabel="{p.enabled ? 'Pause' : 'Resume'} repairs on {p.repo}"
                >{p.enabled ? 'Pause' : 'Resume'}</Button
              >
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <section class="card" id="watching" aria-labelledby="watching-heading">
      <h2 id="watching-heading">What Eli is watching now</h2>
      <p>
        Each row is one repository: what it was set to do, how many pull requests are being
        followed, when it last saw something and whether anything needs a person. Pausing a
        repository stops new pull requests being admitted; the ones already being followed carry on
        until they are paused on their own.
      </p>
      <WatchedRepositories
        {decisions}
        {watches}
        state={watching}
        busy={watchBusy}
        failure={watchError}
        onpausewatch={(watch) => confirmControl(pauseWatchCopy(watch), () => pauseWatch(watch))}
        onresumewatch={(watch) => void changeWatch(watch.id, () => resumeWatch(watch))}
        onstopwatch={(watch) => confirmControl(stopWatchCopy(watch), () => stopWatch(watch))}
        onpauserepo={(decision) => void changeWatch(decision.id, () => pauseRepository(decision))}
        onresumerepo={(decision) => void changeWatch(decision.id, () => resumeRepository(decision))}
        onstopall={() => confirmControl(stopAllCopy(watches), () => stopAllWatches(watches))}
      />
    </section>
  {/if}
  <details class="card" id="access" open={!admin}>
    <summary>Personal repair requests{admin ? ' (optional)' : ''}</summary>
    <p>
      Use your own model provider when requesting an individual repair. This is separate from
      repository automation.
    </p>
    <div id="personal-account">
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
  </details>
  {#if admin}
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

<ConfirmDialog
  bind:open={confirmOpen}
  title={confirming?.copy.title ?? 'Confirm'}
  description={confirming?.copy.description ?? ''}
  consequences={confirming ? [confirming.copy.inFlight] : []}
  confirmLabel={confirming?.copy.confirm ?? 'Confirm'}
  cancelLabel="Cancel"
  tone={confirming?.copy.confirm.startsWith('Stop') ? 'danger' : 'default'}
  busy={confirmBusy}
  onconfirm={runConfirmed}
  oncancel={() => {
    if (confirmBusy) return;
    confirming = null;
    confirmError = '';
  }}
>
  {#if confirmError}<p class="error" role="alert">{confirmError}</p>{/if}
</ConfirmDialog>

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
  #policy-form-heading {
    margin: 0;
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  #policy-form-heading:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-4);
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
    display: grid;
    gap: var(--z-space-3);
    margin-top: var(--z-space-3);
  }
  .policy-row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
    align-items: center;
  }
  .policy-row > div {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .policy-row p {
    margin: var(--z-space-1) 0 0;
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
