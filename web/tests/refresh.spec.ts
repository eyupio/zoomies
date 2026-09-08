/**
 * The refresh control.
 *
 * Zoomies keeps itself current over one SSE stream, so this button is never
 * how a page keeps up. It is how an operator settles the question -- and the
 * only thing that makes it worth having is that it is the same control, in the
 * same place, with the same name, wherever they happen to be. So that is what
 * these protect: it is on every page that has something to fetch, it is not on
 * the pages that have not, it never sits ahead of the action that changes the
 * fleet, and the keyboard reaches it too.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, goto, pageHeading, SECTIONS, sectionHeading } from './support/fixtures';

test.use(browserOverride);

/** The one control. Named, not guessed at: every page must call it this. */
function refreshButton(page: Page) {
  return page.getByRole('button', { name: 'Refresh', exact: true });
}

/** Sections whose pages fetch something, and therefore have the button. */
const REFRESHABLE = SECTIONS.filter((section) => section.path !== '/migrate');

for (const section of REFRESHABLE) {
  test(`${section.label} offers refresh, and pressing it leaves the page standing`, async ({
    page,
  }) => {
    await goto(page, section.path, sectionHeading(section));

    const refresh = refreshButton(page);
    await expect(refresh).toHaveCount(1);
    await expect(refresh).toBeVisible();
    await refresh.click();

    // The heading is the proof the page did not go anywhere: a refresh that
    // remounts the route would take the operator's scroll position, their
    // selection and their place in a form with it.
    await expect(pageHeading(page, sectionHeading(section))).toBeVisible();
    await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
    await expect(refresh).toBeEnabled();
  });
}

test('a page with nothing to fetch offers nothing to press', async ({ page }) => {
  // The migration wizard holds a flow, not a view of the fleet. Refreshing it
  // would either do nothing or throw away what somebody has half-filled in,
  // and a control that succeeds at neither is worse than no control.
  await goto(page, '/migrate', 'Migrate repositories');
  await expect(refreshButton(page)).toHaveCount(0);
});

test('the refresh sits before the action that changes the fleet, not after it', async ({
  page,
}) => {
  // Reading order is the whole affordance. If refresh drifts to the right of
  // "Add a host" on one page and the left of it on another, an operator stops
  // reaching for it without looking -- which is the only reason it is uniform.
  await goto(page, '/hosts', 'Hosts');
  const actions = page.getByRole('button', { name: /^(Refresh|Add a host)$/ });
  await expect(actions.first()).toHaveAccessibleName('Refresh');
});

test('R refreshes the page, without taking the letter from anything else', async ({ page }) => {
  await goto(page, '/hosts', 'Hosts');
  const refresh = refreshButton(page);

  await page.keyboard.press('r');
  // The button reports the work whichever way it was started, which is what
  // makes the key and the button one gesture rather than two.
  await expect(refresh).toHaveAttribute('aria-busy', 'true');
  await expect(refresh).not.toHaveAttribute('aria-busy', 'true');

  // `g r` is still Runners: the chord gets the key first, and a shortcut that
  // quietly ate half the navigation would be a bad trade for a refresh.
  await page.keyboard.press('g');
  await page.keyboard.press('r');
  await expect(pageHeading(page, 'Runners')).toBeVisible();

  // And in a search field an `r` is a letter, as it is everywhere else.
  await goto(page, '/pools', 'Pools');
  const search = page.getByRole('searchbox', { name: 'Search pools' });
  await search.click();
  await page.keyboard.press('r');
  await expect(search).toHaveValue('r');
});

test('the refresh says when it last landed', async ({ page }) => {
  // Somebody reaching for this button is usually asking "is this current?",
  // and the honest answer is a time, not a spinner.
  await goto(page, '/hosts', 'Hosts');
  const refresh = refreshButton(page);
  await expect(refresh).toHaveAttribute('title', /Fetch this page again/);

  await refresh.click();
  await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
  await expect(refresh).toHaveAttribute('title', /^Refreshed .+\. Fetch it again\./);
});
