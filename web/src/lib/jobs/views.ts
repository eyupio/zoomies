/**
 * The Jobs page's status views.
 *
 * Status is the filter this page is opened for -- "what is running now", "what
 * went wrong" -- and it used to be three clicks down a facet menu. A view is a
 * whole answer rather than one value of one key, because the questions an
 * operator asks are not all the same key: "Failed" is the server's own
 * reckoning of a job that went wrong, which includes a runner that died under
 * one, and no conclusion value says that.
 *
 * Each view therefore writes every status-shaped key, so pressing a chip
 * gives exactly the view it names and never half of the last one. The narrower
 * filters -- repository, workflow, pool, labels, dates -- are untouched, so
 * switching view keeps the operator where they were looking.
 */
import { JOB_STATES, type JobState } from '../api/types';

export type JobView =
  'attention' | 'running' | 'queued' | 'failed' | 'faulted' | 'finished' | 'all';

/** The keys a view owns outright. Everything else on the page is orthogonal to it. */
export interface JobStatusFilters {
  state: JobState[];
  conclusion: string[];
  failed: boolean;
  /** The half of `failed` this fleet caused, rather than the workflows. */
  faulted: boolean;
  unmatched: boolean;
  /** The composite question "which failures still want a person"; see JOB_VIEWS. */
  attention: boolean;
}

export interface JobViewDef {
  id: JobView;
  label: string;
  /** One line an operator can act on, for the tooltip. */
  hint: string;
  filters: JobStatusFilters;
}

const NOTHING: JobStatusFilters = {
  state: [],
  conclusion: [],
  failed: false,
  faulted: false,
  unmatched: false,
  attention: false,
};

/**
 * The order starts with the question that has a person waiting (which
 * failures nobody has dealt with), and then follows the lifecycle an operator
 * reads left to right: running, then waiting, then what went wrong, then what
 * is done, with the way out on the end. "All" is last because it is the widest,
 * not because it is the default.
 *
 * "Needs attention" is not one value of one key like the rest: it is the
 * server's composite question, failed and recent and unacknowledged and not
 * replaced by a later attempt, kept in one place so the chip, the Overview's
 * failures panel and the API's own filter cannot disagree about what is still
 * owed.
 */
export const JOB_VIEWS: readonly JobViewDef[] = [
  {
    id: 'attention',
    label: 'Needs attention',
    hint: 'Failures from the last week that nobody has acknowledged and no later attempt has replaced. GitHub knows a job failed; it has no idea which failures anybody has read.',
    filters: { ...NOTHING, attention: true },
  },
  {
    id: 'running',
    label: 'Running',
    hint: 'Jobs a runner is working on right now.',
    filters: { ...NOTHING, state: ['in_progress'] },
  },
  {
    id: 'queued',
    label: 'Queued',
    hint: 'Jobs GitHub has queued and nothing has started yet.',
    filters: { ...NOTHING, state: ['queued'] },
  },
  {
    id: 'failed',
    label: 'Failed',
    hint: 'Failing conclusions, and runners that stopped under a job.',
    filters: { ...NOTHING, failed: true },
  },
  {
    /*
     * The half of "Failed" that is this deployment's doing. It sits beside it
     * rather than inside a facet menu because it is the question somebody
     * arrives with -- "is CI broken, or is our CI broken" -- and because it is
     * the only one of the two that anybody here can fix.
     */
    id: 'faulted',
    label: 'Our failures',
    hint: 'Failures this fleet caused: a runner that never started, or one that stopped under the job. GitHub records these as ordinary failures, so nothing on GitHub tells them apart.',
    filters: { ...NOTHING, faulted: true },
  },
  {
    id: 'finished',
    label: 'Finished',
    hint: 'Every job that has ended, however it ended.',
    filters: { ...NOTHING, state: ['completed'] },
  },
  {
    id: 'all',
    /*
     * Written out in full rather than left absent, because an absent `state`
     * is what the page's default reads as "running". Four repeated keys in the
     * address bar is the price of "all" and "ask me nothing" being different
     * things, and it is the shape Queue's provisioning statuses already use.
     */
    label: 'All',
    hint: 'Every job in this view, whatever its status.',
    filters: { ...NOTHING, state: [...JOB_STATES] },
  },
];

/**
 * What a page nobody has asked anything of shows.
 *
 * Running is the question this page is opened with, so it is what the page
 * answers before being asked. It applies to a bare `/jobs` only: a link that
 * carries a filter has already said what it wants, and narrowing it to running
 * would answer a different question than the one the sender asked.
 */
export const DEFAULT_JOB_STATE: readonly JobState[] = ['in_progress'];

function sameSet(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && [...a].sort().join() === [...b].sort().join();
}

/**
 * A state filter reduced to what it actually narrows. No states and every
 * state narrow nothing, so both are the same view -- which is what lets "All"
 * write itself out in full without becoming a view of its own.
 */
function narrowing(state: readonly string[]): string {
  const set = [...new Set(state)].sort();
  return set.length === 0 || set.length >= JOB_STATES.length ? '' : set.join(',');
}

/**
 * Which view the filters currently in force are, or `''` when they are
 * something no chip says: a conclusion facet, the unmatched switch, two
 * states at once. No chip is pressed then, which is the truth:
 * the chips above the grid are what is narrowing it.
 */
export function currentJobView(value: JobStatusFilters): JobView | '' {
  for (const view of JOB_VIEWS) {
    if (
      narrowing(value.state) === narrowing(view.filters.state) &&
      sameSet(value.conclusion, view.filters.conclusion) &&
      value.failed === view.filters.failed &&
      value.faulted === view.filters.faulted &&
      value.unmatched === view.filters.unmatched &&
      value.attention === view.filters.attention
    ) {
      return view.id;
    }
  }
  return '';
}
