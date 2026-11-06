import type { Schemas } from '../api/types';
import { pluralise, relativeTime } from '../format';

/*
  What the pages about continuous watching say, worked out here so a unit test
  can hold it without a browser: the three behaviours and the capabilities each
  one asks the watcher for, the state of one watched pull request, the summary of
  a repository, and the sentences a pause or a stop promises.

  Two rules run through all of it. A paragraph never claims more than the rows
  say: an observation that failed is not "all clear", it is "unknown". And every
  reason a person is needed is carried up from the watch row that has it, so a
  stopped repair chain is visible on the repository line and not only inside a
  collapsed row.
*/

export type Watch = Schemas['EliRepairWatch'];
export type RepositoryWatch = Schemas['EliRepairWatchRepository'];

/** The number of repairs the watcher will try for one failure before asking for a person. */
export const MAX_ATTEMPTS = 3;

/* The three behaviours, and the capabilities each one asks for. */

export type Behaviour = 'babysit' | 'checks' | 'conflicts';

export interface BehaviourChoice {
  value: Behaviour;
  label: string;
  /** What turns up in the repository over the following days, in a person's words. */
  description: string;
}

/**
 * The three choices, in the order a person weighs them. They are the two watch
 * capabilities together, which is what a repository decision records; nothing
 * else about a repository decides what Eli does.
 */
export const BEHAVIOURS: readonly BehaviourChoice[] = [
  {
    value: 'babysit',
    label: 'Baby sit PRs',
    description:
      'Follow checks and merge conflicts, repair failed checks and supported conflicts, then keep watching until the pull request closes or you pause it.',
  },
  {
    value: 'checks',
    label: 'Fix failed checks only',
    description:
      'Repair failed checks and keep watching the result. Merge conflicts are left for a person.',
  },
  {
    value: 'conflicts',
    label: 'Fix merge conflicts only',
    description:
      'Resolve supported merge conflicts and follow the checks that follow. A failing check is reported, not repaired.',
  },
];

export const behaviourLabel = (behaviour: Behaviour): string =>
  BEHAVIOURS.find((choice) => choice.value === behaviour)?.label ?? behaviour;

/** The capabilities one behaviour asks the watcher for. */
export function capabilitiesFor(behaviour: Behaviour): ('checks' | 'conflicts')[] {
  switch (behaviour) {
    case 'checks':
      return ['checks'];
    case 'conflicts':
      return ['conflicts'];
    default:
      return ['checks', 'conflicts'];
  }
}

/**
 * The behaviour a decision or a watch records, or null when it records neither
 * capability. Null is not a fourth behaviour: a row in that state was written by
 * something else, and a page that guessed would describe work nobody asked for.
 */
export function behaviourOf(checks: boolean, conflicts: boolean): Behaviour | null {
  if (checks && conflicts) return 'babysit';
  if (checks) return 'checks';
  if (conflicts) return 'conflicts';
  return null;
}

/** The capability words for a decision, for a line that has no room for a label. */
export function capabilityWords(checks: boolean, conflicts: boolean): string {
  if (checks && conflicts) return 'checks and merge conflicts';
  if (checks) return 'failed checks';
  if (conflicts) return 'merge conflicts';
  return 'nothing';
}

/* The state of one watch. */

export type WatchState = 'enabled' | 'paused' | 'stopped';

/**
 * Which of the three a watch is in. A stopped watch carries a stop reason and a
 * paused one does not, which is how the API tells them apart; enabled wins over
 * both, because a resumed watch keeps the reason it was paused with in its
 * history but is running again.
 */
export function watchState(watch: Pick<Watch, 'enabled' | 'stop_reason'>): WatchState {
  if (watch.enabled) return 'enabled';
  return watch.stop_reason ? 'stopped' : 'paused';
}

export const stateLabel = (state: WatchState): string =>
  state === 'enabled' ? 'Watching' : state === 'stopped' ? 'Stopped' : 'Paused';

/* The evidence a watch recorded: checks, and mergeability. */

export interface CheckReading {
  name: string;
  status: string;
  conclusion: string;
  app: string;
  url: string;
}

export interface CheckEvidence {
  /** What the watcher decided from this reading: passed, failed, waiting or unknown. */
  state: string;
  checks: CheckReading[];
  /** How many checks GitHub reported, which can exceed the ones kept here. */
  total: number;
  truncated: boolean;
}

