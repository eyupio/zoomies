<!--
  The update settings, edited where their effects are shown.

  The mode, the soak and the check interval used to be found only on the
  Configuration page, so an operator reading "Manual offers v1.3.2" and wanting
  Auto had to leave the page that prompted the thought. They are the same
  settings, saved through the same PATCH, so a value the environment is holding
  reads as pinned here exactly as it does there.

  The mode is a choice among three and takes effect at once, so it is one
  segmented control. Choosing auto asks first, because it is the one choice that
  lets the controller restart itself with nobody pressing anything. The soak and
  the interval are durations, and they use the Configuration page's own row
  rather than a second editor that could disagree with it.

  Only the platform role is sent these rows. Everyone else keeps the read-only
  description the page already had, which is why this is shown to that role alone.
-->
<script lang="ts">
  import { supportHint } from '$lib/errors';
  import { ApiError, getSettings, updateSettings } from '$lib/api/client';
  import type { Problem, Setting, Settings } from '$lib/api/types';
  import { toasts } from '$lib/state/toasts.svelte';
  import { updates } from '$lib/state/updates.svelte';
  import type { UpdateMode } from '$lib/updates/words';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import SettingRow from './SettingRow.svelte';

  const MODE = 'updates.mode';
  const OPTIONS = [
    { value: 'off', label: 'Off', name: 'Never look for a new release' },
    { value: 'manual', label: 'Manual', name: 'Look for releases, install on request' },
    { value: 'auto', label: 'Auto', name: 'Install new releases as they soak' },
  ] as const;

  let settings = $state<Settings | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let failure = $state('');
  let confirming = $state(false);

  $effect(() => {
    const controller = new AbortController();
    loading = true;
    void getSettings(controller.signal)
      .then((result) => (settings = result))
      // The page above still reads the status, so a failure here only costs the
      // controls and not the page.
      .catch(() => (settings = null))
      .finally(() => (loading = false));
    return () => controller.abort();
  });

  function row(key: string): Setting | undefined {
    return settings?.settings?.find((s) => s.key === key);
  }

  const mode = $derived(row(MODE));
  const soak = $derived(row('updates.soak'));
  const interval = $derived(row('updates.check_interval'));
  // What the controller acts on, which the status carries, falls back for the
  // moment before the settings arrive.
  const current = $derived<UpdateMode>(
    updates.status?.mode ?? (mode?.value as UpdateMode) ?? 'off',
  );
  const pinned = $derived(mode?.source === 'environment');

  const findingsBySetting = $derived.by(() => {
    const map: Record<string, Problem[]> = {};
    for (const finding of settings?.findings ?? []) {
      if (!finding.setting) continue;
      (map[finding.setting] ??= []).push(finding);
    }
    return map;
  });

  /** Send one key, and return what was wrong with it for the caller to show. */
  async function save(key: string, value: unknown): Promise<string> {
    try {
      const result = await updateSettings({ [key]: value } as Record<string, unknown>);
      settings = result;
      const now = result.settings?.find((s) => s.key === key);
      if (value === null) {
        toasts.success(`${key} reset`, 'It is back to the configuration file or the default.');
      } else if (now?.pending) {
        toasts.success(`${key} saved`, 'It takes effect the next time the controller restarts.');
      } else {
        toasts.success(`${key} changed`, 'It is in force now.');
      }
      // The sentence under the release depends on all three, so it is asked for
      // again rather than waiting for the stream to bring it.
      void updates.refresh();
      return '';
    } catch (cause) {
      if (cause instanceof ApiError) return cause.fieldErrors()[key] ?? cause.message;
      return `That change could not be made. ${supportHint()}`;
    }
  }

  async function choose(next: string): Promise<boolean> {
    if (next === current) return true;
    saving = true;
    failure = '';
    failure = await save(MODE, next);
    saving = false;
    return failure === '';
  }

  function pick(next: string): void {
    if (next === 'auto' && current !== 'auto') {
      confirming = true;
      return;
    }
    void choose(next);
  }
</script>

{#if loading && !settings}
  <Skeleton lines={3} />
{:else if mode}
  <div class="controls">
    <div class="mode">
      <div class="head">
        <span class="label">Change mode</span>
        <Segmented
          label="Update mode"
          options={OPTIONS}
          value={current}
          onchange={pick}
          disabled={saving || pinned || mode.editable === false}
        />
      </div>
      {#if pinned}
        <p class="detail" role="status">
          The environment is holding this value, so it cannot be changed here. Unset
          <code>ZOOMIES_UPDATE_MODE</code> to change it from this page.
        </p>
      {/if}
      {#if failure}<p class="failure" role="alert">{failure}</p>{/if}
    </div>

    {#each [soak, interval] as setting (setting?.key)}
      {#if setting}
        <SettingRow {setting} findings={findingsBySetting[setting.key] ?? []} onsave={save} />
      {/if}
    {/each}
  </div>

  <ConfirmDialog
    bind:open={confirming}
    title="Let Zoomies update itself"
    description="Switch the update mode to auto?"
    consequences={[
      'Once a release has been public for the soak, the controller updates itself through its update helper and restarts, then updates every host that is behind it, one at a time.',
      'Jobs that are running keep running, but the controller is unavailable for a short while at each restart.',
      'A host whose update fails halts the rollout until an administrator resumes or cancels it.',
    ]}
    confirmLabel="Switch to auto"
    tone="default"
    onconfirm={() => choose('auto')}
  />
{:else}
  <p class="detail">These settings could not be read. The Configuration page has them too.</p>
{/if}

<style>
  .controls {
    margin-top: var(--z-space-4);
    padding-top: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }
  .label {
    font-size: var(--z-text-base);
    color: var(--z-text-muted);
  }
  .detail {
    max-width: var(--z-measure-prose);
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
  }
  .failure {
    max-width: var(--z-measure-prose);
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-sm);
    color: var(--z-danger);
    overflow-wrap: anywhere;
  }
</style>
