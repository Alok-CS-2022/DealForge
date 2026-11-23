import { expect, test, type BrowserContext, type Page } from '@playwright/test';

const alice = { id: 'alice-id', display_name: 'Alice Example' };
const bob = { id: 'bob-id', display_name: 'Bob Example' };
const north = { id: 'north', name: 'North Company', role: 'admin' };
const south = { id: 'south', name: 'South Company', role: 'member' };
const sales = { id: 'sales', company_id: north.id, name: 'Sales', role: 'manager' };
const retail = { id: 'retail', company_id: north.id, name: 'Retail', role: 'salesperson' };
const support = { id: 'support', company_id: south.id, name: 'Support', role: 'manager' };
const deal = {
  id: 'renewal', company_id: north.id, team_id: retail.id, title: 'Renewal',
  contact_id: 'contact', amount: '999999999999999.9999', currency: 'USD',
  stage: 'proposal', expected_close_date: null, lost_reason: null as string | null,
};
const dealsPath = (team: typeof sales) => `/api/v1/companies/${team.company_id}/teams/${team.id}/deals`;
const stagePath = `${dealsPath(retail)}/${deal.id}/stage`;
const expectedUser = 'x-dealforge-expected-user-id';
const section = (page: Page) => page.getByRole('region', { name: 'Deals' });
const teamPicker = (page: Page) => section(page).getByRole('combobox', { name: 'Team', exact: true });
const editor = (page: Page) => section(page).getByRole('form', { name: 'Update stage for Renewal' });
const change = (page: Page) => section(page).getByRole('button', { name: 'Change stage for Renewal' });

function gate() {
  let release = () => {};
  const wait = new Promise<void>((resolve) => { release = resolve; });
  return { release, wait };
}

async function mockWorkspace(context: BrowserContext) {
  const user = (cookie: string | null) => cookie?.includes('stage-test=bob') ? bob : alice;
  await context.route('**/api/v1/auth/me', async (route) => route.fulfill({ json: user(await route.request().headerValue('cookie')) }));
  await context.route('**/api/v1/companies', async (route) => route.fulfill({
    json: user(await route.request().headerValue('cookie')) === bob ? [south] : [north, south],
  }));
  await context.route('**/api/v1/companies/*/contacts', (route) => route.fulfill({ json: [] }));
  await context.route('**/api/v1/companies/north/teams', (route) => route.fulfill({ json: [sales, retail] }));
  await context.route('**/api/v1/companies/south/teams', (route) => route.fulfill({ json: [support] }));
  await context.route(`**${dealsPath(sales)}`, (route) => route.fulfill({ json: [] }));
  await context.route(`**${dealsPath(retail)}`, (route) => route.fulfill({ json: [deal] }));
  await context.route(`**${dealsPath(support)}`, (route) => route.fulfill({ json: [{
    ...deal, id: 'support-deal', title: 'Support deal', company_id: south.id, team_id: support.id,
  }] }));
}

async function openEditor(page: Page) {
  await page.goto('/login');
  await teamPicker(page).selectOption(retail.id);
  await change(page).click();
}

test.beforeEach(async ({ context }) => {
  await mockWorkspace(context);
});

