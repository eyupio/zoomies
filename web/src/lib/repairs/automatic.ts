import type { Schemas } from '../api/types';
import { pluralise, relativeTime } from '../format';
import { stateLabel } from './history';

/*
  What the pages about automatic repairs say, so a test can hold it without a
  browser: what the automation did last, what the stop dialog promises, what a stop reports, and
  what the repair page tells a person about a repository that repairs itself.
*/

type Repository = Schemas['EliAutomaticRepository'];
type Repair = Schemas['EliRepair'];
type StopResult = Schemas['EliAutomaticStopResult'];
export type StopChoice = Schemas['EliAutomaticStopRequest']['then'];

/** The newest automatic repair still in the history, as a row of the overview shows it. */
export function lastWords(repo: Pick<Repository, 'last'>, now: number = Date.now()): string {
  const last = repo.last;
  if (!last) return 'None yet';
  const on = last.pull_number > 0 ? ` on #${last.pull_number}` : '';
  return `${stateLabel(last.state)}${on}, ${relativeTime(last.updated_at, now)}`;
}

export function runningWords(running: number): string {
  return running > 0 ? `${running} running now` : '';
}

/** Who pays, or that nobody can: a deleted provider leaves a policy with none. */
export function paidByWords(
  repo: Pick<Repository, 'provider_id' | 'provider_name' | 'model'>,
): string {
  if (!repo.provider_name) {
    return repo.provider_id
      ? 'The installation provider cannot be found'
      : 'No installation provider is set';
  }
  return repo.model
    ? `Paid by ${repo.provider_name} (${repo.model})`
    : `Paid by ${repo.provider_name}`;
}

export interface StopCopy {
  title: string;
  intro: string;
  question: string;
  askFirst: { label: string; hint: string };
  off: { label: string; hint: string };
  /** What happens to a repair that is running, which is the first thing a person worries about. */
  inFlight: string;
  confirm: string;
}

function namesOf(repos: readonly string[]): string {
  if (repos.length > 3) return `${repos.slice(0, 3).join(', ')} and ${repos.length - 3} more`;
  if (repos.length === 3) return `${repos[0]}, ${repos[1]} and ${repos[2]}`;
  return repos.join(' and ');
}

/** The stop dialog, for one repository or for several. */
export function stopCopy(repos: readonly string[]): StopCopy {
  const one = repos.length === 1;
  return {
    title: one
      ? 'Stop automatic repairs'
      : `Stop automatic repairs on ${repos.length} repositories`,
    intro: one
      ? `Eli will stop repairing ${repos[0]} by itself.`
      : `Eli will stop repairing ${pluralise(repos.length, 'repository', 'repositories')} by itself: ${namesOf(repos)}.`,
    question: 'What should happen instead?',
    askFirst: {
      label: 'Ask me first',
      hint: 'When checks fail, Eli offers a repair under Needs you and waits for you.',
    },
    off: {
      label: 'Turn repairs off',
      hint: one
        ? 'Eli neither offers nor starts a repair here, and nobody can request one, until it is turned on again in setup.'
        : 'Eli neither offers nor starts a repair on these repositories, and nobody can request one, until each is turned on again in setup.',
    },
    inFlight:
      'A repair that has not pushed yet stops before it changes the branch, unless it is already publishing. One that has pushed carries on watching its checks.',
    confirm: 'Stop automatic repairs',
  };
}

export interface StopOutcome {
  tone: 'success' | 'error';
  title: string;
  message: string;
}

/**
 * What a stop reports. A repository that was already stopped is not an error:
 * somebody else got there first, which is the result that was wanted.
 */
export function stoppedWords(results: readonly StopResult[], then: StopChoice): StopOutcome {
  const failed = results.filter((r) => r.error);
  const changed = results.filter((r) => r.changed);
  if (failed.length > 0) {
    return {
      tone: 'error',
      title: changed.length > 0 ? 'Stopped some, not all' : 'Could not stop automatic repairs',
      message: failed.map((r) => `${r.repo}: ${r.error}`).join(' '),
    };
  }
  if (changed.length === 0) {
    return {
      tone: 'success',
      title: 'Already stopped',
      message: 'Automatic repairs were already off there, so nothing changed.',
    };
  }
  const where = changed.length === 1 ? changed[0]!.repo : `${changed.length} repositories`;
  const now =
    then === 'off'
      ? 'Repairs are off there now.'
      : changed.length === 1
        ? 'It asks first now.'
        : 'They ask first now.';
  return { tone: 'success', title: `Stopped automatic repairs on ${where}`, message: now };
}

/**
 * The attempt Eli made on this commit by itself, newest first. The history's
 * repository filter is a substring, so the name is compared whole, without
 * regard to case as GitHub does.
 */
export function findAutomaticAttempt(
  repairs: readonly Repair[],
  target: { repo: string; pull: number; head: string },
): Repair | undefined {
  const repo = target.repo.toLowerCase();
  return repairs
    .filter(
      (r) =>
        r.trigger === 'automatic' &&
        r.repo.toLowerCase() === repo &&
        r.pull_number === target.pull &&
        r.head_sha === target.head,
    )
    .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0];
}

export interface AutomaticNote {
  kind: 'tried' | 'none' | 'neutral';
  title: string;
  body: string;
  /** The attempt to link to, for `tried`. */
  attemptId?: string;
}

/**
 * What the repair page says about a repository that repairs itself, which
 * depends on what the person looking is allowed to know. Automatic repairs have
 * no owner, so only an administrator can say whether Eli tried this failure; for
 * anyone else a claim either way would be a guess.
 */
export function automaticNote(input: {
  mode: string | undefined;
  admin: boolean;
  attempt: Repair | undefined;
  /** The repository's row of the overview, when it could be read. */
  repository: Repository | undefined;
}): AutomaticNote | null {
  if (input.mode !== 'automatic') return null;
  if (!input.admin) {
    return {
      kind: 'neutral',
      title: 'This repository repairs itself',
      body: 'Eli repairs failed checks here automatically. An administrator can see what it has done.',
    };
  }
  if (input.attempt) {
    return {
      kind: 'tried',
      title: 'Eli already tried this on its own',
      body: `${stateLabel(input.attempt.state)}. Eli does not try the same commit twice by itself, so starting another attempt here is the retry.`,
      attemptId: input.attempt.id,
    };
  }
  const title = 'Nothing has started on its own for this failure';
  const repo = input.repository;
  if (repo?.problem) {
    return { kind: 'none', title, body: `${repo.problem} That is why nothing has started.` };
  }
  return {
    kind: 'none',
    title,
    body: 'Eli starts from GitHub’s report of a failed job, so a failure from before Automatic was switched on, or one whose report did not arrive, is not picked up on its own. You can start one yourself below.',
  };
}
