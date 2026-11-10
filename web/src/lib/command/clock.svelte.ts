/**
 * The one-second clock the command centre's running counters read.
 *
 * The shared `onClockTick` in format.ts ticks every ten seconds, which is right
 * for "5 minutes ago" and wrong for a wall display whose elapsed times are
 * seconds, so this is its own interval. It is one timer for the whole page, and
 * `stop` freezes `now` so a page that is not showing does not keep waking.
 */
let now = $state(Date.now());
let timer: ReturnType<typeof setInterval> | null = null;

export const clock = {
  get now(): number {
    return now;
  },
  start(): void {
    if (timer !== null) return;
    now = Date.now();
    timer = setInterval(() => (now = Date.now()), 1000);
  },
  stop(): void {
    if (timer === null) return;
    clearInterval(timer);
    timer = null;
  },
};
