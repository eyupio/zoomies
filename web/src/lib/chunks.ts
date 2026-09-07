/**
 * Loading route code over a connection that drops things.
 *
 * Every page but the login form arrives as its own content-hashed chunk,
 * fetched the moment somebody navigates to it. Two ordinary events break that
 * fetch, and neither is a bug in the page being opened:
 *
 *  * A phone on a flaky link -- a VPN reconnecting, a lift, a tunnel -- loses
 *    the request. Nothing is wrong; the bytes stopped arriving.
 *  * The controller was upgraded while the tab sat open. Chunk names carry a
 *    content hash, so the URL the open page holds is gone from the new build
 *    and the server answers 404. Every navigation from that tab fails until it
 *    is reloaded.
 *
 * Both look identical to `import()`, and both are fixed by the thing an
 * operator does out of habit anyway: reload. So this does it for them -- a
 * couple of quiet retries first, then a single reload, and only once the
 * controller has been proven reachable. That last condition is the point: when
 * the network really is down, reloading trades a working shell for the
 * browser's offline page, so the caller's error state is the better answer.
 */

/** How long to wait before each retry, in milliseconds. */
const RETRY_DELAYS = [200, 700] as const;

/**
 * Where a spent reload is recorded, so a chunk that is broken rather than
 * missing cannot put the tab in a reload loop.
 */
const RELOAD_KEY = 'zoomies.chunk-reload';

/** A second failure this soon after a reload is not one a reload fixes. */
const RELOAD_COOLDOWN = 30_000;

const sleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Load a route chunk, retrying a failure that looks transient.
 *
 * @param load the route's `() => import(...)`.
 * @param abandoned true once a later navigation has superseded this one, so a
 *   retry is not spent on a page nobody is waiting for any more.
 */
export async function loadChunk<T>(
  load: () => Promise<T>,
  abandoned: () => boolean = () => false,
): Promise<T> {
  let last: unknown;
  try {
    return await load();
  } catch (cause) {
    last = cause;
  }
  for (const delay of RETRY_DELAYS) {
    if (abandoned()) throw last;
    await sleep(delay);
    if (abandoned()) throw last;
    try {
      return await load();
    } catch (cause) {
      last = cause;
    }
  }
  throw last;
}

/**
 * Reload the page to recover from a chunk that would not load, if a reload is
 * both likely to help and safe to spend.
 *
 * @returns true when a reload is under way, so the caller should keep waiting
 *   rather than rendering an error the operator has no time to read.
 */
export async function reloadForFailedChunk(): Promise<boolean> {
  const now = Date.now();
  let spent: number;
  try {
    spent = Number(sessionStorage.getItem(RELOAD_KEY) ?? 0);
  } catch {
    // Private mode, or storage denied. Without somewhere to record the reload
    // there is no way to stop it repeating, so leave it to the operator.
    return false;
  }
  if (Number.isFinite(spent) && now - spent < RELOAD_COOLDOWN) return false;
  if (!(await controllerReachable())) return false;
  try {
    sessionStorage.setItem(RELOAD_KEY, String(now));
  } catch {
    return false;
  }
  location.reload();
  return true;
}

/**
 * Is the controller answering? A cheap unauthenticated request, uncached, is
 * enough to tell "the upgrade moved the chunk" from "this phone is offline".
 */
async function controllerReachable(): Promise<boolean> {
  try {
    const res = await fetch('/healthz', { cache: 'no-store', credentials: 'same-origin' });
    return res.ok;
  } catch {
    return false;
  }
}
