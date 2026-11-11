/**
 * A busy fleet for the command centre, written over the fixture's reads.
 *
 * The fixture is a small, healthy fleet, so a page that has to be checked
 * against the size somebody actually runs (a handful of large hosts, a queue,
 * something wrong) is given one here. The stream is taken away first: every
 * read rewritten here is also carried by a frame, and a frame that landed
 * would put the fixture's fleet back.
 */
import type { Page } from '@playwright/test';
import type { Host, Job, Problem, Runner } from '../../src/lib/api/types';
import { readThrough, withoutTheStream } from './fixtures';

export interface BusyFleet {
  hosts: number;
  slots: number;
  /** Busy runners on each host; the rest of its slots are free. */
  busy?: number;
  waiting: number;
  /** What the queue's read reports as its total, when that is more than it sends. */
  waitingTotal?: number;
}

const pad = (i: number) => String(i).padStart(2, '0');

/** A problem to stand in when the fixture has none open, so the rail has all three lists. */
const PLANTED: Problem = {
  code: 'host.planted',
  severity: 'warning',
  audience: 'fleet',
  title: 'A planted problem, so Needs attention is drawn',
  detail: 'Written by the test, not by the controller.',
};

/** Make sure at least one problem is open, without the stream to take it away. */
export async function plantProblem(page: Page): Promise<void> {
  await withoutTheStream(page);
  await page.route('**/api/v1/problems', async (route) => {
    const read = await readThrough<{ items?: Problem[] }>(route);
    if (!read) return;
    const items = read.body.items?.length ? read.body.items : [PLANTED];
    return route.fulfill({ response: read.response, json: { ...read.body, items } });
  });
}

export async function busyFleet(page: Page, fleet: BusyFleet): Promise<void> {
  await plantProblem(page);
  const ids = Array.from({ length: fleet.hosts }, (_, i) => `hst_busy${pad(i)}`);
  await page.route('**/api/v1/hosts*', async (route) => {
    const read = await readThrough<{ items?: Host[] }>(route);
    if (!read) return;
    const template = read.body.items![0]!;
    const items: Host[] = ids.map((id, i) => ({
      ...template,
      id,
      name: `busy-${pad(i)}`,
      slots: fleet.slots,
      capacity: fleet.slots,
      healthy: true,
      retired: false,
      cordoned: false,
    }));
    return route.fulfill({ response: read.response, json: { ...read.body, items } });
  });
  await page.route('**/api/v1/runners*', async (route) => {
    const read = await readThrough<{ items?: Runner[] }>(route);
    if (!read) return;
    const busy = read.body.items!.find((runner) => runner.state === 'busy' && runner.current_job)!;
    const items: Runner[] = ids.flatMap((host_id, i) =>
      Array.from({ length: fleet.busy ?? Math.ceil(fleet.slots / 2) }, (_, slot) => ({
        ...busy,
        id: `run_busy${pad(i)}${pad(slot)}`,
        name: `busy-${pad(i)}-${pad(slot)}`,
        host_id,
        state: 'busy' as const,
      })),
    );
    return route.fulfill({
      response: read.response,
      json: { ...read.body, items, total: items.length },
    });
  });
  await page.route(
    (url) => url.pathname === '/api/v1/provisioning',
    async (route) => {
      const read = await readThrough<{ items?: Job[]; total?: number }>(route);
      if (!read) return;
      const template = read.body.items![0]!;
      const queuedAt = Date.now() - 20 * 60_000;
      const items: Job[] = Array.from({ length: fleet.waiting }, (_, i) => ({
        ...template,
        id: `job_wait${pad(i)}`,
        job_name: `waiting job ${pad(i)}`,
        state: 'queued',
        queued_at: new Date(queuedAt + i * 1000).toISOString(),
      }));
      return route.fulfill({
        response: read.response,
        json: { ...read.body, items, total: fleet.waitingTotal ?? items.length },
      });
    },
  );
}
