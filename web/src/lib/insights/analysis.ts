/**
 * The pure half of the analysis card: how a confidence reads, how a next step
 * becomes a line with a setting and a link, which period a page asks about,
 * and what Eli is handed. Kept out of the component so a unit test can hold it
 * without rendering anything, as the job drawer's why.ts is.
 *
 * The card reasons about nothing. The summary, the confidence and the steps
 * are the controller's, and the same words the CLI, the MCP tools and Eli
 * read, so the page cannot disagree with them; what is here is wording for
 * the evidence's enumerations and the plumbing around the one request.
 */
import type { EliContext } from '../assistant/prompts';
import { sentence, supportHint } from '../errors';
import { capitalise, formatDuration, formatPercent, joinWords } from '../format';
import { readableJobText } from '../jobs/name';

/** How far to lean on a finding, as words: the level, and what was thin. */
export function confidenceWords(c: string, reason?: string | null): string {
  const level = c ? capitalise(c) : 'Unknown';
  const why = reason?.trim();
  return why ? `${level} confidence: ${why}` : `${level} confidence`;
}

export interface StepLine {
  text: string;
  /** The setting to change, named as the settings and the API name it. */
  setting: string | null;
  /** The code's catalog page, to read on in a new tab. */
  href: string | null;
}

/**
 * One next step as the card draws it. The link is the one the controller put
 * on the step (F8: `catalogLink` reads a link a step already carries and
 * cannot make one from a code), so a code the catalog has no page for is shown
 * as the code alone rather than as an address the card made up.
 */
export function stepLine(s: {
  text: string;
  setting?: string;
  code?: string;
  link?: string;
}): StepLine {
  const href = s.code && s.link && /^https?:\/\//.test(s.link) ? s.link : null;
  return { text: s.text, setting: s.setting || null, href };
}

/**
 * The period a page asks about. Its end is always explicit, so the finding on
 * screen is the one anyone asking with the same address is given; the route's
 * own default would have moved on by the time they asked.
 *
 * A page with no window of its own leaves `since` out, so the route takes its
 * own default before that end: a day, or `limits.job_stats_window` where that
 * is shorter. Asking for a day by name would be refused on a fleet that set the
 * limit lower, where the CLI and the tools, which also leave it out, answer.
 *
 * A window that runs past now ends now: the rest has not happened, and a job
 * still waiting would have its wait counted into it. The controller clamps the
 * same way for every caller; doing it here too keeps the period the card
 * describes the one it was given. A window that does not end after it starts
 * asks nothing, and `whyNoWindow` says why where the answer would be.
 */
export function analysisWindow(
  since: string | undefined,
  until: string | undefined,
  now: number,
): { since?: string; until: string } | null {
  const end = Math.min(until ? Date.parse(until) : now, now);
  if (Number.isNaN(end)) return null;
  if (!since) return { until: new Date(end).toISOString() };
  const start = Date.parse(since);
  if (Number.isNaN(start) || start >= end) return null;
  return { since: new Date(start).toISOString(), until: new Date(end).toISOString() };
}

/**
 * Why `analysisWindow` asked nothing, as the card says it in place of a
 * finding: a titled frame with nothing under it says nothing at all. The date
 * picker points out a range that runs backwards, in an alert of its own, and
 * nothing points out one that has not started, where the list is merely empty,
 * so the card names whichever it is. The future one repeats the route's own
 * refusal of a start that is not in the past (ErrSinceNotPast), which is why
 * the page does not ask.
 *
 * Only for a window `analysisWindow` gave null, with the same two bounds; any
 * other has an answer. A bound left out is undefined, as there; one that is
 * there and empty, or not a date, is one the page could not read (Usage's
 * address parses to empty bounds, and does not reach `analysisWindow`).
 */
