<script module lang="ts">
  import type { JobState, ProvisioningStatus } from '$lib/api/types';

  /** Everything the Jobs page filters by. It lives in the URL, so a view is shareable. */
  export interface JobFilterState {
    q: string;
    repo: string[];
    workflow: string[];
    pool_id: string[];
    /** The hosts the jobs ran on, as stamped when a runner took them: a queued job has none. */
    host_id: string[];
    label: string[];
    conclusion: string[];
    state: JobState[];
    /**
     * Which provisioning statuses to show, for the queued work an operator has
     * paused or removed. Empty narrows nothing, which is what every view but
     * Queued wants: a job removed from the queue belongs in the history the
     * page is, badged as removed, and only the view that calls itself Queued
     * has to leave it out.
     */
    provisioning: ProvisioningStatus[];
    /**
     * Calendar dates, `YYYY-MM-DD`, or with a time of day, `YYYY-MM-DDTHH:MM` on the
     * operator's clock, which is how a link from a figure that counts a moving window
     * (the last 7 days, to the minute) asks for the same jobs. Converted to instants
     * when the API is called.
     */
    since: string;
    until: string;
    /**
     * The same two bounds over when a job finished, not when it was queued, in the same
     * forms. The activity squares count a job in the hour it finished, so the list behind
     * one asks the same question. There is no input for them: they are set by pressing a
     * square or by a link, and drawn as chips so a shorter list says why.
     */
    completed_since: string;
    completed_until: string;
    unmatched: boolean;
    /**
     * `'false'` leaves out the jobs that ran on somebody else's hosted runners, GitHub's
     * own or a vendor's, and nothing else: the scope the Overview's figures count in, so
     * a link from one lists the jobs it counted. A string so the address reads
     * `hosted=false`, which is what the API calls it.
     */
    hosted: '' | 'false';
    /**
     * Only jobs that went wrong, on either side: a failing conclusion, or a
     * runner of this fleet that stopped under the job.
     */
    failed: boolean;
    /**
     * The half of `failed` this fleet caused: a runner that never started, or
     * one that stopped under the job. GitHub records these as ordinary
     * failures, so nothing on GitHub tells the two apart.
     */
    faulted: boolean;
    /**
     * The composite triage question: failures from the last week that nobody
     * has acknowledged and that no later attempt has replaced. It is the
     * server's own filter rather than a combination of the keys around it, so
     * the button, the Overview's failures panel and the API cannot disagree
     * about what is still owed.
     */
    attention: boolean;
    /**
     * Show every job GitHub reported, not only the ones this fleet has a hand in.
     * Inverted on purpose: the default view is Zoomies' own jobs, so an absent
     * URL key means the default rather than "show everything".
     */
    all: boolean;
  }

  export const EMPTY_JOB_FILTERS: JobFilterState = {
    q: '',
    repo: [],
    workflow: [],
    pool_id: [],
    host_id: [],
    label: [],
    conclusion: [],
    state: [],
    provisioning: [],
    since: '',
    until: '',
    completed_since: '',
    completed_until: '',
    unmatched: false,
    hosted: '',
    failed: false,
    faulted: false,
    attention: false,
    all: false,
  };
</script>

<!--
  The Jobs page's filters.

  The menus are populated from GET /jobs/facets, which reports what the database
  actually holds rather than what happens to be on this page -- so the
  repository list offers every repository that has ever run a job here, not the
  fifty most recent.

  Nothing is stored here: the page owns the state and puts it in the URL.