export interface ConflictEvidence {
  mergeable: boolean;
  mergeableKnown: boolean;
  mergeState: string;
  baseRef: string;
  baseSHA: string;
  headSHA: string;
  mergeBaseSHA: string;
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');

/**
 * The check evidence a watch recorded, or null when there is none or it cannot
 * be read. The stored value is bounded JSON written by the controller; a page
 * that threw on a row from an older build would lose the whole list over one
 * row, so an unreadable record is "no evidence", never a crash.
 */
export function readCheckEvidence(raw: string | undefined): CheckEvidence | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
  const record = parsed as Record<string, unknown>;
  const rows = Array.isArray(record.checks) ? record.checks : [];
  const checks: CheckReading[] = [];
  for (const row of rows) {
    if (!row || typeof row !== 'object') continue;
    const item = row as Record<string, unknown>;
    const name = text(item.name);
    if (!name) continue;
    checks.push({
      name,
      status: text(item.status),
      conclusion: text(item.conclusion),
      app: text(item.app),
      url: text(item.url),
    });
  }
  const state = text(record.state);
  // Nothing at all in the record is no reading, in the same way the controller's
  // empty array is: a page must not report a state it was not given.
  if (!state && checks.length === 0) return null;
  return {
    state,
    checks,
    total: typeof record.total === 'number' ? record.total : checks.length,
    truncated: record.truncated === true,
  };
}

/** The conflict evidence a watch recorded, or null when there is none to read. */
export function readConflictEvidence(raw: string | undefined): ConflictEvidence | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
  const record = parsed as Record<string, unknown>;
  // The controller writes `{}` for a watch it has not read mergeability for.
  // An object naming no mergeability is no evidence, not evidence that there is
  // no conflict.
  if (record.mergeable_known !== true) return null;
  return {
    mergeable: record.mergeable === true,
    mergeableKnown: true,
    mergeState: text(record.merge_state),
    baseRef: text(record.base_ref),
    baseSHA: text(record.base_sha),
    headSHA: text(record.head_sha),
    mergeBaseSHA: text(record.merge_base_sha),
  };
}

/** The checks that did not pass, in the order GitHub listed them. */
export function failingChecks(evidence: CheckEvidence | null): CheckReading[] {
  if (!evidence) return [];
  return evidence.checks.filter((check) => {
    if (check.conclusion) return !['success', 'neutral', 'skipped'].includes(check.conclusion);
    return check.status !== 'completed';
  });
}

/** A check's outcome in a word, for a line that lists them. */
export function checkOutcome(check: CheckReading): string {
  if (check.conclusion) return check.conclusion === 'success' ? 'passed' : check.conclusion;
  if (check.status && check.status !== 'completed') return check.status;
  return 'no conclusion';
}

/* One watched pull request. */

export interface WatchSummary {
  behaviour: Behaviour | null;
  state: WatchState;
  /** What Eli is doing for this pull request now. */
  activity: string;
  /** What it will do when the next observation says so. */
  intended: string;
  /** What happens next, and who it waits for. */
  next: string;
  /** Everything that needs a person, in the order the row carries it. */
  attention: string[];
  /** The last observation succeeded and its evidence is readable. */
  observed: boolean;
  /** When it last succeeded, absent while it never has. */
  observedAt: string | undefined;
  checks: CheckEvidence | null;
  conflict: ConflictEvidence | null;
}

const shortSHA = (sha: string | undefined): string => (sha ? sha.slice(0, 8) : '');

/**
 * Everything one watch row supports saying. `now` decides the tense of the next
 * observation, so a test can put the clock where it wants it.
 */