test('saves a stage once, preserves a blank loss reason, and reopens the deal', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  const next = gate();
  let saved = deal;
  const bodies: { stage: string; lost_reason?: string }[] = [];
  let reads = 0;
  await page.route(`**${dealsPath(retail)}`, (route) => {
    reads++;
    return route.fulfill({ json: [saved] });
  });
  await page.route(`**${stagePath}`, async (route) => {
    const request = route.request();
    expect(request.method()).toBe('PATCH');
    expect(new URL(request.url()).origin).toBe(new URL(page.url()).origin);
    expect(await request.headerValue(expectedUser)).toBe(alice.id);
    const body = request.postDataJSON();
    bodies.push(body);
    if (bodies.length === 1) await next.wait;
    saved = { ...saved, stage: body.stage, lost_reason: body.stage === 'lost' ? body.lost_reason ?? saved.lost_reason : null };
    return route.fulfill({ json: saved });
  });
  await openEditor(page);
  const picker = editor(page).getByLabel('Stage', { exact: true });
  await expect(picker).toHaveValue('proposal');
  await expect(picker.getByRole('option')).toHaveText(['New', 'Qualified', 'Proposal', 'Won', 'Lost']);
  await picker.selectOption('lost');
  await editor(page).getByLabel('Lost reason (optional)').fill(' Pricing changed ');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
  try {
    await editor(page).getByRole('button', { name: 'Save stage' }).press('Enter');
    await expect(editor(page).getByRole('button', { name: 'Saving stage…' })).toBeDisabled();
    await editor(page).dispatchEvent('submit');
  } finally {
    next.release();
  }
  await expect(section(page).getByRole('status').filter({ hasText: 'Stage updated.' })).toBeVisible();
  await expect(change(page)).toBeFocused();
  await expect(teamPicker(page)).toHaveValue(retail.id);
  const row = section(page).getByRole('listitem');
  await expect(row.locator('dd')).toContainText(['999999999999999.9999 USD', 'Lost', 'Pricing changed']);
  await change(page).click();
  await expect(editor(page).getByText('Leave blank to keep the existing reason.')).toBeVisible();
  await editor(page).getByRole('button', { name: 'Save stage' }).click();
  await expect(section(page).getByText('Stage updated.')).toBeVisible();
  await expect(row.getByText('Pricing changed')).toBeVisible();
  await change(page).click();
  await picker.selectOption('qualified');
  await expect(editor(page).getByLabel('Lost reason (optional)')).toHaveCount(0);
  await editor(page).getByRole('button', { name: 'Save stage' }).click();
  await expect(row.locator('dd').getByText('Qualified', { exact: true })).toBeVisible();
  await expect(row.getByText('Pricing changed')).toHaveCount(0);
  expect(bodies).toEqual([{ stage: 'lost', lost_reason: 'Pricing changed' }, { stage: 'lost' }, { stage: 'qualified' }]);
  expect(reads).toBe(1);
});

for (const failure of [400, 500, 'network', 'wrong deal'] as const) {
  test(`keeps the saved stage and draft when the update fails with ${failure}`, async ({ page }) => {
    let writes = 0;
    await page.route(`**${stagePath}`, (route) => {
      writes++;
      if (failure === 'network') return route.abort();
      if (failure === 'wrong deal') return route.fulfill({ json: { ...deal, id: 'other', stage: 'lost' } });
      return route.fulfill({ status: failure, json: { error: 'private details' } });
    });
    await openEditor(page);
    await editor(page).getByLabel('Stage', { exact: true }).selectOption('lost');
    const reason = editor(page).getByLabel('Lost reason (optional)');
    const save = editor(page).getByRole('button', { name: 'Save stage' });
    if (failure === 400) {
      await reason.fill('x'.repeat(501));
      await save.click();
      await expect(editor(page).getByRole('alert')).toContainText('up to 500 characters');
      expect(writes).toBe(0);
    }
    await reason.fill('Keep this draft');
    await save.click();
    await expect(editor(page).getByRole('alert')).toContainText(failure === 400 ? 'Check the stage' : 'Could not confirm');
    await expect(editor(page).getByRole('alert')).not.toContainText('private details');
    await expect(reason).toHaveValue('Keep this draft');
    await expect(section(page).getByRole('listitem').locator('dd').getByText('Proposal', { exact: true })).toBeVisible();
    await expect(section(page).getByText('Stage updated.')).toHaveCount(0);
    expect(writes).toBe(1);
  });
}

test('refreshes the selected team and removes a deal after a 404', async ({ page }) => {
  let available = true;
  await page.route(`**${dealsPath(retail)}`, (route) => route.fulfill({ json: available ? [deal] : [] }));
  await page.route(`**${stagePath}`, (route) => {
    available = false;
    return route.fulfill({ status: 404 });
  });
  await openEditor(page);
  await editor(page).getByRole('button', { name: 'Save stage' }).click();
  await expect(section(page).getByRole('alert')).toContainText('This deal is no longer available.');
  await expect(section(page).getByText('No deals yet for this team.')).toBeVisible();
  await expect(teamPicker(page)).toHaveValue(retail.id);
  await expect(change(page)).toHaveCount(0);
});