-->
<script lang="ts">
  import { Search } from '@lucide/svelte';
  import { registerSearch } from '$lib/keys';
  import { JOB_STATES, PROVISIONING_STATUSES } from '$lib/api/types';
  import type { Host, Pool } from '$lib/api/types';
  import { jobStatus, QUEUE_STATUS_LABELS } from '$lib/status';
  import FilterBar from '$lib/components/FilterBar.svelte';
  import type { FilterChip } from '$lib/components/FilterBar.svelte';
  import Input from '$lib/components/Input.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import DateRange from './DateRange.svelte';
  import FacetMenu from './FacetMenu.svelte';
  import type { FacetOption } from './FacetMenu.svelte';

  interface Props {
    queue?: boolean;
    compact?: boolean;
    /**
     * Whether the status filters in force are worth a chip.
     *
     * The Jobs page's row of status buttons says which view is in force, and a
     * chip repeating it would be a second, differently-shaped control for the
     * same thing -- one of which cannot be removed, since clearing the status
     * is what the default puts back. The page passes `false` whenever a button
     * is pressed, and `true` for a status no button says.
     */
    statusChips?: boolean;
    value: JobFilterState;
    /** Distinct values from GET /jobs/facets. */
    facets: { repos?: string[]; workflows?: string[]; conclusions?: string[] };
    pools: readonly Pool[];
    /** The fleet's hosts, to offer by name. Omitted where a job cannot have run on one yet. */
    hosts?: readonly Host[];
    /** Labels worth offering: those the pools answer to, plus those on this page. */
    labelOptions: readonly string[];
    onchange: (patch: Partial<JobFilterState>) => void;
    onclear: () => void;
  }

  let {
    queue = false,
    compact = false,
    statusChips = true,
    value,
    facets,
    pools,
    hosts = [],
    labelOptions,
    onchange,
    onclear,
  }: Props = $props();

  let expanded = $state(false);
  let searchElement = $state<HTMLInputElement | null>(null);

  // `/` focuses this page's search rather than opening the palette.
  $effect(() => registerSearch(searchElement));

  const repoOptions = $derived<FacetOption[]>(
    (facets.repos ?? []).map((r) => ({ value: r, label: r })),
  );
  const workflowOptions = $derived<FacetOption[]>(
    (facets.workflows ?? []).map((w) => ({ value: w, label: w })),
  );
  const poolOptions = $derived<FacetOption[]>(
    pools.map((p) => ({
      value: p.id ?? '',
      label: p.name ?? p.id ?? '',
      hint: (p.labels ?? []).join(', '),
    })),
  );
  const hostOptions = $derived<FacetOption[]>(
    hosts.map((h) => ({ value: h.id ?? '', label: h.name ?? h.id ?? '' })),
  );
  const labelChoices = $derived<FacetOption[]>(labelOptions.map((l) => ({ value: l, label: l })));
  const conclusionOptions = $derived<FacetOption[]>(
    (facets.conclusions ?? []).map((c) => ({
      value: c,
      label: jobStatus('completed', c).label,
    })),
  );
  const stateOptions = $derived<FacetOption[]>(
    JOB_STATES.map((s) => ({ value: s, label: jobStatus(s).label, hint: jobStatus(s).hint })),
  );

  // A bound with a time of day needs the control that can show it: a date input handed
  // `2026-10-02T05:12` shows nothing, and the chip beside it would say a filter was in
  // force that the field does not.
  const withTime = $derived(value.since.includes('T') || value.until.includes('T'));
  const moment = (v: string) => v.replace('T', ' ');

  const poolName = $derived(new Map(pools.map((p) => [p.id ?? '', p.name ?? p.id ?? ''])));
  // A host the fleet no longer has still filters, so its chip says the id rather than nothing.
  const hostName = $derived(new Map(hosts.map((h) => [h.id ?? '', h.name ?? h.id ?? ''])));

  function listChip(
    key: 'repo' | 'workflow' | 'pool_id' | 'host_id' | 'label' | 'conclusion' | 'state',
    label: string,
    display: (v: string) => string = (v) => v,
  ): FilterChip[] {
    return value[key].map((v) => ({
      id: `${key}:${v}`,
      label,
      value: display(v),
      onremove: () =>
        onchange({ [key]: value[key].filter((x) => x !== v) } as Partial<JobFilterState>),
    }));
  }

  const chips = $derived<FilterChip[]>([
    ...(value.q
      ? [{ id: 'q', label: 'Text', value: value.q, onremove: () => onchange({ q: '' }) }]
      : []),
    ...listChip('repo', 'Repository'),
    ...listChip('workflow', 'Workflow'),
    ...listChip('pool_id', 'Pool', (v) => poolName.get(v) ?? v),
    ...listChip('host_id', 'Host', (v) => hostName.get(v) ?? v),
    ...listChip('label', 'Label'),
    ...listChip('conclusion', 'Outcome', (v) => jobStatus('completed', v).label),
    ...(statusChips ? listChip('state', 'State', (v) => jobStatus(v as JobState).label) : []),
    /*
     * One chip for the whole filter rather than one per status, and only while
     * it actually narrows something.
     *
     * The Queued view sets this for itself, to leave out the work an operator
     * removed from the queue, and a filter a page applies on its own behalf is
     * exactly the kind that has to be visible: without the chip, a Queued list
     * shorter than the queue somebody remembers is a page that has quietly
     * dropped rows. Removing it writes every status out in full, the way the
     * Jobs page's All button writes out every state, so that "widen this" and
     * "whatever the view defaults to" stay distinguishable -- otherwise the
     * chip would come straight back and its cross would do nothing.
     */
    ...(value.provisioning.length > 0 && value.provisioning.length < PROVISIONING_STATUSES.length
      ? [
          {
            id: 'provisioning',
            label: 'Queue status',
            value: value.provisioning.map((v) => QUEUE_STATUS_LABELS[v]).join(', '),
            onremove: () => onchange({ provisioning: [...PROVISIONING_STATUSES] }),
          },
        ]
      : []),
    ...(value.since
      ? [
          {
            id: 'since',
            label: 'From',
            value: moment(value.since),
            onremove: () => onchange({ since: '' }),
          },
        ]
      : []),
    ...(value.until
      ? [
          {
            id: 'until',
            label: 'To',
            value: moment(value.until),
            onremove: () => onchange({ until: '' }),
          },
        ]
      : []),
    ...(value.completed_since
      ? [
          {
            id: 'completed_since',
            label: 'Finished from',
            value: moment(value.completed_since),
            onremove: () => onchange({ completed_since: '' }),
          },
        ]
      : []),
    ...(value.completed_until
      ? [
          {
            id: 'completed_until',
            label: 'Finished to',
            value: moment(value.completed_until),
            onremove: () => onchange({ completed_until: '' }),
          },
        ]
      : []),
    ...(value.unmatched
      ? [
          {
            id: 'unmatched',
            label: 'Only',
            value: 'unmatched jobs',
            onremove: () => onchange({ unmatched: false }),
          },
        ]
      : []),
    ...(value.hosted === 'false'
      ? [
          {
            id: 'hosted',
            label: 'Leaving out',
            value: 'jobs on hosted runners',
            onremove: () => onchange({ hosted: '' }),
          },
        ]
      : []),
    ...(value.failed && statusChips
      ? [
          {
            id: 'failed',
            label: 'Only',
            value: 'jobs that went wrong',
            onremove: () => onchange({ failed: false }),
          },
        ]
      : []),
    ...(value.all
      ? [
          {
            id: 'all',
            label: 'Showing',
            value: 'jobs from every runner',
            onremove: () => onchange({ all: false }),
          },
        ]
      : []),
  ]);
