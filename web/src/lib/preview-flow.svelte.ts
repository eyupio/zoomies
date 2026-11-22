import { describeFailure } from './preview-failure';

/**
 * The state every preview page keeps about the request it has in flight.
 *
 * Five pages (cache, checkout, sharding, readiness and the Kennel fix) each wrote
 * the same few variables and the same few functions out, and a change to how one
 * handled a rate limit had to be made five times. What is the same is here; what
 * is each page's own (its inputs, its API calls, what it does with an answer) is
 * not.
 *
 * Only one request is live at a time. `begin` abandons the one before it, and a
 * page checks `isCurrent` after every await so an answer that arrives late for a
 * request that was replaced changes nothing.
 */
export class PreviewFlow {
  /** A request is in flight. */
  busy = $state(false);
  /** The sentence for the request that last failed, or empty. */
  error = $state('');
  /** When the server said the buttons may be used again, and whether they may not be yet. */
  retryAt = $state(0);
  retryBlocked = $state(false);
  /** Counts the 403s, so a gate that watches it looks again. */
  gateRefresh = $state(0);

  #operation = 0;
  #controller: AbortController | undefined;
  #timer: ReturnType<typeof setTimeout> | undefined;
  readonly #notFound: string;

  /** `notFound` is what the page says for a preview that is not there. */
  constructor(notFound: string) {
    this.#notFound = notFound;
  }

  /** The signal to hand the request, which `begin` or `cancel` aborts. */
  get signal(): AbortSignal | undefined {
    return this.#controller?.signal;
  }

  /** Start a request: abandon the last one, clear the error, and say which this is. */
  begin(): number {
    const current = ++this.#operation;
    this.#controller?.abort();
    this.#controller = new AbortController();
    this.busy = true;
    this.error = '';
    return current;
  }

  /** Whether `operation` is still the request the page is waiting on. */
  isCurrent(operation: number): boolean {
    return operation === this.#operation;
  }

  /** Record a failure, if its request is still the live one. */
  fail(e: unknown): void {
    const failure = describeFailure(e, this.#notFound);
    this.error = failure.message;
    if (failure.refreshGate) this.gateRefresh++;
    if (failure.retryAt !== null) {
      this.retryAt = failure.retryAt;
      this.retryBlocked = true;
      clearTimeout(this.#timer);
      this.#timer = setTimeout(
        () => {
          this.retryBlocked = false;
        },
        Math.max(0, failure.retryAt - Date.now()),
      );
    }
  }

  /** The request is over, if it is still the live one. */
  settle(operation: number): void {
    if (this.isCurrent(operation)) this.busy = false;
  }

  /** Abandon whatever is in flight and any wait on a rate limit; for the page's teardown. */
  cancel(): void {
    this.#operation++;
    this.#controller?.abort();
    clearTimeout(this.#timer);
  }
}
