import type { Problem } from '../api/types';
import { kennelListHref } from '../kennel/pages';

export interface Target {
  href: string;
  label: string;
}

/**
 * Where a host problem's "open" link goes.
 *
 * Kept apart from `ProblemItem.svelte`, which cannot be loaded outside a
 * browser, so the one rule here is tested in Node: the three OS health codes
 * are about what a host's report says, and the page that has the report is the
 * host's own. The two update codes are about one host's agent being updated, and
 * the host's page is where an administrator asks for that. No other host
 * problem belongs there: those are about the agent going quiet, the host's
 * capacity or its folders, and the list is where a host's state and its slots
 * are.
 */
const HOST_PAGE_CODES: ReadonlySet<string> = new Set([
  'host.os_health',
  'host.health_stale',
  'host.reboot_pending',
  'host.update_failed',
  'host.update_helper_stuck',
  'host.update_unavailable',
]);

export function hostTarget(code: string | undefined, id: string): { href: string; label: string } {
  return code !== undefined && HOST_PAGE_CODES.has(code)
    ? { href: `/hosts/${id}`, label: 'Open the host' }
    : { href: '/hosts', label: 'Open hosts' };
}

/**
 * Where a controller problem's "open" link goes, or null for one with no page
 * of its own.
 *
 * A controller problem names no pool, host or setting, so the code is all there
 * is to go by. The notice that a newer release exists is about updating, and the
 * Updates page reads that same release in full and says what the update mode
 * would do about it. A code is added here when it has been given a page, never
 * guessed at: a link to a page that says nothing about what the problem said is
 * worse than no link.
 */
export function controllerTarget(code: string | undefined): { href: string; label: string } | null {
  return code === 'controller.update_available'
    ? { href: '/settings/updates', label: 'Open Updates' }
    : null;
}

/**
 * Where the operator goes next. The pool, the runner, the provider and the
 * machine have pages of their own; the rest land on the list that holds the
 * thing, which is still one click closer than the navigation.
 */
export function problemTarget(p: Problem): Target | null {
  const id = p.target_id;
  if (id) {
    switch (p.target_kind) {
      case 'pool':
        return { href: `/pools/${id}`, label: 'Open the pool' };
      case 'runner':
        return { href: `/runners/${id}`, label: 'Open the runner' };
      case 'host':
        return hostTarget(p.code, id);
      case 'provider':
        return { href: `/providers/${id}`, label: 'Open the provider' };
      case 'ai_context':
        // The card on that page carries the run, the cause and the buttons.
        return { href: '/kennel/ai-context', label: 'Open AI Context' };
      case 'machine':
        // A machine has a page of its own because the answer to "why is this
        // taking so long" is on it: the phase timeline, the last failure and
        // the handle that finds the other half of the story at the provider.
        return { href: `/machines/${id}`, label: 'Open the machine' };
      case 'installation':
        return { href: '/installations', label: 'Open installations' };
      case 'job':
        // The list that holds the thing, already narrowed to it: the
        // unmatched job among the unmatched, the lost runner's job among
        // the failed.
        // The fleet's own half, not everything that failed: this problem is
        // about the jobs this deployment broke, and the list it opens should
        // be the one it is talking about.
        if (p.code === 'jobs.runner_lost')
          return { href: '/jobs?faulted=true', label: 'Open failed jobs' };
        if (p.code === 'jobs.unmatched')
          return { href: '/jobs?unmatched=true', label: 'Open unmatched jobs' };
        return { href: '/jobs', label: 'Open jobs' };
      default:
        break;
    }
  }
  // Kennel Club's exposure problem is one for the whole fleet, so it has no ID:
  // the list of repositories that have an error, where each is named and where
  // the finding, what to change and the button to say it is acceptable are. It
  // counts every repository, quiet ones too, so the list must show every one.
  if (p.target_kind === 'kennel') {
    return {
      href: kennelListHref({ severity: 'error', everything: true }),
      label: 'Open Kennel Club',
    };
  }
  // The setting itself, not the page holding eighty-eight of them. A problem
  // that names a key and then lands somebody on a list they have to search is
  // a problem that has done nine tenths of the work and stopped.
  if (p.setting) {
    return {
      href: `/settings/configuration?setting=${encodeURIComponent(p.setting)}`,
      label: 'Open the setting',
    };
  }
  return controllerTarget(p.code);
}
