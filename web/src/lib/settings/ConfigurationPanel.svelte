<!--
  The configuration.

  Everything here lives in this fleet's database, so a change made here is kept
  rather than going back to a file at the next restart. Three keys are the
  exception and say so in the row: the path to the database and the two ways of
  naming the key that unseals what is in it cannot live inside the thing they
  open.

  A hundred settings is too many to read, so the page is built to be searched,
  filtered and scanned: the search is the first thing under the heading, the
  validator's remarks are one quiet line that opens on request, and every
  section is a card with a human name and a count. What an operator usually
  wants is one setting, the ones somebody has changed, or the ones the
  controller is complaining about; each is one action away, and none of them
  makes the rows move down the page to make room.

  Secrets are absent rather than starred out: the API does not send them at all.
-->
<script lang="ts">
  import { supportHint } from '$lib/errors';
  import {
    ChevronDown,
    FileDown,
    FileUp,
    Lock,
    RotateCcw,
    Search,
    TriangleAlert,
  } from '@lucide/svelte';
  import { getSettings, settingsExportUrl, updateSettings, ApiError } from '$lib/api/client';
  import { registerSearch } from '$lib/keys';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import type { Problem, Setting, Settings } from '$lib/api/types';
  import { severityStatus } from '$lib/status';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import DropdownMenu from '$lib/components/DropdownMenu.svelte';
  import ImportSettingsDialog from './ImportSettingsDialog.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import Input from '$lib/components/Input.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import SettingRow from './SettingRow.svelte';
  import {
    SECTION_BLURB,
    SECTIONS_ELSEWHERE,
    displayValue,
    matches,
    sectionTitle,
  } from './settings';

  let settings = $state<Settings | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let reload = $state(0);

  $effect(() => {
    void reload;
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

  /* -- moving a configuration ----------------------------------------------- */

  /*
    An export is a navigation rather than a fetch: the point is the browser's
    own download, and the cookie goes with it. YAML is the shape zoomies.yaml
    takes, so the file can be started from; JSON wraps the same tree with when
    it was taken and where from.
  */
  const exportItems = [
    {
      id: 'yaml',
      label: 'As zoomies.yaml',
      icon: FileDown,
      onSelect: () => window.location.assign(settingsExportUrl('yaml')),
    },
    {
      id: 'json',
      label: 'As JSON, with provenance',
      icon: FileDown,
      onSelect: () => window.location.assign(settingsExportUrl('json')),
    },
  ];
  let importOpen = $state(false);

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
  // Plain, not $state: the effect must not track it. The effect reads
  // `settings`, which is replaced after every save, refresh and import, and the
  // address keeps its `?setting=` for the whole visit -- so without this the
  // filters were wiped and the page scrolled back to the linked row each time
  // somebody saved a different one.
  let handled = '';

  $effect(() => {
    const key = wanted;
    if (!key) {
      handled = '';
      return;
    }
    if (!settings || key === handled) return;
    handled = key;
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
   * `/` puts the cursor in the search box. The shortcut, the guard against
   * swallowing a keystroke meant for a field, and the line in the shortcuts
   * sheet all already exist in keys.ts -- a page only has to say which of its
   * inputs is the search one, as Jobs, Runners and the log viewer do.
   */
  $effect(() => registerSearch(searchField));

  /*
    Every setting this page owns. A section another page edits is left to it --
    the backup schedule belongs beside the backups it produces -- and the rail
    links there, so the keys are still findable from here without this page
    offering a second editor for them.
  */
  /*
    A controller that runs no agent has nothing for the rest of the agent
    section to describe: its backend, socket and capacity are the joined
    hosts' business, set on those hosts. Listing them here invites an edit that
    changes nothing. The switch that would bring an agent back stays, so the
    choice is still reachable from this page.
  */
  const noAgent = $derived((settings?.findings ?? []).some((f) => f.code === 'agent.none'));
  const all = $derived<readonly Setting[]>(
    (settings?.settings ?? []).filter(
      (s) =>
        !SECTIONS_ELSEWHERE[s.section] &&
        !(noAgent && s.key.startsWith('agent.') && s.key !== 'agent.embedded'),
    ),
  );
  /*
    The pointer is shown while a search is running too, and that is the point
    of it: somebody who types "backup interval" into this page's search box
    finds no rows, and the one thing that would help is being told where the
    rows went. It is hidden only when the query plainly means something else.
  */
  const elsewhere = $derived(
    Object.entries(SECTIONS_ELSEWHERE).filter(
      ([name]) =>
        (settings?.settings ?? []).some((s) => s.section === name) &&
        (query.trim() === '' ||
          (settings?.settings ?? []).some((s) => s.section === name && matches(s, query))),
    ),
  );

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

  /*
    The API already orders the list, so grouping preserves that order. The card
    heading carries what the old flat list made the reader work out: the
    section's human name, how many settings are in it, and whether anything in
    it is asking for attention.
  */
  type NoteSeverity = 'error' | 'warning' | 'info';

  function worstSeverity(problems: readonly Problem[] | undefined): NoteSeverity | null {
    let worst: NoteSeverity | null = null;
    for (const problem of problems ?? []) {
      const severity: NoteSeverity =
        problem.severity === 'error'
          ? 'error'
          : problem.severity === 'warning'
            ? 'warning'
            : 'info';
      if (worst === null || severity === 'error' || (severity === 'warning' && worst === 'info')) {
        worst = severity;
      }
    }
    return worst;
  }

  const sections = $derived.by(() => {
    const out: {
      name: string;
      title: string;
      rows: Setting[];
      notes: number;
      worst: NoteSeverity | null;
      pending: number;
    }[] = [];
    for (const setting of changeable) {
      const name = setting.section ?? 'other';
      let last = out[out.length - 1];
      if (last?.name !== name) {
        last = { name, title: sectionTitle(name), rows: [], notes: 0, worst: null, pending: 0 };
        out.push(last);
      }
      last.rows.push(setting);
      const findings = findingsBySetting[setting.key] ?? [];
      last.notes += findings.length;
      const severity = worstSeverity(findings);
      if (
        severity === 'error' ||
        (severity === 'warning' && last.worst !== 'error') ||
        (severity === 'info' && last.worst === null)
      ) {
        last.worst = severity;
      }
      if (setting.pending) last.pending += 1;
    }
    return out;
  });

  /** Findings the validator did not tie to a setting this page renders. */
  const generalFindings = $derived(
    (settings?.findings ?? []).filter(
      (finding) => !finding.setting || !all.some((s) => s.key === finding.setting),
    ),
  );

  /*
    The validator's general remarks are usually information, and information
    does not belong between an operator and the search box. They are one line
    until asked for; a warning or an error opens the line by itself, because
    that is the one case where reading it may change what the operator does.
  */
  const noteCounts = $derived.by(() => {
    const counts = { error: 0, warning: 0, info: 0 };
    for (const finding of generalFindings) {
      if (finding.severity === 'error') counts.error += 1;
      else if (finding.severity === 'warning') counts.warning += 1;
      else counts.info += 1;
    }
    return counts;
  });
  const notesNeedAttention = $derived(noteCounts.error + noteCounts.warning > 0);
  const notesLabel = $derived.by(() => {
    const parts: string[] = [];
    if (noteCounts.error) parts.push(plural(noteCounts.error, 'error'));
    if (noteCounts.warning) parts.push(plural(noteCounts.warning, 'warning'));
    if (noteCounts.info) parts.push(plural(noteCounts.info, 'note'));
    return notesNeedAttention ? parts.join(' · ') : `${parts.join(' · ')}, all informational`;
  });
  const notesWorst = $derived<NoteSeverity>(
    noteCounts.error > 0 ? 'error' : noteCounts.warning > 0 ? 'warning' : 'info',
  );
  let notesOpen = $state(false);
  let notesChosen = $state(false);
  $effect(() => {
    if (!notesChosen) notesOpen = notesNeedAttention;
  });
  function toggleNotes(): void {
    notesChosen = true;
    notesOpen = !notesOpen;
  }

  const pending = $derived(settings?.pending_restart ?? []);

  function plural(count: number, one: string): string {
    return `${count} ${one}${count === 1 ? '' : 's'}`;
  }

  /* -- the index ------------------------------------------------------------ */

  let rows = $state<HTMLDivElement | null>(null);
  let controls = $state<HTMLDivElement | null>(null);
  let activeSection = $state('');

  /*
    Which section the reader is in, so the index can say so. The last heading
    that has passed under the toolbar wins. A scroll listener rather than an
    intersection observer, because the answer depends on where each heading is
    relative to one line and not on how much of a section is on screen -- and
    a section of one row is on screen entirely while the reader is still three
    sections above it.
  */
  $effect(() => {
    const root = rows;
    const bar = controls;
    if (!root || !bar || sections.length < 2) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const line = bar.getBoundingClientRect().bottom;
      let current = '';
      for (const head of root.querySelectorAll<HTMLElement>('[data-section]')) {
        if (current === '' || head.getBoundingClientRect().top <= line) {
          current = head.dataset.section ?? '';
        } else {
          break;
        }
      }
      activeSection = current;
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    measure();
    addEventListener('scroll', onScroll, { passive: true });
    return () => {
      removeEventListener('scroll', onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  });

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
      return `That change could not be made. ${supportHint()}`;
    }
  }
</script>

<PageHeader
  title="Configuration"
  subtitle="What Zoomies runs with, and where each value came from. Changes are kept in the database; the environment has the last word."
  onrefresh={() => {
    reload += 1;
  }}
>
  <DropdownMenu
    items={exportItems}
    label="Export the settings"
    triggerLabel="Export"
    triggerIcon={FileDown}
  />
  <Button variant="secondary" icon={FileUp} onclick={() => (importOpen = true)}>Import</Button>
</PageHeader>

<ImportSettingsDialog
  bind:open={importOpen}
  onapplied={(result) => {
    settings = result;
    void session.reloadMeta();
  }}
/>

<LoadingBoundary {loading} {error} onretry={() => (reload += 1)}>
  {#snippet skeleton()}
    <div class="panel pad"><Skeleton lines={8} /></div>
  {/snippet}

  {#if settings}
    <!--
      The search and the view switch stay reachable while the list scrolls.
      On a page this long, a filter that has to be scrolled back to is one
      that gets used once. They come before every notice for the same reason:
      finding one setting is the common visit, and the validator's remarks are
      not what it is for.
    -->
    <div class="controls" bind:this={controls}>
      <div class="search">
        <Input
          bind:value={query}
          bind:element={searchField}
          size="sm"
          icon={Search}
          placeholder="Search settings, descriptions and ZOOMIES_* names  (press /)"
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

    {#if pending.length > 0}
      <section class="notice waiting" aria-labelledby="pending-restart">
        <Badge tone="draining" label="Saved" size="sm" dot={false} />
        <div>
          <h2 id="pending-restart">
            {pending.length === 1
              ? 'One setting is waiting for a restart'
              : `${pending.length} settings are waiting for a restart`}
          </h2>
          <p>
            Stored, and in force the next time Zoomies starts. It cannot apply them to itself,
            rebinding a listener or rebuilding the container backends under running jobs is how a
            reload becomes an outage.
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
      <section class="notes" class:attention={notesNeedAttention} data-tone={notesWorst}>
        <button
          type="button"
          class="notes-head"
          aria-expanded={notesOpen}
          aria-controls="setup-notes-body"
          onclick={toggleNotes}
        >
          <span class="notes-icon"><TriangleAlert size={15} aria-hidden="true" /></span>
          <span class="notes-heading">Setup notes</span>
          <span class="notes-label">{notesLabel}</span>
          {#if !notesOpen && generalFindings[0]}
            <span class="notes-peek">{generalFindings[0].title}</span>
          {/if}
          <ChevronDown size={15} class="chev{notesOpen ? ' open' : ''}" aria-hidden="true" />
        </button>
        <div id="setup-notes-body" class="notes-body" hidden={!notesOpen}>
          <p class="notes-lede">
            What the startup validator says about this configuration. Notes are worth knowing; a
            warning or an error is worth acting on.
          </p>
          <ul class="notes-list">
            {#each generalFindings as finding (finding.code)}
              {@const meta = severityStatus(finding.severity)}
              <li>
                <Badge status={meta} size="sm" />
                <div class="note-copy">
                  <p class="note-title">{finding.title}</p>
                  {#if finding.detail}<p class="note-detail">{finding.detail}</p>{/if}
                  {#if finding.fix}
                    <p class="note-detail"><strong>To fix:</strong> {finding.fix}</p>
                  {/if}
                  <!--
                    Which layer set it, when that is not where the fix points.
                    Told only to "change server.bind", an operator edits the
                    configuration file -- and a value stored here or pinned by
                    a variable is one the file cannot change, so they restart
                    into the same complaint having learned nothing.
                  -->
                  {#if finding.undo}
                    <p class="note-undo">
                      {#if finding.source === 'environment'}
                        The environment is the last word, so neither the file nor this page can
                        change it. With the controller stopped, run
                      {:else}
                        This value is stored in this fleet, so editing the configuration file will
                        not change it. With the controller stopped, run
                      {/if}
                      <code>{finding.undo}</code>
                    </p>
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
        </div>
      </section>
    {/if}

    <div class="body">
      <div class="rows" bind:this={rows}>
        {#if sections.length === 0 && held.length === 0 && elsewhere.length === 0}
          <div class="group empty-group">
            <p class="empty">
              <RotateCcw size={14} aria-hidden="true" />
              Nothing matches
              {#if query}“{query}”{/if}
              {#if view === 'changed'}among the settings that have been changed{/if}
              {#if view === 'attention'}among the settings needing attention{/if}.
            </p>
          </div>
        {/if}

        {#each sections as section (section.name)}
          <section class="group" aria-labelledby="section-{section.name}">
            <header class="section-head" data-section={section.name}>
              <div class="section-titles">
                <h2 id="section-{section.name}">{section.title}</h2>
                {#if SECTION_BLURB[section.name]}<p>{SECTION_BLURB[section.name]}</p>{/if}
              </div>
              <div class="section-meta">
                {#if section.notes > 0}
                  <Badge
                    tone={section.worst === 'error'
                      ? 'danger'
                      : section.worst === 'warning'
                        ? 'pending'
                        : 'neutral'}
                    label={plural(section.notes, 'note')}
                    size="sm"
                    dot={false}
                  />
                {/if}
                {#if section.pending > 0}
                  <Badge
                    tone="draining"
                    label="{plural(section.pending, 'change')} waiting"
                    size="sm"
                    dot={false}
                  />
                {/if}
                <span class="section-count">{plural(section.rows.length, 'setting')}</span>
              </div>
            </header>
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

        {#each elsewhere as [name, where] (name)}
          <section class="group elsewhere" aria-labelledby="section-{name}">
            <header class="section-head" data-section={name}>
              <div class="section-titles">
                <h2 id="section-{name}">{sectionTitle(name)}</h2>
                {#if SECTION_BLURB[name]}<p>{SECTION_BLURB[name]}</p>{/if}
              </div>
            </header>
            <p class="pointer">
              These are set on <a href={where.href}>{where.page}</a>, beside what they affect.
            </p>
          </section>
        {/each}

        {#if held.length > 0}
          <fieldset class="group held">
            <legend>
              <Lock size={13} aria-hidden="true" />
              Held by the environment
            </legend>
            <p class="held-note">
              {held.length === 1 ? 'This setting is' : 'These settings are'} set by a
              <code>ZOOMIES_*</code> variable, and the environment is the last word, it overrides
              both the database and the configuration file. To change
              {held.length === 1 ? 'it' : 'them'}, amend the environment file Zoomies starts with
              and restart it. Removing a variable hands that setting back to this page.
            </p>
            <ul class="held-rows">
              {#each held as setting (setting.key)}
                <li>
                  <span class="held-label">{setting.label}</span>
                  <span class="held-key mono">{setting.key}</span>
                  <span class="held-env mono">{setting.env}</span>
                  <span class="held-value mono">{displayValue(setting)}</span>
                </li>
              {/each}
            </ul>
          </fieldset>
        {/if}
      </div>

      <!--
        The index. A hundred settings under fourteen headings is a page
        nobody scrolls twice, and the search box only helps somebody who
        already knows what the setting is called -- which is the case this
        page is least needed for. The index is what the operator who is
        looking around uses: it says which section they are in, takes them
        to any other, and narrows with the filters so a section the search
        emptied is not offered as somewhere to go.
      -->
      {#if sections.length > 1}
        <nav class="index" aria-label="Jump to a section">
          <p class="index-label" aria-hidden="true">On this page</p>
          <ul>
            {#each sections as section (section.name)}
              {@const here = section.name === activeSection}
              <li>
                <a
                  href="#section-{section.name}"
                  class:current={here}
                  class:has-notes={section.notes > 0}
                  aria-current={here ? 'true' : undefined}
                >
                  <span class="index-name">{section.title}</span>
                  {#if section.notes > 0}
                    <span class="index-note" data-tone={section.worst}>
                      <TriangleAlert
                        size={11}
                        aria-hidden="true"
                        title={plural(section.notes, 'note')}
                      />
                    </span>
                  {/if}
                  <span class="index-count">{section.rows.length}</span>
                </a>
              </li>
            {/each}
          </ul>
        </nav>
      {/if}
    </div>

    <!--
      Where the layers under this page live. A footnote rather than a block
      between the toolbar and the rows: an operator reads it once, when they
      are wondering which file to edit, and the rows are what the page is for.
    -->
    <div class="paths">
      <span>
        <span class="paths-label">Configuration file</span>
        {#if settings.config_path}
          <CopyButton
            value={settings.config_path}
            label="Copy the configuration file path"
            showValue
          />
        {:else}
          <span class="paths-none">None. Everything comes from the database and the defaults.</span>
        {/if}
      </span>
      <span>
        <span class="paths-label">Database</span>
        <CopyButton value={settings.database_path ?? ''} label="Copy the database path" showValue />
      </span>
    </div>
  {/if}
</LoadingBoundary>

<style>
  .panel {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .pad {
    padding: var(--z-space-5);
  }

  /*
    Under the top bar, which is sticky too: at `top: 0` the toolbar slid up
    behind it and the search box was there but could not be reached.
  */
  .controls {
    position: sticky;
    top: calc(var(--z-topbar-height) + var(--z-space-2));
    z-index: var(--z-layer-sticky);
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    margin-bottom: var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    box-shadow: var(--z-shadow-sm);
  }
  .search {
    flex: 1 1 22rem;
    min-width: 0;
    max-width: 40rem;
  }

  .notice {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    margin-bottom: var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
  }
  .notice.waiting {
    border-color: var(--z-draining-border);
    background: var(--z-draining-subtle);
  }
  .notice h2 {
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

  /*
    The validator's general remarks are a line, not a wall. The line says how
    many there are and what kind; the body is only in the way when somebody
    asks for it.
  */
  .notes {
    margin-bottom: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .notes.attention {
    border-color: var(--z-pending-border);
  }
  .notes[data-tone='error'] {
    border-color: var(--z-danger-border);
  }
  .notes-head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    width: 100%;
    min-height: var(--z-control-touch);
    padding: var(--z-space-2) var(--z-space-4);
    border: 0;
    border-radius: var(--z-radius-md);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .notes-head:hover {
    background: var(--z-surface-hover);
  }
  .notes-head:focus-visible {
    outline: 2px solid var(--z-focus-colour);
    outline-offset: -2px;
  }
  .notes-icon {
    display: inline-flex;
    flex: none;
    color: var(--z-text-muted);
  }
  .notes[data-tone='warning'] .notes-icon {
    color: var(--z-pending);
  }
  .notes[data-tone='error'] .notes-icon {
    color: var(--z-danger);
  }
  .notes-heading {
    flex: none;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .notes-label {
    flex: none;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .notes-peek {
    flex: 1;
    min-width: 0;
    margin-left: var(--z-space-2);
    overflow: hidden;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .notes-head :global(.chev) {
    flex: none;
    margin-left: auto;
    color: var(--z-text-subtle);
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  .notes-head :global(.chev.open) {
    transform: rotate(180deg);
  }
  .notes-body {
    padding: 0 var(--z-space-4) var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .notes-lede {
    margin: var(--z-space-3) 0 var(--z-space-2);
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .notes-list {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .notes-list li {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
  }
  .note-copy {
    min-width: 0;
  }
  .note-title {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
  }
  .note-detail,
  .note-undo {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .note-undo code {
    font-size: var(--z-text-2xs);
    overflow-wrap: anywhere;
  }

  .body {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-4);
  }
  .rows {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }

  /*
    A section is a card, not a band in one endless list: the heading is the
    thing an operator scans for, and a card gives it an edge to stop at.
  */
  .group {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    overflow: hidden;
  }
  .section-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
  }
  .section-titles {
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    /* The index's links, and a browser's own address-bar anchor, jump to this
       heading's id -- not to .section-head that carries it -- so the margin
       clearing the sticky top bar and toolbar has to sit on the heading
       itself. Set on .section-head instead, it did nothing: the jump landed
       with the heading hidden behind both bars and the first row of the
       section in its place. */
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-space-16));
  }
  .section-head p {
    margin: var(--z-space-1) 0 0;
    max-width: 80ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .section-meta {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--z-space-1) var(--z-space-2);
    padding-top: var(--z-space-1);
  }
  .section-count {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .group :global(.row:last-child) {
    border-bottom: 0;
  }

  .elsewhere .pointer {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-4);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }

  .empty-group {
    padding: var(--z-space-3) 0;
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

  .index {
    position: sticky;
    top: calc(var(--z-topbar-height) + var(--z-space-16));
    flex: none;
    width: 12rem;
    max-height: calc(100dvh - (var(--z-topbar-height) + var(--z-space-16)) - var(--z-space-4));
    overflow-y: auto;
    padding: var(--z-space-2) 0 var(--z-space-2) var(--z-space-4);
  }
  .index-label {
    margin: 0 0 var(--z-space-1);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-wide);
    text-transform: uppercase;
    color: var(--z-text-subtle);
  }
  .index ul {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .index a {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2);
    min-height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    border-radius: var(--z-radius-sm);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    text-decoration: none;
  }
  .index a:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  .index a.current {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  .index-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .index-note {
    display: inline-flex;
    flex: none;
  }
  .index-note[data-tone='info'] {
    color: var(--z-text-subtle);
  }
  .index-note[data-tone='warning'] {
    color: var(--z-pending);
  }
  .index-note[data-tone='error'] {
    color: var(--z-danger);
  }
  .index-count {
    flex: none;
    font-family: var(--z-font-sans);
    color: var(--z-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .index a.current .index-count {
    color: inherit;
  }
  .index a.current .index-note {
    color: inherit;
  }

  /*
    A real fieldset, because that is what this is: a group of controls with one
    explanation that applies to all of them. It is quiet and it is last -- the
    settings nobody can change from here should not be the first thing between
    an operator and the ones they can.
  */
  .held {
    margin: 0;
    padding: var(--z-space-4);
    border-color: var(--z-pending-border);
    background: var(--z-pending-subtle);
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
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
    gap: var(--z-space-1) var(--z-space-4);
    align-items: baseline;
    padding-top: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-pending-border);
  }
  .held-rows li:first-child {
    padding-top: 0;
    border-top: 0;
  }
  .held-label {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .held-key {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    overflow-wrap: anywhere;
  }
  .held-env {
    font-size: var(--z-text-xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .held-value {
    font-size: var(--z-text-xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }

  .paths {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-5);
    margin: var(--z-space-4) 0 0;
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    color: var(--z-text-subtle);
  }
  /*
    min-width: 0, so a long path truncates instead of widening the page. A
    flex item's floor is its content, so CopyButton's own ellipsis never fires
    until something above it says the item may be narrower than what is inside
    it -- and a database under a deep state directory took a phone sideways.
  */
  .paths > span {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
    max-width: 100%;
  }
  .paths-label {
    flex: none;
    color: var(--z-text-muted);
  }
  .paths-none {
    color: var(--z-text-subtle);
  }

  /*
    A tablet has no column to spare for the index, so it becomes a row of
    chips above the rows -- the same list, read across -- and the cards take
    the whole width.
  */
  @media (max-width: 1180px) {
    .body {
      flex-direction: column;
      align-items: stretch;
    }
    .rows {
      width: 100%;
    }
    .index {
      order: -1;
      position: static;
      width: auto;
      padding: 0 0 var(--z-space-3);
    }
    .index-label {
      display: none;
    }
    .index ul {
      flex-direction: row;
      flex-wrap: wrap;
      gap: var(--z-space-2);
    }
    .index a {
      border: var(--z-border-width) solid var(--z-border);
      background: var(--z-surface);
    }
    .index a.current {
      border-color: var(--z-accent-border);
    }
  }
  @media (max-width: 768px) {
    .notes-head {
      padding: var(--z-space-2) var(--z-space-3);
    }
    .notes-label {
      flex: 1 1 auto;
      min-width: 0;
      margin-left: 0;
    }
    .notes-peek {
      display: none;
    }
    /*
      The index is sixteen sections, and wrapping them on a phone spent more of
      the screen than the page's own heading. One scrollable row keeps the map
      without pushing the settings below it.
    */
    .index ul {
      flex-wrap: nowrap;
      overflow-x: auto;
      padding-bottom: var(--z-space-1);
      scrollbar-width: none;
    }
    .index ul::-webkit-scrollbar {
      display: none;
    }
    .section-head {
      flex-direction: column;
    }
    .section-meta {
      justify-content: flex-start;
      padding-top: 0;
    }
    .held-rows li {
      grid-template-columns: minmax(0, 1fr);
    }
    .held-key {
      grid-column: 1;
    }
  }
</style>
