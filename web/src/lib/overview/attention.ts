/**
 * The line the Needs attention card shows while it is shut: how many of each
 * severity, worst first, and the worst problem's own title, so the page says
 * what is wrong before anything is opened. A plain function, so the sentence
 * is held to the counts by a test and not by a glance at a screenshot.
 */
import type { Problem, Severity } from '../api/types';
import { pluralise } from '../format';
import { SEVERITY_NOUN } from '../problems/dismissals';

export interface AttentionGroup {
  severity: Severity;
  items: readonly Problem[];
}

/** "1 error and 2 warnings · host demo-1 is cordoned with 1 job queued". */
export function attentionSummary(groups: readonly AttentionGroup[]): string {
  const counts = groups
    .filter((group) => group.items.length > 0)
    .map((group) => pluralise(group.items.length, SEVERITY_NOUN[group.severity]));
  if (counts.length === 0) return '';
  const worst = groups.find((group) => group.items.length > 0)?.items[0];
  const title = worst?.title?.trim();
  const line = counts.join(' and ');
  return title ? `${line} · ${title}` : line;
}
