import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';

const LUCIDE = 'zoomies-test:lucide-stub';
const iconNames = (
  /import\s*\{([^}]*)\}\s*from\s*'@lucide\/svelte'/.exec(
    readFileSync(new URL('../src/lib/status.ts', import.meta.url), 'utf8'),
  )?.[1] ?? ''
)
  .split(',')
  .map((name) => name.trim())
  .filter((name) => name !== '');

registerHooks({
  resolve(specifier, context, next) {
    if (specifier === '@lucide/svelte')
      return { url: LUCIDE, format: 'module', shortCircuit: true };
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url === LUCIDE)
      return {
        format: 'module',
        shortCircuit: true,
        source: iconNames.map((name) => `export function ${name}() {}`).join('\n'),
      };
    return next(url, context);
  },
});

const { runnerDisplayStatus } = await import('../src/lib/runners/runner-status.ts');
const { runnerStatus } = await import('../src/lib/status.ts');

test('active boosts and throttles replace busy or idle but retain lifecycle detail', () => {
  for (const state of ['busy', 'idle'] as const) {
    for (const [cpu, label] of [
      ['maximum_zoomies', 'Squirrel spotted'],
      ['zoomies', 'Rabbit spotted'],
      ['throttled', 'Leash tightened'],
    ] as const) {
      const status = runnerDisplayStatus({ state, cpu_resource: { state: cpu } });
      assert.equal(status.label, label);
      assert.equal(status.lifecycle.key, state);
      assert.equal(status.active, true);
    }
  }
});

test('stale CPU allocation never masks startup, draining or terminal states', () => {
  for (const state of ['provisioning', 'registering', 'draining', 'failed', 'removed'] as const) {
    for (const cpu of ['maximum_zoomies', 'zoomies', 'throttled'] as const) {
      const status = runnerDisplayStatus({ state, cpu_resource: { state: cpu } });
      assert.equal(status.key, state);
      assert.equal(status.active, false);
    }
  }
});

test('observation and normal allocations keep the lifecycle label', () => {
  // The lifecycle's own word, read from the state map rather than written
  // here: what is being tested is that a quiet allocation does not replace it,
  // not what the word is, and the runner vocabulary has already changed once
  // under a test that spelt it out.
  for (const cpu of ['observing', 'guaranteed', 'sit_and_stay'] as const) {
    const status = runnerDisplayStatus({ state: 'busy', cpu_resource: { state: cpu } });
    assert.equal(status.label, runnerStatus('busy').label);
    assert.ok(status.cpuDetail);
  }
  assert.equal(
    runnerDisplayStatus({ state: 'registering' }).label,
    runnerStatus('registering').label,
  );
  assert.equal(runnerDisplayStatus({}).label, 'Unknown');
});
