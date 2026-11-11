/**
 * The sentence at the top of the command centre: how the fleet is doing, said
 * once, in words, so a person across the room reads it without reading a
 * number.
 *
 * Pure, like the band sentences: the same snapshot always says the same thing,
 * and the order of precedence below is held by a test. The order is the point.
 * A page that says "Busy and healthy" while the controller has gone quiet, or
 * "Quiet" while a problem is open, is the one failure a wall display cannot
 * afford, so what is least trustworthy is checked first.
 */
import { formatDuration, pluralise } from '../format';
import { QUEUE_HREF } from './links';

export type VerdictKind = 'not-live' | 'attention' | 'queue' | 'busy' | 'quiet' | 'empty';

export interface VerdictPart {
  text: string;
  tone?: 'pending' | 'danger';
  href?: string;
  opens?: 'problems';
}

export interface Verdict {
  kind: VerdictKind;
  headline: string;
  parts: VerdictPart[];
}

export interface VerdictInput {
  /** Whether the event stream is connected: without it no count can be trusted. */
  live: boolean;
  lastHeardAt: number | null;
  now: number;
  hosts: number;
  slots: number;
  busy: number;
  starting: number;
  waiting: number;
  /** Jobs waiting that no free slot on any host could take right now. */
  waitingWithoutSlot: number;
  longestWaitMs: number;
  waitingPool: string | null;
  problems: number;
  firstProblemTarget: string | null;
}

const plain = (text: string): VerdictPart => ({ text });

function working(i: VerdictInput): VerdictPart[] {
  const parts: VerdictPart[] = [];
  if (i.busy > 0) {
    parts.push(
      plain(`${i.busy} of ${i.slots} ${i.busy === 1 ? 'slots is' : 'slots are'} working. `),
    );
  } else {
    parts.push(plain('Nothing is running. '));
  }
  if (i.starting > 0) {
    parts.push(plain(`${i.starting} ${i.starting === 1 ? 'runner is' : 'runners are'} starting. `));
  }
  return parts;
}

function queueParts(i: VerdictInput): VerdictPart[] {
  if (i.waiting <= 0) return [];
  const head: VerdictPart = {
    text: `${i.waiting} ${i.waiting === 1 ? 'job waits' : 'jobs wait'}`,
    tone: 'pending',
    href: QUEUE_HREF,
  };
  let tail = '';
  if (i.longestWaitMs > 0) tail += `, the longest for ${formatDuration(i.longestWaitMs)}`;
  if (i.waitingPool) tail += `, for ${i.waitingPool}`;
  return [head, plain(`${tail}. `)];
}

function problemParts(i: VerdictInput): VerdictPart[] {
  if (i.problems <= 0) return [];
  return [
    {
      text: `${i.problems} ${i.problems === 1 ? 'problem is' : 'problems are'} open`,
      tone: 'danger',
      opens: 'problems',
    },
    plain(i.firstProblemTarget ? `, the first on ${i.firstProblemTarget}. ` : '. '),
  ];
}

/** The sentence is built with a trailing space after each clause; the last has none. */
function trimmed(parts: VerdictPart[]): VerdictPart[] {
  const last = parts[parts.length - 1];
  if (!last || last.href || last.opens) return parts;
  return [...parts.slice(0, -1), { ...last, text: last.text.trimEnd() }];
}

export function verdict(i: VerdictInput): Verdict {
  if (!i.live) {
    // No count here: a number from a stream that has dropped is a guess that
    // looks like a measurement.
    const heard =
      i.lastHeardAt === null
        ? 'Waiting to hear from the controller.'
        : `Last heard from the controller ${formatDuration(Math.max(0, i.now - i.lastHeardAt))} ago.`;
    return { kind: 'not-live', headline: 'Not live.', parts: [plain(heard)] };
  }
  if (i.hosts === 0) {
    return {
      kind: 'empty',
      headline: 'No hosts yet.',
      parts: [
        { text: 'Add a host', href: '/hosts/new' },
        plain(' to give the fleet somewhere to run.'),
      ],
    };
  }
  if (i.problems > 0) {
    return {
      kind: 'attention',
      headline: 'Needs a look.',
      parts: trimmed([...working(i), ...queueParts(i), ...problemParts(i)]),
    };
  }
  if (i.waitingWithoutSlot > 0) {
    return {
      kind: 'queue',
      headline: 'Busy, with a queue.',
      parts: trimmed([...working(i), ...queueParts(i)]),
    };
  }
  if (i.busy > 0 || i.waiting > 0 || i.starting > 0) {
    return {
      kind: 'busy',
      headline: 'Busy and healthy.',
      parts: trimmed([...working(i), ...queueParts(i)]),
    };
  }
  return {
    kind: 'quiet',
    headline: 'Quiet.',
    parts: [plain(`${pluralise(i.slots, 'slot stands', 'slots stand')} ready.`)],
  };
}
