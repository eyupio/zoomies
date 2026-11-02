/**
 * The one menu for putting a problem away, built here so the drawer and the
 * Overview's card offer the same choices in the same order and a dismissal
 * means the same thing wherever it is made: snooze for a while, snooze every
 * problem of this kind, or dismiss until the controller stops reporting it.
 */
import { Clock, X } from '@lucide/svelte';
import type { Problem } from '../api/types';
import type { MenuItem } from '../components/DropdownMenu.svelte';
import { SNOOZE_OPTIONS } from '../state/notifications.svelte';

export interface DismissHandlers {
  ondismiss?: (problem: Problem) => void;
  onsnooze?: (problem: Problem, ms: number) => void;
  onsnoozetype?: (problem: Problem, ms: number) => void;
}

/** Snooze options first, then dismissing outright: the menu reads as a choice
 * of how long, with "indefinitely" as the last, most drastic one. */
export function dismissMenuItems(problem: Problem, handlers: DismissHandlers): MenuItem[] {
  const { ondismiss, onsnooze, onsnoozetype } = handlers;
  return [
    ...SNOOZE_OPTIONS.map((option) => ({
      id: `snooze-${option.id}`,
      label: `Snooze for ${option.label}`,
      icon: Clock,
      onSelect: () => onsnooze?.(problem, option.ms),
    })),
    ...(onsnoozetype
      ? SNOOZE_OPTIONS.map((option, i) => ({
          id: `snooze-type-${option.id}`,
          label: `Snooze all like this for ${option.label}`,
          icon: Clock,
          separated: i === 0,
          onSelect: () => onsnoozetype(problem, option.ms),
        }))
      : []),
    {
      id: 'dismiss',
      label: 'Dismiss until resolved',
      icon: X,
      separated: true,
      onSelect: () => ondismiss?.(problem),
    },
  ];
}
