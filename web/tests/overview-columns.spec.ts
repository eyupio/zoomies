import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';
test.use({ viewport: { width: 1440, height: 900 } });
// The two columns of the Overview's work area end on one line: a shorter events card
// used to stop short of Pools and Active jobs beside it and leave a ragged edge.
test('the recent events card is as tall as Pools and Active jobs beside it', async ({ page }) => {
  await goto(page, '/', 'Overview');
  const r = await page.evaluate(() => {
    const b = (id: string) => document.getElementById(id)!.getBoundingClientRect();
    const e = document.querySelector('#events .panel')!.getBoundingClientRect();
    return { jobsBottom: b('jobs').bottom, eventsBottom: e.bottom };
  });
  expect(Math.abs(r.jobsBottom - r.eventsBottom)).toBeLessThan(2);
});

// The feed is the one panel whose length says nothing about the fleet, so it
// never sets the height of the row: expanding it scrolls inside its own card
// and leaves the Active jobs card exactly where it was.
test('expanding the recent events scrolls inside its card, never taller than Active jobs', async ({
  page,
}) => {
  await goto(page, '/', 'Overview');
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  const jobs = page.getByRole('region', { name: 'Active jobs', exact: true });
  await expect(feed.getByRole('listitem').first()).toBeVisible();

  const before = await jobs.boundingBox();
  await feed.getByRole('button', { name: 'Expand events' }).click();
  await expect.poll(() => feed.getByRole('listitem').count()).toBeGreaterThan(3);

  const after = await jobs.boundingBox();
  expect(after?.height, 'the jobs card keeps its height').toBe(before?.height);
  const scrolled = await page.evaluate(() => {
    const list = document.querySelector('#events .feed')!;
    return { client: list.clientHeight, scroll: list.scrollHeight };
  });
  expect(scrolled.scroll, 'the feed scrolls inside its own card').toBeGreaterThan(scrolled.client);
});
