import { expect, test, type Page } from '@playwright/test';

const companiesPath = '**/api/v1/companies';
const workspace = {
  company: { id: 'f1654ebb-5a4e-40a6-a691-45c01ac84daf', name: 'Northwind' },
  team: {
    id: '76f50494-2dfa-4f71-8e78-db0a73d92739',
    company_id: 'f1654ebb-5a4e-40a6-a691-45c01ac84daf',
    name: 'General',
  },
};

async function openCompanyScreen(page: Page) {
  await page.route('**/api/v1/auth/me', (route) => route.fulfill({
    status: 200,
    json: { id: 'd008aa53-5fce-42ec-bdaf-d9495dcaaf24', username: 'alice', email: 'alice@example.test', display_name: 'Alice Example' },
  }));
  await page.goto('/login');
  await expect(page.getByRole('heading', { name: 'Create your company' })).toBeVisible();
}

test('creates a company only on submit and shows the returned workspace', async ({ page }) => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let submissions = 0;
  await page.route(companiesPath, async (route) => {
    submissions++;
    await gate;
    await route.fulfill({ status: 201, json: workspace });
  });
  await openCompanyScreen(page);
  expect(submissions).toBe(0);
  await page.getByLabel('Company name').fill('  Northwind  ');
  const request = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/companies');
  try {
    await page.getByRole('button', { name: 'Create company' }).click();
    const submitted = await request;
    expect(submitted.method()).toBe('POST');
    expect(new URL(submitted.url()).origin).toBe(new URL(page.url()).origin);
    expect(submitted.headers()['content-type']).toBe('application/json');
    expect(submitted.postDataJSON()).toEqual({ name: 'Northwind' });
    await expect(page.getByRole('button', { name: 'Creating company…' })).toBeDisabled();
    await expect(page.getByRole('button', { name: 'Logout' })).toBeDisabled();
    await page.locator('form').evaluate((form) => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
  } finally {
    release();
  }
  await expect(page.getByRole('heading', { name: 'Workspace created' })).toBeFocused();
  await expect(page.getByText('Northwind', { exact: true })).toBeVisible();
  await expect(page.getByText('General', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Logout' })).toBeEnabled();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Create your company' })).toBeVisible();
  expect(submissions).toBe(1);
});

test('rejects blank and long names and keeps the field accessible on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 640 });
  let submissions = 0;
  await page.route(companiesPath, (route) => {
    submissions++;
    return route.abort();
  });
  await openCompanyScreen(page);
  const input = page.getByLabel('Company name');
  const submit = page.getByRole('button', { name: 'Create company' });
  await input.fill('   ');
  await submit.click();
  await expect(input).toBeFocused();
  await expect(input).toHaveAccessibleDescription('Enter a company name of 1–100 characters.');
  await input.fill('a'.repeat(101));
  await submit.click();
  await expect(input).toHaveAttribute('aria-invalid', 'true');
  expect(submissions).toBe(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
});

test('returns to login when the company request reports an expired session', async ({ page }) => {
  await page.route(companiesPath, (route) => route.fulfill({ status: 401 }));
  await openCompanyScreen(page);
  await page.getByLabel('Company name').fill('Northwind');
  await page.getByRole('button', { name: 'Create company' }).click();
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeFocused();
  await expect(page.getByRole('alert')).toContainText('Your session expired.');
  await expect(page.getByText('You are logged out.')).toHaveCount(0);
});

for (const failure of [
  { label: 'validation', status: 400, message: 'Check that the company name is 1–100 characters.' },
  { label: 'server', status: 500, message: 'The server did not confirm company creation.' },
  { label: 'malformed success', status: 201, message: 'Could not confirm whether the company was created.' },
  { label: 'network', status: 0, message: 'Could not confirm whether the company was created.' },
]) {
  test(`handles a ${failure.label} response without claiming success`, async ({ page }) => {
    await page.route(companiesPath, (route) => failure.status
      ? route.fulfill({ status: failure.status, json: { error: 'private details' } })
      : route.abort());
    await openCompanyScreen(page);
    await page.getByLabel('Company name').fill('Northwind');
    await page.getByRole('button', { name: 'Create company' }).click();
    await expect(page.getByRole('alert')).toContainText(failure.message);
    await expect(page.getByRole('alert')).not.toContainText('private details');
    await expect(page.getByLabel('Company name')).toHaveValue('Northwind');
    await expect(page.getByRole('button', { name: 'Create company' })).toBeEnabled();
    await expect(page.getByRole('heading', { name: 'Workspace created' })).toHaveCount(0);
  });
}

test('retains the workspace on logout failure and returns to login after retry succeeds', async ({ page }) => {
  let attempts = 0;
  await page.route(companiesPath, (route) => route.fulfill({ status: 201, json: workspace }));
  await page.route('**/api/v1/auth/logout', (route) => route.fulfill({ status: ++attempts === 1 ? 500 : 204 }));
  await openCompanyScreen(page);
  await page.getByLabel('Company name').fill('Northwind');
  await page.getByRole('button', { name: 'Create company' }).click();
  await expect(page.getByRole('heading', { name: 'Workspace created' })).toBeVisible();
  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(page.getByRole('alert')).toContainText('could not confirm logout', { ignoreCase: true });
  await expect(page.getByRole('heading', { name: 'Workspace created' })).toBeVisible();
  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeFocused();
  await expect(page.getByRole('status')).toHaveText('You are logged out.');
});