export function whyNoWindow(since: string | undefined, until: string | undefined): string {
  const start = since === undefined ? Number.NEGATIVE_INFINITY : Date.parse(since);
  // An end left out is now, which a start is after only by not having happened
  // yet, so only an end that was written can make a range run backwards.
  const end = until === undefined ? undefined : Date.parse(until);
  if (Number.isNaN(start) || (end !== undefined && Number.isNaN(end))) {
    return 'The range has a date this page cannot read, so there is no period to analyse.';
  }
  if (end !== undefined && start >= end) {
    return 'The end is before the start, so there is no period to analyse.';
  }
  return 'This period has not started, and an analysis reads only what has already happened; choose a start in the past.';
}

/** The list's filters, as far as the queue-wait card needs to know them. */
export interface QueueListFilters {
  pool_id: readonly string[];
  repo: readonly string[];
  workflow: readonly string[];
  host_id: readonly string[];
  label: readonly string[];
  q: string;
  /** Provisioning statuses the address chose; never the Queued view's default. */
  provisioning?: readonly string[];
  /** Unmatched, when an address keeps the list queued with it on. */
  unmatched?: boolean;
  /** "Include other runners": the list holds jobs another vendor's runners ran. */
  all?: boolean;
}

/**
 * What the Queued view's card covers, and which of the list's filters it does
 * not. The route narrows to one pool or none, and the list beside it can be
 * narrowed by much more; the fleet's answer under a list narrowed to one
 * repository would read as that repository's, and could send an operator to
 * raise the wrong pool's ceiling. So the card says its scope, and names the
 * filters it leaves out rather than borrowing their meaning.
 *
 * The switch for other runners is the odd one out. The list leaves another
 * vendor's jobs out until it is on, and the analysis leaves them out always,
 * so it is named only when on, when the list holds jobs the card does not.
 */
export function queueWaitScope(
  f: QueueListFilters,
  poolName: (id: string) => string,
): { scope: string; unapplied: string | null } {
  const one = f.pool_id.length === 1 ? f.pool_id[0] : undefined;
  const left = [
    f.pool_id.length > 1 ? 'pool' : '',
    f.repo.length ? 'repository' : '',
    f.workflow.length ? 'workflow' : '',
    f.host_id.length ? 'host' : '',
    f.label.length ? 'label' : '',
    f.q.trim() ? 'search' : '',
    f.provisioning?.length ? 'provisioning' : '',
    f.unmatched ? 'unmatched' : '',
  ].filter((w) => w !== '');
  const sentences = [
    left.length
      ? `The list's ${joinWords(left)} ${left.length === 1 ? 'filter is' : 'filters are'} not applied here: this analysis reads every pool, or one pool.`
      : '',
    f.all
      ? "The list's Include other runners switch is not applied here: this analysis leaves out the jobs another vendor's runners ran."
      : '',
  ].filter((s) => s !== '');
  return {
    scope: one ? `Pool ${poolName(one)}` : 'Every pool',
    unapplied: sentences.length ? sentences.join(' ') : null,
  };
}

/**
 * What the Failed view's card covers, and which of the list's filters it does
 * not. The route reads every job this fleet had a hand in over the period and
 * takes no other narrowing, so a list narrowed to one repository sits above an
 * answer about all of them; the card says so rather than borrowing the list's
 * meaning.
 */
export function recurringFailuresScope(f: QueueListFilters): {
  scope: string;
  unapplied: string | null;
} {
  const left = [
    f.pool_id.length ? 'pool' : '',
    f.repo.length ? 'repository' : '',
    f.workflow.length ? 'workflow' : '',
    f.host_id.length ? 'host' : '',
    f.label.length ? 'label' : '',
    f.q.trim() ? 'search' : '',
  ].filter((w) => w !== '');
  return {
    scope: 'Every repository and workflow',
    unapplied: left.length
      ? `The list's ${joinWords(left)} ${left.length === 1 ? 'filter is' : 'filters are'} not applied here: this analysis reads every job this fleet ran in the period.`
      : null,
  };
}

