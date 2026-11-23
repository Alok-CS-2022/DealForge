import { expect, test, type Page } from '@playwright/test';

const alice = { id: '5a9669d0-84d3-41fe-97a5-0e9c40b41dfb', display_name: 'Alice Example' };
const bob = { id: 'b73dbb24-1e19-4836-9378-31ad6db38a89', display_name: 'Bob Example' };
const north = { id: '31a67f28-d6c5-49ae-bb5e-de325bc01a46', name: 'North Company', role: 'admin' };
const south = { id: '9638d229-3bb0-415b-9afa-a405a19ffbaf', name: 'South Company', role: 'member' };
const sales = { id: 'b213a62a-d3a9-45c3-8341-46b3320287f2', company_id: north.id, name: 'Sales', role: 'manager' };
const retail = { id: '5c77bbdd-a15b-4e58-9c20-ac6b2e5bbe6e', company_id: north.id, name: 'Retail', role: 'salesperson' };
const support = { id: '8395851a-9ac0-4717-a0a3-02e5a85c06ae', company_id: south.id, name: 'Support', role: 'manager' };
const contact = {
  id: '8b619f39-6193-4930-95dd-61751da9019e', company_id: north.id, name: 'Ada Lovelace',
  email: 'ada@example.test', phone: null, company_name: null,
};
const deal = {
  id: '236a98a0-d546-4bdc-aaa7-cf4459eaca21', company_id: north.id, team_id: sales.id, contact_id: contact.id,
  title: 'Renewal', amount: '999999999999999.9999', currency: 'USD', stage: 'proposal',
  expected_close_date: '2028-02-29', lost_reason: null,
  assignee_user_id: null, created_at: '2026-11-23T00:00:00Z', updated_at: '2026-11-23T00:00:00Z',
};
const teamsPath = (id: string) => `**/api/v1/companies/${id}/teams`;
const dealsPath = (team: typeof sales) => `**/api/v1/companies/${team.company_id}/teams/${team.id}/deals`;
const section = (page: Page) => page.getByRole('region', { name: 'Deals' });
const form = (page: Page) => section(page).getByRole('form', { name: 'Create deal' });
const teamPicker = (page: Page) => section(page).getByRole('combobox', { name: 'Team', exact: true });
const expectedUser = 'x-dealforge-expected-user-id';

function gate() {
  let release = () => {};
  const wait = new Promise<void>((resolve) => { release = resolve; });
  return { wait, release };
}

async function openWorkspace(page: Page, teams = [sales], companies = [north]) {
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ json: alice }));
  await page.route('**/api/v1/companies', (route) => route.fulfill({ json: companies }));
  await page.route(teamsPath(north.id), (route) => route.fulfill({ json: teams }));
  await page.route('**/api/v1/companies/*/contacts', (route) => route.fulfill({ json: [contact] }));
  await page.goto('/login');
}

async function fillDeal(page: Page, amount: string) {
  await section(page).getByRole('button', { name: 'Create deal', exact: true }).click();
  await form(page).getByLabel('Deal title').fill('  New deal  ');
  await form(page).getByLabel('Contact', { exact: true }).selectOption(contact.id);
  await form(page).getByLabel('Amount', { exact: true }).fill(amount);
  await form(page).getByLabel('Currency code').fill('usd');
}

test('shows an empty message when the company has no authorized teams', async ({ page }) => {
  await openWorkspace(page, []);
  await expect(section(page).getByText('You don’t belong to any active teams in this company.')).toBeVisible();
  await expect(teamPicker(page)).toHaveCount(0);
  await expect(section(page).getByRole('button', { name: 'Create deal' })).toHaveCount(0);
});

test('lists only returned teams and displays the selected team’s deals', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  const read = page.waitForRequest(dealsPath(sales));
  await page.route(dealsPath(sales), (route) => route.fulfill({ json: [deal] }));
  await page.route(dealsPath(retail), (route) => route.fulfill({ json: [] }));
  await openWorkspace(page, [sales, retail]);
  const request = await read;
  expect(await request.headerValue(expectedUser)).toBe(alice.id);
  expect(new URL(request.url()).origin).toBe(new URL(page.url()).origin);
  await expect(teamPicker(page).getByRole('option')).toHaveText(['Sales', 'Retail']);
  const entry = section(page).getByRole('listitem');
  await expect(entry).toContainText(deal.title);
  await expect(entry).toContainText(contact.name);
  await expect(entry).toContainText('999999999999999.9999 USD');
  await expect(entry).toContainText('Proposal');
  await expect(entry.locator('time')).toHaveAttribute('datetime', '2028-02-29');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
  await teamPicker(page).selectOption(retail.id);
  await expect(section(page).getByText('No deals yet for this team.')).toBeVisible();
  await expect(section(page).getByText(deal.title)).toHaveCount(0);
});

