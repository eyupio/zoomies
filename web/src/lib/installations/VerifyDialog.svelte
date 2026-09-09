<!--
  What the credentials can actually do.

  A verify is only useful if it says which permission is missing, so this
  renders the probe's own lists by name: the permissions GitHub reports for the
  installation, the events it will send, and the ones Zoomies needs and did not
  get. Missing entries come first, because they are the reason somebody opened
  this.
-->
<script lang="ts">
  import { Check, ExternalLink, X } from '@lucide/svelte';
  import type { Installation, InstallationHealth } from '$lib/api/types';
  import { formatNumber, joinWords } from '$lib/format';
  import Button from '$lib/components/Button.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';

  interface Props {
    open?: boolean;
    installation: Installation | null;
    health: InstallationHealth | null;
    loading?: boolean;
    /** Set when the probe itself could not be made. */
    error?: string;
    onclose?: () => void;
  }

  let {
    open = $bindable(false),
    installation,
    health,
    loading = false,
    error = '',
    onclose,
  }: Props = $props();

  const permissions = $derived(Object.entries(health?.permissions ?? {}));
  /**
   * What the installation can actually see.
   *
   * The second commonest setup mistake after a missing permission, and the
   * quietest one: an App with every permission correct, installed on "only
   * select repositories" and not on the one somebody pushes to, is a fleet
   * where nothing ever queues and no page says why. GitHub knows; until this,
   * nothing asked it.
   */
  const repositories = $derived(health?.repositories ?? []);
  const selection = $derived(health?.repository_selection ?? '');
  const repositoryCount = $derived(health?.repository_count ?? 0);
  const capped = $derived(health?.repositories_capped === true);
  const missingPermissions = $derived(health?.missing_permissions ?? []);
  const missingEvents = $derived(health?.missing_events ?? []);
  const events = $derived(health?.events ?? []);
  const ok = $derived(health?.ok === true);

  /**
   * Where the operator actually fixes this.
   *
   * The dialog names the missing permission and then says "open the App's
   * settings on GitHub" -- a page whose URL is not guessable, among however
   * many Apps the organisation has. Both halves are already in hand:
   * `installation.web_url` is GitHub's base for this deployment, and
   * `health.app_slug` names the App.
   */
  const settingsURL = $derived.by(() => {
    const base = installation?.web_url?.replace(/\/+$/, '');
    const slug = health?.app_slug;
    if (!base || !slug) return '';
    return installation?.target_type === 'org'
      ? `${base}/organizations/${installation.target}/settings/apps/${slug}`
      : `${base}/settings/apps/${slug}`;
  });

  /** Where the organisation accepts a permission change the App has requested. */
  const installationsURL = $derived.by(() => {
    const base = installation?.web_url?.replace(/\/+$/, '');
    if (!base || !installation?.target) return '';
    return installation.target_type === 'org'
      ? `${base}/organizations/${installation.target}/settings/installations`
      : `${base}/settings/installations`;
  });
</script>

<Dialog
  bind:open
  title="Verify {installation?.target || 'installation'}"
  description="Zoomies asked GitHub what these credentials can do."
  size="md"
  {onclose}
