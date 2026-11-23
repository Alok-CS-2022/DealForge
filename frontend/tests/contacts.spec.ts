import { expect, test, type Page } from '@playwright/test';

const alice = { id: '5a9669d0-84d3-41fe-97a5-0e9c40b41dfb', display_name: 'Alice Example' };
const bob = { id: 'b73dbb24-1e19-4836-9378-31ad6db38a89', display_name: 'Bob Example' };
const north = { id: '31a67f28-d6c5-49ae-bb5e-de325bc01a46', name: 'North Company', role: 'member' };
const south = { id: '9638d229-3bb0-415b-9afa-a405a19ffbaf', name: 'South Company', role: 'admin' };
const contactsPath = (id: string) => `**/api/v1/companies/${id}/contacts`;
const ada = {
  id: '8b619f39-6193-4930-95dd-61751da9019e', company_id: north.id, name: 'Ada Lovelace',
  email: 'ada@example.test', phone: '+1 555 0100', company_name: 'Analytical Engines',
  assignee_user_id: null, created_at: '2026-11-23T00:00:00Z', updated_at: '2026-11-23T00:00:00Z',
};
const grace = { ...ada, id: '74c34700-3cb2-498d-b315-3379051d665e', company_id: south.id, name: 'Grace Hopper',
  email: 'grace@example.test', phone: null, company_name: null };
const expectedUser = 'x-dealforge-expected-user-id';
const contacts = (page: Page) => page.getByRole('region', { name: 'Contacts' });
const form = (page: Page) => page.getByRole('form', { name: 'Add contact' });

test.beforeEach(async ({ context }) => {
  await context.route('**/api/v1/companies/*/teams', (route) => route.fulfill({ json: [] }));
});

function gate() {
  let release = () => {};
  const wait = new Promise<void>((resolve) => { release = resolve; });
  return { wait, release };
}

async function openWorkspace(page: Page, companies = [north]) {
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ json: alice }));
  await page.route('**/api/v1/companies', (route) => route.fulfill({ json: companies }));
  await page.goto('/login');
}

async function fillContact(page: Page) {
  await form(page).getByLabel('Name', { exact: true }).fill(' Ada Lovelace ');
  await form(page).getByLabel('Email', { exact: true }).fill(' ada@example.test ');
}

test('lists contact details in the active company on a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 740 });
  const request = page.waitForRequest(contactsPath(north.id));
  await page.route(contactsPath(north.id), (route) => route.fulfill({ json: [ada] }));
  await openWorkspace(page);
  const get = await request;
  expect(get.method()).toBe('GET');
  expect(new URL(get.url()).origin).toBe(new URL(page.url()).origin);
  expect(await get.headerValue(expectedUser)).toBe(alice.id);
  const entry = contacts(page).getByRole('listitem');
  await expect(entry.getByRole('heading', { name: ada.name })).toBeVisible();
  await expect(entry).toContainText(ada.email);
  await expect(entry).toContainText(ada.phone);
  await expect(entry).toContainText(ada.company_name);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(360);
});

test('adds a contact once and displays it from the refreshed list', async ({ page }) => {
  let saved = false;
  let posts = 0;
  const responseGate = gate();
  await page.route(contactsPath(north.id), async (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: saved ? [ada] : [] });
    posts++;
    await responseGate.wait;
    saved = true;
    return route.fulfill({ status: 201, json: ada });
  });
  await openWorkspace(page);
  await expect(contacts(page).getByText('No contacts yet.')).toBeVisible();
  await fillContact(page);
  await form(page).getByLabel('Phone (optional)').fill(' +1 555 0100 ');
  await form(page).getByLabel('Company name (optional)').fill(' Analytical Engines ');
  const pending = page.waitForRequest((request) => request.url().endsWith('/contacts') && request.method() === 'POST');
  try {
    await form(page).getByRole('button', { name: 'Add contact' }).click();
    const request = await pending;
    expect(await request.headerValue(expectedUser)).toBe(alice.id);
    expect(request.postDataJSON()).toEqual({ name: ada.name, email: ada.email, phone: ada.phone, company_name: ada.company_name });
    await expect(form(page).getByRole('button', { name: 'Adding contact…' })).toBeDisabled();
    await form(page).dispatchEvent('submit');
  } finally {
    responseGate.release();
  }
  await expect(contacts(page).getByText('Contact added.')).toBeVisible();
  await expect(contacts(page).getByRole('listitem')).toContainText(ada.name);
  await expect(form(page).getByLabel('Name', { exact: true })).toHaveValue('');
  expect(posts).toBe(1);
});