test('submits the exact create contract, prevents a duplicate, and reads the refreshed list', async ({ page }) => {
  let posts = 0;
  let saved = false;
  const release = gate();
  const created = { ...deal, title: 'New deal', stage: 'lost', lost_reason: 'Customer declined' };
  await page.route(dealsPath(sales), async (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: saved ? [created] : [] });
    posts++;
    await release.wait;
    saved = true;
    return route.fulfill({ status: 201, json: created });
  });
  await openWorkspace(page);
  await fillDeal(page, deal.amount);
  await expect(form(page).getByLabel('Lost reason (optional)')).toHaveCount(0);
  await form(page).getByLabel('Stage', { exact: true }).selectOption('lost');
  await form(page).getByLabel('Expected closing date (optional)').fill('2028-02-29');
  await form(page).getByLabel('Lost reason (optional)').fill(' Customer declined ');
  const pending = page.waitForRequest((request) => request.url().endsWith('/deals') && request.method() === 'POST');
  try {
    await form(page).getByRole('button', { name: 'Create deal' }).click();
    const request = await pending;
    expect(await request.headerValue(expectedUser)).toBe(alice.id);
    expect(request.postDataJSON()).toEqual({
      title: 'New deal', contact_id: contact.id, amount: deal.amount, currency: 'USD',
      stage: 'lost', expected_close_date: '2028-02-29', lost_reason: 'Customer declined',
    });
    await expect(form(page).getByRole('button', { name: 'Creating deal…' })).toBeDisabled();
    await form(page).dispatchEvent('submit');
  } finally {
    release.release();
  }
  await expect(section(page).getByText('Deal created.')).toBeVisible();
  await expect(section(page).getByRole('listitem')).toContainText('Customer declined');
  expect(posts).toBe(1);
});

test('validates exact decimals locally and preserves the draft on server errors', async ({ page }) => {
  let posts = 0;
  await page.route(dealsPath(sales), (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: [] });
    posts++;
    expect(route.request().postDataJSON()).toEqual({ title: 'New deal', contact_id: contact.id, amount: '10.0001', currency: 'USD' });
    return route.fulfill({ status: 500, json: { error: 'private details' } });
  });
  await openWorkspace(page);
  await fillDeal(page, '10.00001');
  await form(page).getByRole('button', { name: 'Create deal' }).click();
  await expect(form(page).getByRole('alert')).toContainText('4 decimal places');
  expect(posts).toBe(0);
  await form(page).getByLabel('Amount', { exact: true }).fill('10.0001');
  await form(page).getByRole('button', { name: 'Create deal' }).click();
  await expect(form(page).getByRole('alert')).toContainText('Could not confirm');
  await expect(form(page).getByRole('alert')).not.toContainText('private details');
  await expect(form(page).getByLabel('Amount', { exact: true })).toHaveValue('10.0001');
  expect(posts).toBe(1);
});

test('retries a failed deal list and hides creation when the team becomes inaccessible', async ({ page }) => {
  let reads = 0;
  await page.route(dealsPath(sales), (route) => ++reads === 1
    ? route.abort()
    : route.fulfill({ status: 404 }));
  await openWorkspace(page);
  await expect(section(page).getByRole('alert')).toContainText('Could not load deals');
  await section(page).getByRole('button', { name: 'Reload deals' }).click();
  await expect(section(page).getByRole('alert')).toContainText('team’s deals are unavailable');
  await expect(section(page).getByRole('button', { name: 'Create deal' })).toHaveCount(0);
});

test('never offers contacts returned for a different company', async ({ page }) => {
  await page.route(dealsPath(sales), (route) => route.fulfill({ json: [] }));
  await openWorkspace(page);
  await page.route(`**/api/v1/companies/${north.id}/contacts`, (route) =>
    route.fulfill({ json: [{ ...contact, company_id: south.id }] }));
  await section(page).getByRole('button', { name: 'Create deal' }).click();
  await expect(section(page).getByRole('alert')).toContainText('Could not load company contacts');
  await expect(form(page)).toHaveCount(0);
});

test('ignores a late team response after the user switches away', async ({ page }) => {
  const requested = gate();
  const reply = gate();
  await page.addInitScript((id) => {
    const original = window.fetch.bind(window);
    window.fetch = (input, init) => original(input, typeof input === 'string' && input.endsWith(`/teams/${id}/deals`)
      ? { ...init, signal: undefined } : init);
  }, retail.id);
  await page.route(dealsPath(sales), (route) => route.fulfill({ json: [deal] }));
  await page.route(dealsPath(retail), async (route) => {
    requested.release();
    await reply.wait;
    await route.fulfill({ json: [{ ...deal, team_id: retail.id, title: 'Late retail deal' }] });
  });
  await openWorkspace(page, [sales, retail]);
  await expect(section(page).getByRole('heading', { name: deal.title })).toBeVisible();
  try {
    await teamPicker(page).selectOption(retail.id);
    await requested.wait;
    await expect(section(page).getByText('Loading deals…')).toBeVisible();
    await expect(section(page).getByRole('heading', { name: deal.title })).toHaveCount(0);
    await teamPicker(page).selectOption(sales.id);
    await expect(section(page).getByRole('heading', { name: deal.title })).toBeVisible();
    const delayed = page.waitForResponse(dealsPath(retail));
    reply.release();
    await (await delayed).finished();
    await expect(section(page).getByRole('heading', { name: 'Late retail deal' })).toHaveCount(0);
  } finally {
    reply.release();
  }
});

