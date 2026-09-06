<!--
  Step three: what a runner is made of, how it is run, and how much Docker a job
  gets.

  The platform is first because it is the answer with the widest blast radius:
  it picks the image every runner boots and it decides which hosts they may be
  placed on. It is a list rather than a text field because it is served from the
  images Zoomies actually publishes, and a pool that names one we do not publish
  is a pool that validates and then never starts a runner.

  The two dangerous answers in the whole wizard also live here, so both are
  spelled out in the consequence rather than in a footnote, and the host-socket
  option cannot be left selected without a deliberate confirmation.
-->
<script lang="ts">
  import { ShieldAlert } from '@lucide/svelte';
  import type { BackendKind, DockerMode, Host, PoolPlatform } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import Select from '$lib/components/Select.svelte';
  import { BACKENDS, DOCKER_MODES, platformKey } from './PoolVocabulary.svelte';
  import type { BackendOffer, PoolDraft } from './PoolWizardForm.svelte';

  interface Props {
    draft: PoolDraft;
    errors: Record<string, string>;
    touch: (field: string) => void;
    offers: readonly BackendOffer[];
    /** False until the fleet cache has landed, so we do not cry wolf about hosts. */
    hostsKnown: boolean;
    /** The operating systems a runner image is published for. */
    platforms: readonly PoolPlatform[];
    /** Every connected host, so the step can say how many match the platform. */
    hosts: readonly Host[];
    socketConfirmed?: boolean;
  }

  let {
    draft,
    errors,
    touch,
    offers,
    hostsKnown,
    platforms,
    hosts,
    socketConfirmed = $bindable(false),
  }: Props = $props();

  function offer(kind: BackendKind): BackendOffer | undefined {
    return offers.find((entry) => entry.kind === kind);
  }

  function availability(kind: BackendKind): string {
    if (!hostsKnown) return '';
    const found = offer(kind);
    if (!found) return '';
    if (found.hosts === 0) {
      return found.detail
        ? `No connected host offers it: ${found.detail}`
        : 'No connected host offers it, so this pool would never place a runner.';
    }
    return `Offered by ${pluralise(found.hosts, 'connected host')}.`;
  }

  const backendOptions = $derived(
    BACKENDS.map((choice) => ({
      value: choice.value,
      label: choice.label,
      description: `${choice.consequence} ${availability(choice.value)}`.trim(),
    })),
  );

  const dindHosts = $derived(offer(draft.backend)?.dindHosts ?? 0);

  const dockerOptions = $derived(
    DOCKER_MODES.map((choice) => {
      let description = choice.consequence;
      if (choice.value === 'dind' && hostsKnown && dindHosts === 0) {
        description += ' No connected host reports that it can do this.';
      }
      return { value: choice.value, label: choice.label, description };
    }),
  );

  const usesImage = $derived(draft.backend !== 'process');

  /* -- platform ----------------------------------------------------------- */

  const osOptions = $derived([
    { value: '', label: 'Any — use the default image' },
    ...platforms.map((p) => ({
      value: platformKey(p.os, p.os_version),
      label: p.default ? `${p.label} (default)` : (p.label ?? ''),
    })),
  ]);

  const ARCHES = [
    { value: '', label: 'Any' },
    { value: 'amd64', label: 'amd64' },
    { value: 'arm64', label: 'arm64' },
  ];

  const chosenOS = $derived(platformKey(draft.platform_os, draft.platform_os_version));

  const chosen = $derived(
    platforms.find((p) => platformKey(p.os, p.os_version) === chosenOS) ?? undefined,
  );

  /**
   * The image this pool will actually boot. A pool that names its own image
   * keeps it; otherwise the platform picks one, and saying which turns an
   * abstract choice into a concrete one.
   */
  const effectiveImage = $derived(draft.image.trim() || chosen?.image || '');

  /**
   * How many connected hosts this platform could be placed on. A pool that
   * matches none of them will never start a runner, and finding that out here
   * is far better than finding it out from an empty Runners page.
   */
  const matchingHosts = $derived(
    hosts.filter((host) => {
      if (draft.platform_os && host.platform?.os && host.platform.os !== draft.platform_os)
        return false;
      if (
        draft.platform_os_version &&
        host.platform?.os_version &&
        host.platform.os_version !== draft.platform_os_version
      )
        return false;
      if (draft.platform_arch && host.platform?.arch && host.platform.arch !== draft.platform_arch)
        return false;
      return true;
    }).length,
  );

  function chooseOS(value: string): void {
    const found = platforms.find((p) => platformKey(p.os, p.os_version) === value);
    draft.platform_os = found?.os ?? '';
    draft.platform_os_version = found?.os_version ?? '';
    // An architecture the chosen variant is not built for would be refused by
    // the server, so clear it rather than carry an impossible pair forward.
    if (draft.platform_arch && found && !(found.arches ?? []).includes(draft.platform_arch)) {
      draft.platform_arch = '';
    }
    touch('platform.os');
  }

  function chooseArch(value: string): void {
    draft.platform_arch = value;
    touch('platform.arch');
  }

  function chooseBackend(value: string): void {
    draft.backend = value as BackendKind;
    touch('backend');
  }

  function chooseDockerMode(value: string): void {
    const next = value as DockerMode;
    // Choosing the socket again is a fresh decision, so it needs fresh consent.
    if (next === 'host-socket' && draft.docker_mode !== 'host-socket') socketConfirmed = false;
    draft.docker_mode = next;
    touch('docker_mode');
  }
