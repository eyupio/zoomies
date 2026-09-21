/**
 * What the fleet wants a person to know, and what that person has already read.
 *
 * The controller reports problems; it has no opinion about whether an operator
 * has seen them. That opinion lives here, per browser, because the two useful
 * answers are different for every operator: "I know about the privileged pool,
 * I chose it" is a settled decision, while an unhealthy host is news. Without
 * somewhere to put the first kind, a panel that is never clear stops being
 * read, which costs the operator the second kind too.
 *
 * Three rules keep a dismissal from becoming a way to hide a real fault:
 *
 *  * A dismissal is forgotten the moment the controller stops reporting the
 *    problem, so the same fault happening again is news again.
 *  * A dismissal only covers the severity it was made at. A warning that
 *    becomes an error comes back, because it is not the thing that was read.
 *  * A snooze is a dismissal with an expiry the operator picks -- 15 minutes,
 *    an hour, a day -- rather than "until resolved". It returns to the active
 *    list on its own once the clock passes that time, with no further click.
 *
 * Dismissals are per-operator preference, not fleet state, so they are stored
 * beside the other preferences rather than on the server. Nothing here changes
 * what `GET /api/v1/problems`, `zoomies status` or an alerting rule sees.
 */
import type { Problem, Severity } from '../api/types';
import { onClockTick } from '../format';
import { problemKey } from '../problems/identity';
import { fleet } from './fleet.svelte';
import { storage } from './prefs.svelte';

// One definition of "the same fault", shared with the Overview's feed, which
// reports a problem the first time it is raised.
export { problemKey };

const DISMISSED_KEY = 'zoomies.problems.dismissed';

/** Worst first. The panel, the badge and the summary all read this order. */
export const SEVERITY_ORDER: readonly Severity[] = ['error', 'warning', 'info'];

/** What one of each is called in a sentence an operator reads. */
export const SEVERITY_NOUN: Record<Severity, string> = {
  error: 'error',
  warning: 'warning',
  info: 'note',
};

function severity(problem: Problem): Severity {
  return problem.severity ?? 'info';
}

function rank(value: Severity): number {
  const i = SEVERITY_ORDER.indexOf(value);
  return i < 0 ? SEVERITY_ORDER.length : i;
}

export interface SnoozeOption {
  id: string;
  /** What the menu item reads: "Snooze for 15 minutes". */
  label: string;
  ms: number;
}

/** The durations offered on every problem. Fixed and short, on purpose: a
 * custom picker is one more decision on the way to putting a fault down. */
export const SNOOZE_OPTIONS: readonly SnoozeOption[] = [
  { id: '15m', label: '15 minutes', ms: 15 * 60_000 },
  { id: '1h', label: '1 hour', ms: 60 * 60_000 },
  { id: '4h', label: '4 hours', ms: 4 * 60 * 60_000 },
  { id: '24h', label: '24 hours', ms: 24 * 60 * 60_000 },
];

interface Dismissal {
  /** The severity that was read. Anything worse comes back. */
  severity: Severity;
  /** When it was dismissed, so the drawer can say how old the decision is. */
  at: string;
  /** Set on a snooze: the moment it expires and the problem becomes active
   * again on its own. Absent on a plain "dismiss until resolved". */
  until?: string;
}

type Dismissals = Record<string, Dismissal>;

function load(): Dismissals {
  const raw = storage.get(DISMISSED_KEY);
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return {};
    return parsed as Dismissals;
  } catch {
    return {};
  }
}

export interface ProblemGroup {
  severity: Severity;
  items: readonly Problem[];
}

class Notifications {
  #dismissals = $state<Dismissals>(load());
  #open = $state(false);
  #showDismissed = $state(false);
  // Ticks off the same shared clock every other relative time on the page
  // reads, so a snooze expiring costs nothing beyond the timer already
  // running for "4m ago" elsewhere on screen.
  #now = $state(Date.now());

