/**
 * Full screen for the command centre, and the screen wake lock that goes with it.
 *
 * A browser grants full screen only to a press, so nothing here enters it by
 * itself: `toggle` is what the page's button calls. While full screen holds,
 * the page also holds a `screen` wake lock where the browser offers one,
 * because a wall display that dims after ten minutes is a wall display nobody
 * can read. The lock follows full screen rather than the page: it is taken
 * when full screen begins and let go when it ends, whichever way it ends
 * (the button, Exit, or the browser's own Esc).
 */

/** Whether this browser, in this frame, lets a page ask for full screen at all. */
const supported = typeof document !== 'undefined' && document.fullscreenEnabled === true;

let active = $state(typeof document !== 'undefined' && document.fullscreenElement !== null);
let lock: WakeLockSentinel | null = null;
/**
 * The request in flight. `fullscreenchange` and `visibilitychange` can both
 * fire before it answers, and a second request would leave a sentinel that
 * nothing ever releases, holding the screen awake after full screen ends.
 */
let requesting: Promise<void> | null = null;

function holdLock(): Promise<void> {
  if (lock || typeof navigator === 'undefined' || !('wakeLock' in navigator)) {
    return Promise.resolve();
  }
  requesting ??= requestLock().finally(() => {
    requesting = null;
  });
  return requesting;
}

async function requestLock(): Promise<void> {
  try {
    const sentinel = await navigator.wakeLock.request('screen');
    // Full screen may have ended while the request was in flight, or a lock
    // may already be held.
    if (!active || lock) {
      void sentinel.release();
      return;
    }
    lock = sentinel;
    sentinel.addEventListener('release', () => {
      if (lock === sentinel) lock = null;
    });
  } catch {
    // Refused (a low battery, a policy, a hidden tab). The page works the same
    // without it; the screen may simply dim.
  }
}

function dropLock(): void {
  const held = lock;
  lock = null;
  if (held) void held.release();
}

if (typeof document !== 'undefined') {
  document.addEventListener('fullscreenchange', () => {
    active = document.fullscreenElement !== null;
    if (active) void holdLock();
    else dropLock();
  });
  // The browser lets go of a wake lock whenever the tab is hidden, and does
  // not take it back. A wall display whose tab was switched away from and
  // back would otherwise dim for good while still full screen.
  document.addEventListener('visibilitychange', () => {
    if (active && document.visibilityState === 'visible') void holdLock();
  });
}

export const fullscreen = {
  /** Whether the full-screen control should be offered at all. */
  supported,
  /** Whether the document is full screen now, whoever put it there. */
  get active(): boolean {
    return active;
  },
  /**
   * Enter full screen on `el`, or leave it. A refusal leaves things as they
   * were: the button simply does nothing, which is what the browser decided.
   */
  async toggle(el: HTMLElement): Promise<void> {
    if (!supported) return;
    if (document.fullscreenElement) {
      await fullscreen.exit();
      return;
    }
    try {
      await el.requestFullscreen();
    } catch {
      // Refused, usually because the press that asked has expired.
    }
  },
  /** Leave full screen if the document is in it; a no-op otherwise. */
  async exit(): Promise<void> {
    if (typeof document === 'undefined' || !document.fullscreenElement) return;
    try {
      await document.exitFullscreen();
    } catch {
      // Already on its way out.
    }
  },
};
