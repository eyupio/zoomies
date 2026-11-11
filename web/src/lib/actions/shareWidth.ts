/**
 * Sharing out the width a table's columns leave unused.
 *
 * A grid whose columns add up to less than its frame used to stop short and
 * leave a blank strip down the right, which on a wide screen read as a page
 * that had not finished drawing. The spare is now given to the columns, but
 * only to the ones the operator has not sized: a column dragged to a width
 * keeps exactly that width, or the resize handle would lie about what it did.
 *
 * Pure, so the arithmetic is read by a unit test rather than inferred from a
 * screenshot. The widths the operator stored are never touched; what this
 * returns is drawn and thrown away, and a later resize starts from it.
 */

export interface WidthColumn {
  /** The width the column is drawn at before any sharing, in pixels. */
  width: number;
  /** The operator dragged this one to its width: it keeps it. */
  hand?: boolean;
  /**
   * A column of controls (the selection tick, the row actions): its width is
   * the controls', and a wider one would only be empty space beside them.
   */
  fixed?: boolean;
}

/**
 * Each column's width once the spare is shared out, in the order given.
 *
 * The spare goes to the columns that are neither fixed nor sized by hand, in
 * proportion to their widths. When every column that could take it has been
 * sized by hand, the last of them that is not fixed takes it all, because a
 * strip at the end is the thing being designed out and one column growing is
 * the smallest change to what the operator chose. Fixed columns never grow.
 *
 * Nothing changes when the columns already fill or overflow the container (the
 * frame scrolls instead), or when the container has no width to speak of yet.
 * Whole pixels go to the columns that are owed the largest fractions, and the
 * last sliver to the last of them, so the total is the container exactly.
 */
export function shareWidth(columns: readonly WidthColumn[], container: number): number[] {
  const widths = columns.map((column) => column.width);
  if (!Number.isFinite(container) || container <= 0 || columns.length === 0) return widths;
  const total = widths.reduce((sum, width) => sum + width, 0);
  const spare = container - total;
  if (!(spare > 0)) return widths;

  let takers = columns.flatMap((column, index) =>
    !column.fixed && !column.hand && column.width > 0 ? [index] : [],
  );
  if (takers.length === 0) {
    const last = columns.findLastIndex((column) => !column.fixed);
    if (last < 0) return widths;
    takers = [last];
  }

  const weight = takers.reduce((sum, index) => sum + Math.max(0, widths[index]!), 0);
  const owed = takers.map((index) =>
    weight > 0 ? (spare * widths[index]!) / weight : spare / takers.length,
  );
  const whole = owed.map((value) => Math.floor(value));
  let left = spare - whole.reduce((sum, value) => sum + value, 0);
  // Largest fraction first; ties go to the earlier column, so the result does
  // not depend on how the sort happens to break them.
  const byFraction = owed
    .map((value, position) => ({ position, fraction: value - Math.floor(value) }))
    .sort((a, b) => b.fraction - a.fraction || a.position - b.position);
  for (const { position } of byFraction) {
    if (left < 1) break;
    whole[position]! += 1;
    left -= 1;
  }
  // Whatever is under a pixel (only when the widths themselves were not whole)
  // goes to the last taker, so the sum lands on the container rather than near it.
  whole[whole.length - 1]! += left;

  takers.forEach((index, position) => {
    widths[index] = widths[index]! + whole[position]!;
  });
  return widths;
}
