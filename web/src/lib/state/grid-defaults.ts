/**
 * What a saved grid layout means for a column that is hidden by default.
 *
 * A column hidden by default is drawn only when the grid's `shown` list names
 * it. A layout saved before a column became hidden by default has no such
 * list, and an operator who arranged that grid must see no change when a
 * release trims its defaults, so the grid, which alone knows which of its
 * columns are hidden by default, names them all as shown on its first mount
 * after the upgrade. Every later layout write pins the list, empty if need
 * be, so a grid arranged after the upgrade takes the new default.
 */
import type { GridPrefs } from './prefs.svelte';

/** Whether the operator has arranged this grid: hidden columns, widths or an order. */
export function hasLayout(grid: GridPrefs | undefined): boolean {
  if (!grid) return false;
  return (
    (grid.hidden?.length ?? 0) > 0 ||
    Object.keys(grid.widths ?? {}).length > 0 ||
    (grid.order?.length ?? 0) > 0
  );
}

/**
 * The `shown` list a grid from before the upgrade should carry, or undefined
 * when the record already says what is shown or was never arranged.
 */
export function shownAfterUpgrade(
  grid: GridPrefs | undefined,
  hiddenByDefault: readonly string[],
): string[] | undefined {
  if (!grid || grid.shown !== undefined || !hasLayout(grid)) return undefined;
  return [...hiddenByDefault];
}

/** The record a layout write leaves: with its `shown` list pinned. */
export function pinShown(grid: GridPrefs | undefined): GridPrefs {
  return { ...grid, shown: grid?.shown ?? [] };
}