/** What a job's failures look like, as the finding classes them. */
export const FAILURE_CLASS_WORDS: Record<string, string> = {
  infrastructure: 'Mostly runner faults',
  flaky: 'Passes when rerun',
  persistent: 'Has not passed since',
  intermittent: 'Passes between failures',
};

/**
 * What the failed runners' card covers, and which of the list's filters it
 * does not. The route reads every start in the period, narrowed to the pool
 * and host the page chose; the list beside it also takes a search, which an
 * analysis of starts has no use for. The fleet's answer under a list searched
 * down to one runner would read as that runner's, so the card says what it
 * covers and names the search it leaves out, as the Queued card does its own.
 */
export function startupFailuresScope(
  f: { pool_id: string; host_id: string; q: string },
  name: { pool: (id: string) => string; host: (id: string) => string },
): { scope: string; unapplied: string | null } {
  const covered = [
    f.pool_id ? `Pool ${name.pool(f.pool_id)}` : '',
    f.host_id ? `${f.pool_id ? 'host' : 'Host'} ${name.host(f.host_id)}` : '',
  ].filter((w) => w !== '');
  return {
    scope: covered.length ? covered.join(', ') : 'Every pool and host',
    unapplied: f.q.trim()
      ? "The list's search filter is not applied here: this analysis reads every start in the period, narrowed only by pool and host."
      : null,
  };
}

/** The fields a card's description line is drawn from. */
export interface DescribedFinding {
  analysis: string;
  since: string;
  until: string;
  evidence?: unknown;
}

interface ComparedSides {
  a: { label: string; since: string; until: string };
  b: { label: string; since: string; until: string };
  excluded: readonly string[];
}

/**
 * The line under a card's title: its scope and period. A comparison names
 * each side's own dates instead, since a side can cover less than the period
 * asked for (a release ran for part of it, a roll-up side ends at a midnight)
 * and the asked period would read as the span both cover; and it says what
 * it left out, because that is what every figure below it means.
 */
export function findingDescription(
  f: DescribedFinding,
  format: (iso: string) => string,
  scope?: string,
): string {
  if (f.analysis === 'compare' && f.evidence) {
    const ev = f.evidence as ComparedSides;
    const side = (x: ComparedSides['a']) =>
      `${x.label} from ${format(x.since)} to ${format(x.until)}`;
    const left = joinWords(ev.excluded);
    const out = left
      ? ` ${capitalise(left)} jobs are left out of every figure read from the job rows.`
      : '';
    return `${side(ev.a)}; ${side(ev.b)}.${out}`;
  }
  const period = `${format(f.since)} to ${format(f.until)}`;
  return scope ? `${scope}, ${period}` : period;
}

/**
 * The refusals whose own sentence names a remedy the page has no control for,
 * said instead in terms of what the page does have. Too few releases tells a
 * caller to name the releases with a and b, to widen the period, or to compare
 * by period; the Usage card asks by version with no a and b and never compares
 * periods, so two of the three are not on the page and the third is its range.
 * A Map, since a code is whatever the server sent and a plain object would
 * answer "constructor".
 */
const SAID_ON_THE_PAGE = new Map([
  [
    'analysis.too_few_releases',
    'Fewer than two releases completed a job in this range, so there is no pair to compare; widen the range.',
  ],
]);

/**
 * What a card shows when its analysis was not given, and whether that is a
 * refusal. A 4xx is the route declining a question it understood (a period
 * over limits.job_stats_window, too few releases to compare), in its own
 * sentence that says what to change; on Usage's default day that is the
 * ordinary answer, so it is a status, not an alert. Anything else is a
 * request that failed. A server failure names no cause on purpose and carries
 * its request ID in `detail`, so the card says which request to quote.
 */
