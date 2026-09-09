<script module lang="ts">
  import type {
    BackendKind,
    DockerMode,
    Installation,
    Platform,
    Pool,
    PoolCreate,
    Resources,
    RunnerGroup,
  } from '$lib/api/types';
  import { parseGoDuration } from '$lib/format';
  import {
    backendOffers,
    backendUnavailable,
    STEP_FIELDS,
    WIZARD_STEPS,
    stepForField,
  } from './PoolVocabulary.svelte';
  import type { BackendOffer } from './PoolVocabulary.svelte';

  /**
   * What the wizard is editing.
   *
   * Numbers are held as strings because that is what a text input gives back,
   * and because "" and "0" are different answers -- one is "not filled in yet",
   * the other is a deliberate zero. They become numbers exactly once, in
   * `toPoolBody`.
   */
  export interface PoolDraft {
    name: string;
    installation_id: string;
    runner_group: string;
    labels: string[];
    backend: BackendKind;
    /**
     * The machine these runners need. Empty means the pool promises nothing,
     * which is what a pool created before platforms existed looks like.
     */
    platform_os: string;
    platform_os_version: string;
    platform_arch: string;
    image: string;
    runner_version: string;
    min_runners: string;
    max_runners: string;
    priority: string;
    idle_timeout: string;
    ephemeral: boolean;
    docker_mode: DockerMode;
    run_as_root: boolean;
    enabled: boolean;
    cpus: string;
    memory_mb: string;
    disk_gb: string;
    cache_enabled: boolean;
    cache_scope: 'pool' | 'repository';
    cache_size_limit: string;
    cache_source: string;
    cache_repository: string;
    /** Carried through untouched: the wizard does not edit it, and must not lose it. */
    pids_limit: string;
    host_selector: Record<string, string>;
    /**
     * The wizard's own state, not the pool's: whether the operator chose to
     * keep this pool to some hosts. The selector cannot answer it, because
     * "only hosts that match" with no rule typed yet is an empty map exactly
     * like "any host" -- and reading the choice back off the selector is how
     * a half-made rule reset itself on the way to the next step and back.
     */
    restrict_hosts: boolean;
    env: Record<string, string>;
  }

  export function emptyDraft(): PoolDraft {
    return {
      name: '',
      installation_id: '',
      runner_group: '',
      labels: [],
      backend: 'docker',
      platform_os: '',
      platform_os_version: '',
      platform_arch: '',
      image: '',
      runner_version: '',
      min_runners: '0',
      max_runners: '4',
      priority: '0',
      idle_timeout: '5m',
      ephemeral: true,
      docker_mode: 'none',
      run_as_root: false,
      enabled: true,
      cpus: '',
      memory_mb: '',
      disk_gb: '',
      cache_enabled: false,
      cache_scope: 'pool',
      cache_size_limit: '',
      cache_source: '',
      cache_repository: '',
      pids_limit: '',
      host_selector: {},
      restrict_hosts: false,
      env: {},
    };
  }

  function fromNumber(value: number | undefined): string {
    return value === undefined || value === null ? '' : String(value);
  }

  export function draftFromPool(pool: Pool): PoolDraft {
    const base = emptyDraft();
    const resources = pool.resources ?? {};
    return {
      ...base,
      name: pool.name ?? '',
      installation_id: pool.installation_id ?? '',
      runner_group: pool.runner_group ?? '',
      labels: [...(pool.labels ?? [])],
      backend: pool.backend ?? 'docker',
      platform_os: pool.platform?.os ?? '',
      platform_os_version: pool.platform?.os_version ?? '',
      platform_arch: pool.platform?.arch ?? '',
      image: pool.image ?? '',
      runner_version: pool.runner_version ?? '',
      min_runners: fromNumber(pool.min_runners),
      max_runners: fromNumber(pool.max_runners),
      priority: fromNumber(pool.priority),
      idle_timeout: pool.idle_timeout ?? base.idle_timeout,
      ephemeral: pool.ephemeral !== false,
      docker_mode: pool.docker_mode ?? 'none',
      run_as_root: pool.run_as_root === true,
      enabled: pool.enabled !== false,
      cpus: fromNumber(resources.cpus),
      memory_mb: fromNumber(resources.memory_mb),
      disk_gb: fromNumber(resources.disk_gb),
      cache_enabled: pool.cache?.enabled === true,
      cache_scope: pool.cache?.scope ?? 'pool',
      cache_size_limit: fromNumber(pool.cache?.size_limit),
      cache_source: pool.cache?.source ?? '',
      cache_repository: pool.cache?.repository ?? '',
      pids_limit: fromNumber(resources.pids_limit),
      host_selector: { ...(pool.host_selector ?? {}) },
      restrict_hosts: Object.keys(pool.host_selector ?? {}).length > 0,
      env: { ...(pool.env ?? {}) },
    };
  }

  function toNumber(value: string): number | undefined {
    const trimmed = value.trim();
    if (trimmed === '') return undefined;
    const parsed = Number(trimmed);
    return Number.isFinite(parsed) ? parsed : undefined;
  }

  function toInteger(value: string): number | undefined {
    const parsed = toNumber(value);
    return parsed !== undefined && Number.isInteger(parsed) ? parsed : undefined;
  }

  /**
   * The request body this draft produces.
   *
   * On create, empty optional fields are left out and the server fills in its
   * defaults. On edit they are sent as empty: a PATCH treats an absent key as
   * "leave it as it is", so leaving them out would make clearing the image, a
   * resource limit or the last host-selector entry a change the server never
   * hears about -- while the toast says it was saved.
   */
  export function toPoolBody(draft: PoolDraft, options: { complete?: boolean } = {}): PoolCreate {
    const resources: Resources = {};
    const cpus = toNumber(draft.cpus);
    const memory = toInteger(draft.memory_mb);
    const disk = toInteger(draft.disk_gb);
    const pids = toInteger(draft.pids_limit);
    if (cpus !== undefined) resources.cpus = cpus;
    if (memory !== undefined) resources.memory_mb = memory;
    if (disk !== undefined) resources.disk_gb = disk;
    if (pids !== undefined) resources.pids_limit = pids;

    const body: PoolCreate = {
      name: brandedName(draft.name),
      installation_id: draft.installation_id,
      labels: draft.labels.map((label) => label.trim()).filter(Boolean),
      backend: draft.backend,
      min_runners: toInteger(draft.min_runners) ?? 0,
      max_runners: toInteger(draft.max_runners) ?? 1,
      priority: toInteger(draft.priority) ?? 0,
      idle_timeout: draft.idle_timeout.trim() || '5m',
      ephemeral: draft.ephemeral,
      docker_mode: draft.docker_mode,
      run_as_root: draft.run_as_root,
      enabled: draft.enabled,
      cache: {
        enabled: draft.cache_enabled,
        scope: draft.cache_scope,
        size_limit: toInteger(draft.cache_size_limit) ?? 0,
        source: draft.cache_source.trim(),
        repository: draft.cache_scope === 'repository' ? draft.cache_repository.trim() : '',
      },
    };
    // The draft holds plain strings because that is what a <select> gives
    // back; the API's enums are narrower, and the server is the one that
    // rejects a value outside them.
    const platform: Platform = {};
    if (draft.platform_os) platform.os = draft.platform_os as Platform['os'];
    if (draft.platform_os_version) platform.os_version = draft.platform_os_version;
    if (draft.platform_arch) platform.arch = draft.platform_arch as Platform['arch'];
    // Always sent, so that clearing a platform on an edit actually clears it
    // rather than being read as "leave it alone".
    body.platform = platform;

    const complete = options.complete === true;
    if (complete || draft.runner_group.trim()) body.runner_group = draft.runner_group.trim();
    if (complete || draft.image.trim()) body.image = draft.image.trim();
    if (complete || draft.runner_version.trim()) body.runner_version = draft.runner_version.trim();
    if (complete || Object.keys(resources).length > 0) body.resources = resources;
    if (complete || Object.keys(draft.host_selector).length > 0) {
      body.host_selector = draft.host_selector;
    }
    if (complete || Object.keys(draft.env).length > 0) body.env = draft.env;
    return body;
  }

  const NAME_SHAPE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

  /**
   * The rules the wizard can check without asking the server. The server checks
   * the same things and more; these exist so the operator finds out while they
   * are still typing rather than at the end.
   */
  export function draftErrors(
    draft: PoolDraft,
    socketConfirmed: boolean,
    offers: readonly BackendOffer[] = [],
    hostsKnown = false,
  ): Record<string, string> {
    const errors: Record<string, string> = {};
    const name = draft.name.trim();
    if (name === '') errors['name'] = 'Give the pool a name so it can be told apart in the fleet.';
    else if (name.length > 64) errors['name'] = 'Keep the name to 64 characters or fewer.';
    else if (!NAME_SHAPE.test(name))
      errors['name'] =
        'Use letters, digits, dots, dashes and underscores, starting with a letter or digit.';

    if (draft.installation_id === '')
      errors['installation_id'] = 'Choose the GitHub installation these runners register with.';

    if (draft.labels.length === 0)
      errors['labels'] = 'Add at least one label, or no workflow can ask for this pool.';

    const min = toInteger(draft.min_runners);
    const max = toInteger(draft.max_runners);
    const priority = toInteger(draft.priority);
    if (min === undefined || min < 0) errors['min_runners'] = 'Use a whole number, zero or more.';
    if (max === undefined || max < 1) errors['max_runners'] = 'Use a whole number, one or more.';
    else if (min !== undefined && max < min)
      errors['max_runners'] = `The maximum must be at least the minimum, which is ${min}.`;
    if (priority === undefined) errors['priority'] = 'Use a whole number.';

    if (parseGoDuration(draft.idle_timeout) === null)
      errors['idle_timeout'] = 'Use a Go duration such as 5m, 90s or 1h30m.';

    if (draft.cpus.trim() !== '') {
      const cpus = toNumber(draft.cpus);
      if (cpus === undefined || cpus <= 0)
        errors['resources.cpus'] =
          'Use a number greater than zero, or leave it empty for no limit.';
    }
    if (draft.memory_mb.trim() !== '') {
      const memory = toInteger(draft.memory_mb);
      if (memory === undefined || memory <= 0)
        errors['resources.memory_mb'] =
          'Use a whole number of megabytes, or leave it empty for no limit.';
    }
    if (draft.disk_gb.trim() !== '') {
      const disk = toInteger(draft.disk_gb);
      if (disk === undefined || disk <= 0)
        errors['resources.disk_gb'] =
          'Use a whole number of gigabytes, or leave it empty for no limit.';
    }

    if (draft.docker_mode === 'host-socket' && !socketConfirmed)
      errors['docker_mode'] =
        'Confirm that you understand what mounting the host socket gives every job on this pool.';

    const unrunnable = backendUnavailable(
      draft.backend,
      offers,
      hostsKnown,
      Object.keys(draft.host_selector).length > 0,
    );
    if (unrunnable) errors['backend'] = unrunnable;

    return errors;
  }