test('recovers from a shared-cookie account switch before submitting an old draft', async ({ page, context }) => {
  const who = (cookie: string | null) => cookie?.includes('deal-test=bob') ? bob : alice;
  const southContact = { ...contact, company_id: south.id, name: 'Bob contact' };
  const southDeal = { ...deal, company_id: south.id, team_id: support.id, title: 'Bob deal' };
  await context.route('**/api/v1/auth/me', async (route) => route.fulfill({ json: who(await route.request().headerValue('cookie')) }));
  await context.route('**/api/v1/companies', async (route) =>
    route.fulfill({ json: who(await route.request().headerValue('cookie')) === alice ? [north] : [south] }));
  await context.route(teamsPath(north.id), (route) => route.fulfill({ json: [sales] }));
  await context.route(teamsPath(south.id), (route) => route.fulfill({ json: [support] }));
  await context.route(`**/api/v1/companies/${north.id}/contacts`, (route) => route.fulfill({ json: [contact] }));
  await context.route(`**/api/v1/companies/${south.id}/contacts`, (route) => route.fulfill({ json: [southContact] }));
  await context.route(dealsPath(sales), async (route) => {
    const request = route.request();
    if (request.method() === 'GET') return route.fulfill({ json: [deal] });
    expect(who(await request.headerValue('cookie'))).toBe(bob);
    expect(await request.headerValue(expectedUser)).toBe(alice.id);
    return route.fulfill({ status: 412 });
  });
  await context.route(dealsPath(support), (route) => route.fulfill({ json: [southDeal] }));
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  await fillDeal(page, '10');
  await other.evaluate(() => { document.cookie = 'deal-test=bob; Path=/'; });
  await form(page).getByRole('button', { name: 'Create deal' }).click();
  await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
  await expect(teamPicker(page).getByRole('option')).toHaveText(['Support']);
  await expect(section(page).getByRole('heading', { name: 'Bob deal' })).toBeVisible();
  await expect(section(page).getByRole('heading', { name: deal.title })).toHaveCount(0);
  await expect(form(page)).toHaveCount(0);
});

test('removes deals and drafts when the session expires', async ({ page }) => {
  await page.route(dealsPath(sales), (route) => route.request().method() === 'GET'
    ? route.fulfill({ json: [deal] })
    : route.fulfill({ status: 401 }));
  await openWorkspace(page);
  await fillDeal(page, '10');
  await form(page).getByRole('button', { name: 'Create deal' }).click();
  await expect(page.getByRole('alert')).toContainText('Your session expired');
  await expect(section(page)).toHaveCount(0);
});

test('ignores a previous company’s in-flight deals after company selection changes', async ({ page }) => {
  const requested = gate();
  const reply = gate();
  await page.addInitScript((id) => {
    const original = window.fetch.bind(window);
    window.fetch = (input, init) => original(input, typeof input === 'string' && input.endsWith(`/teams/${id}/deals`)
      ? { ...init, signal: undefined } : init);
  }, sales.id);
  await page.route(teamsPath(south.id), (route) => route.fulfill({ json: [support] }));
  await page.route(dealsPath(sales), async (route) => {
    requested.release();
    await reply.wait;
    await route.fulfill({ json: [deal] });
  });
  await page.route(dealsPath(support), (route) => route.fulfill({ json: [{
    ...deal, company_id: south.id, team_id: support.id, title: 'South deal',
  }] }));
  await openWorkspace(page, [sales], [north, south]);
  await page.route(`**/api/v1/companies/${south.id}/contacts`, (route) =>
    route.fulfill({ json: [{ ...contact, company_id: south.id }] }));
  try {
    await requested.wait;
    await page.getByRole('combobox', { name: 'Company', exact: true }).selectOption(south.id);
    await expect(section(page).getByRole('heading', { name: 'South deal' })).toBeVisible();
    const delayed = page.waitForResponse(dealsPath(sales));
    reply.release();
    await (await delayed).finished();
    await expect(section(page).getByRole('heading', { name: 'South deal' })).toBeVisible();
    await expect(section(page).getByRole('heading', { name: deal.title })).toHaveCount(0);
  } finally {
    reply.release();
  }
});