  constructor() {
    // A dismissal outlives nothing. Once the controller stops reporting a
    // problem the decision to ignore it is spent, so the fault recurring is
    // reported again rather than being silently swallowed by a click somebody
    // made last week. A snooze is spent the moment its clock runs out, for
    // the same reason. Sweeping here -- rather than at dismissal time -- is
    // what makes both true without any bookkeeping at the call sites.
    $effect.root(() => {
      $effect(() => onClockTick((now) => (this.#now = now)));
      $effect(() => {
        if (!fleet.loaded) return;
        this.#sweep(fleet.problems, this.#now);
      });
    });
  }

  /* -- reads -------------------------------------------------------------- */

  /** Everything the controller reports, worst first, dismissed or not. */
  get all(): readonly Problem[] {
    return fleet.problems;
  }

  /** What still wants a person: the badge, the banner and the panel show these. */
  get active(): readonly Problem[] {
    return fleet.problems.filter((p) => !this.isDismissed(p));
  }

  /** What has been read and put away. Reachable, never in the way. */
  get dismissed(): readonly Problem[] {
    return fleet.problems.filter((p) => this.isDismissed(p));
  }

  /** True when there is genuinely nothing wrong -- the common case. */
  get clear(): boolean {
    return fleet.problems.length === 0 && fleet.problemsOk;
  }

  get errorCount(): number {
    return this.active.filter((p) => severity(p) === 'error').length;
  }

  /** The worst severity still active, or null when nothing is. */
  get worst(): Severity | null {
    let worst: Severity | null = null;
    for (const p of this.active) {
      const s = severity(p);
      if (worst === null || rank(s) < rank(worst)) worst = s;
    }
    return worst;
  }

  /** The active problems grouped by severity, worst group first. */
  get groups(): readonly ProblemGroup[] {
    return this.group(this.active);
  }

  /** Group any list the same way, so the drawer's two lists read alike. */
  group(items: readonly Problem[]): readonly ProblemGroup[] {
    const out: ProblemGroup[] = [];
    for (const s of SEVERITY_ORDER) {
      const group = items.filter((p) => severity(p) === s);
      if (group.length > 0) out.push({ severity: s, items: group });
    }
    return out;
  }

  isDismissed(problem: Problem): boolean {
    const record = this.#dismissals[problemKey(problem)];
    if (!record) return false;
    if (record.until !== undefined && new Date(record.until).getTime() <= this.#now) return false;
    // A warning that has since become an error was never read as an error.
    return rank(severity(problem)) >= rank(record.severity);
  }

  dismissedAt(problem: Problem): string | undefined {
    return this.#dismissals[problemKey(problem)]?.at;
  }

  /** When a snoozed problem comes back on its own, or undefined when it was
   * dismissed outright rather than snoozed. */
  snoozedUntil(problem: Problem): string | undefined {
    return this.#dismissals[problemKey(problem)]?.until;
  }

  /* -- the drawer ---------------------------------------------------------- */

  get open(): boolean {
    return this.#open;
  }

  set open(value: boolean) {
    this.#open = value;
    if (!value) this.#showDismissed = false;
  }

  /** Whether the drawer is currently listing what has been put away. */
  get showDismissed(): boolean {
    return this.#showDismissed;
  }

  set showDismissed(value: boolean) {
    this.#showDismissed = value;
  }

  /* -- writes -------------------------------------------------------------- */

  dismiss(problem: Problem): void {
    this.#dismissals = {
      ...this.#dismissals,
      [problemKey(problem)]: { severity: severity(problem), at: new Date().toISOString() },
    };
    this.#persist();
    this.#closeIfClear();
  }

  /** Put this one away for a fixed while rather than until it resolves. */
  snooze(problem: Problem, ms: number): void {
    const now = new Date();
    this.#dismissals = {
      ...this.#dismissals,
      [problemKey(problem)]: {
        severity: severity(problem),
        at: now.toISOString(),
        until: new Date(now.getTime() + ms).toISOString(),
      },
    };
    this.#persist();
    this.#closeIfClear();
  }

  /** Put away everything currently listed. The drawer's one bulk action. */
  dismissAll(): void {
    const next = { ...this.#dismissals };
    const at = new Date().toISOString();
    for (const problem of this.active) {
      next[problemKey(problem)] = { severity: severity(problem), at };
    }
    this.#dismissals = next;
    this.#persist();
    this.#closeIfClear();
  }

  restore(problem: Problem): void {
    const key = problemKey(problem);
    if (!(key in this.#dismissals)) return;
    const next = { ...this.#dismissals };
    delete next[key];
    this.#dismissals = next;
    this.#persist();
  }

  restoreAll(): void {
    this.#dismissals = {};
    this.#persist();
  }

  /* -- internals ------------------------------------------------------------ */

  /**
   * Dismissing the last problem still asking for attention closes the panel
   * that was showing it: there is nothing left for it to hold open for.
   */
  #closeIfClear(): void {
    if (this.#open && this.active.length === 0) this.open = false;
  }

  /** Drops a dismissal once the problem it covered stops being reported, or
   * once its snooze has run out -- the two ways a dismissal is spent. */
  #sweep(problems: readonly Problem[], now: number): void {
    const live = new Set(problems.map(problemKey));
    const next = { ...this.#dismissals };
    let changed = false;
    for (const [key, record] of Object.entries(next)) {
      const expired = record.until !== undefined && new Date(record.until).getTime() <= now;
      if (live.has(key) && !expired) continue;
      delete next[key];
      changed = true;
    }
    if (!changed) return;
    this.#dismissals = next;
    this.#persist();
  }

  #persist(): void {
    storage.set(DISMISSED_KEY, JSON.stringify(this.#dismissals));
  }
}

export const notifications = new Notifications();
