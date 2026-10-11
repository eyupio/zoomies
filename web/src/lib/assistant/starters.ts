/**
 * The questions the empty conversation offers. Without the fleet to look at
 * they are about Zoomies in general. With it, the first is always the open
 * one, so the entry point is the same every time, and the rest are about what
 * the fleet is doing now: the worst open problem by name, the jobs waiting,
 * the jobs that failed, the host that is unwell. A fixed list of four read the
 * same on a quiet fleet and a burning one; these read differently, which is
 * what makes one worth pressing.
 */

export const GENERAL_STARTERS = [
  'How do labels decide which pool runs a job?',
  'Why might a job sit queued?',
  'How do ephemeral runners work?',
  'What should I check when a host goes unhealthy?',
] as const;

export const FLEET_STARTERS = [
  'How is the fleet doing right now?',
  'Is anything wrong that I should look at?',
  'Which jobs are queued, and why?',
  'How busy are the pools?',
] as const;

/** How many the panel shows: a glance, not a menu. */
const COUNT = 4;

export interface StarterInput {
  fleetAccess: boolean;
  stats: {
    window?: string;
    queued_jobs?: number;
    failed?: number;
    fleet_failed?: number;
  } | null;
  problems: readonly { code: string; severity: string; title: string }[];
  hosts: readonly { name?: string; healthy?: boolean }[];
}

const RANK: Record<string, number> = { error: 0, warning: 1, info: 2 };

export function starterQuestions(input: StarterInput): string[] {
  if (!input.fleetAccess) return [...GENERAL_STARTERS];
  const out: string[] = [FLEET_STARTERS[0]];
  const add = (q: string | undefined) => {
    if (q && !out.includes(q) && out.length < COUNT) out.push(q);
  };
  // The problems, worst first; the drawer's own order is by time, which is
  // not what decides where to look first.
  const problems = [...input.problems].sort(
    (a, b) => (RANK[a.severity] ?? 3) - (RANK[b.severity] ?? 3),
  );
  const [worst, next] = problems;
  if (worst) add(`Explain "${worst.title}" and what to do about it.`);
  const queued = input.stats?.queued_jobs ?? 0;
  if (queued > 0) add(`Why ${queued === 1 ? 'is 1 job' : `are ${queued} jobs`} queued right now?`);
  const failed = input.stats?.failed ?? 0;
  if (failed > 0) {
    const window = windowWords(input.stats?.window);
    add(
      `Why did ${failed === 1 ? '1 job' : `${failed} jobs`} fail ${window}, and was the fleet at fault for any?`,
    );
  }
  const sick = input.hosts.find((h) => h.healthy === false && h.name);
  if (sick) add(`What is wrong with host ${sick.name}?`);
  if (next && next.title !== worst?.title) add(`Explain "${next.title}" and what to do about it.`);
  for (const q of FLEET_STARTERS) {
    // "Is anything wrong?" is answered by the problem already named above it.
    if (worst && q === FLEET_STARTERS[1]) continue;
    add(q);
  }
  return out;
}

/** "in the last hour" for "1h", "in the last 24 hours" for "24h", and so on. */
function windowWords(window: string | undefined): string {
  const m = /^(\d+)([hdm])$/.exec(window ?? '');
  if (!m) return 'recently';
  const n = Number(m[1]);
  const unit = { h: 'hour', d: 'day', m: 'minute' }[m[2] as 'h' | 'd' | 'm'];
  return n === 1 ? `in the last ${unit}` : `in the last ${n} ${unit}s`;
}
