import { test } from 'node:test';
import assert from 'node:assert/strict';
import { toolLabel } from '../src/lib/assistant/tools.ts';

test('a tool is called by what it shows, and one nobody named is its own words', () => {
  assert.equal(toolLabel('list_runners'), 'runners');
  assert.equal(toolLabel('get_runner_log'), 'a runner’s log');
  assert.equal(toolLabel('some_new_tool'), 'some new tool');
});

test('the provider tools are named for what a person would call them', () => {
  assert.equal(toolLabel('list_providers'), 'infrastructure providers');
  assert.equal(toolLabel('provider_pairings'), 'which providers serve which pools');
  assert.equal(toolLabel('list_machines'), 'rented machines');
});

test('the catalog is named as what it is, not by its route', () => {
  assert.equal(toolLabel('get_catalog'), 'the catalog of codes and checks');
  assert.equal(toolLabel('get_usage'), 'usage and cost');
});

// The window's tools read as what a person would ask about, so a look at
// list_workflow_runs is not shown as "Looked at list workflow runs".
test('the fleet window is named for what it shows', () => {
  assert.equal(toolLabel('recent_events'), 'what changed lately');
  assert.equal(toolLabel('wait_for_change'), 'the next change');
  assert.equal(toolLabel('scaling_decisions'), 'the scheduler’s decisions');
  assert.equal(toolLabel('host_samples'), 'hosts minute by minute');
  assert.equal(toolLabel('list_workflow_runs'), 'workflow runs');
});

// The four analyses each answer one question, and the chat names the question
// rather than the tool, as the cards on the pages do.
test('the analysis tools are named for the question each answers', () => {
  assert.equal(toolLabel('queue_wait_breakdown'), 'why jobs waited');
  assert.equal(toolLabel('compare_periods'), 'what got worse or better');
  assert.equal(toolLabel('host_pressure'), 'whether a host’s failures came under pressure');
  assert.equal(toolLabel('startup_failures'), 'what keeps failing to start');
  assert.equal(toolLabel('recurring_failures'), 'which jobs keep failing');
});