export function refusalOf(cause: unknown): { message: string; refused: boolean } {
  const c = (cause ?? {}) as {
    status?: unknown;
    code?: unknown;
    message?: unknown;
    detail?: unknown;
  };
  const status = typeof c.status === 'number' ? c.status : undefined;
  const raw =
    typeof c.message === 'string' && c.message.trim()
      ? c.message
      : 'The analysis could not be read';
  if (status !== undefined && status >= 500) {
    const id = typeof c.detail === 'string' ? /^request (\S+)$/.exec(c.detail)?.[1] : undefined;
    const said = raw.replace(supportHint(), '').trim();
    return { message: `${sentence(said)} ${supportHint(id)}`, refused: false };
  }
  const refused = status !== undefined && status >= 400 && status < 500;
  const onThePage =
    refused && typeof c.code === 'string' ? SAID_ON_THE_PAGE.get(c.code) : undefined;
  return { message: onThePage ?? sentence(raw), refused };
}

/** What the card hands Eli: the fields every finding shares. */
export interface FindingFacts {
  analysis: string;
  since: string;
  until: string;
  summary: string;
  confidence: string;
  confidence_reason?: string;
  next_steps: readonly { text: string; setting?: string; code?: string }[];
  codes: readonly string[];
}

/** The pool or host a card was narrowed to, which its finding does not say. */
export interface AnalysisNarrowing {
  pool?: { id: string; name?: string };
  host?: { id: string; name?: string };
}

/** A name with its id, or the id alone when the page does not know the name. */
function namedWithId(x: { id: string; name?: string }): string {
  return x.name && x.name !== x.id ? `${x.name} (${x.id})` : x.id;
}

/**
 * The finding as the card shows it, named one by one. The evidence stays on
 * the page: it quotes names a workflow wrote, and Eli has the analysis tools
 * to read it whole.
 *
 * What the card was narrowed to goes with it, and so does the instruction to
 * pass it. The prompt tells Eli to check the snapshot with its tools, which
 * read the last day of the whole fleet unless told otherwise, and a finding
 * narrowed to one pool says nothing of the pool ("No start failed in this
 * period") for Eli to repeat.
 */
export function analysisContext(
  title: string,
  f: FindingFacts,
  narrowed: AnalysisNarrowing = {},
): EliContext {
  const passed = joinWords(
    [
      narrowed.pool ? `pool_id ${narrowed.pool.id}` : '',
      narrowed.host ? `host_id ${narrowed.host.id}` : '',
    ].filter((a) => a !== ''),
  );
  const of =
    narrowed.pool && narrowed.host
      ? 'this pool and host'
      : narrowed.pool
        ? 'this pool'
        : narrowed.host
          ? 'this host'
          : 'this fleet';
  return {
    kind: 'analysis',
    title,
    facts: {
      Analysis: f.analysis,
      From: f.since,
      To: f.until,
      Pool: narrowed.pool ? namedWithId(narrowed.pool) : undefined,
      Host: narrowed.host ? namedWithId(narrowed.host) : undefined,
      Summary: f.summary,
      Confidence: confidenceWords(f.confidence, f.confidence_reason),
      'Next steps': f.next_steps.length
        ? f.next_steps.map((s) => (s.setting ? `${s.text} (${s.setting})` : s.text)).join(' ')
        : undefined,
      Codes: f.codes.length ? f.codes.join(', ') : undefined,
    },
    question:
      `What does this analysis mean for ${of}, and what should I change first? ` +
      `To check it against the fleet now, ask for the same analysis again with since ${f.since}, until ${f.until}` +
      `${passed ? `, ${passed}` : ''}; without them it reads the last day of the whole fleet.`,
  };
}

/**
 * A job's repository, workflow and name, the empty ones skipped: with the
 * names withheld every one is empty, and a row of separators would read as
 * names that are blank rather than left out. The step that says they were
 * withheld is the controller's, and the card shows it once.
 */
export function jobWords(e: { repo?: string; workflow?: string; job_name?: string }): string {
  return [e.repo, e.workflow, readableJobText(e.job_name)]
    .map((s) => (s ?? '').trim())
    .filter((s) => s !== '')
    .join(' · ');
}