export function summariseWatch(watch: Watch, now: number = Date.now()): WatchSummary {
  const behaviour = behaviourOf(watch.checks, watch.conflicts);
  const state = watchState(watch);
  const checks = readCheckEvidence(watch.check_evidence);
  const conflict = readConflictEvidence(watch.conflict_evidence);
  const reading = watch.check_state ?? '';
  // A watch that only follows conflicts records no check reading, so its
  // observation is the conflict read: known mergeability is the proof it worked.
  const observed = watch.checks
    ? checks !== null && reading !== '' && reading !== 'unknown'
    : conflict !== null;

  if (state !== 'enabled') {
    const reason = state === 'stopped' ? watch.stop_reason : watch.paused_reason;
    return {
      behaviour,
      state,
      activity: reason || (state === 'stopped' ? 'Stopped.' : 'Paused.'),
      intended: 'Nothing, until watching is resumed.',
      next: 'Resume this pull request to follow it again.',
      attention: [state === 'stopped' ? 'Watching was stopped.' : 'Watching is paused.'],
      observed,
      observedAt: watch.last_observed_at,
      checks,
      conflict,
    };
  }

  const attention: string[] = [];
  if (watch.chain_stopped_reason) attention.push(watch.chain_stopped_reason);
  if (watch.checks && reading === 'unknown') {
    attention.push(
      watch.last_observed_at
        ? 'The last check observation could not be read, so nothing is known about the checks on this commit.'
        : 'No checks have been read for this pull request yet.',
    );
  }

  return {
    behaviour,
    state,
    activity: describeActivity(watch, reading),
    intended: describeIntention(watch, conflict, reading),
    next: describeNext(watch, conflict, reading, observed, now),
    attention,
    observed,
    observedAt: watch.last_observed_at,
    checks,
    conflict,
  };
}

function describeActivity(watch: Watch, reading: string): string {
  if (watch.checks) {
    switch (reading) {
      case 'failed':
        return watch.chain_attempts
          ? `Repairing failed checks; ${pluralise(watch.chain_attempts, 'attempt')} of ${MAX_ATTEMPTS} used`
          : 'A repair for the failed checks is starting';
      case 'waiting':
        return 'Waiting for the checks to report';
      case 'passed':
        return 'Checks passed';
      default:
        // A timestamp with no reading behind it means the last read failed;
        // without one, nothing has succeeded yet. "No successful observation
        // yet" for both would hide a watch that has stopped being able to read.
        return watch.last_observed_at
          ? 'The last check read failed'
          : 'No successful observation yet';
    }
  }
  const conflict = readConflictEvidence(watch.conflict_evidence);
  if (!conflict) {
    return watch.last_observed_at
      ? 'Mergeability could not be read last time'
      : 'No successful observation yet';
  }
  if (!conflict.mergeable) return `Conflicts with ${conflict.baseRef || 'the base branch'}`;
  return `No conflict with ${conflict.baseRef || 'the base branch'}`;
}

function describeIntention(
  watch: Watch,
  conflict: ConflictEvidence | null,
  reading: string,
): string {
  const parts: string[] = [];
  if (watch.checks && reading === 'failed' && !watch.chain_stopped_reason) {
    const head = shortSHA(watch.head_sha);
    parts.push(head ? `Repair the failed checks at ${head}` : 'Repair the failed checks');
  }
  if (watch.conflicts && conflict && !conflict.mergeable) {
    parts.push(
      `Merge ${conflict.baseRef || 'the base branch'} and resolve the supported conflicts`,
    );
  }
  if (parts.length) return parts.join('; ');
  if (watch.checks && reading === 'waiting') return 'Wait for the checks to finish';
  if (!watch.checks && conflict?.mergeable) return 'Nothing to resolve; keep watching';
  return 'Keep observing; no action is due';
}

function describeNext(
  watch: Watch,
  conflict: ConflictEvidence | null,
  reading: string,
  observed: boolean,
  now: number,
): string {
  if (watch.chain_stopped_reason) {
    return 'A person needs to look; watching continues but no further repair starts.';
  }
  if (watch.conflicts && conflict && !conflict.mergeable) {
    return 'The merged commit is checked again before anything is published.';
  }
  if (watch.next_check_at) return `Observe again ${relativeTime(watch.next_check_at, now)}.`;
  if (!observed) return 'The next observation says what the checks and mergeability are.';
  if (watch.checks && reading === 'failed')
    return 'The failed commit is repaired, then checked again.';
  return 'The next observation says whether anything is due.';
}

/* One repository, and the watches it holds. */

export interface RepositorySummary {
  behaviour: Behaviour | null;
  /** Watches still following their pull request. */
  watching: number;
  paused: number;
  stopped: number;
  /** How many of the watching ones carry something for a person. */
  attention: number;
  /** The newest successful observation across the repository, absent while none worked. */
  observedAt: string | undefined;
  /** What the repository is doing now, in one sentence. */
  activity: string;
  /** A watching pull request could not be observed, so no line here is an all-clear. */
  observationFailed: boolean;
  state: WatchState;
}

