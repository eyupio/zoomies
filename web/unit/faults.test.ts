/**
 * The fault vocabulary, which is pure and is read by every page that shows a
 * failure.
 *
 * `faults.ts` deliberately imports no icons, so it loads in Node as-is. That is
 * the whole reason it is a file of its own rather than part of `status.ts`, and
 * this walks the two things a Playwright pass cannot: every category the server
 * can send, and the older-controller case where a job carries the fleet's prose
 * and no domain at all.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  FAULT_KINDS,
  faultDetail,
  faultDomain,
  faultLabel,
  fleetFailed,
} from '../src/lib/faults.ts';

test('every category has a label and a sentence', () => {
  for (const kind of FAULT_KINDS) {
    assert.notEqual(faultLabel(kind), '', `${kind} has no label`);
    assert.notEqual(faultDetail(kind), '', `${kind} has no detail`);
  }
});

test('a category this build does not know reads as itself, never as nothing', () => {
  // A fleet whose controller is newer sends kinds this bundle has never heard
  // of. An empty badge would say the failure had no category, which is the one
  // thing that is not true of it.
  assert.equal(faultLabel('quantum_decoherence'), 'Quantum decoherence');
  assert.equal(faultDetail('quantum_decoherence'), '');
  assert.equal(faultLabel(''), '');
  assert.equal(faultLabel(null), '');
  assert.equal(faultLabel(undefined), '');
});

test("the domain is the server's answer, and the fallback never blames a workflow", () => {
  assert.equal(faultDomain({ fault_domain: 'fleet' }), 'fleet');
  assert.equal(faultDomain({ fault_domain: 'workflow' }), 'workflow');
  assert.equal(faultDomain({ conclusion: 'success' }), '');

  // A job from a controller too old to send the domain carries the prose and
  // the category only. Reading it as the workflow's would hand somebody a bug
  // hunt for a failure this fleet caused, which is the exact mistake the whole
  // split exists to prevent.
  assert.equal(
    faultDomain({ runner_fault: 'runner zoomies-a stopped', conclusion: 'failure' }),
    'fleet',
  );
  assert.equal(faultDomain({ fault_kind: 'out_of_memory' }), 'fleet');
  assert.equal(fleetFailed({ runner_fault: 'runner zoomies-a stopped' }), true);
  assert.equal(fleetFailed({ conclusion: 'failure' }), false);
});

test('a domain the server sends that this build does not know falls back rather than being trusted', () => {
  // Not 'fleet' and not 'workflow', so the fields decide. Trusting an unknown
  // string would put it straight into a comparison that fails every branch and
  // renders nothing.
  assert.equal(faultDomain({ fault_domain: 'cosmic', fault_kind: 'out_of_memory' }), 'fleet');
  assert.equal(faultDomain({ fault_domain: 'cosmic' }), '');
});
