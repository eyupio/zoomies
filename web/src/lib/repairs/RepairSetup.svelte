<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ApiError,
    getEliRepairSettings,
    getOptimisationEnablement,
    setOptimisationEnablement,
    listInstallations,
    setEliIdentity,
    setEliRepairConsent,
    setEliRepairPolicy,
  } from '$lib/api/client';
  import type { Schemas, Installation } from '$lib/api/types';
  import { session } from '$lib/state/session.svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Select from '$lib/components/Select.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';

  let { repo, oncomplete }: { repo: string; oncomplete: () => Promise<void> } = $props();
  let identity = $state<Schemas['EliIdentity'] | undefined>();
  let policy = $state<Schemas['EliRepairPolicy'] | undefined>();
  let installations = $state<Installation[]>([]);
  let installation = $state('');
  let login = $state('');
  // Offered, never assumed: ticking it is the consent to let Zoomies read a
  // workflow file and keep a proposed edit, which repairing a pull request
  // does not need.
  let previewsOff = $state(false);
  let enablePreviews = $state(false);
  let loading = $state(true);
  let busy = $state(false);
  let error = $state('');
  const admin = $derived(session.can('admin'));
  const needsPolicy = $derived(!policy?.enabled);
  const canSetup = $derived(
    !!session.identity?.id &&
      (identity || (admin && login.trim() && installation)) &&
      (!needsPolicy || (admin && installation)),
  );

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      const [settings, installs, previews] = await Promise.all([
        getEliRepairSettings(),
        admin ? listInstallations() : Promise.resolve({ items: [] }),
        admin ? getOptimisationEnablement().catch(() => undefined) : Promise.resolve(undefined),
      ]);
      previewsOff = !!previews && !previews.enabled && previews.can_change;
      identity = settings.identities.find((item) => item.user_id === session.identity?.id);
      policy = settings.policies.find((item) => item.repo.toLowerCase() === repo.toLowerCase());
      installations = installs.items ?? [];
      const exact = installations.find((item) => item.target?.toLowerCase() === repo.toLowerCase());
      const owner = installations.filter(
        (item) => item.target?.toLowerCase() === repo.split('/')[0]?.toLowerCase(),
      );
      installation =
        policy?.installation_id || exact?.id || (owner.length === 1 ? owner[0]?.id : '') || '';
    } catch (cause) {
      error =
        cause instanceof ApiError ? cause.message : 'Could not read repair settings. Try again.';
    } finally {
      loading = false;
    }
  }

  async function setup(): Promise<void> {
    if (!canSetup || busy) return;
    busy = true;
    error = '';
    try {
      // Each step is safe to retry after a later step fails. Never move an
      // existing account link or infer somebody's GitHub identity from a name.
      if (!identity) {
        identity = await setEliIdentity({
          user_id: session.identity!.id!,
          github_login: login.trim(),
          installation_id: installation,
          repo,
        });
      }
      if (!identity.confirmed) {
        await setEliRepairConsent({ github_user_id: identity.github_user_id, enabled: true });
        identity = { ...identity, confirmed: true };
      }
      if (needsPolicy && admin) {
        policy = await setEliRepairPolicy({
          repo,
          installation_id: installation,
          provider_id: '',
          enabled: true,
          automatic: false,
          ask_first: false,
          allow_workflows: policy?.allow_workflows ?? false,
        });
      }
      if (enablePreviews && admin && previewsOff) {
        await setOptimisationEnablement(true);
        previewsOff = false;
      }
      await oncomplete();
    } catch (cause) {
      error = cause instanceof ApiError ? cause.message : 'Could not finish setup. Try again.';
    } finally {
      busy = false;
    }
  }
  onMount(() => void load());
</script>

<section aria-label="Set up repairs for this failure" class="setup">
  <h2>Set up repairs for {repo}</h2>
  {#if loading}<p role="status">Checking existing setup…</p>
  {:else if error}<p role="alert">{error}</p>
    <Button onclick={() => void load()}>Reload setup</Button>
  {:else}
    {#if !identity && admin}
      <Field
        id="repair-setup-login"
        label="Your GitHub username"
        hint="Enter your own account. Eli verifies write access to this repository."
      >
        <Input id="repair-setup-login" bind:value={login} required />
      </Field>
    {:else if !identity}
      <p>Ask an administrator to link your GitHub account for {repo} in repair settings.</p>
    {:else if !identity.confirmed}
      <p>
        Continue to confirm that @{identity.github_login} is your GitHub account and allow it to request
        repairs using your personal provider.
      </p>
    {/if}
    {#if admin && (!identity || needsPolicy)}
      <Field id="repair-setup-installation" label="GitHub installation">
        <Select
          id="repair-setup-installation"
          bind:value={installation}
          options={installations.map((item) => ({
            value: item.id ?? '',
            label: item.target ?? 'Installation',
          }))}
          placeholder="Choose the installation for this repository"
        />
      </Field>
    {/if}
    {#if needsPolicy}
      {#if admin}<p>
          Enable manual repairs for {repo}. Automatic repairs stay off. Workflow edits are {policy?.allow_workflows
            ? 'allowed by the existing policy'
            : 'disabled'}.
        </p>
      {:else}<p>An administrator needs to enable repairs for {repo}.</p>{/if}
    {/if}
    {#if previewsOff}
      <Checkbox
        bind:checked={enablePreviews}
        label="Also let Zoomies preview workflow speed-ups"
        description="Lets it read one workflow file at a run's commit and keep a proposed change for 14 days. Nothing is published and no model sees the file. Repairs do not need this."
      />
    {/if}
    <p>
      Eli checks this failed run again after setup. You review the target PR before starting a
      repair.
    </p>
    <Button variant="primary" disabled={!canSetup} loading={busy} onclick={() => void setup()}
      >Save and check this failure</Button
    >
  {/if}
</section>

<style>
  .setup {
    display: grid;
    gap: var(--z-space-3);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
  }
  h2,
  p {
    margin: 0;
  }
  p {
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-lg);
  }
</style>
