/**
 * Structured content for the shared tooltip: a title line and a short list of
 * label and value rows, for help that is a handful of figures about one thing
 * (a runner, a host, a job) rather than a sentence.
 *
 * It is data, not markup. The bubble draws it as a heading and a `<dl>`, and
 * every value is put on the page as text, because most of what it carries (a
 * job's, a branch's or a repository's name) is written by whoever can open a
 * pull request. A caller that wants a layout of its own still has the `content`
 * snippet; this is for the common case, so each one does not invent its own.
 */
export interface TipRow {
  label: string;
  value: string;
}

export interface TipDetail {
  title: string;
  rows: TipRow[];
}

/**
 * A card, or the function that works it out. A card that reads a clock (how
 * long a job has run) is given as a function, so it is worked out only while
 * its bubble is open: a wall of two hundred tiles each rebuilding a card every
 * second for a bubble nobody has opened is work for nothing.
 */
export type TipSource = TipDetail | (() => TipDetail);

/** The card itself. Called by the bubble alone, which exists only while it is open. */
export function resolveDetail(source: TipSource): TipDetail {
  return typeof source === 'function' ? source() : source;
}

/** A value with its closing full stop taken off, so a sentence of rows does not double them. */
function bare(value: string): string {
  return value.trim().replace(/\.+$/, '');
}

/**
 * The card as one run of sentences, for assistive technology: the bubble is
 * for the eye, and what it shows has to reach a screen reader too.
 */
export function detailSentence(detail: TipDetail): string {
  const parts = [bare(detail.title), ...detail.rows.map((r) => `${r.label}: ${bare(r.value)}`)];
  return `${parts.filter(Boolean).join('. ')}.`;
}

/** Rows as a sentence each, with no title: the short description an item's own name stands beside. */
export function rowsSentence(rows: readonly TipRow[]): string {
  return rows.length === 0 ? '' : `${rows.map((r) => `${r.label}: ${bare(r.value)}`).join('. ')}.`;
}

/**
 * What a tooltip gives assistive technology. A card given as a function is
 * never called for it: a description that changed every second would be
 * rewritten every second, so such a card needs `text`, its short and steady
 * description, and without one it has none.
 */
export function tipDescription(tip: {
  text?: string;
  reason?: string;
  content?: unknown;
  detail?: TipSource;
}): string {
  if (tip.content) return tip.text ?? '';
  if (tip.detail) {
    if (tip.text) return tip.text;
    return typeof tip.detail === 'function' ? '' : detailSentence(tip.detail);
  }
  return [tip.text, tip.reason].filter(Boolean).join(' ');
}

/** A row only for a value that is known: an unknown one is left out, never drawn empty. */
export function row(label: string, value: string | null | undefined): TipRow[] {
  return value === null || value === undefined || value.trim() === '' ? [] : [{ label, value }];
}