/**
 * What one repository's row says. The decision carries the behaviour, because
 * that is what was chosen for the repository; a watch carries a copy, and a
 * watch admitted before the decision changed mentions the older answer, so the
 * decision is the one shown as effective. A repository admitted one pull request
 * at a time has no decision, and then the watches are all there is to go on.
 */
export function summariseRepository(
  decision: RepositoryWatch | null,
  watches: readonly Watch[],
  now: number = Date.now(),
): RepositorySummary {
  const mine = decision ? watchesFor(watches, decision.repo) : [...watches];
  let watching = 0;
  let paused = 0;
  let stopped = 0;
  let attention = 0;
  let observedAt: string | undefined;
  let observationFailed = false;
  let repairing = 0;
  let failing = 0;
  for (const watch of mine) {
    const summary = summariseWatch(watch, now);
    if (summary.state === 'paused') paused += 1;
    else if (summary.state === 'stopped') stopped += 1;
    else {
      watching += 1;
      if (summary.attention.length) attention += 1;
      if (!summary.observed) observationFailed = true;
      if (watch.check_state === 'failed') {
        failing += 1;
        if (watch.chain_attempts) repairing += 1;
      }
    }
    const when = watch.last_observed_at;
    if (when && summary.observed && (!observedAt || Date.parse(when) > Date.parse(observedAt))) {
      observedAt = when;
    }
  }
  const behaviour = decision
    ? behaviourOf(decision.checks, decision.conflicts)
    : (mine[0] && behaviourOf(mine[0].checks, mine[0].conflicts)) || null;
  const state: WatchState = decision
    ? decision.enabled
      ? 'enabled'
      : 'paused'
    : watching > 0
      ? 'enabled'
      : 'paused';
  return {
    behaviour,
    watching,
    paused,
    stopped,
    attention,
    observedAt,
    activity: repositoryActivity(state, watching, repairing, failing, attention),
    observationFailed,
    state,
  };
}

function repositoryActivity(
  state: WatchState,
  watching: number,
  repairing: number,
  failing: number,
  attention: number,
): string {
  if (state !== 'enabled') return 'Not admitting new pull requests';
  // A person being needed is said first: a repair that is under way and also
  // stopped is not "being repaired", it is waiting on somebody.
  if (attention > 0) return `${pluralise(attention, 'pull request')} needs a person`;
  if (repairing > 0) return `Repairing ${pluralise(repairing, 'pull request')}`;
  if (watching === 0) return 'No open pull requests are being followed';
  if (failing > 0) return 'Repairing failed checks';
  return `Following ${pluralise(watching, 'pull request')}`;
}

/** GitHub repository names are case-insensitive, so two spellings are one repository. */
export function sameRepo(a: string, b: string): boolean {
  return a.toLowerCase() === b.toLowerCase();
}

export function watchesFor(watches: readonly Watch[], repo: string): Watch[] {
  return watches.filter((watch) => sameRepo(watch.repo, repo));
}

/** The repository's own decision, or undefined when the repository has none. */
export function decisionFor(
  decisions: readonly RepositoryWatch[],
  repo: string,
): RepositoryWatch | undefined {
  return decisions.find((decision) => sameRepo(decision.repo, repo));
}

/**
 * Every repository a watch or a decision mentions, once, in the order they were
 * listed. A watch can outlive its repository decision, so neither list alone is
 * the set.
 */
export function repositoryNames(
  decisions: readonly RepositoryWatch[],
  watches: readonly Watch[],
): string[] {
  const seen: string[] = [];
  for (const name of [...decisions.map((d) => d.repo), ...watches.map((w) => w.repo)]) {
    if (!seen.some((existing) => sameRepo(existing, name))) seen.push(name);
  }
  return seen;
}

/** What needs a person first, then what is being repaired, then the rest, then paused and stopped. */
export function watchingOrder(watches: readonly Watch[], now: number = Date.now()): Watch[] {
  const rank = (watch: Watch): number => {
    if (!watch.enabled) return 3;
    if (summariseWatch(watch, now).attention.length) return 0;
    if (watch.check_state === 'failed') return 1;
    return 2;
  };
  return [...watches].sort((a, b) => rank(a) - rank(b) || b.pull_number - a.pull_number);
}

/* What the section says, which is never an all-clear for a failed read. */

export type WatchingState = 'loading' | 'failed' | 'quiet' | 'working';

