/**
 * A figure a band carries, and the shape every page hands it.
 *
 * The values arrive already worded (`"12"`, `"1,204"`, the no-value placeholder), as a
 * band's sentence does: the band lays figures out and never decides what a number
 * means, so a page cannot be made to say something its counts do not.
 */
export interface BandStat {
  label: string;
  /** The figure, already formatted. Ignored when `since` is set. */
  value?: string;
  /** An instant to show as a live age ("4m 10s") instead of a fixed figure. */
  since?: string;
  /** One sentence for the tooltip and the accessible description. */
  hint?: string;
  /** Makes the figure a link. */
  href?: string;
  /**
   * The section on this page that holds the figure's evidence. Pressing the
   * figure jumps there, so a band's number is a way into the list under it
   * rather than only a link somewhere else.
   */
  section?: string;
  /** Makes the figure a button, as the Queue's status filters are. */
  onselect?: () => void;
  /** The button is the filter that is on now. */
  active?: boolean;
  /** Drawn in the pending colour on the plain panel; a band carries no status colour. */
  tone?: 'warning';
}

/** The way on to the page's fuller view, drawn at the end of the strip. */
export interface BandMore {
  label: string;
  href: string;
}