</script>

<!--
  The search is the one control a phone keeps in view. Everywhere else it is the
  first item of the controls, on the same row as the menus, so the whole filter
  set reads as one bar. The disclosure is open on a wide screen and collapsible
  on a phone, which is the only difference between the two.
-->
{#snippet search()}
  <div class="search">
    <Input
      bind:element={searchElement}
      value={value.q}
      type="search"
      size="sm"
      icon={Search}
      placeholder="Search repository, workflow or job name"
      ariaLabel="Search jobs"
      oninput={(event) => onchange({ q: (event.currentTarget as HTMLInputElement).value })}
    />
  </div>
{/snippet}

<FilterBar {chips} onclear={chips.length > 0 ? onclear : undefined}>
  {#if compact}
    {@render search()}
  {/if}

  <details
    class="advanced"
    class:compact
    open={!compact || expanded}
    ontoggle={(event) => {
      if (compact) expanded = event.currentTarget.open;
    }}
  >
    <summary hidden={!compact}>Filters ({chips.filter((chip) => chip.id !== 'q').length})</summary>
    <div class="advanced-controls">
      {#if !compact}
        {@render search()}
      {/if}
      {@render controls()}
    </div>
  </details>
</FilterBar>

<!--
  The menus, then the scope: which runs or jobs, and when, and which of the
  three yes-or-no narrowings apply. Everything in the first group is the same
  small control, so the row reads as one set of choices; the second group sits
  on its own line because it answers a different question (when, and whether to
  widen), and a line break says so better than a gap would.
-->
{#snippet controls()}
  <FacetMenu
    label="Repository"
    options={repoOptions}
    selected={value.repo}
    emptyHint="No repository has run a job here yet."
    onchange={(next) => onchange({ repo: next })}
  />
  <FacetMenu
    label="Workflow"
    options={workflowOptions}
    selected={value.workflow}
    emptyHint="No workflow has run a job here yet."
    onchange={(next) => onchange({ workflow: next })}
  />
  <FacetMenu
    label="Pool"
    options={poolOptions}
    selected={value.pool_id}
    emptyHint="There are no pools yet."
    onchange={(next) => onchange({ pool_id: next })}
  />
  {#if !queue}
    <FacetMenu
      label="Host"
      options={hostOptions}
      selected={value.host_id}
      emptyHint="There are no hosts yet."
      onchange={(next) => onchange({ host_id: next })}
    />
  {/if}
  <FacetMenu
    label="Label"
    options={labelChoices}
    selected={value.label}
    emptyHint="Labels appear once a pool defines them or a job asks for them."
    onchange={(next) => onchange({ label: next })}
  />
  {#if !queue}
    <FacetMenu
      label="Outcome"
      options={conclusionOptions}
      selected={value.conclusion}
      emptyHint="No job has finished here yet."
      onchange={(next) => onchange({ conclusion: next })}
    />
    <FacetMenu
      label="State"
      options={stateOptions}
      selected={value.state}
      onchange={(next) => onchange({ state: next as JobState[] })}
    />
  {/if}

  <div class="scope">
    <DateRange
      since={value.since}
      until={value.until}
      {withTime}
      label="Queued between"
      onchange={(next) => onchange(next)}
    />

    <!-- The description is the switch's help, on hover and focus, and is still
         linked to it for assistive technology: it lives in the tooltip, not
         under the label, so a row of switches stays a row. -->
    <Tooltip bare text="Queued jobs no enabled pool claims">
      <Switch
        size="sm"
        label="Unmatched only"
        checked={value.unmatched}
        onchange={(on) => onchange({ unmatched: on })}
      />
    </Tooltip>

    {#if !queue}
      <Tooltip bare text="Failing conclusions, and runners that stopped under a job">
        <Switch
          size="sm"
          label="Failed only"
          checked={value.failed}
          onchange={(on) => onchange({ failed: on })}
        />
      </Tooltip>

      <Tooltip bare text="Also show jobs GitHub ran without this fleet">
        <Switch
          size="sm"
          label="Include other runners"
          checked={value.all}
          onchange={(on) => onchange({ all: on })}
        />
      </Tooltip>
    {/if}
  </div>
{/snippet}

<style>
  /*
    The disclosure is a plain full-width block, and the controls inside it are
    one wrapping row. They used to be laid out with display: contents, which
    browsers do not reliably pass through a details element: on a wide page the
    menus stacked into one column beside the search.
  */
  .advanced {
    display: block;
    width: 100%;
  }
  .advanced-controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .compact[open] .advanced-controls {
    margin-top: var(--z-space-3);
  }
  .search {
    flex: 1 1 240px;
    min-width: 200px;
    max-width: 360px;
  }
  /* On a tablet or phone the search has a line to itself, so its placeholder is
     readable and the menus below it wrap in whole rows, as the Usage page's
     controls do. */
  @media (max-width: 768px) {
    /*
      One row on a phone: the search takes what is left and the menus are behind
      "Filters (n)" beside it, rather than the search taking a filled row of its
      own and the summary the next one. The summary before the search reads
      better still, but moving it in the DOM would put a control that is second
      in reading order first for the keyboard, so the order stays and the row is
      shared instead.
    */
    .search {
      flex: 1 1 10rem;
      max-width: none;
    }
    .advanced.compact:not([open]) {
      flex: 1 1 auto;
    }
    .advanced.compact[open] {
      flex: 1 1 100%;
    }
  }
  /* The second line: when, and the narrowings that are not menus. It takes the
     whole row, so it always starts on its own line under the menus. */
  .scope {
    display: flex;
    flex: 1 1 100%;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }

  /* Phones collapse the menus behind "Filters (n)"; the search stays above it. */
  summary[hidden] {
    display: none;
  }
  summary {
    display: inline-flex;
    align-items: center;
    height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    cursor: pointer;
    color: var(--z-accent);
    font-size: var(--z-text-sm);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }

  @media (pointer: coarse) {
    summary {
      min-height: var(--z-control-touch);
    }
  }
</style>
