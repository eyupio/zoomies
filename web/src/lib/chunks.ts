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

/**
 * The controller version a reload has already been spent on. An upgrade is a
 * new reason to reload however recently one was spent; the same upgrade twice
 * is a loop.
 */
const RELOAD_VERSION_KEY = 'zoomies.chunk-reload-version';

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
  let reloadedFor: string | null;
  try {
    spent = Number(sessionStorage.getItem(RELOAD_KEY) ?? 0);
    reloadedFor = sessionStorage.getItem(RELOAD_VERSION_KEY);
  } catch {
    // Private mode, or storage denied. Without somewhere to record the reload
    // there is no way to stop it repeating, so leave it to the operator.
    return false;
  }

  const health = await controllerHealth();
  // Nothing is answering. Reloading now would trade a working shell for the
  // browser's offline page, so the caller's error state is the better answer.
  if (!health) return false;

  // An upgrade is what actually renames chunks, and the controller says which
  // version it is. Seeing a version this tab has not already reloaded for is
  // proof that a reload will help, so it is spent whatever the cooldown says:
  // the cooldown exists to stop a broken chunk looping, and a version that has
  // moved is not that. Without this, an upgrade landing within thirty seconds
  // of any earlier failure left the tab stuck on an error for a page that a
  // single reload would have fixed -- and every other page in that tab still
  // running code the fleet had moved on from.
  const upgraded = health.version !== '' && health.version !== reloadedFor;
  if (!upgraded && Number.isFinite(spent) && now - spent < RELOAD_COOLDOWN) return false;

  try {
    sessionStorage.setItem(RELOAD_KEY, String(now));
    if (health.version !== '') sessionStorage.setItem(RELOAD_VERSION_KEY, health.version);
  } catch {
    return false;
  }
  location.reload();
  return true;
}

/**
 * What the controller is, if it is there at all.
 *
 * A cheap unauthenticated request, uncached, answers two questions at once:
 * whether this phone is online, and which build is now being served. The
 * second is what tells "the upgrade moved the chunk" from "the request was
 * dropped", which are the two things that break an `import()` and want
 * different answers.
 *
 * A response without a version is still a reachable controller -- an older one,
 * or a proxy answering the probe -- so the version is optional and its absence
 * falls back to the cooldown alone.
 */
async function controllerHealth(): Promise<{ version: string } | null> {
  try {
    const res = await fetch('/healthz', { cache: 'no-store', credentials: 'same-origin' });
    if (!res.ok) return null;
    const body: unknown = await res.json().catch(() => null);
    const version =
      body &&
      typeof body === 'object' &&
      typeof (body as { version?: unknown }).version === 'string'
        ? (body as { version: string }).version
        : '';
    return { version };
  } catch {
    return null;
  }
}
