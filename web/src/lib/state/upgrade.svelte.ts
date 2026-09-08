/**
 * Whether the controller has been upgraded under this tab.
 *
 * A tab left open across a deployment goes on running the build it loaded, and
 * nothing tells it otherwise. The chunk recovery in `$lib/chunks` catches the
 * loud half of that -- a route whose code no longer exists 404s, and the tab
 * reloads -- but a tab that already holds every chunk it needs never fails, so
 * it can serve an old page indefinitely. On a phone, where a tab is backgrounded
 * for days rather than closed, that is the ordinary case rather than the odd
 * one: the fleet moves on and the screen in somebody's pocket does not.
 *
 * So the build is re-read when the tab comes back to the foreground, and the
 * next navigation is a full page load rather than a client-side one. The
 * navigation is the safe moment by construction -- it discards the page's state
 * anyway -- so nothing is lost, and an operator sees the current UI without
 * being asked to do anything about it.
 */

import { getMeta } from '$lib/api/client';

/** Where a tab records that it has already moved itself to a newer build. */
const RELOADED_KEY = 'zoomies.upgrade-reloaded';

/** How long after a check before another one is worth a request. */
const RECHECK_AFTER = 60_000;

class Upgrade {
  #booted = '';
  #pending = $state(false);
  #lastCheck = 0;

  /**
   * Record the build this tab is running, from the meta the shell already
   * fetched at boot. Called once; a second call is ignored, because the point
   * of reference is the code in this tab and that never changes.
   */
  note(version: string): void {
    if (this.#booted === '' && version !== '') this.#booted = version;
  }

  /** True once the controller has reported a build this tab is not running. */
  get pending(): boolean {
    return this.#pending;
  }

  /**
   * Ask the controller what it is now.
   *
   * Rate-limited, and silent about failures: a controller that cannot be
   * reached says nothing about whether this tab is current, and an operator
   * on a train does not need to hear about it.
   */
  async check(now: number = Date.now()): Promise<void> {
    if (this.#pending || this.#booted === '') return;
    if (now - this.#lastCheck < RECHECK_AFTER) return;
    this.#lastCheck = now;
    try {
      const meta = await getMeta();
      const version = meta.version ?? '';
      if (version !== '' && version !== this.#booted) this.#pending = true;
    } catch {
      /* as above */
    }
  }

  /**
   * Whether the next navigation should be a full page load, and claim it if so.
   *
   * Once per tab, and the record has to outlive the load it permits -- which is
   * why it is in session storage rather than in this object. A flag on the
   * instance is destroyed by the very reload it granted, so a rollout part way
   * through, or two controllers behind one address on different builds, answers
   * with a new version to each fresh document and every navigation in the tab
   * becomes a page load for as long as the rollout takes.
   *
   * Session storage is per tab and survives a reload, which is exactly the
   * scope wanted: one load to reach the current build, and never a loop.
   * Nowhere to record it is treated as "already claimed", because an old page
   * is a smaller problem than a tab that will not stay still.
   */
  claimReload(): boolean {
    if (!this.#pending) return false;
    try {
      if (sessionStorage.getItem(RELOADED_KEY) !== null) return false;
      sessionStorage.setItem(RELOADED_KEY, '1');
    } catch {
      return false;
    }
    return true;
  }

  /** Start watching. Returns the teardown, for symmetry with the router. */
  listen(): () => void {
    const onVisible = (): void => {
      if (document.visibilityState === 'visible') void this.check();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }
}

export const upgrade = new Upgrade();
