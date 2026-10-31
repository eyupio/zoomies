import type { Schemas } from '../api/types';
import { relativeTime } from '../format';
import { safeHttpUrl } from './links';

/*
  The nine things readiness looks at, as rows a person can read. The controller
  says what each one found, in its own words; this file gives each a name and,
  when it is blocked, the one way forward that helps. The names are the UI's and
  are keyed by the check's code, so a code this build has not heard of is shown
  as itself instead of being hidden.
*/

export type CheckStatus = 'ok' | 'blocked' | 'unchecked';

/**
 * What a blocked check offers. `recheck` is for the ones that only need time or
 * a fix made elsewhere, `setup` opens the repair setup for the failure in hand,
 * `choose` picks a different pull request, and `link` goes somewhere else.
 */
export type CheckAction =
  | { kind: 'recheck' }
  | { kind: 'setup' }
  | { kind: 'choose' }
  | { kind: 'link'; href: string; label: string; external?: boolean };

export interface CheckRow {
  code: string;
  label: string;
  status: CheckStatus;
  message: string;
  /**
   * Why a blocked check is blocked: the code of the blocker the controller
   * recorded for it, which the check's own code cannot say. A controller older
   * than this build sends none, and the row reads by the check alone.
   */
  blocker?: string;
  action?: CheckAction;
}

/**
 * Blockers that say a check could not be made, not that it failed. The controller
 * folds them into the check it was making, so the check's own code would send the
 * person to choose another pull request or to open the installations because GitHub
 * did not answer. Whichever check it was, the way forward is to look again.
 */
const COULD_NOT_CHECK = ['github', 'budget_unavailable'];
export const couldNotCheck = (blocker: string | undefined): blocker is string =>
  blocker !== undefined && COULD_NOT_CHECK.includes(blocker);

type Readiness = Schemas['EliReadiness'];

/** A controller older than this build answers with blockers and no checks. */
type ReadinessLike = Omit<Readiness, 'checks'> & { checks?: Readiness['checks'] };

/** The checks in the order the controller makes them. */
const ORDER = [
  'identity',
  'provider',
  'installation',
  'access',
  'policy',
  'budget',
  'pull',
  'checks',
  'permissions',
];

const LABELS: Record<string, string> = {
  identity: 'GitHub account',
  provider: 'Model provider',
  installation: 'GitHub App installation',
  access: 'Write access',
  policy: 'Repository policy',
  budget: 'Attempts left',
  pull: 'Pull request',
  checks: 'Failing checks',
  permissions: 'App permissions',
  github: 'GitHub',
};

/**
 * The check each blocker belongs to, for an answer with no checks in it. A
 * blocker about the target changing says what became of the pull request or of
 * the checks, so it sits with the check that found it. GitHub not answering
 * belongs to none of them, so it is a row of its own, and the way forward from
 * it is to look again.
 */
const CHECK_OF_BLOCKER: Record<string, string> = {
  identity: 'identity',
  provider: 'provider',
  installation: 'installation',
  github: 'github',
  access: 'access',
  policy: 'policy',
  budget: 'budget',
  budget_unavailable: 'budget',
  pull: 'pull',
  closed: 'pull',
  superseded: 'pull',
  fixed: 'checks',
  checks_pending: 'checks',
  permissions: 'permissions',
};

function wayForward(code: string, repoUrl: string | undefined): CheckAction {
  switch (code) {
    case 'identity':
    case 'policy':
      return { kind: 'setup' };
    case 'provider':
      return { kind: 'link', href: '/settings/assistant', label: 'Add a model provider' };
    case 'installation':
      return { kind: 'link', href: '/installations', label: 'Open installations' };
    case 'permissions':
      return { kind: 'link', href: '/installations', label: 'Review installation permissions' };
    case 'pull':
      return { kind: 'choose' };
    case 'access': {
      // The host is the one the job's address named. With none known there is
      // nowhere honest to send somebody, so the row offers to look again.
      const repo = safeHttpUrl(repoUrl);
      if (!repo) return { kind: 'recheck' };
      return {
        kind: 'link',
        href: `${repo.replace(/\/+$/, '')}/settings/access`,
        label: 'Manage access on GitHub',
        external: true,
      };
    }
    default:
      return { kind: 'recheck' };
  }
}