test('clears the workspace if the stage request reports an expired session', async ({ page }) => {
  await page.route(`**${stagePath}`, (route) => route.fulfill({ status: 401 }));
  await openEditor(page);
  await editor(page).getByRole('button', { name: 'Save stage' }).click();
  await expect(page.getByRole('alert')).toContainText('Your session expired');
  await expect(section(page)).toHaveCount(0);
});

test('revalidates identity when another tab changes the cookie before saving', async ({ page, context }) => {
  await page.route(`**${stagePath}`, async (route) => {
    const request = route.request();
    expect(await request.headerValue('cookie')).toContain('stage-test=bob');
    expect(await request.headerValue(expectedUser)).toBe(alice.id);
    return route.fulfill({ status: 412 });
  });
  const other = await context.newPage();
  await other.goto('/');
  await openEditor(page);
  await other.evaluate(() => { document.cookie = 'stage-test=bob; Path=/'; });
  await editor(page).getByRole('button', { name: 'Save stage' }).click();
  await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
  await expect(section(page).getByRole('heading', { name: 'Support deal' })).toBeVisible();
  await expect(change(page)).toHaveCount(0);
  await expect(editor(page)).toHaveCount(0);
});

for (const scope of ['team', 'company', 'account'] as const) {
  test(`ignores a late stage response after switching ${scope}`, async ({ page, context }) => {
    const requested = gate();
    const reply = gate();
    await page.addInitScript(() => {
      const original = window.fetch.bind(window);
      window.fetch = (input, init) => original(input, init?.method === 'PATCH' ? { ...init, signal: undefined } : init);
    });
    await page.route(`**${stagePath}`, async (route) => {
      requested.release();
      await reply.wait;
      // An old 401 must leave the new account's session alone.
      return scope === 'account' ? route.fulfill({ status: 401 }) :
        route.fulfill({ json: { ...deal, title: 'Late deal', stage: 'won' } });
    });
    await openEditor(page);
    try {
      await editor(page).getByLabel('Stage', { exact: true }).selectOption('won');
      await editor(page).getByRole('button', { name: 'Save stage' }).click();
      await requested.wait;
      if (scope === 'team') {
        await teamPicker(page).selectOption(sales.id);
        await expect(section(page).getByText('No deals yet for this team.')).toBeVisible();
      } else if (scope === 'company') {
        await page.getByRole('combobox', { name: 'Company', exact: true }).selectOption(south.id);
        await expect(section(page).getByRole('heading', { name: 'Support deal' })).toBeVisible();
      } else {
        const other = await context.newPage();
        await other.goto('/');
        await other.evaluate(() => {
          document.cookie = 'stage-test=bob; Path=/';
          const channel = new BroadcastChannel('dealforge.auth');
          channel.postMessage('session-changed');
          channel.close();
        });
        await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
        await expect(section(page).getByRole('heading', { name: 'Support deal' })).toBeVisible();
      }
      await expect(editor(page)).toHaveCount(0);
      const delayed = page.waitForResponse(`**${stagePath}`);
      reply.release();
      await (await delayed).finished();
      await expect(section(page)).toBeVisible();
      await expect(section(page).getByText('Stage updated.')).toHaveCount(0);
      await expect(section(page).getByRole('heading', { name: 'Late deal' })).toHaveCount(0);
      if (scope === 'team') {
        await teamPicker(page).selectOption(retail.id);
        await expect(section(page).getByRole('listitem').locator('dd').getByText('Proposal', { exact: true })).toBeVisible();
      } else {
        await expect(section(page).getByRole('heading', { name: 'Support deal' })).toBeVisible();
      }
    } finally {
      reply.release();
    }
  });
}
