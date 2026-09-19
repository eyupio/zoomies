/**
 * What the Overview's feed can tell an operator, and which of it a browser
 * that has never been asked shows.
 *
 * The feed used to be the scheduler's decisions and nothing else, which made
 * it the only live history in the product and left every other thing worth
 * knowing -- a host that stopped answering, a machine that never became one,
 * a job whose runner died under it -- to be found by opening the page it
 * belongs to. Those are now categories here, and each is a switch on
 * Settings -> Events, because "what belongs on my dashboard" is a question
 * only the operator can answer: a fleet renting machines wants them, a fleet
 * of three fixed hosts never will.
 *
 * Two rules keep the list honest. A category is an *area of the fleet*, not an
 * event kind on the wire -- an operator thinks "hosts", not "host.updated" --
 * and a category never reports a frame that carries no news, so turning one on
 * cannot flood the panel with heartbeats. What makes a frame news is in
 * `changes.ts`.
 */
import {
  Boxes,
  Play,
  Plug,
  ScrollText,
  Server,
  ServerCog,
  TrendingUp,
  TriangleAlert,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';

export type FeedCategoryID =
  'scaling' | 'runners' | 'jobs' | 'hosts' | 'machines' | 'pools' | 'github' | 'problems' | 'audit';

export interface FeedCategory {
  id: FeedCategoryID;
  /** What the settings row calls it. */
  label: string;
  /** One line saying what an entry of this kind is. */
  description: string;
  icon: LucideIcon;
  /** Whether a browser that has never been told shows it. */
  on: boolean;
  /**
   * Whether the feed can show this category's past when a tab opens cold, or
   * only what happens from now on. It is said on the settings page because a
   * category that is on and empty otherwise looks exactly like one that is
   * broken.
   */
  history: boolean;
}

/**
 * The categories, in the order the settings page lists them and the feed's
 * filter offers them: what the fleet decided, then what went wrong, then the
 * things that change underneath it, then the record of who changed them.
 */
export const FEED_CATEGORIES: readonly FeedCategory[] = [
  {
    id: 'scaling',
    label: 'Scaling decisions',
    description:
      'Every time the scheduler creates or removes runners, in its own words: “scaled linux-x64 2 → 4: 3 jobs queued > 30s”.',
    icon: TrendingUp,
    on: true,
    history: true,
  },
  {
    id: 'runners',
    label: 'Runner failures',
    description:
      'A runner that stopped unexpectedly, with what to do about it. Ordinary lifecycle — provisioning, idle, busy, gone — stays on the Runners page.',
    icon: TriangleAlert,
    on: true,
    history: true,
  },
  {
    id: 'jobs',
    label: 'Job failures',
    description:
      'A job this fleet had a hand in that failed, and the step it stopped at. A job whose runner died under it is named as the fleet’s failure rather than the workflow’s.',
    icon: Play,
    on: true,
    history: true,
  },
  {
    id: 'hosts',
    label: 'Hosts',
    description:
      'A host that joined, stopped answering, came back, was cordoned, or that the fleet throttled after sustained pressure.',
    icon: Server,
    on: true,
    history: false,
  },
  {
    id: 'machines',
    label: 'Rented machines',
    description:
      'Machines a provider is renting you: one being created, one that became a host, one that failed or was quarantined, and a provider paused or unreachable.',
    icon: ServerCog,
    on: true,
    history: false,
  },
  {
    id: 'pools',
    label: 'Pool changes',
    description: 'A pool created, changed or deleted — the fleet’s shape, rather than its weather.',
    icon: Boxes,
    on: true,
    history: false,
  },
  {
    id: 'github',
    label: 'GitHub connection',
    description:
      'An App installation that started or stopped working, and webhook deliveries GitHub sent that this controller refused.',
    icon: Plug,
    on: true,
    history: false,
  },
  {
    id: 'problems',
    label: 'Problems raised',
    description:
      'Each problem the moment it is first reported. Off by default: the bell in the top bar already carries them, and this is the same list arriving twice.',
    icon: TriangleAlert,
    on: false,
    history: false,
  },
  {
    id: 'audit',
    label: 'Who changed what',
    description:
      'Every recorded action and who took it. Off by default: on a fleet with automation against the API it is the busiest thing here, and the Audit page has all of it with filters.',
    icon: ScrollText,
    on: false,
    history: true,
  },
];

/** The category an id names, or undefined for one this build does not have. */
export function feedCategory(id: string): FeedCategory | undefined {
  return FEED_CATEGORIES.find((category) => category.id === id);
}