function row(
  code: string,
  status: CheckStatus,
  message: string,
  repoUrl?: string,
  blocker?: string,
): CheckRow {
  const out: CheckRow = { code, label: LABELS[code] ?? code, status, message };
  if (status === 'blocked') {
    if (blocker) out.blocker = blocker;
    out.action = wayForward(couldNotCheck(blocker) ? blocker : code, repoUrl);
  }
  return out;
}

const rank = (code: string): number => {
  const at = ORDER.indexOf(code);
  return at < 0 ? ORDER.length : at;
};

function rowsFromBlockers(blockers: Readiness['blockers'], repoUrl?: string): CheckRow[] {
  const seen = new Map<string, { message: string; blocker: string }>();
  for (const blocker of blockers) {
    const code = CHECK_OF_BLOCKER[blocker.code] ?? blocker.code;
    if (!seen.has(code)) seen.set(code, { message: blocker.message, blocker: blocker.code });
  }
  return [...seen.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([code, { message, blocker }]) => row(code, 'blocked', message, repoUrl, blocker));
}

/**
 * Every check readiness made, as rows. `repoUrl` is the repository's address on
 * the host that served the failure, which the access row sends somebody to; it
 * is not guessed when it is not known.
 */
export function checkRows(readiness: ReadinessLike, repoUrl?: string): CheckRow[] {
  const checks = readiness.checks ?? [];
  if (checks.length === 0) return rowsFromBlockers(readiness.blockers, repoUrl);
  return checks.map((check) =>
    row(
      check.code,
      check.status === 'ok' || check.status === 'blocked' ? check.status : 'unchecked',
      check.message,
      repoUrl,
      check.blocker,
    ),
  );
}

const HEADLINES: Record<string, string> = {
  identity: 'Link your GitHub account',
  provider: 'Add a model provider',
  installation: 'Connect the GitHub installation',
  access: 'Fix write access first',
  policy: 'Turn repairs on for this repository',
  budget: 'No attempts left right now',
  pull: 'Choose a pull request',
  checks: 'Wait for the checks',
  permissions: 'Grant the GitHub App its permissions',
  github: 'GitHub did not answer',
  budget_unavailable: 'Could not check the allowance',
};

/**
 * What the decision card says about the check that blocks, as the one thing to do.
 * A check that could not be made says so in place of what it was checking.
 */
export function blockedHeadline(code: string, blocker?: string): string {
  return HEADLINES[couldNotCheck(blocker) ? blocker : code] ?? 'Something needs your attention';
}

/** The first thing in the way, which is the one the card is about. */
export function blockingRow(rows: readonly CheckRow[]): CheckRow | undefined {
  return rows.find((r) => r.status === 'blocked');
}

const WAIT_CLOCK = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });

/**
 * When a spent allowance has a place again, in the words a person can act on.
 * The controller says when the binding limit's oldest attempt leaves the
 * 24-hour window; this is that instant as a time of day, with "tomorrow" when
 * midnight comes first, and how long it is, because "in 3h" is the part
 * somebody plans around. With no instant it says only what is true.
 */
export function allowanceWait(resetAt: string | undefined, now: number = Date.now()): string {
  const free = resetAt ? new Date(resetAt) : null;
  if (!free || Number.isNaN(free.getTime())) {
    return 'The next attempt can start once the oldest attempt leaves the 24-hour window.';
  }
  const time = WAIT_CLOCK.format(free);
  const today = new Date(now).toDateString() === free.toDateString();
  return `The next attempt can start ${today ? time : `tomorrow ${time}`} (${relativeTime(free.getTime(), now)}).`;
}
