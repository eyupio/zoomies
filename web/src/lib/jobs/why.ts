// The pure half of the drawer's Why section: how a next step becomes a
// button, a link or a row, and how a piece of evidence reads. Kept out of the
// component so a unit test can hold it without rendering anything.

import { faultLabel, fleetFailed } from '../faults';
import { formatDuration } from '../format';

export interface WhyEvidence {
  kind: string;
  label: string;
  value: string;
  unit?: string;
  ref?: string;
}

/**
 * The one phrase a list row has room for on a job that went wrong: the fleet's
 * own category, or the step the workflow failed at.
 *
 * The category rather than "Runner lost" for every one of them, because a
 * column is read down rather than across: nine rows saying the same two words
 * say only that the fleet is unwell, and six saying "Out of memory" say what to
 * do about it.
 */
export function failureSummary(job: {
  fault_domain?: string | null;
  fault_kind?: string | null;
  runner_fault?: string | null;
  failed_step?: { name?: string | null; number?: number | null } | null;
}): string {
  if (fleetFailed(job)) return faultLabel(job.fault_kind) || 'Runner lost';
  if (job.failed_step) return job.failed_step.name ?? `step ${job.failed_step.number ?? '?'}`;
  return '';
}

export interface WhyStep {
  text: string;
  kind: string;
  link?: string;
}

export interface StepAction {
  label: string;
  /** The drawer's own re-run button. */
  rerun?: boolean;
  /** Somewhere to go: a path in this UI, or a URL. */
  href?: string;
  /** True for a URL, which opens in a new tab; false for a path here. */
  external?: boolean;
}

/** The evidence kinds whose value is a figure, read in a monospace column. */
const FIGURES = new Set(['exit_code', 'signal', 'memory_limit', 'memory_peak', 'queue_wait']);

export function stepAction(step: WhyStep): StepAction {
  if (step.kind === 'rerun') return { label: step.text, rerun: true };
  if (step.link) {
    return { label: step.text, href: step.link, external: /^https?:\/\//.test(step.link) };
  }
  return { label: step.text };
}

export interface EvidenceRow {
  label: string;
  value: string;
  mono: boolean;
  href?: string;
}

/**
 * A wait arrives as a count of seconds, and every other duration in the drawer
 * reads as "8m 00s". "480 s" beside them made an operator do the division.
 */
function evidenceValue(e: WhyEvidence): string {
  if (e.kind === 'queue_wait' && e.unit === 's' && /^\d+(\.\d+)?$/.test(e.value)) {
    return formatDuration(Number(e.value) * 1000);
  }
  return e.unit ? `${e.value} ${e.unit}` : e.value;
}

export function evidenceRows(evidence: readonly WhyEvidence[]): EvidenceRow[] {
  return evidence.map((e) => {
    const row: EvidenceRow = {
      label: e.label,
      value: evidenceValue(e),
      mono: FIGURES.has(e.kind),
    };
    if (e.ref) row.href = e.ref;
    return row;
  });
}

/** The catalog page a problem code reads on at, from the controller's own "read" step. */
export function catalogLink(steps: readonly WhyStep[]): string | null {
  for (const s of steps) {
    if (s.kind === 'read' && s.link && /^https?:\/\//.test(s.link) && s.link.includes('#')) {
      return s.link;
    }
  }
  return null;
}
