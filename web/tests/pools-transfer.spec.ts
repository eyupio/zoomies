/**
 * Moving pools as a file.
 *
 * The Pools page's Export and Import, which is how the pools on one instance
 * reach another the same people run, or a repository. What this protects is
 * the preview: before anything is written the dialog says which pools would
 * change and in which settings -- the value now and the value incoming --
 * refuses what this instance cannot take with a reason, and leaves a refused
 * pool out rather than blocking the rest.
 */
import { readFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

const dialog = (page: Page, name: string | RegExp) => page.getByRole('dialog', { name });

/** Put the fixture back through the same route, so the next spec sees it. */
async function restore(page: Page, document: string): Promise<void> {
  const status = await page.evaluate(async (doc) => {
    const resp = await fetch('/api/v1/pools/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ document: doc }),
    });
    return resp.status;
  }, document);
  expect(status).toBe(200);
}

test('the pools can be exported and an import is previewed before it is applied', async ({
  page,
}, testInfo) => {
  await goto(page, '/pools', 'Pools');

  // The export is a download, and it names the installation by what it
  // covers, never by an id, and carries no environment value.
  await page.getByRole('button', { name: 'Export the pools' }).click();
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('menuitem', { name: 'As YAML' }).click(),
  ]);
  expect(download.suggestedFilename()).toMatch(/^zoomies-pools-\d{8}-\d{6}\.yaml$/);
  const exported = await readFile(await download.path(), 'utf8');
  expect(exported).toContain(`name: ${FIXTURE.linuxPool}`);
  expect(exported).toContain('installation: acme');
  expect(exported).not.toContain(FIXTURE.installationId);

  // Each project runs this against the same controller, so each asks for a
  // figure only it uses: the preview then always has something to change.
  const limit = testInfo.project.name === 'mobile' ? 6 : 5;
  const document = [
    'pools:',
    `  - name: ${FIXTURE.linuxPool}`,
    '    installation: acme',
    `    repository_scale_up_limit: ${limit}`,
    '  - name: zoomies-elsewhere',
    '    installation: globex',
    '    labels: [elsewhere]',
    '',
  ].join('\n');

  await page.getByRole('button', { name: 'Import', exact: true }).click();
  const importing = dialog(page, 'Import pools');
  await importing.getByRole('textbox', { name: 'Document' }).fill(document);
  await importing.getByRole('button', { name: 'Check the document' }).click();

  const changed = importing.getByRole('row').filter({ hasText: FIXTURE.linuxPool });
  await expect(changed).toContainText('Will change');
  await expect(changed).toContainText('repository_scale_up_limit');
  await expect(changed).toContainText(String(limit));
  const refused = importing.getByRole('row').filter({ hasText: 'zoomies-elsewhere' });
  await expect(refused).toContainText('Refused');
  await expect(refused).toContainText('no installation on this instance covers globex');
  // Refused pools start out left out, so Apply says what it would really do.
  await expect(refused.getByRole('checkbox')).not.toBeChecked();

  const apply = importing.getByRole('button', { name: 'Apply to 1 pool' });
  await expect(apply).toBeEnabled();
  await apply.click();
  await expect(importing).toBeHidden();
  await expect(page.getByText('1 pool imported')).toBeVisible();

  await restore(
    page,
    `pools:\n  - name: ${FIXTURE.linuxPool}\n    installation: acme\n    repository_scale_up_limit: 0\n`,
  );
});