/**
 * Which shape the section has. A failed read outranks an empty list, because
 * "nothing is being watched" is exactly the sentence a failed read must never
 * produce; and one of the two reads failing is still a failure, since the
 * repositories and the watches then disagree about what exists.
 */
export function watchingState(input: {
  loaded: boolean;
  failed: boolean;
  repositories: number;
  watches: number;
}): WatchingState {
  if (input.failed) return 'failed';
  if (!input.loaded) return 'loading';
  return input.repositories > 0 || input.watches > 0 ? 'working' : 'quiet';
}

/* The copy a control promises, in-flight work included. */

export interface WatchControlCopy {
  title: string;
  description: string;
  /** What happens to a repair already under way, which is what a person asks first. */
  inFlight: string;
  confirm: string;
}

const where = (watch: Pick<Watch, 'repo' | 'pull_number'>): string =>
  `${watch.repo} #${watch.pull_number}`;

/** Pausing one pull request keeps its history and its settings for a resume. */
export function pauseWatchCopy(watch: Pick<Watch, 'repo' | 'pull_number'>): WatchControlCopy {
  return {
    title: 'Pause this pull request',
    description: `Eli will stop following ${where(watch)}. Its history and what it was set to do are kept.`,
    inFlight:
      'A repair that has not published stops before it changes the branch. A repair already publishing finishes, and the commit it pushed stays on the branch. Nothing observes the pull request again until you resume it.',
    confirm: 'Pause watching',
  };
}

/** Stopping is terminal: only a new admission follows the pull request again. */
export function stopWatchCopy(watch: Pick<Watch, 'repo' | 'pull_number'>): WatchControlCopy {
  return {
    title: 'Stop watching this pull request',
    description: `Eli will stop following ${where(watch)}, and this watch cannot be resumed. Its history stays on record.`,
    inFlight:
      'A repair that has not published stops before it changes the branch. A repair already publishing finishes, and the commit it pushed stays on the branch. Admit the pull request again to follow it from the start.',
    confirm: 'Stop watching',
  };
}

/** Pausing a repository stops new pull requests being admitted and nothing else. */
export function pauseRepositoryCopy(
  decision: Pick<RepositoryWatch, 'repo'>,
  watches: readonly Watch[],
): WatchControlCopy {
  const following = watchesFor(watches, decision.repo).filter((watch) => watch.enabled).length;
  return {
    title: 'Pause new pull requests here',
    description: `Eli will stop admitting new pull requests in ${decision.repo}. Its behaviour and provider are kept for a resume.`,
    inFlight:
      following > 0
        ? `${pluralise(following, 'pull request')} already being followed carries on. Pause one of those on its own line.`
        : 'Nothing is being followed here, so nothing changes until you resume.',
    confirm: 'Stop admitting new pull requests',
  };
}

/** Stopping everything: every watching pull request here, in one answer. */
export function stopAllCopy(watches: readonly Watch[]): WatchControlCopy {
  const following = watches.filter((watch) => watch.enabled).length;
  return {
    title: `Stop watching ${pluralise(following, 'pull request')}`,
    description:
      'Eli will stop following every pull request it is watching here. Each watch keeps its history, and admitting a pull request again starts it afresh.',
    inFlight:
      'A repair that has not published stops before it changes the branch. Repairs already publishing finish, and their commits stay on the branches. The repositories keep their settings.',
    confirm: `Stop ${pluralise(following, 'watch', 'watches')}`,
  };
}

export interface OutcomeWords {
  tone: 'success' | 'error';
  title: string;
  message: string;
}

/** What a pause, resume or stop answered, for a toast. */
export function changeWords(
  action: 'pause' | 'resume' | 'stop',
  watch: Pick<Watch, 'repo' | 'pull_number'>,
  error: string | undefined,
): OutcomeWords {
  const at = where(watch);
  if (error) {
    return { tone: 'error', title: `Could not ${action} watching for ${at}`, message: error };
  }
  switch (action) {
    case 'pause':
      return {
        tone: 'success',
        title: `Paused ${at}`,
        message: 'Its history and settings are kept.',
      };
    case 'resume':
      return {
        tone: 'success',
        title: `Resumed ${at}`,
        message: 'The policy, provider and pull request were checked again.',
      };
    default:
      return {
        tone: 'success',
        title: `Stopped watching ${at}`,
        message: 'Admit it again to follow it afresh.',
      };
  }
}
