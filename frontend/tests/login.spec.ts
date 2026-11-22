import { expect, test, type Page, type Route } from '@playwright/test';

const loginPath = '**/api/v1/auth/login';
const mePath = '**/api/v1/auth/me';
const logoutPath = '**/api/v1/auth/logout';
const credentials = { username: 'Alice', password: 'test-only login passphrase' };
const profile = {
  id: '560469c2-b87d-4163-8d47-11104313defd',
  username: 'alice',
  email: 'alice@example.test',
  display_name: 'Alice Example',
};

function respondSignedOut(route: Route) {
  return route.fulfill({ status: 401, json: { error: 'invalid or missing session' } });
}

async function loginForm(page: Page) {
  await page.getByLabel('Username').fill(credentials.username);
  await page.getByLabel('Password').fill(credentials.password);
  await page.getByRole('button', { name: 'Log in', exact: true }).click();
}

test('navigates between registration and login', async ({ page }) => {
  await page.route(mePath, respondSignedOut);
  await page.goto('/');
  await page.getByRole('link', { name: 'Log in' }).click();
  await expect(page.getByLabel('Password')).toHaveAttribute('autocomplete', 'current-password');
  await page.getByRole('link', { name: 'Create an account' }).click();
  await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible();
});

test('logs in once, reads the profile, and restores the session after refresh', async ({ page }) => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let signedIn = false;
  let loginRequests = 0;
  let profileRequests = 0;
  await page.route(mePath, (route) => {
    profileRequests++;
    return signedIn ? route.fulfill({ status: 200, json: profile }) : respondSignedOut(route);
  });
  await page.route(loginPath, async (route) => {
    loginRequests++;
    await gate;
    signedIn = true;
    await route.fulfill({ status: 204 });
  });
  await page.goto('/login');
  const request = page.waitForRequest((req) => new URL(req.url()).pathname === '/api/v1/auth/login');
  try {
    await loginForm(page);
    const submitted = await request;
    expect(submitted.method()).toBe('POST');
    expect(new URL(submitted.url()).origin).toBe(new URL(page.url()).origin);
    expect(submitted.headers()['content-type']).toBe('application/json');
    expect(submitted.postDataJSON()).toEqual(credentials);
    await expect(page.getByRole('button', { name: 'Logging in…' })).toBeDisabled();
    await expect(page.getByLabel('Password')).toHaveValue('');
    await page.locator('form').evaluate((form) => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
  } finally {
    release();
  }
  const welcome = page.getByRole('heading', { name: 'Welcome, Alice Example' });
  await expect(welcome).toBeFocused();
  expect(profileRequests).toBe(2);
  await page.reload();
  await expect(welcome).toBeVisible();
  expect(profileRequests).toBe(3);
  expect(loginRequests).toBe(1);
  await expect(page.getByLabel('Password')).toHaveCount(0);
});

for (const failure of [
  { status: 401, message: 'Invalid username or password.' },
  { status: 500, message: 'The server could not log you in.' },
  { status: 0, message: 'Could not reach DealForge.' },
]) {
  test(`handles ${failure.status || 'network'} login failures without keeping the password`, async ({ page }) => {
    await page.route(mePath, respondSignedOut);
    await page.route(loginPath, (route) => failure.status
      ? route.fulfill({ status: failure.status, json: { error: 'private details' } })
      : route.abort());
    await page.setViewportSize({ width: 360, height: 640 });
    await page.goto('/login');
    await loginForm(page);
    await expect(page.getByRole('alert')).toContainText(failure.message);
    await expect(page.getByRole('alert')).not.toContainText('private details');
    await expect(page.getByLabel('Username')).toHaveValue(credentials.username);
    await expect(page.getByLabel('Password')).toHaveValue('');
    await expect(page.getByRole('button', { name: 'Log in', exact: true })).toBeEnabled();
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
  });
}

test('retries a failed profile request after login without resubmitting credentials', async ({ page }) => {
  let loginRequests = 0;
  let profileRequests = 0;
  await page.route(loginPath, (route) => {
    loginRequests++;
    return route.fulfill({ status: 204 });
  });
  await page.route(mePath, (route) => {
    profileRequests++;
    if (profileRequests === 1) return respondSignedOut(route);
    if (profileRequests === 2) return route.abort();
    return route.fulfill({ status: 200, json: profile });
  });
  await page.goto('/login');
  await loginForm(page);
  await expect(page.getByRole('alert')).toContainText('Could not check your session.');
  await expect(page.getByRole('heading', { name: 'Welcome, Alice Example' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Retry session check' }).click();
  await expect(page.getByRole('heading', { name: 'Welcome, Alice Example' })).toBeVisible();
  expect(loginRequests).toBe(1);
});

test('keeps the welcome screen until logout succeeds and shows the login form afterward', async ({ page }) => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let active = true;
  await page.route(mePath, (route) => active ? route.fulfill({ status: 200, json: profile }) : respondSignedOut(route));
  await page.route(logoutPath, async (route) => {
    await gate;
    active = false;
    await route.fulfill({ status: 204 });
  });
  await page.goto('/login');
  const pending = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/auth/logout');
  try {
    await page.getByRole('button', { name: 'Logout' }).click();
    expect((await pending).method()).toBe('POST');
    await expect(page.getByRole('button', { name: 'Logging out…' })).toBeDisabled();
    await expect(page.getByRole('heading', { name: 'Welcome, Alice Example' })).toBeVisible();
    await expect(page.getByText('You are logged out.')).toHaveCount(0);
  } finally {
    release();
  }
  await expect(page.getByRole('status')).toHaveText('You are logged out.');
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeFocused();
  await page.reload();
  await expect(page.getByRole('button', { name: 'Log in', exact: true })).toBeVisible();
});

for (const fail of ['server', 'network']) {
  test(`preserves the authenticated view after a ${fail} logout failure`, async ({ page }) => {
    await page.route(mePath, (route) => route.fulfill({ status: 200, json: profile }));
    await page.route(logoutPath, (route) => fail === 'server'
      ? route.fulfill({ status: 500, json: { error: 'private details' } })
      : route.abort());
    await page.goto('/login');
    await page.getByRole('button', { name: 'Logout' }).click();
    await expect(page.getByRole('alert')).toContainText('could not confirm logout', { ignoreCase: true });
    await expect(page.getByRole('heading', { name: 'Welcome, Alice Example' })).toBeVisible();
    await expect(page.getByText('You are logged out.')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Logout' })).toBeEnabled();
  });
}
