/**
 * The sections of the product, in the one order they are ever listed in.
 *
 * The order is fixed and matches docs/ui-guidelines.md, because muscle memory
 * is the whole point of a persistent navigation -- and it is shared here rather
 * than written twice, since a phone lists the same sections in two places (the
 * bar along the bottom edge and the sheet it opens) and a list that drifts
 * between them is a list an operator cannot learn.
 *
 * The sidebar reads the sections in groups: what the fleet is doing, where it
 * runs, how it is joined to GitHub, and who looks after it. The groups change
 * nothing about the order -- they are headings over the same list, so `g j` is
 * still `g j` -- but twelve entries under four words are found by neighbourhood
 * rather than read from the top each time.
 */
import {
  Boxes,
  ChartNoAxesCombined,
  Cloud,
  GitPullRequestArrow,
  HardDrive,
  LayoutDashboard,
  ListOrdered,
  Plug,
  ScrollText,
  Server,
  Settings,
  Trophy,
  Workflow,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';

export type NavGroupName = 'Fleet' | 'Infrastructure' | 'GitHub' | 'Administration';

export interface NavItem {
  path: string;
  label: string;
  icon: LucideIcon;
  /** The second key of the `g` chord. */
  key: string;
  /**
   * What the page is for, in one short sentence: the page's own subtitle, cut
   * down to fit a bubble beside the rail. It is what the shared tooltip says
   * when the entry is hovered or focused, which matters most collapsed, where
   * the rail shows an icon and nothing else.
   */
  hint: string;
  /**
   * Whether the phone's bottom bar carries this one itself. Ten icons across a
   * 412px screen is a row of targets too small and too alike to hit, so the bar
   * keeps the four an operator watches a fleet with and the rest live one press
   * away in the menu.
   */
  primary?: boolean;
  /** The heading the sidebar lists it under. The Overview stands alone. */
  group?: NavGroupName;
}

export const SECTIONS: readonly NavItem[] = [
  {
    path: '/',
    label: 'Overview',
    icon: LayoutDashboard,
    key: 'o',
    hint: 'What this fleet is doing right now.',
    primary: true,
  },
  {
    path: '/pools',
    label: 'Pools',
    icon: Boxes,
    key: 'p',
    hint: 'What labels your runners answer to, and how many exist.',
    primary: true,
    group: 'Fleet',
  },
  {
    path: '/runners',
    label: 'Runners',
    icon: Server,
    key: 'r',
    hint: 'Every runner the controller knows about, and what it is doing.',
    primary: true,
    group: 'Fleet',
  },
  {
    path: '/queue',
    label: 'Queue',
    icon: ListOrdered,
    key: 'q',
    hint: 'Shape runner demand. Keep the right work moving.',
    group: 'Fleet',
  },
  {
    path: '/workflows',
    label: 'Workflows',
    icon: Workflow,
    key: 'w',
    hint: 'Workflow runs, and where each of their jobs is.',
    primary: true,
    group: 'Fleet',
  },
  {
    path: '/usage',
    label: 'Usage',
    icon: ChartNoAxesCombined,
    key: 'u',
    hint: 'Runner capacity and job activity over a range of time.',
    group: 'Fleet',
  },
  {
    path: '/hosts',
    label: 'Hosts',
    icon: HardDrive,
    key: 'h',
    hint: 'The machines that run runners, and the room each has left.',
    group: 'Infrastructure',
  },
  {
    path: '/providers',
    label: 'Providers',
    icon: Cloud,
    key: 'v',
    hint: 'Where machines are rented from.',
    group: 'Infrastructure',
  },
  {
    path: '/installations',
    label: 'Installations',
    icon: Plug,
    key: 'i',
    hint: 'The GitHub App connections Zoomies uses.',
    group: 'GitHub',
  },
  {
    path: '/repairs',
    label: 'Eli repairs',
    icon: GitPullRequestArrow,
    key: 'e',
    hint: 'Catch failed PR checks and review repair offers.',
    group: 'GitHub',
  },
  {
    path: '/migrate',
    label: 'Migrate',
    icon: GitPullRequestArrow,
    key: 'm',
    hint: 'Rewrite runs-on in your workflows, by pull request.',
    group: 'GitHub',
  },
  {
    path: '/kennel',
    label: 'Kennel Club',
    icon: Trophy,
    key: 'k',
    hint: 'How your repositories measure up against what affects CI.',
    group: 'GitHub',
  },
  {
    path: '/audit',
    label: 'Audit',
    icon: ScrollText,
    key: 'a',
    hint: 'Every change made through Zoomies, and who made it.',
    group: 'Administration',
  },
  {
    path: '/settings',
    label: 'Settings',
    icon: Settings,
    key: 's',
    hint: 'Your account, users, tokens, backups and updates.',
    group: 'Administration',
  },
];

export interface NavGroup {
  /** Null for the sections that stand alone at the top. */
  label: NavGroupName | null;
  items: NavItem[];
}

/**
 * The pages a public view's visitor is not offered. Each is about the people
 * who run the fleet or a change to it, the API refuses a visitor every one of
 * them, and a link that can only lead to "sign in to see this" is a link the
 * visitor learns to distrust along with the ones that work.
 */
const HIDDEN_FROM_VISITORS: readonly string[] = [
  '/repairs',
  '/migrate',
  '/kennel/ai-context',
  '/ai-context',
  '/audit',
  '/settings',
];

/** Whether a visitor is kept away from `path` and everything under it. */
export function hiddenFromVisitors(path: string): boolean {
  return HIDDEN_FROM_VISITORS.some((hidden) => path === hidden || path.startsWith(`${hidden}/`));
}

/** The sections this person is offered. */
export function sectionsFor(visitor: boolean): readonly NavItem[] {
  return visitor ? SECTIONS.filter((item) => !hiddenFromVisitors(item.path)) : SECTIONS;
}

function grouped(items: readonly NavItem[]): NavGroup[] {
  return items.reduce<NavGroup[]>((groups, item) => {
    const label = item.group ?? null;
    const last = groups[groups.length - 1];
    if (last && last.label === label) last.items.push(item);
    else groups.push({ label, items: [item] });
    return groups;
  }, []);
}

/** The sections as the sidebar lists them: consecutive entries under one heading. */
export const NAV_GROUPS: readonly NavGroup[] = grouped(SECTIONS);

/** The sidebar's groups for this person, without a heading left empty. */
export function navGroupsFor(visitor: boolean): readonly NavGroup[] {
  return visitor ? grouped(sectionsFor(true)) : NAV_GROUPS;
}

/**
 * Pages that draw their own breadcrumb, and so must not be given a second one
 * by the top bar.
 *
 * A detail page carries the trail in its own header, with the link back to the
 * section and the page's name: the top bar repeating it put the same words on
 * screen twice, and on a phone it was the top bar's copy that had to be cut to
 * "Settings › App…". These are patterns rather than a list of addresses because
 * a detail page is every id, and a new one should not have to remember to join
 * a list to avoid the duplication.
 */
const OWNS_ITS_BREADCRUMB: readonly RegExp[] = [
  /^\/hosts\/(new|[^/]+)$/,
  /^\/pools\/(new|[^/]+)$/,
  /^\/runners\/[^/]+$/,
  /^\/providers\/(new|[^/]+)$/,
  /^\/machines\/[^/]+$/,
  /^\/repairs\/(new|setup|[^/]+)$/,
  /^\/ai-context\/setup$/,
];

/** Whether the page draws the trail itself, and the top bar should keep quiet. */
export function ownsItsBreadcrumb(path: string): boolean {
  return OWNS_ITS_BREADCRUMB.some((pattern) => pattern.test(path));
}

/**
 * Pages that belong to a section without living under its path. The Jobs
 * list is the Workflows page one step down -- the same runs, opened out to
 * every job -- so the sidebar keeps Workflows lit while an operator is there.
 * AI Context lives under Kennel Club now; its old addresses still open the same
 * pages, and the sidebar lights the section they belong to from either.
 */
const WITHIN: Readonly<Record<string, string>> = {
  '/jobs': '/workflows',
  '/ai-context': '/kennel',
  '/ai-context/setup': '/kennel',
};

/** Whether `path` is the section the address bar is currently inside. */
export function isCurrentSection(path: string, here: string): boolean {
  const section = WITHIN[here] ?? here;
  return path === '/' ? section === '/' : section === path || section.startsWith(`${path}/`);
}
