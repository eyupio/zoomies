/*
  Where a repair that is running has got to: the five steps the worker takes, in
  order, and the mark on the one it is on. The attempt page's track, the hub's
  small bars and the live region all read it, so they cannot tell different
  stories about the same repair, and a test can hold it without a browser.
*/

import { isActive } from './history';

/** A step of the track against the mark: behind it, on it, still to come, or where a repair stopped. */
export type StepState = 'done' | 'current' | 'todo' | 'stopped';

/**
 * The work a running repair is doing, in the order the worker does it. The
 * labels are what the operator reads while they wait, so they say what is
 * happening rather than which function is running.
 */
export const REPAIR_TRACK = [
  { id: 'queued', label: 'Queued' },
  { id: 'evidence', label: 'Reading the failed checks' },
  { id: 'prepare', label: 'Preparing the fix' },
  { id: 'publish', label: 'Pushing the commit' },
  { id: 'watch', label: 'Watching CI' },
] as const;

/**
 * A row read between two writes can lack fields, and the queue's own word for
 * a repair it has not touched yet is the queued state: an absent state is not
 * a terminal one, or every follow would read a half-written row as finished.
 */
export const stateOf = (repair: { state?: string } | null): string => repair?.state || 'queued';

/**
 * Where a repair is on its track. The stage field is the worker's own word and
 * the commit is proof: a row read between two writes may still carry yesterday's
 * stage, so a published commit only ever moves the mark forwards.
 */
export function trackIndex(repair: {
  state?: string;
  commit_sha?: string;
  details?: { stage?: string } | null;
}): number {
  const state = stateOf(repair);
  const stage = repair.details?.stage ?? '';
  let index = 0;
  // A row recorded before the worker wrote its first stage can have left the
  // queue with no stage; it is at least reading evidence. A row that says
  // "queued" stopped before it read anything, and stays on the first step.
  if (state !== 'queued' && stage === '') index = 1;
  if (stage === 'evidence' || stage === 'source') index = 1;
  if (stage === 'prepare_fix') index = 2;
  if (stage === 'publish') index = 3;
  if (stage === 'watch_ci' || state === 'checking') index = 4;
  if (repair.commit_sha) index = Math.max(index, REPAIR_TRACK.length - 1);
  return Math.min(index, REPAIR_TRACK.length - 1);
}

/** The fields of a repair that say what it is for, and so whether it is the one being looked for. */
interface RepairRow {
  state: string;
  repo: string;
  run_id: number;
  pull_number: number;
  head_sha: string;
}

/** What a failure is known by when its repair is looked for. */
export interface RepairWanted {
  repo: string;
  /** The run the failure was reached by, which only a repair Eli queued itself records. */
  run?: number;
  pull?: number;
  /** The commit readiness resolved, which a repair a person queued is pinned to. */
  head?: string;
}

/**
 * The repair already working on a failure, if there is one.
 *
 * A failure reached from a run is first looked for by the run's id, but only a
 * repair Eli queues by itself records a run: one a person queued from this page
 * carries the pull request and the commit and no run, so by the run alone it is
 * never found and is offered a second time. Once readiness has said which pull
 * request and commit the run is, the same question is asked again of those.
 * Pressing the button would not queue a second repair, because the server answers
 * with the first, but it would offer as new something that is already under way.
 */
export function runningRepair<T extends RepairRow>(
  rows: readonly T[],
  want: RepairWanted,
): T | undefined {
  const repo = want.repo.toLowerCase();
  return rows.find((row) => {
    if (!isActive(row.state) || row.repo.toLowerCase() !== repo) return false;
    if (want.run) return row.run_id === want.run;
    return row.pull_number === want.pull && (want.head === undefined || row.head_sha === want.head);
  });
}

/**
 * The track with each step's place, which is what draws it: the steps behind
 * the mark are done, the one on it is current, the rest are still to come. A
 * repair that has ended keeps the whole sequence: a repair that finished the
 * work has every step done, and one that stopped at a step marks that step
 * stopped rather than pretending it finished or that it never began.
 */
export function trackSteps(repair: Parameters<typeof trackIndex>[0]): {
  id: (typeof REPAIR_TRACK)[number]['id'];
  label: string;
  state: StepState;
}[] {
  const at = trackIndex(repair);
  const state = stateOf(repair);
  if (isActive(state)) {
    return REPAIR_TRACK.map((step, index) => ({
      id: step.id,
      label: step.label,
      state: index < at ? 'done' : index === at ? 'current' : 'todo',
    }));
  }
  // These outcomes mean the worker got through every step, whatever CI then
  // said about the commit it pushed.
  const completed = state === 'succeeded' || state === 'checks_failed' || state === 'unverified';
  return REPAIR_TRACK.map((step, index) => ({
    id: step.id,
    label: step.label,
    state: completed ? 'done' : index < at ? 'done' : index === at ? 'stopped' : 'todo',
  }));
}

const STAGE_WORDS: Record<string, string> = {
  evidence: REPAIR_TRACK[1].label,
  source: 'Reading the source',
  prepare_fix: REPAIR_TRACK[2].label,
  publish: REPAIR_TRACK[3].label,
  watch_ci: REPAIR_TRACK[4].label,
};

/**
 * A stage the worker recorded, in words. The technical details say which stage a
 * repair reached, and a stage a newer worker invented is still readable: its id
 * is made into a sentence rather than hidden or shown as a code.
 */
export function stageWords(stage: string | null | undefined): string {
  if (!stage) return 'Not recorded';
  if (Object.hasOwn(STAGE_WORDS, stage)) return STAGE_WORDS[stage]!;
  const spaced = stage.replace(/_+/g, ' ').trim();
  return spaced ? spaced.charAt(0).toUpperCase() + spaced.slice(1) : 'Not recorded';
}