for (const failure of [
  { status: 409, message: 'already exists in this company', apiError: 'internal details' },
  { status: 400, message: 'Enter a valid email address', apiError: 'email must be a valid address of at most 254 bytes' },
  { status: 500, message: 'Could not confirm that the contact was added', apiError: 'internal details' },
  { status: 0, message: 'Could not reach DealForge', apiError: '' },
]) {
  test(`handles ${failure.status || 'network'} errors while adding a contact`, async ({ page }) => {
    await page.route(contactsPath(north.id), (route) => {
      if (route.request().method() === 'GET') return route.fulfill({ json: [] });
      expect(route.request().postDataJSON()).toEqual({ name: ada.name, email: ada.email });
      return failure.status ? route.fulfill({ status: failure.status, json: { error: failure.apiError } }) : route.abort();
    });
    await openWorkspace(page);
    await fillContact(page);
    await form(page).getByRole('button', { name: 'Add contact' }).click();
    await expect(form(page).getByRole('alert')).toContainText(failure.message);
    await expect(form(page).getByRole('alert')).not.toContainText('internal details');
    await expect(form(page).getByLabel('Email', { exact: true })).toHaveValue(' ada@example.test ');
    await expect(contacts(page).getByText('Contact added.')).toHaveCount(0);
  });
}

test('lets the user retry loading and hides the form for an inaccessible company', async ({ page }) => {
  let loads = 0;
  await page.route(contactsPath(north.id), (route) => ++loads === 1
    ? route.abort()
    : route.fulfill({ status: 404 }));
  await openWorkspace(page);
  await expect(contacts(page).getByRole('alert')).toContainText('Could not load contacts');
  await contacts(page).getByRole('button', { name: 'Reload contacts' }).click();
  await expect(contacts(page).getByRole('alert')).toContainText('no longer have access');
  await expect(form(page)).toHaveCount(0);
});

test('clears the Contacts workspace when a session expires', async ({ page }) => {
  await page.route(contactsPath(north.id), (route) => route.request().method() === 'GET'
    ? route.fulfill({ json: [ada] })
    : route.fulfill({ status: 401 }));
  await openWorkspace(page);
  await fillContact(page);
  await form(page).getByRole('button', { name: 'Add contact' }).click();
  await expect(page.getByRole('alert')).toContainText('Your session expired');
  await expect(contacts(page)).toHaveCount(0);
  await expect(page.getByText(ada.email)).toHaveCount(0);
});

test('clears old contacts on company switch and ignores a late response after switching back', async ({ page }) => {
  const responseGate = gate();
  const southRequested = gate();
  // Let the delayed response reach the app even if the browser would cancel it.
  await page.addInitScript((id) => {
    const request = window.fetch.bind(window);
    window.fetch = (input, init) => request(input, input === `/api/v1/companies/${id}/contacts`
      ? { ...init, signal: undefined }
      : init);
  }, south.id);
  await page.route(contactsPath(north.id), (route) => route.fulfill({ json: [ada] }));
  await page.route(contactsPath(south.id), async (route) => {
    southRequested.release();
    await responseGate.wait;
    await route.fulfill({ json: [grace] });
  });
  await openWorkspace(page, [north, south]);
  await expect(contacts(page).getByText(ada.email)).toBeVisible();
  const picker = page.getByRole('combobox', { name: 'Company' });
  try {
    await picker.selectOption(south.id);
    await southRequested.wait;
    await expect(contacts(page).getByRole('status')).toHaveText('Loading contacts…');
    await expect(contacts(page).getByText(ada.email)).toHaveCount(0);
    await picker.selectOption(north.id);
    await expect(contacts(page).getByText(ada.email)).toBeVisible();
    const delayed = page.waitForResponse(contactsPath(south.id));
    responseGate.release();
    await (await delayed).finished();
    await expect(contacts(page).getByText(ada.email)).toBeVisible();
    await expect(contacts(page).getByText(grace.email)).toHaveCount(0);
  } finally {
    responseGate.release();
  }
});

test('rejects a stale form before a cross-tab switch notification arrives', async ({ page, context }) => {
  const activeUser = (cookie: string | null) => cookie?.includes('fixture=bob') ? bob : alice;
  await context.route('**/api/v1/auth/me', async (route) =>
    route.fulfill({ json: activeUser(await route.request().headerValue('cookie')) }));
  await context.route('**/api/v1/companies', async (route) =>
    route.fulfill({ json: activeUser(await route.request().headerValue('cookie')) === alice ? [north] : [south] }));
  let contactWrites = 0;
  await context.route('**/api/v1/companies/*/contacts', async (route) => {
    const request = route.request();
    const user = activeUser(await request.headerValue('cookie'));
    if (await request.headerValue(expectedUser) !== user.id) return route.fulfill({ status: 412 });
    if (request.method() === 'POST') {
      contactWrites++;
      return route.fulfill({ status: 201 });
    }
    return route.fulfill({ json: [user === alice ? ada : grace] });
  });
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  await fillContact(page);
  await expect(contacts(other).getByText(ada.email)).toBeVisible();
  await other.evaluate(() => { document.cookie = 'fixture=bob; Path=/'; });
  await form(page).getByRole('button', { name: 'Add contact' }).click();
  await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
  await expect(contacts(page).getByText(grace.email)).toBeVisible();
  await expect(contacts(page).getByText(ada.email)).toHaveCount(0);
  expect(contactWrites).toBe(0);
  await other.evaluate(() => {
    const channel = new BroadcastChannel('dealforge.auth');
    channel.postMessage('session-changed');
    channel.close();
  });
  await expect(contacts(page).getByText(grace.email)).toBeVisible();
});
