/**
 * The Kennel Club band's picture: one square per repository, worst first,
 * from the overview the page already holds.
 *
 * The overview names only the repositories with something open (up to ten);
 * the rest are counts by state. So the named squares come first, with their
 * findings, and the unnamed ones follow as their state alone. Plain functions,
 * so the squares add up to the band's own counts under a test.
 */
import type { KennelOverview } from '../api/types';

/** What a square says: an error open, warnings only, clean, or not yet looked at. */
export type RepoKind = 'error' | 'warning' | 'clean' | 'pending';

export interface RepoSquare {
  kind: RepoKind;
  /** The repository, where the overview names it. */
  name?: string;
  /** The square in words, for its title. */
  note: string;
}

export interface RepoSquares {
  squares: RepoSquare[];
  /** Repositories past the ones drawn. */
  more: number;
  summary: string;
}

export const MAX_REPO_SQUARES = 48;

export function repoSquares(overview: KennelOverview | null): RepoSquares {
  if (!overview || !overview.enabled || overview.repositories === 0) {
    return { squares: [], more: 0, summary: 'Nothing to draw' };
  }
  const named: RepoSquare[] = overview.attention.map((repo) => {
    const kind: RepoKind = repo.counts.error > 0 ? 'error' : 'warning';
    const parts = [repo.name];
    if (repo.counts.error > 0)
      parts.push(`${repo.counts.error} ${repo.counts.error === 1 ? 'error' : 'errors'}`);
    if (repo.counts.warning > 0)
      parts.push(`${repo.counts.warning} ${repo.counts.warning === 1 ? 'warning' : 'warnings'}`);
    return { kind, name: repo.name, note: parts.join(' · ') };
  });
  const states = overview.states;
  const unnamedAttention = Math.max(0, states.attention - named.length);
  const rest: RepoSquare[] = [
    ...Array.from({ length: unnamedAttention }, () => ({
      kind: 'warning' as const,
      note: 'Needs attention',
    })),
    ...Array.from({ length: states.partial }, () => ({
      kind: 'clean' as const,
      note: 'Partly checked, nothing open',
    })),
    ...Array.from({ length: states.best_in_show }, () => ({
      kind: 'clean' as const,
      note: 'Best in show',
    })),
    ...Array.from({ length: states.pending }, () => ({
      kind: 'pending' as const,
      note: 'Not yet evaluated',
    })),
  ];
  const all = [...named, ...rest];
  const errors = all.filter((s) => s.kind === 'error').length;
  const summary = `${overview.repositories} tracked · ${states.attention} need attention${errors > 0 ? ` · ${errors} with an error` : ''}${states.pending > 0 ? ` · ${states.pending} awaiting a first look` : ''}`;
  return {
    squares: all.slice(0, MAX_REPO_SQUARES),
    more: Math.max(0, all.length - MAX_REPO_SQUARES),
    summary,
  };
}