</script>

<!--
  Pool creation and pool editing, in the same six steps.

  The draft is one object held here, so going back never loses what was typed;
  the steps are presentation only. Client-side rules run continuously and gate
  the Next button; the server's own verdict is asked for on the review step,
  before anything is created, because "the pool exists but no host can run it"
  is a much worse place to find out.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import {
    ApiError,
    createPool,
    listInstallations,
    listPoolPlatforms,
    listRunnerGroups,
    updatePool,
    validatePool,
  } from '$lib/api/client';
  import type { Body, PoolPlatform, Result } from '$lib/api/types';
  import { BRAND_LABEL, brandedLabel, brandedName } from '$lib/brand';
  import { nicknamedPoolName, poolName, spinWord } from './names';
  import { fleet } from '$lib/state/fleet.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Wizard from '$lib/components/Wizard.svelte';
  import StepTarget from './StepTarget.svelte';
  import StepLabels from './StepLabels.svelte';
  import StepHosts from './StepHosts.svelte';
  import { hostMatchesSelector } from './HostSelectorEditor.svelte';
  import StepBackend from './StepBackend.svelte';
  import StepScaling from './StepScaling.svelte';
  import StepReview from './StepReview.svelte';

  interface Props {
    /** The pool being edited. Leave it out to create a new one. */
    pool?: Pool;
    oncancel: () => void;
    ondone: (pool: Pool) => void;
    class?: string;
  }

  let { pool, oncancel, ondone, class: className = '' }: Props = $props();

  const editing = $derived(pool !== undefined);

  // Captured once on purpose: the routes remount this form with a {#key} when
  // they start editing a different pool, so a live reference would be wrong.
  let draft = $state<PoolDraft>(untrack(() => (pool ? draftFromPool(pool) : emptyDraft())));
  let current = $state(0);
  let touched = $state<Record<string, boolean>>({});
  let serverErrors = $state<Record<string, string>>({});
  let socketConfirmed = $state(untrack(() => pool?.docker_mode === 'host-socket'));

  /*
    Auto-naming, on creation only.

    `autoName` is the last name the wizard produced. While the field still
    holds it the name is the wizard's to keep current -- so choosing Podman on
    the backend step renames the pool -- and the moment an operator types over
    it the wizard stops touching it, because a field that rewrites itself under
    someone's cursor is worse than no help at all. Editing an existing pool
    never generates anything: its name is already in workflows.

    `autoLabel` plays the same part for the labels, with one more state:
    `null`, meaning the operator has taken the labels over. Removing the
    suggested chip has to stick, and without that third state an empty list
    looks exactly like a list nothing has been put in yet.
  */
  let kennelWord = $state(spinWord());
  let autoName = $state('');
  let autoLabel = $state<string | null>('');
  let submitting = $state(false);
  let panel = $state<HTMLDivElement | null>(null);

  let installations = $state<Installation[]>([]);
  let installationsLoading = $state(true);
  let installationsError = $state<unknown>(null);
  /** Bumped by the error state's retry, which re-runs the fetch below. */
  let installationsAttempt = $state(0);

  let groups = $state<RunnerGroup[]>([]);
  let groupsLoading = $state(false);
  let groupsError = $state<unknown>(null);

  // The operating systems a runner image is published for. Served rather than
  // hard-coded so the picker cannot offer one that does not exist.
  let platforms = $state<PoolPlatform[]>([]);

  let verdict = $state<Result<'validatePool'> | null>(null);
  let validating = $state(false);
  let validateError = $state<unknown>(null);

  const reviewStep = WIZARD_STEPS.length - 1;
  const hostsStep = WIZARD_STEPS.findIndex((step) => step.id === 'hosts');
  // The backend step counts over the hosts this pool is allowed to land on, not
  // the whole fleet: "offered by 3 hosts" is a lie if two of them are the amd64
  // boxes an arm64 pool will never touch. Placement is chosen first for exactly
  // this reason.
  const selectedHosts = $derived(
    fleet.hosts.filter((host) => hostMatchesSelector(host, draft.host_selector)),
  );
  const restrictedToHosts = $derived(Object.keys(draft.host_selector).length > 0);
  const offers = $derived(backendOffers(selectedHosts));
  const clientErrors = $derived(draftErrors(draft, socketConfirmed, offers, fleet.loaded));
  const body = $derived(toPoolBody(draft));

  /** Client rules show once a field has been left; server rules show at once. */
  const errors = $derived.by(() => {
    const out: Record<string, string> = { ...serverErrors };
    for (const [field, message] of Object.entries(clientErrors)) {
      if (touched[field]) out[field] = message;
    }
    return out;
  });

  const blocking = $derived.by(() => {
    const fields =
      current === reviewStep ? Object.keys(clientErrors) : (STEP_FIELDS[current] ?? []);
    return fields
      .map((field) => clientErrors[field])
      .filter((message): message is string => Boolean(message));
  });
  const canAdvance = $derived(blocking.length === 0 && !submitting);

  const installationLabel = $derived(
    installations.find((entry) => entry.id === draft.installation_id)?.target ?? '',
  );

  function touch(field: string): void {
    touched = { ...touched, [field]: true };
    if (serverErrors[field] !== undefined) {
      const rest = { ...serverErrors };
      delete rest[field];
      serverErrors = rest;
    }
  }

  function touchStep(step: number): void {
    const fields = STEP_FIELDS[step] ?? [];
    if (fields.length === 0) return;
    const next = { ...touched };
    for (const field of fields) next[field] = true;
    touched = next;
  }

  function goTo(step: number): void {
    current = Math.min(Math.max(step, 0), reviewStep);
  }

  /* -- what the fleet and GitHub can offer --------------------------------- */

  $effect(() => {
    void installationsAttempt;
    const controller = new AbortController();
    // A failure here is not worth an error state: the picker falls back to
    // "Any", which is what a pool got before platforms existed.
    listPoolPlatforms(controller.signal)
      .then((response) => {
        platforms = response.items ?? [];
      })
      .catch(() => {});
    return () => controller.abort();
  });

  $effect(() => {
    const controller = new AbortController();
    installationsLoading = true;
    installationsError = null;
    listInstallations(controller.signal)
      .then((response) => {
        const items = response.items ?? [];
        installations = items;
        // One installation is the common case; choosing it for the operator is
        // the difference between a wizard and a form.
        const only = items[0];
        if (draft.installation_id === '' && items.length === 1 && only?.id) {
          draft.installation_id = only.id;
        }
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        installationsError = cause;
      })
      .finally(() => {
        installationsLoading = false;
      });
    return () => controller.abort();
  });

  $effect(() => {
    const id = draft.installation_id;
    groups = [];
    groupsError = null;
    if (id === '') {
      groupsLoading = false;
      return;
    }
    const controller = new AbortController();
    groupsLoading = true;
    listRunnerGroups(id, controller.signal)
      .then((response) => {
        groups = response.items ?? [];
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        groupsError = cause;
      })
      .finally(() => {
        groupsLoading = false;
      });
    return () => controller.abort();
  });

  /* -- the name, and the label it implies ---------------------------------- */

  /** The names already in use, so a new pool is not offered one of them. */
  const poolNames = $derived(fleet.pools.map((p) => p.name ?? ''));

  /**
   * Whether the operator has asked for a spaniel in the name.
   *
   * Without this the dice would be undone by the next thing they typed: the
   * suggested name is recomputed as the draft changes, and a shape that needs
   * no word would quietly drop the one they just rolled.
   */
  let nicknamed = $state(false);

  // The name follows the shape as the operator fills it in: a name generated
  // before any host had connected must not go on claiming the pool is 2 vCPU
  // Ubuntu after they have said 8 vCPU Debian, and the pools already in the
  // fleet decide whether this one needs a spaniel to be told from them.
  $effect(() => {
    if (editing) return;
    const suggested = nicknamed
      ? nicknamedPoolName(kennelWord, draft, fleet.hosts)
      : poolName(kennelWord, draft, fleet.hosts, poolNames);
    untrack(() => {
      if (draft.name !== '' && draft.name !== autoName) return;
      draft.name = suggested;
      autoName = suggested;
    });
  });

  $effect(() => {
    if (editing) return;
    const suggested = brandedLabel(draft.name);
    untrack(() => {
      if (autoLabel === null) return;
      const pristine =
        autoLabel === '' ? draft.labels.length === 0 : draft.labels.join() === autoLabel;
      if (!pristine) {
        autoLabel = null;
        return;
      }
      // A name that reduces to the brand alone says nothing the server does not
      // already add on save, so there is nothing worth filling in yet.
      if (suggested === BRAND_LABEL) return;
      if (draft.labels.join() === suggested) return;
      draft.labels = [suggested];
      autoLabel = suggested;
    });
  });

  /**
   * Roll a name from the kennel, whatever is in the field now.
   *
   * The suggested name carries a spaniel only when the shape cannot tell this
   * pool from another, so the dice ask for one outright: pressing it on a pool
   * whose shape is already unique has to change something, or it reads as a
   * broken button.
   */
  function spin(): void {
    if (editing) return;
    nicknamed = true;
    kennelWord = spinWord(kennelWord);
    const next = nicknamedPoolName(kennelWord, draft, fleet.hosts);
    draft.name = next;
    autoName = next;
    touch('name');
  }

  /* -- the server's verdict, before anything is created --------------------- */

  $effect(() => {
    // From the placement step on, not only at the end. Every step after it
    // edits something the controller's count depends on -- which hosts, which
    // backend, how big a runner is -- so the answer belongs beside the setting
    // that changes it, while there is still a reason to change it.
    if (current < hostsStep) return;
    const payload = body;
    const controller = new AbortController();
    validating = true;
    const timer = setTimeout(() => {
      validatePool(payload, pool?.id, controller.signal)
        .then((result) => {
          verdict = result;
          validateError = null;
        })
        .catch((cause: unknown) => {
          if (cause instanceof DOMException && cause.name === 'AbortError') return;
          validateError = cause;
        })
        .finally(() => {
          validating = false;
        });
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  });

  /* -- focus follows the step ----------------------------------------------- */

  let lastStep = -1;
  $effect(() => {
    const step = current;
    if (step === lastStep) return;
    const moved = lastStep !== -1;
    lastStep = step;
    // Not on the first render: the shell has just put focus on the page
    // heading, and taking it away again would undo that.
    if (moved) untrack(() => panel)?.focus();
  });

  /* -- submitting ------------------------------------------------------------ */

  function applyFieldErrors(cause: ApiError): void {
    const fields = cause.fieldErrors();
    serverErrors = fields;
    const first = Object.keys(fields)[0];
    if (first !== undefined) goTo(stepForField(first));
  }

  async function finish(): Promise<void> {
    if (submitting) return;
    touchStep(current);
    const outstanding = Object.keys(clientErrors);
    if (outstanding.length > 0) {
      // Show every one of them inline, then land on the first offending step.
      const next = { ...touched };
      for (const field of outstanding) next[field] = true;
      touched = next;
      goTo(stepForField(outstanding[0] ?? 'name'));
      return;
    }
    submitting = true;
    try {
      const payload = toPoolBody(draft, { complete: editing });
      const saved =
        pool && pool.id
          ? await updatePool(pool.id, payload as Body<'updatePool'>)
          : await createPool(payload);
      toasts.success(
        editing ? `Saved ${payload.name}` : `Created ${payload.name}`,
        editing
          ? 'The scheduler picks the new settings up on its next pass.'
          : 'Runners appear as soon as a job asks for these labels.',
      );
      void fleet.reconcile();
      ondone(saved);
    } catch (cause) {
      if (cause instanceof ApiError) applyFieldErrors(cause);
      toasts.fromError(cause, editing ? 'The pool was not saved' : 'The pool was not created');
    } finally {
      submitting = false;
    }
  }
</script>

<Wizard
  class={className}
  steps={WIZARD_STEPS}
  bind:current
  {canAdvance}
  busy={submitting}
  finishLabel={editing ? 'Save changes' : 'Create pool'}
  cancelLabel="Cancel"
  onnext={() => touchStep(current)}
  onback={() => touchStep(current)}
  onfinish={finish}
  {oncancel}
>
  {#snippet children(step)}
    <div class="step" bind:this={panel} tabindex="-1" role="group" aria-label={step.title}>
      {#if step.id === 'target'}
        <StepTarget
          {draft}
          {errors}
          {touch}
          {installations}
          loading={installationsLoading}
          error={installationsError}
          onretry={() => (installationsAttempt += 1)}
          {groups}
          {groupsLoading}
          {groupsError}
          onspin={editing ? undefined : spin}
        />
      {:else if step.id === 'labels'}
        <StepLabels {draft} {errors} {touch} />
      {:else if step.id === 'hosts'}
        <StepHosts
          {draft}
          {touch}
          hosts={fleet.hosts}
          hostsKnown={fleet.loaded}
          {verdict}
          {validating}
        />
      {:else if step.id === 'backend'}
        <StepBackend
          {draft}
          {errors}
          {touch}
          {offers}
          {platforms}
          hosts={fleet.hosts}
          hostsKnown={fleet.loaded}
          restricted={restrictedToHosts}
          bind:socketConfirmed
        />
      {:else if step.id === 'scaling'}
        <StepScaling {draft} {errors} {touch} {verdict} {validating} />
      {:else}
        <StepReview
          {draft}
          {body}
          {editing}
          {installationLabel}
          {verdict}
          {validating}
          error={validateError}
          ongoto={goTo}
        />
      {/if}

      {#if blocking.length > 0}
        <div class="blocking">
          <p class="blocking-title">
            {WIZARD_STEPS[current + 1] ? 'Before the next step' : 'Before this pool can be saved'}
          </p>
          <ul>
            {#each blocking as message, index (index)}
              <li>{message}</li>
            {/each}
          </ul>
        </div>
      {/if}
    </div>
  {/snippet}
</Wizard>

<style>
  .step {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-5);
  }
  /*
    The step is focused programmatically when the wizard advances, so that a
    screen reader lands on the new content. That is not a keyboard tab, so
    :focus-visible is the right test: it draws no ring for the move the wizard
    made, and still draws one if somebody tabs here themselves.
  */
  .step:focus:not(:focus-visible) {
    outline: none;
  }
  .blocking {
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }
  .blocking-title {
    margin: 0;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .blocking ul {
    margin: var(--z-space-1) 0 0;
    padding-left: var(--z-space-5);
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
</style>