/**
 * The pool the jobs no pool claimed are counted under, which has no id or name
 * of its own. The MCP tools say it in the same words, so a person and an agent
 * read the row alike.
 */
export const UNCLAIMED_POOL = 'unclaimed (no pool claimed these jobs)';

/**
 * Every pool's longest waits, longest first, as one list, each naming its
 * pool. A row with no pool is the jobs no pool claimed, which would otherwise
 * read as waiting "in" nothing.
 */
export function longestWaits<J extends { wait_seconds: number }>(
  pools: readonly { pool_id: string; pool_name?: string; longest: readonly J[] }[],
  n = 5,
): (J & { pool: string })[] {
  return pools
    .flatMap((p) =>
      p.longest.map((j) => ({ ...j, pool: p.pool_name || p.pool_id || UNCLAIMED_POOL })),
    )
    .sort((a, b) => b.wait_seconds - a.wait_seconds)
    .slice(0, n);
}

/** Each queue-wait cause, as the time it names. */
export const CAUSE_WORDS: Record<string, string> = {
  held_for_review: 'Held for review',
  unmatched: 'No pool claimed it',
  pool_ceiling: 'Pool at its maximum runners',
  throttled: 'Host throttled',
  no_room: 'No room on a host',
  runner_startup: 'Runner starting',
  github_delay: 'GitHub handing it over',
  unattributed: 'Nothing on record explains it',
};

/** Each comparison measure, as Usage's table of releases heads its columns. */
export const MEASURE_WORDS: Record<string, string> = {
  duration_p50: 'Duration p50',
  duration_p95: 'Duration p95',
  queue_wait_p50: 'Queue wait p50',
  queue_wait_p95: 'Queue wait p95',
  startup_p50: 'Startup p50',
  startup_p95: 'Startup p95',
  fleet_failure_rate: 'Lost to the fleet',
  failure_rate: 'Failed',
  oom_rate: 'OOM kills',
};

export const DIRECTION_WORDS: Record<string, string> = {
  regressed: 'worse',
  improved: 'better',
  unchanged: 'held',
  not_enough: 'too few jobs to judge',
};

/** A comparison figure: a time, or a share of the side's jobs. */
export function changeFigure(value: number | null | undefined, unit: string): string {
  return unit === 'rate' ? formatPercent(value, 1) : formatDuration(value);
}

export const EVENT_WORDS: Record<string, string> = {
  failure: 'Failed',
  oom: 'OOM killed',
  slow_startup: 'Slow startup',
};

export const PRESSURE_MEASURE_WORDS: Record<string, string> = {
  load_per_cpu: 'Load per core',
  cpu_percent: 'CPU',
  memory_used: 'Memory in use',
};

/**
 * A threshold's value in its measure's own terms, as the controller's summary
 * says it: the CPU line and the memory line to a tenth of a percent, the load
 * to a hundredth, a trailing zero dropped. The controller floors the line to
 * that, so rounding it further would name a line some events below it would
 * read as above, and the card would disagree with the summary over it.
 */
export function thresholdValue(measure: string, value: number): string {
  if (measure === 'cpu_percent') return `${Math.round(value * 10) / 10}%`;
  if (measure === 'memory_used') return `${Math.round(value * 1000) / 10}%`;
  return String(Math.round(value * 100) / 100);
}

export const STAGE_WORDS: Record<string, string> = {
  before_container: 'before its container started',
  before_registration: 'before it registered with GitHub',
  after_registration: 'after it registered',
};

export const EXPLANATION_WORDS: Record<string, string> = {
  image: 'its runner image',
  image_pull_incident: 'an image pull incident on the host',
  runtime_incident: 'a container runtime incident on the host',
  throttle: 'the host being throttled',
};

/** A word for an enumeration value this build has no word for: its own, readable. */
export function wordFor(words: Record<string, string>, value: string): string {
  return words[value] ?? value.replace(/_/g, ' ');
}