</script>

{#if usesImage}
  <div class="platform">
    <Field
      label="Operating system"
      error={errors['platform.os'] ?? errors['platform.os_version']}
      hint="Picks the zoomies-runner image these runners boot, and restricts them to hosts running the same thing."
    >
      {#snippet children({ id, describedBy, invalid })}
        <Select
          value={chosenOS}
          options={osOptions}
          {id}
          {describedBy}
          {invalid}
          onchange={chooseOS}
        />
      {/snippet}
    </Field>

    <Field
      label="Architecture"
      error={errors['platform.arch']}
      hint="Leave it as Any unless your fleet has both, and this pool belongs on one of them."
    >
      {#snippet children({ id, describedBy, invalid })}
        <Select
          value={draft.platform_arch}
          options={ARCHES}
          {id}
          {describedBy}
          {invalid}
          onchange={chooseArch}
        />
      {/snippet}
    </Field>
  </div>

  <p class="platform-note">
    {#if effectiveImage}
      Runners boot <code>{effectiveImage}</code>.
    {:else}
      Runners boot the controller's default image.
    {/if}
    {#if hostsKnown}
      {matchingHosts === 0
        ? ' No connected host matches, so this pool would never place a runner.'
        : ` ${pluralise(matchingHosts, 'connected host')} match.`}
    {/if}
  </p>
{/if}

<RadioGroup
  name="pool-backend"
  legend="Backend"
  value={draft.backend}
  options={backendOptions}
  onchange={chooseBackend}
/>

{#if usesImage}
  <Field
    label="Image"
    error={errors['image']}
    hint="Override the image the operating system above selects. Leave it empty unless you build your own."
  >
    {#snippet children({ id, describedBy, invalid })}
      <Input
        bind:value={draft.image}
        {id}
        {describedBy}
        {invalid}
        mono
        placeholder={chosen?.image ?? 'ghcr.io/eyupio/zoomies-runner:latest'}
        autocomplete="off"
        onblur={() => touch('image')}
      />
    {/snippet}
  </Field>
{/if}

<Field
  label="Runner version"
  error={errors['runner_version']}
  hint="Pin the GitHub Actions runner version, or leave it empty to track the latest."
>
  {#snippet children({ id, describedBy, invalid })}
    <Input
      bind:value={draft.runner_version}
      {id}
      {describedBy}
      {invalid}
      mono
      placeholder="latest"
      autocomplete="off"
      onblur={() => touch('runner_version')}
    />
  {/snippet}
</Field>

<div class="docker">
  <RadioGroup
    name="pool-docker-mode"
    legend="Docker in jobs"
    value={draft.docker_mode}
    options={dockerOptions}
    onchange={chooseDockerMode}
  />

  {#if draft.docker_mode === 'host-socket'}
    <div class="danger" role="group" aria-labelledby="host-socket-warning">
      <p class="danger-title" id="host-socket-warning">
        <ShieldAlert size={16} aria-hidden="true" />
        Any job on this pool can become root on the host
      </p>
      <p class="danger-body">
        Mounting the host's Docker socket lets a job start a privileged container, mount the host
        filesystem and read every secret on that machine — including the credentials of every other
        pool's runners. A pull request from a fork is enough to do it. Use Docker in Docker unless
        you control every workflow that can reach these labels.
      </p>
      <Checkbox
        bind:checked={socketConfirmed}
        label="I understand that this gives every job on this pool root on the host"
        onchange={() => touch('docker_mode')}
      />
      {#if errors['docker_mode']}
        <p class="danger-error">{errors['docker_mode']}</p>
      {/if}
    </div>
  {/if}
</div>

<Checkbox
  bind:checked={draft.run_as_root}
  label="Run jobs as root inside the runner"
  description="Convenient for installing packages mid-job, and it means a compromised job owns the whole runner. Leave it off unless a workflow genuinely needs it."
  onchange={() => touch('run_as_root')}
/>

<style>
  .platform {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    gap: var(--z-space-4);
  }
  @media (max-width: 40rem) {
    .platform {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .platform-note {
    margin: calc(var(--z-space-3) * -1) 0 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .platform-note code {
    font-family: var(--z-font-mono);
  }
  .docker {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .danger {
    padding: var(--z-space-4);
    border: 2px solid var(--z-danger-border);
    border-radius: var(--z-radius-md);
    background: var(--z-danger-subtle);
  }
  .danger-title {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-bold);
    color: var(--z-danger);
  }
  .danger-body {
    margin: var(--z-space-2) 0 var(--z-space-3);
    max-width: 70ch;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text);
  }
  .danger-error {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-danger);
  }
</style>
