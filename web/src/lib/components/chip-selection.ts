/**
 * How a press on a FilterChips row changes what is in force. Pure, and
 * beside the component rather than in a page's module, because every list
 * page reads it: the Runners and Queue chips hold several values, the Pools
 * and Audit chips hold one.
 */

/** A chip pressed: in the selection if it was out, out if it was in. Empty is All. */
export function toggleChip<T extends string>(selected: readonly T[], value: T): T[] {
  return selected.includes(value)
    ? selected.filter((other) => other !== value)
    : [...selected, value];
}

/**
 * The one value a single-valued filter holds, read from a chip press. The
 * press lands as the value that was not in force before, so a second chip
 * replaces the first; a selection emptied by All, or by pressing the pressed
 * chip again, is no filter.
 */
export function pickOne<T extends string>(current: T | '', next: readonly T[]): T | '' {
  return next.find((value) => value !== current) ?? '';
}
