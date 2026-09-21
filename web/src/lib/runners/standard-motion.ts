const STATES = [
  'busy',
  'zoomies',
  'maximum_zoomies',
  'idle',
  'provisioning',
  'registering',
  'throttled',
  'draining',
  'failed',
  'removed',
  'unknown',
] as const;
export type StandardMotionState = (typeof STATES)[number];

/** Stable phases keep a fleet (and each workflow pack) from moving in lockstep. */
export function standardMotion(state: string, seed: string) {
  const motion = STATES.includes(state as StandardMotionState)
    ? (state as StandardMotionState)
    : 'unknown';
  let hash = 2166136261;
  for (const char of seed) hash = Math.imul(hash ^ char.charCodeAt(0), 16777619) >>> 0;
  const fraction = (hash % 1000) / 1000;
  const running = ['busy', 'zoomies', 'maximum_zoomies'].includes(motion);
  const stride =
    (motion === 'maximum_zoomies' ? 0.32 : motion === 'zoomies' ? 0.48 : 0.64) + fraction * 0.02;
  const spin = (motion === 'maximum_zoomies' ? 10 : 16) + fraction * 3;
  return {
    state: motion,
    running,
    spinning: motion === 'maximum_zoomies' || motion === 'zoomies',
    still: ['failed', 'removed', 'unknown'].includes(motion),
    stride,
    phase: -fraction * stride,
    spin,
    spinPhase: -fraction * spin,
    gesture: 6 + fraction * 3,
    blink: 5.9 + fraction * 2,
  };
}
