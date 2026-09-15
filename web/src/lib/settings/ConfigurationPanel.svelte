<!--
  The configuration.

  Everything here lives in this fleet's database, so a change made here is kept
  rather than going back to a file at the next restart. Three keys are the
  exception and say so in the row: the path to the database and the two ways of
  naming the key that unseals what is in it cannot live inside the thing they
  open.

  Eighty-eight settings is too many to scroll, so the page is built to be
  searched and filtered rather than read: a box that matches keys, summaries and
  environment variable names, a filter for the ones somebody has actually
  changed, and a jump to each section. What an operator usually wants is one of
  three things -- the setting they came for, everything that is not a default,
  or whatever the controller is complaining about -- and all three are one
  action away.

  Secrets are absent rather than starred out: the API does not send them at all.
-->
<script lang="ts">
  import { Lock, RotateCcw, Search, TriangleAlert } from '@lucide/svelte';
  import { getSettings, updateSettings, ApiError } from '$lib/api/client';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import type { Problem, Setting, Settings } from '$lib/api/types';
  import { severityStatus } from '$lib/status';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import Input from '$lib/components/Input.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import SettingRow from './SettingRow.svelte';
  import { SECTION_BLURB, displayValue, matches } from './settings';

  interface Props {
    class?: string;
    /**
     * Bumped by the page's refresh button. Read inside the fetch effect, which
     * is what makes one press at the top of Settings re-read whichever panel is
     * open rather than only the tab the operator happens to be looking past.
     */
    reloadKey?: number;
  }

  let { class: className = '', reloadKey = 0 }: Props = $props();

  let settings = $state<Settings | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let reload = $state(0);

  $effect(() => {
    void reload;
    void reloadKey;
    const controller = new AbortController();
    loading = true;
    void getSettings(controller.signal)
      .then((result) => {
        settings = result;
        error = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        error = cause;
      })
      .finally(() => (loading = false));
    return () => controller.abort();
  });

  /* -- filtering ------------------------------------------------------------ */

  type View = 'all' | 'changed' | 'attention';

  let query = $state('');
  let view = $state<View>('all');
  let searchField = $state<HTMLInputElement | null>(null);

  /**
   * The setting a link asked for, from the address bar.
   *
   * A problem that names a key used to land somebody on a list of eighty-eight
   * and leave them to find it -- nine tenths of the work done and then stopped.
   * The row is scrolled to and marked instead, and the filters are stood down
   * for it, because a link that arrives while "Changed" is selected would
   * otherwise open a page the setting is not on.
   */
  const wanted = $derived(router.param('setting'));
  let sought = $state('');

  $effect(() => {
    const key = wanted;
    if (!key || !settings) return;
    // Untangled from the filters first, or the row may not be rendered to
    // scroll to. Both are cleared rather than only the one that would hide it:
    // which of them does depends on the key, and a page that sometimes honours
    // a link is worse than one that always does.
    query = '';
    view = 'all';
    sought = key;
    // After the filter change has rendered. A frame is enough, and the read is
    // guarded because a key nobody recognises simply scrolls nowhere.
    requestAnimationFrame(() => {
      document.getElementById(`setting-${key}`)?.scrollIntoView({ block: 'center' });
    });
  });

  /**
   * `/` puts the cursor in the search box, the way it does in every list an
   * operator reads rather than fills in. Ignored while something else is taking
   * typing, or the key would be swallowed halfway through a value somebody was
   * editing.
   */
  function onKey(event: KeyboardEvent): void {
    if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return;
    const active = document.activeElement;
    const tag = active?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    if (active instanceof HTMLElement && active.isContentEditable) return;
    event.preventDefault();
    searchField?.focus();
  }

  const all = $derived<readonly Setting[]>(settings?.settings ?? []);

  const findingsBySetting = $derived.by(() => {
    const map: Record<string, Problem[]> = {};
    for (const finding of settings?.findings ?? []) {
      if (!finding.setting) continue;
      (map[finding.setting] ??= []).push(finding);
    }
    return map;
  });

  const changedCount = $derived(all.filter((s) => s.source !== 'default').length);
  const attentionCount = $derived(
    all.filter((s) => s.pending || (findingsBySetting[s.key]?.length ?? 0) > 0).length,
  );

  const views = $derived([
    { value: 'all' as const, label: `All ${all.length}` },
    { value: 'changed' as const, label: `Changed ${changedCount}` },
    ...(attentionCount > 0
      ? [{ value: 'attention' as const, label: `Attention ${attentionCount}` }]
      : []),
  ]);

  const visible = $derived(
    all.filter((s) => {
      if (!matches(s, query)) return false;
      if (view === 'changed') return s.source !== 'default';
      if (view === 'attention') return s.pending || (findingsBySetting[s.key]?.length ?? 0) > 0;
      return true;
    }),
  );

  /*
    A setting the environment is holding is not something an administrator can
    do anything about from here, so it does not belong among the ones they can.
    It is gathered at the end instead, where it answers "why is that value not
    what I set" without standing between somebody and the setting they came for.
  */
  const held = $derived(visible.filter((s) => s.source === 'environment'));
  const changeable = $derived(visible.filter((s) => s.source !== 'environment'));

  /* The API already orders the list, so grouping preserves that order. */
  const sections = $derived.by(() => {
    const out: { name: string; rows: Setting[] }[] = [];
    for (const setting of changeable) {
      const name = setting.section ?? 'other';
      const last = out[out.length - 1];
      if (last?.name === name) last.rows.push(setting);
      else out.push({ name, rows: [setting] });
    }
    return out;
  });

  /** Findings the validator did not tie to a setting this page renders. */
  const generalFindings = $derived(
    (settings?.findings ?? []).filter(
      (finding) => !finding.setting || !all.some((s) => s.key === finding.setting),
    ),
  );

  const pending = $derived(settings?.pending_restart ?? []);

  /* -- changing one --------------------------------------------------------- */

  /**
   * Send one key. The API takes dotted keys directly, and answers 422 with a
   * message written for a person -- which is returned here so the row can put
   * it under the field rather than in a toast that floats away.
   */
  async function save(key: string, value: unknown): Promise<string> {
    try {
      const result = await updateSettings({ [key]: value } as Record<string, unknown>);
      settings = result;
      // Some of what changed is also what every page reads from /meta.
      void session.reloadMeta();
      const now = result.settings?.find((s) => s.key === key);
      if (value === null) {
        toasts.success(`${key} reset`, 'It is back to the configuration file or the default.');
      } else if (now?.pending) {
        toasts.success(`${key} saved`, 'It takes effect the next time the controller restarts.');
      } else {
        toasts.success(`${key} changed`, 'It is in force now.');
      }
      return '';
    } catch (cause) {
      if (cause instanceof ApiError) {
        const errors = cause.fieldErrors();
        if (errors[key]) return errors[key];
        /*
          A refusal about the whole request rather than about this field --
          the combination would leave a controller that will not start, and
          the setting at fault is one nobody touched. Its reason is the useful
          half, so it is carried through rather than dropped for the envelope's
          one-line summary.
        */
        const general = Object.values(errors).filter(Boolean);
        return general.length > 0 ? `${cause.message}: ${general.join(' ')}` : cause.message;
      }
      return 'That change could not be made. The controller log will say why.';
    }
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="panel {className}">
  <header>
    <div>
      <h2>Configuration</h2>
      <p>
        What this controller is running, and where each value came from. Settings are kept in this
        fleet's database, so a change made here survives a restart. Four layers stack — the built-in
        defaults, then the configuration file, then the database, then <code>ZOOMIES_*</code> in the environment
        — and each one wins over the one before it.
      </p>
    </div>
  </header>

  <LoadingBoundary {loading} {error} onretry={() => (reload += 1)}>
    {#snippet skeleton()}
      <div class="pad"><Skeleton lines={8} /></div>
    {/snippet}

    {#if settings}
      {#if pending.length > 0}
        <section class="notice waiting" aria-labelledby="pending-restart">
          <Badge tone="draining" label="Saved" size="sm" dot={false} />
          <div>
            <h3 id="pending-restart">
              {pending.length === 1
                ? 'One setting is waiting for a restart'
                : `${pending.length} settings are waiting for a restart`}
            </h3>
            <p>
              They are stored and will be in force the next time this controller starts. It cannot
              apply them to itself — rebinding a listener or rebuilding the container backends under
              running jobs is how a reload becomes an outage.
            </p>
            <ul class="keys">
              {#each pending as key (key)}
                <li class="mono">{key}</li>
              {/each}
            </ul>
          </div>
        </section>
      {/if}

      {#if generalFindings.length > 0}
        <section class="general" aria-labelledby="general-findings">
          <h3 id="general-findings">
            <TriangleAlert size={14} aria-hidden="true" />
            What the validator says
          </h3>
          <ul>
            {#each generalFindings as finding (finding.code)}
              {@const meta = severityStatus(finding.severity)}
              <li>
                <Badge status={meta} size="sm" />
                <div>
                  <p class="finding-title">{finding.title}</p>
                  {#if finding.detail}<p class="finding-detail">{finding.detail}</p>{/if}
                  {#if finding.fix}
                    <p class="finding-detail"><strong>Fix:</strong> {finding.fix}</p>
                  {/if}
                  <!--
                    Which layer set it, when that is not where the fix points.
                    Told only to "change server.bind", an operator edits the
                    configuration file -- and a value stored here or pinned by
                    a variable is one the file cannot change, so they restart
                    into the same complaint having learned nothing.
                  -->
                  {#if finding.undo}
                    <p class="finding-detail">
                      <strong
                        >{finding.source === 'environment'
                          ? 'The environment is setting this'
                          : 'This value is stored in this fleet'}</strong
                      >
                      {finding.source === 'environment'
                        ? ', and it is the last word: neither the file nor this page can change it. With the controller stopped, run'
                        : ', so editing the configuration file will not change it. If it is stopping the controller starting, run this against the stopped controller:'}
                      <code>{finding.undo}</code>
                    </p>
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      <div class="controls">
        <div class="search">
          <Input
            bind:value={query}
            bind:element={searchField}
            size="sm"
            icon={Search}
            placeholder="Search settings, summaries and ZOOMIES_* names  (press /)"
            ariaLabel="Search settings"
          />
        </div>
        <Segmented
          options={views}
          value={view}
          label="Which settings to show"
          onchange={(next) => (view = next as View)}
        />
      </div>

      <div class="meta">
        <div>
          <span class="meta-label">Configuration file</span>
          {#if settings.config_path}
            <CopyButton
              value={settings.config_path}
              label="Copy the configuration file path"
              showValue
            />
          {:else}
            <span class="meta-value"
              >None. Everything comes from the database and the defaults.</span
            >
          {/if}
        </div>
        <div>
          <span class="meta-label">Database</span>
          <CopyButton
            value={settings.database_path ?? ''}
            label="Copy the database path"
            showValue
          />
        </div>
      </div>

      {#if sections.length === 0 && held.length === 0}
        <p class="empty">
          <RotateCcw size={14} aria-hidden="true" />
          Nothing matches
          {#if query}“{query}”{/if}
          {#if view === 'changed'}among the settings that have been changed{/if}
          {#if view === 'attention'}among the settings needing attention{/if}.
        </p>
      {/if}

      <!--
        The jump. A hundred settings under fourteen headings is a page nobody
        scrolls twice, and the search box only helps somebody who already knows
        what the setting is called -- which is the case this panel is least
        needed for. The rail is what the operator who is looking around uses,
        and it narrows with the filters so a section the search emptied is not
        offered as somewhere to go.

        It sits under the file and database paths rather than above them
        because the chips are a row of their own: moved up, they land beside
        that block's columns and stretch to its height.
      -->
      {#if sections.length > 1}
        <nav class="jump" aria-label="Jump to a section">
          {#each sections as section (section.name)}
            <a href="#section-{section.name}" class="mono">
              {section.name}
              <span class="jump-count">{section.rows.length}</span>
            </a>
          {/each}
        </nav>
      {/if}

      {#each sections as section (section.name)}
        <section aria-labelledby="section-{section.name}">
          <div class="section-head">
            <h3 id="section-{section.name}" class="mono">{section.name}</h3>
            {#if SECTION_BLURB[section.name]}<p>{SECTION_BLURB[section.name]}</p>{/if}
          </div>
          {#each section.rows as setting (setting.key)}
            <SettingRow
              {setting}
              findings={findingsBySetting[setting.key] ?? []}
              sought={setting.key === sought}
              onsave={save}
            />
          {/each}
        </section>
      {/each}

      {#if held.length > 0}
        <fieldset class="held">
          <legend>
            <Lock size={13} aria-hidden="true" />
            Held by the environment
          </legend>
          <p class="held-note">
            {held.length === 1 ? 'This setting is' : 'These settings are'} set by a
            <code>ZOOMIES_*</code> variable, and the environment is the last word — it overrides
            both the database and the configuration file. To change
            {held.length === 1 ? 'it' : 'them'}, amend the environment file this controller starts
            with and restart it. Removing a variable hands that setting back to this page.
          </p>
          <ul class="held-rows">
            {#each held as setting (setting.key)}
              <!--
                The order here is the order the grid places them in: the
                variable and its value on one line, the setting's name and its
                key underneath. A `display: contents` row flows in document
                order, so the columns are assigned by where each span sits
                rather than by the grid-column it asks for.
              -->
              <li>
                <span class="held-env mono">{setting.env}</span>
                <span class="held-value mono">{displayValue(setting)}</span>
                <span class="held-label">{setting.label}</span>
                <span class="held-key mono">{setting.key}</span>
              </li>
            {/each}
          </ul>
        </fieldset>
      {/if}
    {/if}
  </LoadingBoundary>
</div>

<style>
  .panel {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  header {
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
    max-width: 84ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  header code {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
  }
  .pad {
    padding: var(--z-space-5);
  }

  .notice {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .notice.waiting {
    background: var(--z-draining-subtle);
    box-shadow: inset var(--z-nudge-1) 0 0 0 var(--z-draining);
  }
  .notice h3 {
    margin: 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .notice p {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .keys {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-3);
    margin: var(--z-space-2) 0 0;
    padding: 0;
    list-style: none;
  }
  .keys li {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
  }
  .pins {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--z-space-1) var(--z-space-3);
    margin: var(--z-space-3) 0 0;
    padding: 0;
    list-style: none;
  }
  .pins li {
    display: contents;
  }
  .pin-env {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .pin-key {
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }

  /*
    The search and the view switch stay reachable while the list scrolls. On a
    page this long, a filter that has to be scrolled back to is one that gets
    used once.
  */
  .controls {
    position: sticky;
    top: 0;
    z-index: var(--z-layer-sticky);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
  }
  .jump {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
  }
  .jump a {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    padding: var(--z-space-1) var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    text-decoration: none;
  }
  .jump a:hover {
    border-color: var(--z-accent);
    color: var(--z-text);
  }
  .jump-count {
    color: var(--z-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .search {
    flex: 1 1 20rem;
    min-width: 0;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-6);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  /*
    min-width: 0, so a long path truncates instead of widening the page. A flex
    item's floor is its content, so CopyButton's own ellipsis never fires until
    something above it says the item may be narrower than what is inside it --
    and a database under a deep state directory took a phone sideways.
  */
  .meta > div {
    flex: 1 1 16rem;
    min-width: 0;
  }
  .meta-label {
    display: block;
    font-size: var(--z-text-2xs);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-muted);
    margin-bottom: var(--z-space-1);
  }
  .meta-value {
    font-size: var(--z-text-sm);
    color: var(--z-text-subtle);
  }

  .general {
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
  }
  .general h3 {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .general ul {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .general li {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
  }
  .finding-title {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .finding-detail {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }

  .empty {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    padding: var(--z-space-6) var(--z-space-5);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }

  .section-head {
    padding: var(--z-space-4) var(--z-space-5) var(--z-space-2);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
    /* Cleared by the sticky controls above, so a jump lands on the heading
       rather than just under it. */
    scroll-margin-top: var(--z-space-10);
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .section-head p {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  section:last-child :global(.row:last-child) {
    border-bottom: 0;
  }

  /*
    A real fieldset, because that is what this is: a group of controls with one
    explanation that applies to all of them. It is quiet and it is last -- the
    settings nobody can change from here should not be the first thing between
    an operator and the ones they can.
  */
  .held {
    margin: var(--z-space-5);
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-pending-subtle);
    min-width: 0;
  }
  .held legend {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .held-note {
    margin: 0 0 var(--z-space-3);
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .held-note code {
    font-size: var(--z-text-2xs);
  }
  .held-rows {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .held-rows li {
    display: grid;
    grid-template-columns: minmax(0, auto) minmax(0, 1fr);
    gap: 0 var(--z-space-3);
    align-items: baseline;
    padding-top: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-pending-border);
  }
  .held-rows li:first-child {
    padding-top: 0;
    border-top: 0;
  }
  .held-env {
    grid-column: 1;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .held-value {
    grid-column: 2;
    font-size: var(--z-text-xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .held-label {
    grid-column: 1;
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }
  .held-key {
    grid-column: 2;
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    overflow-wrap: anywhere;
  }
  @media (max-width: 768px) {
    .held-rows li {
      grid-template-columns: minmax(0, 1fr);
    }
    .held-env,
    .held-value,
    .held-label,
    .held-key {
      grid-column: 1;
    }
  }
</style>