>
  {#if loading}
    <div class="stack" aria-busy="true">
      <Skeleton width="60%" height="1.25rem" />
      <Skeleton lines={4} />
    </div>
  {:else if error}
    <p class="verdict bad" role="alert">{error}</p>
  {:else if health}
    <!-- role="status": the verdict, the missing lists and the remedy all
         replace a skeleton with no focus move, so without this the operator who
         pressed Verify is left holding a Close button and nothing was said. -->
    <div class="stack" role="status">
      <p class="verdict" class:bad={!ok} class:good={ok}>
        {#if ok}
          <Check size={15} aria-hidden="true" />
        {:else}
          <X size={15} aria-hidden="true" />
        {/if}
        <span>
          {health.message ||
            (ok
              ? 'The credentials work and every permission Zoomies needs is granted.'
              : 'The credentials did not work.')}
        </span>
      </p>

      {#if health.app_name || health.app_slug}
        <p class="app">
          App: <span class="mono">{health.app_slug || health.app_name}</span>
          {#if health.rate_limit_remaining !== undefined}
            <span class="muted"
              >· {formatNumber(health.rate_limit_remaining)} API requests left in this window</span
            >
          {/if}
        </p>
      {/if}

      {#if missingPermissions.length > 0}
        <section aria-labelledby="missing-permissions">
          <h3 id="missing-permissions" class="bad-heading">Permissions Zoomies still needs</h3>
          <ul class="chips missing">
            {#each missingPermissions as name (name)}
              <li class="mono">{name}</li>
            {/each}
          </ul>
          <p class="fix">
            Open the App's settings on GitHub, grant {joinWords([...missingPermissions])}, then
            accept the updated permissions on the installation.
          </p>
          {#if settingsURL}
            <p class="fix-actions">
              <Button
                variant="primary"
                size="sm"
                href={settingsURL}
                newTab
                iconAfter={ExternalLink}
              >
                Open the App's settings
              </Button>
              {#if installationsURL}
                <a href={installationsURL} target="_blank" rel="noopener noreferrer">
                  Accept the change on the installation<span class="sr-only">
                    (opens in a new tab)</span
                  >
                </a>
              {/if}
            </p>
          {/if}
        </section>
      {/if}

      {#if missingEvents.length > 0}
        <section aria-labelledby="missing-events">
          <h3 id="missing-events" class="bad-heading">Events Zoomies is not subscribed to</h3>
          <ul class="chips missing">
            {#each missingEvents as name (name)}
              <li class="mono">{name}</li>
            {/each}
          </ul>
          <p class="fix">
            Without {joinWords([...missingEvents])} the controller only learns about jobs when it polls,
            which is slower and uses more of the API quota.
          </p>
        </section>
      {/if}

      {#if selection || repositoryCount > 0}
        <section aria-labelledby="repositories">
          <h3 id="repositories">Repositories this installation can see</h3>
          <p class="scope">
            {#if selection === 'all'}
              Every repository in {installation?.target ?? 'the target'}, including ones added
              later.
            {:else if selection === 'selected'}
              Only the {repositoryCount > 0 ? repositoryCount : ''} selected on GitHub. A pool only ever
              sees jobs from these, so a repository that is not here queues nothing and says nothing.
            {:else if repositoryCount > 0}
              {repositoryCount} in reach of these credentials.
            {/if}
          </p>
          {#if repositories.length > 0}
            <ul class="chips">
              {#each repositories as name (name)}
                <li class="mono">{name}</li>
              {/each}
              {#if capped}
                <li class="more">and more</li>
              {/if}
            </ul>
          {/if}
        </section>
      {/if}

      {#if permissions.length > 0}
        <section aria-labelledby="granted-permissions">
          <h3 id="granted-permissions">Permissions granted</h3>
          <ul class="chips">
            {#each permissions as [name, level] (name)}
              <li class="mono">{name}: {level}</li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if events.length > 0}
        <section aria-labelledby="subscribed-events">
          <h3 id="subscribed-events">Events subscribed</h3>
          <ul class="chips">
            {#each events as name (name)}
              <li class="mono">{name}</li>
            {/each}
          </ul>
        </section>
      {/if}
    </div>
  {/if}

  {#snippet footer()}
    <Button
      variant="primary"
      onclick={() => {
        open = false;
        onclose?.();
      }}
    >
      Close
    </Button>
  {/snippet}
</Dialog>

<style>
  .scope {
    margin: 0 0 var(--z-space-2);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .more {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    align-self: center;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-2);
  }
  .verdict {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
    margin: 0;
    padding: var(--z-space-3);
    border-radius: var(--z-radius-sm);
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
  }
  .verdict.good {
    border: var(--z-border-width) solid var(--z-idle-border);
    background: var(--z-idle-subtle);
    color: var(--z-text);
  }
  .verdict.bad {
    border: var(--z-border-width) solid var(--z-danger-border);
    background: var(--z-danger-subtle);
    color: var(--z-text);
  }
  .app {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  h3 {
    margin: 0 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  h3.bad-heading {
    color: var(--z-danger);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .chips li {
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-sm);
  }
  .chips.missing li {
    border-color: var(--z-danger-border);
    background: var(--z-danger-subtle);
    color: var(--z-text);
  }
  .fix {
    margin: var(--z-space-2) 0 0;
    max-width: 70ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .muted {
    color: var(--z-text-subtle);
  }
  .fix-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
    margin: var(--z-space-3) 0 0;
  }
  .fix-actions a {
    font-size: var(--z-text-xs);
    color: var(--z-accent);
  }
</style>
