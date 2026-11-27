import { ApiError } from './api/client';

/** What a preview page does about a request that failed. */
export interface PreviewFailure {
  /** The sentence the page shows. */
  message: string;
  /** Whether the gate above the page may be stale and should look again. */
  refreshGate: boolean;
  /** When the page's buttons may be used again, if the server limited the rate. */
  retryAt: number | null;
}

/** How long a rate limit blocks the buttons when the server names no time. */
const DEFAULT_BLOCK_MS = 60_000;

/**
 * The part of a failed preview request that does not depend on which preview it
 * was. A preview that is not there is said in the page's own words, because what
 * that means differs (expired, or outside this scope, or the finding gone), and
 * everything else is the server's, because a refusal names the role it needs.
 * A 403 asks the gate to look again; a 429 blocks the buttons until the time the
 * server named, or for a minute.
 */
export function describeFailure(
  e: unknown,
  notFound: string,
  now: number = Date.now(),
): PreviewFailure {
  const api = e instanceof ApiError ? e : null;
  return {
    message:
      api?.isNotFound === true
        ? notFound
        : e instanceof Error
          ? e.message
          : 'The request could not be completed.',
    refreshGate: api?.status === 403,
    retryAt: api?.status === 429 ? (api.retryAt ?? now + DEFAULT_BLOCK_MS) : null,
  };
}
