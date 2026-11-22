import { expect, test, type Page } from '@playwright/test';

const companiesPath = '**/api/v1/companies';
const profile = {
  id: 'd008aa53-5fce-42ec-bdaf-d9495dcaaf24',
  username: 'alice',
  email: 'alice@example.test',
  display_name: 'Alice Example',
};
const preferenceKey = `dealforge.selected-company.${profile.id}`;
const first = { id: 'f1654ebb-5a4e-40a6-a691-45c01ac84daf', name: 'Northwind', role: 'admin' };
const second = { id: '08504ebb-5a4e-40a6-a691-45c01ac84d34', name: 'Southridge', role: 'member' };

async function openWorkspace(page: Page, companies: object[]) {
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ status: 200, json: profile }));
  await page.route(companiesPath, (route) => route.fulfill({ status: 200, json: companies }));
  await page.goto('/login');
}

test('loads the authorized company list with the current origin before showing onboarding', async ({ page }) => {
  const request = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/companies');
  await openWorkspace(page, []);
  const query = await request;
  expect(query.method()).toBe('GET');
  expect(new URL(query.url()).origin).toBe(new URL(page.url()).origin);
  expect(new URL(query.url()).search).toBe('');
  await expect(page.getByRole('heading', { name: 'Create your company' })).toBeVisible();
  await expect(page.getByRole('combobox')).toHaveCount(0);
});

test('opens a single company directly and displays the membership role', async ({ page }) => {
  await openWorkspace(page, [second]);
  await expect(page.getByRole('heading', { name: second.name })).toBeVisible();
  await expect(page.getByText('Member', { exact: true })).toBeVisible();
  await expect(page.getByRole('combobox')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Create company' })).toHaveCount(0);
});

test('switches companies and restores the authorized selection on refresh', async ({ page }) => {
  await openWorkspace(page, [first, second]);
  const selector = page.getByRole('combobox', { name: 'Company' });
  await expect(page.getByRole('heading', { name: first.name })).toBeVisible();
  await selector.selectOption(second.id);
  await expect(page.getByRole('heading', { name: second.name })).toBeVisible();
  await expect(page.getByText('Member', { exact: true })).toBeVisible();
  expect(await page.evaluate((key) => localStorage.getItem(key), preferenceKey)).toBe(second.id);
  await page.reload();
  await expect(selector).toHaveValue(second.id);
  await expect(page.getByRole('heading', { name: second.name })).toBeVisible();
});

test('discards a stored selection outside the current list', async ({ page }) => {
  await page.addInitScript((key) => localStorage.setItem(key, 'not-authorized'), preferenceKey);
  await openWorkspace(page, [first, second]);
  await expect(page.getByRole('combobox', { name: 'Company' })).toHaveValue(first.id);
  await expect(page.getByRole('heading', { name: first.name })).toBeVisible();
  await expect(page.getByRole('option')).toHaveCount(2);
  expect(await page.evaluate((key) => localStorage.getItem(key), preferenceKey)).toBe(first.id);
});

test('logout clears workspace UI and a different account gets only its own list', async ({ page }) => {
  let account: 'first' | 'second' | null = 'first';
  await page.route('**/api/v1/auth/me', (route) => account
    ? route.fulfill({
      status: 200,
      json: account === 'first' ? profile : { ...profile, id: 'different-user', display_name: 'Another user' },
    })
    : route.fulfill({ status: 401 }));
  await page.route(companiesPath, (route) => route.fulfill({
    status: 200, json: account === 'first' ? [first] : [second],
  }));
  await page.route('**/api/v1/auth/logout', (route) => {
    account = null;
    return route.fulfill({ status: 204 });
  });
  await page.route('**/api/v1/auth/login', (route) => {
    account = 'second';
    return route.fulfill({ status: 204 });
  });
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: first.name })).toBeVisible();
  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(page.getByRole('status')).toHaveText('You are logged out.');
  await expect(page.getByRole('heading', { name: first.name })).toHaveCount(0);
  await page.getByLabel('Username').fill('another');
  await page.getByLabel('Password').fill('test-only passphrase');
  await page.getByRole('button', { name: 'Log in', exact: true }).click();
  await expect(page.getByRole('heading', { name: second.name })).toBeVisible();
  await expect(page.getByText('Signed in as Another user')).toBeVisible();
  await expect(page.getByRole('heading', { name: first.name })).toHaveCount(0);
});

test('loads again after a list API failure without showing onboarding prematurely', async ({ page }) => {
  let attempts = 0;
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ status: 200, json: profile }));
  await page.route(companiesPath, (route) => ++attempts === 1
    ? route.fulfill({ status: 500, json: { error: 'private details' } })
    : route.fulfill({ status: 200, json: [first] }));
  await page.goto('/login');
  await expect(page.getByRole('alert')).toContainText('Could not load your companies.');
  await expect(page.getByText('private details')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Create your company' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Reload companies' }).click();
  await expect(page.getByRole('heading', { name: first.name })).toBeVisible();
});

test('returns to login when the company list session has expired', async ({ page }) => {
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({ status: 200, json: profile }));
  await page.route(companiesPath, (route) => route.fulfill({ status: 401 }));
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeFocused();
  await expect(page.getByRole('alert')).toContainText('Your session expired.');
});
