<!--
  What a runner is made of, how it is run, and how much Docker a job gets.

  The operating system is first because it is the answer with the widest blast
  radius: it picks the image every runner boots and it keeps them off hosts
  running something else. It is a list rather than a text field because it is
  served from the images Zoomies actually publishes, and a pool that names one
  we do not publish is a pool that validates and then never starts a runner.

  Architecture is not asked here. The hosts section already asks, and the image
  reference does not change with it: every variant is published as a manifest
  covering both, so a host pulls its own.

  Which image comes next. The three published images are a dropdown with the
  words for each beside it, because a choice nobody can read the contents of is
  a choice nobody makes: the full toolchain image existed for some time that
  only a person who had read the configuration reference knew to type in. An
  image of your own is the last option and keeps its free-text field, shown only
  when it is chosen. The Docker variant is not an option; see images.ts.

  The two dangerous answers on the whole page also live here, so both are
  spelled out in the consequence rather than in a footnote, and the host-socket
  option cannot be left selected without a deliberate confirmation. A pinned
  runner version is the least-used control and is kept behind a disclosure of
  its own.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { ServerOff, ShieldAlert } from '@lucide/svelte';
  import type { BackendKind, DockerMode, Host, PoolPlatform } from '$lib/api/types';
  import { pluralise } from '$lib/format';
  import { RUNNER_FULL_IMAGE_URL } from '$lib/links';
  import Button from '$lib/components/Button.svelte';
  import RemedyText from '$lib/components/RemedyText.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import RadioGroup from '$lib/components/RadioGroup.svelte';
  import PoolMore from './PoolMore.svelte';
  import Select from '$lib/components/Select.svelte';
  import { FLAVOURS, flavourCopy, flavourOf, fullImageOf, platformForImage } from './images';
  import type { ImageFlavour } from './images';
  import {
    BACKENDS,
    DOCKER_MODES,
    backendLabel,
    backendUnavailable,
    platformKey,
  } from './vocabulary';
  import type { BackendOffer } from './vocabulary';
  import type { PoolDraft } from './draft';

  interface Props {
    draft: PoolDraft;
    errors: Record<string, string>;
    touch: (field: string) => void;
    offers: readonly BackendOffer[];
    /** False until the fleet cache has landed, so we do not cry wolf about hosts. */
    hostsKnown: boolean;
    /** The operating systems a runner image is published for. */
    platforms: readonly PoolPlatform[];
    /** Every connected host, so the section can say how many match the platform. */
    hosts: readonly Host[];
    /** True when the pool is kept to some of the fleet, so the counts are of those. */
    restricted?: boolean;
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
    restricted = false,
    socketConfirmed = $bindable(false),
  }: Props = $props();

  const uid = $props.id();
  const dockerModeErrorId = $derived(
    errors['docker_mode'] ? `${uid}-docker-mode-error` : undefined,
  );

  function offer(kind: BackendKind): BackendOffer | undefined {
    return offers.find((entry) => entry.kind === kind);
  }

  function availability(kind: BackendKind): string {
    if (!hostsKnown) return '';
    const found = offer(kind);
    if (!found) return '';
    // "matching" rather than "connected" once the pool is kept to some of the
    // fleet: the count is of the hosts it may actually land on.
    const noun = restricted ? 'matching host' : 'connected host';
    if (found.hosts === 0) {
      const none = `No ${noun} offers it`;
      return found.detail
        ? `${none}: ${found.detail}`
        : `${none}, so this pool would never place a runner.`;
    }
    return `Offered by ${pluralise(found.hosts, noun)}.`;
  }

  const backendOptions = $derived(
    BACKENDS.map((choice) => ({
      value: choice.value,
      label: choice.label,
      description: `${choice.consequence} ${availability(choice.value)}`.trim(),
    })),
  );

  const dindHosts = $derived(offer(draft.backend)?.dindHosts ?? 0);

  // Why this pool could not run as chosen, in the same sentence that stops it
  // being saved, plus the backends it could move to.
  const unavailable = $derived(backendUnavailable(draft.backend, offers, hostsKnown, restricted));
  const runnable = $derived(
    offers.filter((entry) => entry.kind !== draft.backend && entry.hosts > 0),
  );

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

  // Open already when something in it is in use, or refused: a setting that
  // is on should never be hidden behind a closed row.
  let moreOpen = $state(
    untrack(
      () =>
        draft.runner_version.trim() !== '' ||
        draft.dind_image.trim() !== '' ||
        Boolean(errors['runner_version'] || errors['dind_image']),
    ),
  );

  /* -- platform ----------------------------------------------------------- */

  const osOptions = $derived([
    { value: '', label: 'Any, use the default image' },
    ...platforms.map((p) => ({
      value: platformKey(p.os, p.os_version),
      label: p.default ? `${p.label} (default)` : (p.label ?? ''),
    })),
  ]);

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
   * How many connected hosts run this operating system. A pool that matches
   * none of them will never start a runner, and finding that out here is far
   * better than finding it out from an empty Runners page.
   *
   * Architecture is not counted here: the Hosts section asks for it, and the
   * image reference does not change with it -- every variant is published as a
   * manifest covering both, so the host pulls its own.
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
      return true;
    }).length,
  );

  /* Image: the published choices, or one of your own. */

  // The platform an image choice is about: the one picked, or the default when
  // the pool says "any", which is the operating system an empty image boots.
  const imagePlatform = $derived(platformForImage(platforms, chosen));
  const fullImage = $derived(fullImageOf(imagePlatform));

  // Custom picked with the field still empty is not something the image string
  // can say, so it is remembered here. Anything typed is Custom without it.
  let customPicked = $state(false);
  const flavour = $derived(flavourOf(draft.image, imagePlatform, customPicked));
  const copy = $derived(flavourCopy(flavour));

  // What was typed under Custom, so leaving it for a moment is not losing it.
  let typedCustom = $state('');
  // Set when changing the operating system took the toolchain image away, so
  // the change is said rather than discovered.
  let fullDropped = $state(false);

  const imageOptions = $derived(
    FLAVOURS.map((entry) => ({
      value: entry.value,
      label:
        entry.value === 'full' && fullImage === ''
          ? `${entry.label} (Ubuntu only)`
          : entry.value === 'standard'
            ? `${entry.label} (default)`
            : entry.label,
      disabled: entry.value === 'full' && fullImage === '' && flavour !== 'full',
    })),
  );

  function chooseFlavour(value: string): void {
    const next = value as ImageFlavour;
    if (flavour === 'custom' && draft.image.trim() !== '') typedCustom = draft.image;
    fullDropped = false;
    customPicked = next === 'custom';
    if (next === 'standard') draft.image = '';
    else if (next === 'full') draft.image = fullImage;
    else if (draft.image.trim() === '' || draft.image.trim() === fullImage) {
      // Start from what was typed last, or from the image just left, so the
      // field is never an empty box with no idea what belongs in it.
      draft.image = typedCustom || draft.image;
    }
    touch('image');
  }

  function chooseOS(value: string): void {
    const wasFull = flavour === 'full';
    const found = platforms.find((p) => platformKey(p.os, p.os_version) === value);
    draft.platform_os = found?.os ?? '';
    draft.platform_os_version = found?.os_version ?? '';
    touch('platform.os');
    if (wasFull) {
      // The toolchain image is per operating system: follow the pool to the new
      // one, or back to Standard where there is none.
      const next = fullImageOf(platformForImage(platforms, found));
      draft.image = next;
      fullDropped = next === '';
      touch('image');
    }
  }

  // A pool's docker_mode gives its jobs a daemon; the image has to bring the
  // client. The stock runner image carries none on purpose, so a pool that
  // asks for a daemon while on a tag this build publishes is switched to the
  // stock image's Docker variant -- the same image plus a client, under the
  // same tag -- as it is saved. Say so in the section that decides it, so the
  // image the pool's page shows afterwards is not a surprise; the pool written
  // out at the foot of the page shows the image the server answers with, which covers an empty field as
  // well.
  //
  // What is not switched is said too, each for its own reason. A digest names
  // one exact image. A tag from some other build -- a commit pin, a release
  // this build knows nothing about -- may name a run whose variant was never
  // published, and moving a pool onto a tag the registry lacks would stop it
  // running every job rather than only the Docker ones. An image that merely
  // looks like the stock one -- a mirror, or one built on it -- may or may not
  // carry a client, and the server cannot know. An image of somebody's own
  // gets no line at all: nagging about it would teach them to ignore this
  // line.
  //
  // Only the shape is decided here. Which tags this build publishes is the
  // server's answer, and the pool written out at the foot of the page shows it.
  const DOCKER_IMAGE = 'ghcr.io/eyupio/zoomies-runner-docker';
  const STOCK_TAGGED = /^ghcr\.io\/eyupio\/zoomies-runner(:[^@]+)?$/;
  const STOCK_DIGEST = /^ghcr\.io\/eyupio\/zoomies-runner(:[^@]+)?@/;
  const LOOKALIKE_IMAGE = /(^|\/)zoomies-runner(:|$)/;

  const imageNotice = $derived.by((): string | undefined => {
    if (!usesImage || (draft.docker_mode ?? 'none') === 'none') return undefined;
    const image = draft.image?.trim() ?? '';
    if (image === '') return undefined;
    const tag = image.includes(':') ? image.slice(image.indexOf(':')) : '';
    if (STOCK_DIGEST.test(image)) {
      return `A digest names one exact image, and this one has no Docker client. Pin a digest of ${DOCKER_IMAGE} instead.`;
    }
    if (STOCK_TAGGED.test(image)) {
      return `The stock runner image has no Docker client, so this pool runs ${DOCKER_IMAGE}${tag} instead where that tag exists for it. The pool written out at the foot of the page shows the image it kept.`;
    }
    if (LOOKALIKE_IMAGE.test(image)) {
      return 'This looks like the stock runner image, which has no Docker client, and it is not one Zoomies switches for you. Jobs that run docker fail on it even with a daemon attached, unless docker is installed in it.';
    }
    return undefined;
  });

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
  <Field
    label="Operating system"
    error={errors['platform.os'] ?? errors['platform.os_version']}
    hint="Picks the zoomies-runner image these runners boot, and keeps them off hosts running something else."
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

  <Field
    label="Runner image"
    error={flavour === 'custom' ? undefined : errors['image']}
    hint="What is installed in the runner your jobs start in. The words below change with the choice."
    notice={fullDropped
      ? 'The toolchain image is published for Ubuntu only, so this pool is back on the standard image.'
      : undefined}
  >
    {#snippet children({ id, describedBy, invalid })}
      <Select
        value={flavour}
        options={imageOptions}
        {id}
        {describedBy}
        {invalid}
        onchange={chooseFlavour}
      />
    {/snippet}
  </Field>

  <section class="flavour" aria-label="About the {copy.label} image">
    <p class="flavour-summary">{copy.summary}</p>
    <dl class="flavour-facts">
      <dt>What is in it</dt>
      <dd>{copy.contains}</dd>
      <dt>Choose it when</dt>
      <dd>{copy.chooseWhen}</dd>
      <dt>Keep in mind</dt>
      <dd>{copy.keepInMind}</dd>
    </dl>
    {#if flavour === 'full'}
      <p class="flavour-link">
        <a href={RUNNER_FULL_IMAGE_URL} target="_blank" rel="noreferrer"
          >The full list of toolchains</a
        >
      </p>
    {/if}
  </section>

  {#if flavour === 'custom'}
    <Field
      label="Image"
      error={errors['image']}
      hint="The image reference to boot, with a tag or a digest. Leave it empty to follow the operating system above."
      notice={imageNotice}
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

  <details class="compare">
    <summary>Compare the three</summary>
    <dl>
      {#each FLAVOURS as entry (entry.value)}
        <dt>{entry.label}</dt>
        <dd>{entry.summary}</dd>
      {/each}
    </dl>
  </details>
{/if}

<RadioGroup
  name="pool-backend"
  legend="Backend"
  value={draft.backend}
  options={backendOptions}
  onchange={chooseBackend}
/>

{#if unavailable}
  <!--
    A pool no host can run is the failure that looks like health: it is enabled,
    its labels match, and it never makes a runner. The editor will not create
    one while the fleet has something else to offer, so this says what is wrong
    and changes it in one click rather than leaving the operator to guess.
  -->
  <div class="unrunnable" role="group" aria-labelledby="backend-unrunnable">
    <p class="unrunnable-title" id="backend-unrunnable">
      <ServerOff size={16} aria-hidden="true" />
      No {restricted ? 'matching' : 'connected'} host can run a {backendLabel(draft.backend)} pool
    </p>
    <p class="unrunnable-body"><RemedyText text={unavailable} /></p>
    <div class="unrunnable-actions">
      {#each runnable as entry (entry.kind)}
        <Button size="sm" onclick={() => chooseBackend(entry.kind)}>
          Use {backendLabel(entry.kind)} ({pluralise(entry.hosts, 'host')})
        </Button>
      {/each}
    </div>
  </div>
{/if}

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
        filesystem and read every secret on that machine, including the credentials of every other
        pool's runners. A pull request from a fork is enough to do it. Use Docker in Docker unless
        you control every workflow that can reach these labels.
      </p>
      <Checkbox
        bind:checked={socketConfirmed}
        label="I understand that this gives every job on this pool root on the host"
        describedBy={dockerModeErrorId}
        onchange={() => touch('docker_mode')}
      />
      {#if errors['docker_mode']}
        <!-- role="alert" so the failure is spoken when it appears; the
             describedby link on the checkbox alone is only read if the field
             is revisited. -->
        <p class="danger-error" id={dockerModeErrorId} role="alert">{errors['docker_mode']}</p>
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

<PoolMore
  title="A pinned runner version or Docker-in-Docker image"
  note={draft.runner_version.trim() !== '' ||
  (draft.docker_mode === 'dind' && draft.dind_image.trim() !== '')
    ? 'in use'
    : 'the defaults follow the platform and the latest runner'}
  bind:open={moreOpen}
>
  {#if draft.docker_mode === 'dind'}
    <Field
      label="Docker-in-Docker image"
      error={errors['dind_image']}
      hint="The image this pool's Docker sidecar runs. Leave it empty to use the host's, which is the agent's Docker-in-Docker image setting. It runs privileged and has to provide dockerd."
    >
      {#snippet children({ id, describedBy, invalid })}
        <Input
          bind:value={draft.dind_image}
          {id}
          {describedBy}
          {invalid}
          mono
          placeholder="docker:27-dind"
          autocomplete="off"
          onblur={() => touch('dind_image')}
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
</PoolMore>

<style>
  .platform-note {
    margin: calc(var(--z-space-3) * -1) 0 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .platform-note code {
    font-family: var(--z-font-mono);
  }
  .flavour {
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised, var(--z-surface));
  }
  .flavour-summary {
    margin: 0;
    max-width: 70ch;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text);
  }
  .flavour-facts {
    display: grid;
    gap: var(--z-space-1) var(--z-space-4);
    margin: var(--z-space-3) 0 0;
    max-width: 70ch;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .flavour-facts dt {
    margin-top: var(--z-space-2);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .flavour-facts dd {
    margin: 0;
    color: var(--z-text-muted);
  }
  .flavour-link {
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-sm);
  }
  .flavour-link a {
    color: var(--z-accent);
  }
  .compare {
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .compare summary {
    cursor: pointer;
  }
  .compare dl {
    display: grid;
    gap: var(--z-space-1);
    margin: var(--z-space-2) 0 0;
    max-width: 70ch;
  }
  .compare dt {
    margin-top: var(--z-space-2);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .compare dd {
    margin: 0;
  }
  .unrunnable {
    margin-top: var(--z-space-4);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-danger-border, var(--z-border));
    border-left: var(--z-border-width-rail) solid var(--z-danger);
    border-radius: var(--z-radius-md);
    background: var(--z-danger-subtle);
  }
  .unrunnable-title {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .unrunnable-body {
    margin: var(--z-space-2) 0 0;
    max-width: 70ch;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
  }
  .unrunnable-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-top: var(--z-space-3);
  }

  .docker {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .danger {
    padding: var(--z-space-4);
    border: var(--z-border-width-thick) solid var(--z-danger-border);
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
