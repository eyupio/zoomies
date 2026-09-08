/**
 * The sections of the product, in the one order they are ever listed in.
 *
 * The order is fixed and matches docs/ui-guidelines.md, because muscle memory
 * is the whole point of a persistent navigation -- and it is shared here rather
 * than written twice, since a phone lists the same sections in two places (the
 * bar along the bottom edge and the menu it opens) and a list that drifts
 * between them is a list an operator cannot learn.
 */
import {
  Boxes,
  ChartNoAxesCombined,
  GitPullRequestArrow,
  HardDrive,
  LayoutDashboard,
  ListChecks,
  Plug,
  ScrollText,
  Server,
  Settings,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';

export interface NavItem {
  path: string;
  label: string;
  icon: LucideIcon;
  /** The second key of the `g` chord. */
  key: string;
  /**
   * Whether the phone's bottom bar carries this one itself. Ten icons across a
   * 412px screen is a row of targets too small and too alike to hit, so the bar
   * keeps the four an operator watches a fleet with and the rest live one press
   * away in the menu.
   */
  primary?: boolean;
}

export const SECTIONS: readonly NavItem[] = [
  { path: '/', label: 'Overview', icon: LayoutDashboard, key: 'o', primary: true },
  { path: '/pools', label: 'Pools', icon: Boxes, key: 'p', primary: true },
  { path: '/runners', label: 'Runners', icon: Server, key: 'r', primary: true },
  { path: '/jobs', label: 'Jobs', icon: ListChecks, key: 'j', primary: true },
  { path: '/usage', label: 'Usage', icon: ChartNoAxesCombined, key: 'u' },
  { path: '/hosts', label: 'Hosts', icon: HardDrive, key: 'h' },
  { path: '/installations', label: 'Installations', icon: Plug, key: 'i' },
  { path: '/migrate', label: 'Migrate', icon: GitPullRequestArrow, key: 'm' },
  { path: '/audit', label: 'Audit', icon: ScrollText, key: 'a' },
  { path: '/settings', label: 'Settings', icon: Settings, key: 's' },
];

/** Whether `path` is the section the address bar is currently inside. */
export function isCurrentSection(path: string, here: string): boolean {
  return path === '/' ? here === '/' : here === path || here.startsWith(`${path}/`);
}
