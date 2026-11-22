import { expect, test, type Page } from '@playwright/test';

const endpoint = '**/api/v1/auth/register';
const input = {
  username: 'Alice_1',
  email: 'Alice@example.test',
  display_name: 'Alice Example',
  password: 'test-only passphrase',
};

async function fillRegistration(page: Page) {
  await page.getByLabel('Username').fill(input.username);
  await page.getByLabel('Email address').fill(input.email);
  await page.getByLabel('Display name').fill(input.display_name);
  await page.getByLabel('Password').fill(input.password);
}

test('submits the backend contract once and shows the login next step', async ({ page }) => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  let submissions = 0;
  await page.route(endpoint, async (route) => {
    submissions++;
    await gate;
    await route.fulfill({
      status: 201,
      json: {
        id: '560469c2-b87d-4163-8d47-11104313defd',
        username: 'alice_1',
        email: 'alice@example.test',
        display_name: input.display_name,
      },
    });
  });
  await page.goto('/');
  await fillRegistration(page);
  const request = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/auth/register');
  try {
    await page.getByRole('button', { name: 'Create account' }).click();
    const submitted = await request;
    expect(submitted.method()).toBe('POST');
    expect(new URL(submitted.url()).origin).toBe(new URL(page.url()).origin);
    expect(submitted.headers()['content-type']).toBe('application/json');
    expect(submitted.postDataJSON()).toEqual(input);
    await expect(page.getByRole('button', { name: 'Creating account…' })).toBeDisabled();
    await expect(page.getByLabel('Password')).toHaveValue('');
    await page.locator('form').evaluate((form) => {
      form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
  } finally {
    release();
  }
  const title = page.getByRole('heading', { name: 'Account created' });
  await expect(title).toBeVisible();
  await expect(title).toBeFocused();
  await expect(page.getByText('Log in with your username and password to get started.')).toBeVisible();
  await expect(page.getByLabel('Password')).toHaveCount(0);
  expect(submissions).toBe(1);
});

test('validates required fields and focuses the first problem on a narrow screen', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 760 });
  let submissions = 0;
  await page.route(endpoint, async (route) => {
    submissions++;
    await route.abort();
  });
  await page.goto('/');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByLabel('Username')).toBeFocused();
  await expect(page.getByLabel('Email address')).toHaveAccessibleDescription('Enter a valid email address.');
  await expect(page.getByLabel('Display name')).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByLabel('Password')).toHaveAttribute('aria-invalid', 'true');
  expect(submissions).toBe(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360);
});

for (const failure of [
  { status: 400, message: 'email must be a valid address of at most 254 bytes', expected: 'email must be a valid address of at most 254 bytes' },
  { status: 409, message: 'username or email already registered', expected: 'That username or email is already registered.' },
  { status: 500, message: 'internal details', expected: 'The server could not create your account.' },
  { status: 503, message: 'registration is busy, try again', expected: 'Registration is busy.' },
]) {
  test(`shows a safe, retryable error for HTTP ${failure.status}`, async ({ page }) => {
    await page.route(endpoint, (route) => route.fulfill({
      status: failure.status,
      json: { error: failure.message },
    }));
    await page.goto('/');
    await fillRegistration(page);
    await page.getByRole('button', { name: 'Create account' }).click();
    await expect(page.getByRole('alert')).toContainText(failure.expected);
    await expect(page.getByRole('alert')).not.toContainText('internal details');
    await expect(page.getByLabel('Username')).toHaveValue(input.username);
    await expect(page.getByLabel('Password')).toHaveValue('');
    await expect(page.getByRole('button', { name: 'Create account' })).toBeEnabled();
  });
}

test('shows a network error and allows a retry', async ({ page }) => {
  await page.route(endpoint, (route) => route.abort());
  await page.goto('/');
  await fillRegistration(page);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('alert')).toContainText('Could not reach DealForge.');
  await expect(page.getByRole('button', { name: 'Create account' })).toBeEnabled();
  await expect(page.getByLabel('Password')).toHaveValue('');
});
