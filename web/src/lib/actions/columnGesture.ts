/**
 * The two column gestures -- pick a heading up and move it, and pull a column's
 * edge -- driven by pointer events, so a finger can do both.
 *
 * Reordering used to be the HTML5 drag-and-drop protocol, which a touchscreen
 * does not speak: a browser raises `dragstart` for a mouse and a stylus, and
 * from a finger only after a long press on Android and never on iOS. So a
 * column order was a preference an operator could set with a mouse and not with
 * a finger, on the one phone layout -- Rows -- that has columns to arrange at
 * all. Pointer events are one code path for mouse, pen and touch alike, which
 * is also one behaviour to keep working rather than two.
 *
 * Resizing was already pointer-driven but listened on the document and never
 * captured the pointer, which holds up for a mouse and not for a finger: a
 * touch that the browser takes over mid-drag (a back-swipe, a second finger, a
 * notification) raises `pointercancel` and no `pointerup`, leaving the listener
 * attached and the column following a finger that had long since let go.
 */

/**
 * How far a pointer travels before a press on the grip becomes a drag.
 *
 * A finger never presses a 24px target without moving a pixel or two, so
 * without a threshold every tap on the grip would reorder the columns.
 */
const DRAG_THRESHOLD_PX = 4;

/**
 * The column under a point, or the empty string if the point is not over one.
 *
 * `elementsFromPoint` rather than `elementFromPoint`: the heading carries the
 * grip and the resize handle on top of it, and the one nearest the pointer is
 * whichever control it happens to be over. Searching down the stack finds the
 * heading under them either way.
 */
function columnAt(root: HTMLElement, x: number, y: number): string {
  for (const element of document.elementsFromPoint(x, y)) {
    if (!root.contains(element)) continue;
    const cell = element.closest<HTMLElement>('[data-table-column]');
    const id = cell?.dataset.tableColumn;
    if (id) return id;
  }
  return '';
}

/**
 * Hold the pointer for the rest of the gesture, so the drag survives the finger
 * leaving the control it started on.
 *
 * Tolerated when it fails: a synthetic pointer dispatched by a test has no
 * capture to take, and a gesture that works without capture is better than a
 * gesture that throws before it begins.
 */
function capture(node: HTMLElement, pointerId: number): void {
  try {
    node.setPointerCapture(pointerId);
  } catch {
    /* as above */
  }
}

export interface ColumnDragHandlers {
  /** The column the pointer is over now, or '' for none. Called as it moves. */
  over(id: string): void;
  /** Where the pointer was lifted, after a drag that really began. */
  drop(id: string): void;
  /**
   * Nothing happened: a press that never travelled far enough to be a drag, or
   * one the browser took the pointer back from. Told apart from a drop so that
   * an interrupted drag abandons the column rather than posting it wherever the
   * finger last happened to be.
   */
  cancel(): void;
}

/**
 * Drag a column by its grip, reporting what the pointer is over as it moves.
 *
 * `root` bounds the hit test to this table: two grids on one page must not be
 * able to drop a column into each other.
 */
export function startColumnDrag(
  event: PointerEvent,
  root: HTMLElement,
  handlers: ColumnDragHandlers,
): void {
  // A secondary touch is a second finger on the screen, not a second drag.
  if (!event.isPrimary || (event.pointerType === 'mouse' && event.button !== 0)) return;
  const grip = event.currentTarget as HTMLElement;
  event.preventDefault();
  event.stopPropagation();
  // preventDefault suppresses the focus the press would have moved, and the
  // grip is also the keyboard control for the same job -- so it takes focus
  // explicitly, and the arrow keys carry on from wherever the drag ended.
  grip.focus();
  capture(grip, event.pointerId);

  const startX = event.clientX;
  const startY = event.clientY;
  // One abort for the three listeners, so there is one way for the gesture to
  // end and no way to leave two of them attached.
  const gesture = new AbortController();
  const { signal } = gesture;
  let dragging = false;

  const finish = (target: string | null): void => {
    gesture.abort();
    if (target === null) handlers.cancel();
    else handlers.drop(target);
  };

  grip.addEventListener(
    'pointermove',
    (move: PointerEvent) => {
      if (move.pointerId !== event.pointerId) return;
      if (
        !dragging &&
        Math.abs(move.clientX - startX) < DRAG_THRESHOLD_PX &&
        Math.abs(move.clientY - startY) < DRAG_THRESHOLD_PX
      )
        return;
      dragging = true;
      move.preventDefault();
      handlers.over(columnAt(root, move.clientX, move.clientY));
    },
    { signal },
  );
  grip.addEventListener(
    'pointerup',
    (up: PointerEvent) => {
      if (up.pointerId !== event.pointerId) return;
      // A press that never became a drag is a press: reporting a target the
      // operator never aimed at is how a stray tap rearranges a table.
      finish(dragging ? columnAt(root, up.clientX, up.clientY) : null);
    },
    { signal },
  );
  grip.addEventListener(
    'pointercancel',
    (cancel: PointerEvent) => {
      if (cancel.pointerId === event.pointerId) finish(null);
    },
    { signal },
  );
}

export interface ColumnResizeHandlers {
  /** The width the pointer is asking for, as it moves. */
  move(width: number): void;
  /** The gesture is over; the last width offered is the one to keep. */
  done(): void;
}

/**
 * Pull a column's trailing edge, from `startWidth` as the pointer went down.
 *
 * A cancelled gesture keeps the width it had reached rather than snapping back:
 * the operator was dragging towards a width, and an interruption they did not
 * ask for is a poor reason to throw away the part of it they had.
 */
export function startColumnResize(
  event: PointerEvent,
  startWidth: number,
  handlers: ColumnResizeHandlers,
): void {
  if (!event.isPrimary || (event.pointerType === 'mouse' && event.button !== 0)) return;
  const handle = event.currentTarget as HTMLElement;
  event.preventDefault();
  event.stopPropagation();
  handle.focus();
  capture(handle, event.pointerId);

  const startX = event.clientX;
  const gesture = new AbortController();
  const { signal } = gesture;
  const finish = (ended: PointerEvent): void => {
    if (ended.pointerId !== event.pointerId) return;
    gesture.abort();
    handlers.done();
  };

  handle.addEventListener(
    'pointermove',
    (move: PointerEvent) => {
      if (move.pointerId !== event.pointerId) return;
      move.preventDefault();
      handlers.move(startWidth + move.clientX - startX);
    },
    { signal },
  );
  handle.addEventListener('pointerup', finish, { signal });
  handle.addEventListener('pointercancel', finish, { signal });
}
