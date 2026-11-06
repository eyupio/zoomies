import type { Schemas } from '../api/types';
import { isActive, stateLabel } from './history';

/*
  What the attempt page says about one repair that is not already a status or a
  case: who asked, which kind of provider paid, how long it took, and the moments
  the row actually recorded. It reads the row and nothing else, so a moment the
  controller did not write down is not on the timeline, rather than being
  invented to make it look fuller.
*/

type Repair = Schemas['EliRepair'];

/** Who began the repair, in the words the header shows. */
export function requestedBy(repair: Pick<Repair, 'trigger' | 'github_login'>): string {
  if (repair.trigger === 'automatic') return 'Started automatically';
  return repair.github_login ? `Requested by @${repair.github_login}` : 'Requested by someone';
}

/**
 * Whose provider the model call was made through. An automatic repair uses the
 * installation's provider, which an administrator chose, and a requested one
 * uses the provider of the person who asked.
 */
export function providerWords(repair: Pick<Repair, 'trigger'>): string {
  return repair.trigger === 'automatic' ? 'Installation provider' : 'Personal provider';
}

/** One attempt on a pull request, reduced to what its history row shows. */
export interface PullAttempt {
  id: string;
  state: string;
  /** How it ended, or how far it has got, in the history's own words. */
  stateLabel: string;
  began: string;
  /** When it was queued, which is what orders the history. */
  at: string;
  headSha: string;
  commitSha: string;
  active: boolean;
}

/**
 * The attempts recorded on one pull request, newest first. The history is read
 * for a repository and can carry other pull requests, so this narrows by
 * repository and number rather than trusting the query alone. An attempt with
 * no timestamp sorts last, because it cannot be placed in the sequence.
 */
export function pullAttempts(
  repairs: readonly Repair[],
  repo: string,
  pullNumber: number,
): PullAttempt[] {
  const when = (repair: Repair): number => {
    const at = Date.parse(repair.created_at);
    return Number.isNaN(at) ? Number.NEGATIVE_INFINITY : at;
  };
  return repairs
    .filter(
      (repair) =>
        repair.pull_number === pullNumber && repair.repo.toLowerCase() === repo.toLowerCase(),
    )
    .sort((a, b) => when(b) - when(a) || (a.id < b.id ? 1 : a.id > b.id ? -1 : 0))
    .map((repair) => ({
      id: repair.id,
      state: repair.state,
      stateLabel: stateLabel(repair.state),
      began: requestedBy(repair),
      at: repair.created_at,
      headSha: repair.head_sha,
      commitSha: repair.commit_sha,
      active: isActive(repair.state),
    }));
}

/**
 * How long a finished repair took. A repair still going has taken nothing yet,
 * and a clock that disagrees with the one that wrote the row is not a negative
 * time: it is nothing worth saying, so it reads as no time at all.
 */
export function tookMs(
  repair: Pick<Repair, 'state' | 'created_at' | 'updated_at'>,
): number | undefined {
  if (isActive(repair.state)) return undefined;
  const took = Date.parse(repair.updated_at) - Date.parse(repair.created_at);
  return Number.isNaN(took) ? undefined : Math.max(0, took);
}

export interface TimelineEntry {
  /** When it happened, if the row recorded when. A pushed commit has no time of its own. */
  at?: string;
  text: string;
}

/**
 * The moments of a repair, from the row alone: it was queued, a commit was
 * pushed, and (once it has stopped) how it ended. The ending is named by its
 * state; the controller's sentence about it is the outcome card's to say, and
 * saying it twice on one screen would only make the second copy harder to trust.
 */
export function timeline(
  repair: Pick<Repair, 'state' | 'commit_sha' | 'created_at' | 'updated_at'>,
): TimelineEntry[] {
  const out: TimelineEntry[] = [{ at: repair.created_at, text: 'Queued' }];
  if (repair.commit_sha) out.push({ text: `Pushed commit ${repair.commit_sha.slice(0, 7)}` });
  if (!isActive(repair.state)) out.push({ at: repair.updated_at, text: stateLabel(repair.state) });
  return out;
}

/**
 * The facts of a repair as plain text, for pasting into a bug report. The
 * figures are exact where the page rounds them, because an investigation wants
 * the number that was recorded. Responses are already redacted by the server,
 * but can quote source; copying this report is an explicit sharing action.
 * It says when an older call has no retained response.
 */
export function investigationText(repair: Repair): string {
  const d = repair.details;
  const out: string[] = [`Eli repair ${repair.id}`, `Repository: ${repair.repo}`];
  if (repair.pull_number > 0) out.push(`Pull request: #${repair.pull_number}`);
  if (repair.run_id > 0) out.push(`Run: ${repair.run_id}`);
  out.push(`Failing head: ${repair.head_sha}`);
  out.push(`Commit pushed: ${repair.commit_sha || 'none'}`);
  out.push(`State: ${repair.state}`);
  if (repair.message) out.push(`Message: ${repair.message}`);
  if (d?.stage) out.push(`Stage reached: ${d.stage}`);
  if (d?.failure_code) out.push(`Failure code: ${d.failure_code}`);
  if (d?.model) out.push(`Model: ${d.model}`);
  if (d?.source_bytes) out.push(`Source supplied: ${d.source_bytes} bytes`);
  if (d?.steps?.length) {
    out.push(`Model calls: ${d.steps.length}`);
    d.steps.forEach((step, index) => {
      const usage = step.usage_reported
        ? `${step.input_tokens} input tokens, ${step.output_tokens} output tokens`
        : 'usage not reported';
      out.push(
        `  ${index + 1}. ${usage}, request ${step.request_bytes} bytes, ${step.elapsed_ms} ms`,
      );
      if (step.error) out.push(`     Call error: ${step.error}`);
      if (step.validation_error) out.push(`     Validation error: ${step.validation_error}`);
      out.push(`     Response${step.response_truncated ? ' (truncated)' : ''}:`);
      out.push(responseText(step));
    });
  }
  out.push(`Started: ${repair.created_at}`, `Updated: ${repair.updated_at}`);
  return out.join('\n');
}

export function responseText(
  step: NonNullable<Schemas['EliRepairDetails']['steps']>[number],
): string {
  if (!step.response_recorded) return 'Response not retained for this older call.';
  return step.response || 'The provider returned no response text or tool calls.';
}
