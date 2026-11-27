/**
 * Event-stream frames laid over a read that was in flight when they arrived.
 *
 * A page asks for its rows, a frame says the fleet has moved, and then the read
 * lands carrying what was true when it was asked. Replacing the rows with it
 * puts back a job that finished, and nothing corrects that: the frame that
 * would have is the one just overwritten. The fleet cache holds its frames back
 * while its own reconcile runs; a panel that fetches for itself does the same
 * through this, by keeping the frames that arrive during a read and applying
 * them on top of what it returns.
 *
 * Frames are kept only while a read is in flight, so nothing accumulates.
 */
export class FrameLayer<T extends { id?: string }> {
  #seq = 0;
  #open = 0;
  #frames = new Map<string, { seq: number; row: T }>();

  /** Reads asked for and neither settled nor ended. */
  get pending(): number {
    return this.#open;
  }

  /** Call when a read is asked for. The mark goes back to `settle`, or to `end` if the read is dropped. */
  begin(): number {
    this.#open += 1;
    return this.#seq;
  }

  /** Record a frame. It matters only to a read that was asked for before it arrived. */
  note(row: T): void {
    if (!row.id || this.#open === 0) return;
    this.#seq += 1;
    this.#frames.set(row.id, { seq: this.#seq, row });
  }

  /**
   * The read's rows with every frame that arrived after `begin` laid on top: a
   * frame replaces the row it names, adds one the read did not return, or takes
   * one away when `belongs` says the frame's row no longer has a place here.
   * Ends the read.
   */
  settle(mark: number, fetched: readonly T[], belongs: (row: T) => boolean): T[] {
    const rows = new Map<string, T>();
    const loose: T[] = [];
    for (const row of fetched) {
      if (row.id) rows.set(row.id, row);
      else loose.push(row);
    }
    for (const [id, frame] of this.#frames) {
      if (frame.seq <= mark) continue;
      if (belongs(frame.row)) rows.set(id, frame.row);
      else rows.delete(id);
    }
    this.end(mark);
    return [...loose, ...rows.values()];
  }

  /** A read that will not settle (aborted, or failed) stops holding frames for itself. */
  end(_mark: number): void {
    this.#open = Math.max(0, this.#open - 1);
    if (this.#open === 0) this.#frames.clear();
  }
}

/**
 * What a row should be once an optimistic change has been refused: the row it
 * was before, but only if the row is still the one the change made. A frame that
 * arrived meanwhile is newer than the row the change started from, and putting
 * that one back would show something the server has already moved on from.
 */
export function restoreUnlessMoved<T>(
  current: T | undefined,
  applied: T,
  previous: T,
): T | undefined {
  return current === applied ? previous : current;
}
