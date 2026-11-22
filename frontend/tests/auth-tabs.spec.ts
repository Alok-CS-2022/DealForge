import { expect, test, type BrowserContext, type Page } from '@playwright/test';

const mePath = '**/api/v1/auth/me';
const companiesPath = '**/api/v1/companies';
const expectedUserHeader = 'x-dealforge-expected-user-id';
const alice = {
  id: 'd008aa53-5fce-42ec-bdaf-d9495dcaaf24',
  username: 'alice',
  email: 'alice@example.test',
  display_name: 'Alice Example',
};
const bob = {
  id: '432ab075-41e4-4da3-89d6-3920fab398d0',
  username: 'bob',
  email: 'bob@example.test',
  display_name: 'Bob Example',
};
const aliceCompany = { id: 'f1654ebb-5a4e-40a6-a691-45c01ac84daf', name: 'Alice Company', role: 'admin' };
const bobCompany = { id: '08504ebb-5a4e-40a6-a691-45c01ac84d34', name: 'Bob Company', role: 'member' };

async function mockSharedSession(context: BrowserContext) {
  let active: typeof alice | null = alice;
  await context.route(mePath, (route) => active
    ? route.fulfill({ status: 200, json: active })
    : route.fulfill({ status: 401 }));
  await context.route(companiesPath, (route) => active
    ? route.fulfill({ status: 200, json: [active === alice ? aliceCompany : bobCompany] })
    : route.fulfill({ status: 401 }));
  await context.route('**/api/v1/auth/logout', (route) => {
    active = null;
    return route.fulfill({ status: 204 });
  });
  await context.route('**/api/v1/auth/login', (route) => {
    active = bob;
    return route.fulfill({ status: 204 });
  });
}

async function submitLogin(page: Page, username: string) {
  await page.getByLabel('Username').fill(username);
  await page.getByLabel('Password').fill('test-only passphrase');
  await page.getByRole('button', { name: 'Log in', exact: true }).click();
}

async function logInAsBob(page: Page) {
  await submitLogin(page, bob.username);
  await expect(page.getByRole('heading', { name: bobCompany.name })).toBeVisible();
}

async function mockCookieSession(context: BrowserContext, aliceCompanies = [aliceCompany]) {
  function cookieAccount(cookie: string | null) {
    const username = cookie?.match(/(?:^|; )test_session=(\w+)/)?.[1];
    return [alice, bob].find((user) => user.username === username);
  }
  await context.route(mePath, async (route) => {
    const account = cookieAccount(await route.request().headerValue('cookie'));
    await route.fulfill(account ? { status: 200, json: account } : { status: 401 });
  });
  await context.route(companiesPath, async (route) => {
    const request = route.request();
    const account = cookieAccount(await request.headerValue('cookie'));
    if (!account) return route.fulfill({ status: 401 });
    const expected = await request.headerValue(expectedUserHeader);
    if ((request.method() === 'POST' || expected !== null) && expected !== account.id) {
      return route.fulfill({ status: 412, json: { error: 'session identity precondition failed' } });
    }
    return route.fulfill({ status: 200, json: account === alice ? aliceCompanies : [bobCompany] });
  });
  await context.route('**/api/v1/auth/login', (route) => {
    const { username } = route.request().postDataJSON();
    return route.fulfill({ status: 204, headers: { 'Set-Cookie': `test_session=${username}; Path=/; HttpOnly` } });
  });
  await context.route('**/api/v1/auth/logout', (route) => route.fulfill({
    status: 204, headers: { 'Set-Cookie': 'test_session=; Max-Age=0; Path=/; HttpOnly' },
  }));
}

function pause() {
  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  return { gate, release };
}

async function holdAuthResponse(page: Page, action: 'login' | 'logout') {
  const pending = pause();
  const arrived = pause();
  await page.route(`**/api/v1/auth/${action}`, async (route) => {
    arrived.release();
    await pending.gate;
    await route.fallback();
  });
  return { arrived: arrived.gate, release: pending.release };
}

async function currentSession(page: Page) {
  return page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/me');
    return { status: response.status, user: response.ok ? await response.json() : null };
  });
}

async function expectAliceWorkspace(page: Page) {
  await expect(page.getByRole('heading', { name: aliceCompany.name })).toBeVisible();
  await expect(page.getByText('Signed in as Alice Example')).toBeVisible();
  await expect(page.getByRole('heading', { name: bobCompany.name })).toHaveCount(0);
  expect(await currentSession(page)).toEqual({ status: 200, user: alice });
}

async function holdLoginNotification(page: Page) {
  await page.addInitScript(() => {
    const fetchResponse = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await fetchResponse(input, init);
      if (input === '/api/v1/auth/login' && response.status === 204) {
        // Hold the app's notification after Chromium has applied the cookie.
        await new Promise<void>((resolve) => {
          window.addEventListener('release-test-login', () => resolve(), { once: true });
          document.documentElement.dataset.loginCookieApplied = 'true';
        });
      }
      return response;
    };
  });
  return () => page.evaluate(() => window.dispatchEvent(new Event('release-test-login')));
}

test('a stale onboarding form cannot create a company under the new account', async ({ context, page }) => {
  await mockCookieSession(context, []);
  const other = await context.newPage();
  const releaseNotification = await holdLoginNotification(other);
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  const login = await holdAuthResponse(other, 'login');
  const sessionCheck = pause();
  const companyList = pause();
  let creationAttempts = 0;
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().endsWith('/api/v1/companies')) creationAttempts++;
  });
  try {
    await submitLogin(other, bob.username);
    await login.arrived;
    await submitLogin(page, alice.username);
    await expect(page.getByText('Signed in as Alice Example')).toBeVisible();
    await page.getByLabel('Company name').fill('Stale company draft');
    login.release();
    await expect(other.locator('html')).toHaveAttribute('data-login-cookie-applied', 'true');
    expect(await currentSession(other)).toEqual({ status: 200, user: bob });

    await page.route(mePath, async (route) => {
      await sessionCheck.gate;
      await route.fallback();
    });
    await page.route(companiesPath, async (route) => {
      if (route.request().method() === 'GET') await companyList.gate;
      await route.fallback();
    });
    const rejection = page.waitForResponse((response) =>
      response.url().endsWith('/api/v1/companies') && response.request().method() === 'POST');
    await page.getByRole('button', { name: 'Create company' }).click();
    const result = await rejection;
    expect(result.status()).toBe(412);
    expect(await result.request().headerValue(expectedUserHeader)).toBe(alice.id);
    await expect(page.getByRole('status')).toHaveText('Checking your session…');
    await expect(page.getByText('Signed in as Alice Example')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Create company' })).toHaveCount(0);

    sessionCheck.release();
    await expect(page.getByRole('status')).toHaveText('Loading your companies…');
    await expect(page.getByRole('button', { name: 'Create company' })).toHaveCount(0);
    companyList.release();
    await expect(page.getByRole('heading', { name: bobCompany.name })).toBeVisible();
    await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
    await expect(page.getByText('Stale company draft')).toHaveCount(0);
    expect(creationAttempts).toBe(1);
    expect(await page.evaluate((id) => localStorage.getItem(`dealforge.selected-company.${id}`), alice.id)).toBeNull();
  } finally {
    login.release();
    sessionCheck.release();
    companyList.release();
    await releaseNotification();
  }
});

test('a company list spanning an account switch preserves both accounts’ preferences', async ({ context, page }) => {
  await mockCookieSession(context);
  const other = await context.newPage();
  const releaseNotification = await holdLoginNotification(other);
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  const login = await holdAuthResponse(other, 'login');
  const oldSession = pause();
  try {
    await submitLogin(other, bob.username);
    await login.arrived;
    await submitLogin(page, alice.username);
    await expectAliceWorkspace(page);

    let firstRead = true;
    const sessionRequested = pause();
    await page.route(mePath, async (route) => {
      if (!firstRead) return route.fallback();
      firstRead = false;
      sessionRequested.release();
      await oldSession.gate;
      await route.fulfill({ status: 200, json: alice });
    });
    await page.reload();
    await sessionRequested.gate;
    login.release();
    await expect(other.locator('html')).toHaveAttribute('data-login-cookie-applied', 'true');
    const rejection = page.waitForResponse((response) =>
      response.url().endsWith('/api/v1/companies') && response.status() === 412);
    oldSession.release();
    expect(await (await rejection).request().headerValue(expectedUserHeader)).toBe(alice.id);
    await expect(page.getByRole('heading', { name: bobCompany.name })).toBeVisible();
    await expect(page.getByText('Signed in as Bob Example')).toBeVisible();
    await expect(page.getByRole('heading', { name: aliceCompany.name })).toHaveCount(0);
    const preferences = await page.evaluate((ids) =>
      ids.map((id) => localStorage.getItem(`dealforge.selected-company.${id}`)), [alice.id, bob.id]);
    expect(preferences).toEqual([aliceCompany.id, bobCompany.id]);
  } finally {
    login.release();
    oldSession.release();
    await releaseNotification();
  }
});

test('the last successful overlapping login revalidates both tabs', async ({ context, page }) => {
  await mockCookieSession(context);
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  const login = await holdAuthResponse(other, 'login');
  try {
    await submitLogin(other, alice.username);
    await login.arrived;
    await logInAsBob(page);
    await expect(other.getByRole('heading', { name: bobCompany.name })).toBeVisible();
    await expect(other.getByRole('button', { name: 'Logout' })).toBeDisabled();
    login.release();
    await Promise.all([expectAliceWorkspace(page), expectAliceWorkspace(other)]);
  } finally {
    login.release();
  }
});

test('a concurrent logout that clears the shared cookie last revalidates both tabs', async ({ context, page }) => {
  await mockCookieSession(context);
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  const login = await holdAuthResponse(other, 'login');
  const logout = await holdAuthResponse(page, 'logout');
  try {
    await submitLogin(other, alice.username);
    await login.arrived;
    await logInAsBob(page);
    await page.getByRole('button', { name: 'Logout', exact: true }).click();
    await logout.arrived;
    login.release();
    await Promise.all([expectAliceWorkspace(page), expectAliceWorkspace(other)]);
    logout.release();
    for (const tab of [page, other]) {
      await expect(tab.getByRole('button', { name: 'Log in', exact: true })).toBeVisible();
      await expect(tab.getByText('Signed in as')).toHaveCount(0);
      expect((await currentSession(tab)).status).toBe(401);
    }
  } finally {
    login.release();
    logout.release();
  }
});

test('a delayed logout completion respects a newer login cookie', async ({ context, page }) => {
  await mockCookieSession(context);
  await page.addInitScript(() => {
    const fetchResponse = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await fetchResponse(input, init);
      if (input === '/api/v1/auth/logout') {
        // The browser has applied Set-Cookie. Hold the response only from the app.
        await new Promise<void>((resolve) => {
          window.addEventListener('release-test-logout', () => resolve(), { once: true });
          document.documentElement.dataset.logoutResponseHeld = 'true';
        });
      }
      return response;
    };
  });
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  const login = await holdAuthResponse(other, 'login');
  try {
    await submitLogin(other, alice.username);
    await login.arrived;
    await logInAsBob(page);
    await page.getByRole('button', { name: 'Logout', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('data-logout-response-held', 'true');
    expect((await currentSession(other)).status).toBe(401);
    login.release();
    await Promise.all([expectAliceWorkspace(page), expectAliceWorkspace(other)]);

    const revalidation = page.waitForResponse((response) =>
      new URL(response.url()).pathname === '/api/v1/auth/me');
    await page.evaluate(() => window.dispatchEvent(new Event('release-test-logout')));
    expect(await (await revalidation).json()).toEqual(alice);
    await Promise.all([expectAliceWorkspace(page), expectAliceWorkspace(other)]);
    await expect(page.getByRole('button', { name: 'Logout', exact: true })).toBeEnabled();
  } finally {
    login.release();
  }
});

test('logout clears the other tab before its session check finishes', async ({ context, page }) => {
  await mockSharedSession(context);
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  await expect(other.getByRole('heading', { name: aliceCompany.name })).toBeVisible();

  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await other.route(mePath, async (route) => {
    await gate;
    await route.fallback();
  });
  try {
    await page.getByRole('button', { name: 'Logout' }).click();
    await expect(page.getByText('You are logged out.')).toBeVisible();
    await expect(other.getByRole('status')).toHaveText('Checking your session…');
    await expect(other.getByRole('heading', { name: aliceCompany.name })).toHaveCount(0);
    await expect(other.getByText('Signed in as Alice Example')).toHaveCount(0);
  } finally {
    release();
  }
  await expect(other.getByRole('button', { name: 'Log in', exact: true })).toBeVisible();
});

test('switching accounts revalidates both endpoints in the other tab', async ({ context, page }) => {
  await mockSharedSession(context);
  const other = await context.newPage();
  await Promise.all([page.goto('/login'), other.goto('/login')]);
  await expect(other.getByRole('heading', { name: aliceCompany.name })).toBeVisible();
  await page.getByRole('button', { name: 'Logout' }).click();
  await expect(other.getByRole('button', { name: 'Log in', exact: true })).toBeVisible();
  const observed = await other.evaluateHandle(() => {
    const channel = new BroadcastChannel('dealforge.auth');
    const messages: unknown[] = [];
    channel.onmessage = (event) => messages.push(event.data);
    return { messages, channel };
  });

  let release = () => {};
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await other.route(companiesPath, async (route) => {
    await gate;
    await route.fallback();
  });
  try {
    await logInAsBob(page);
    await expect(other.getByRole('status')).toHaveText('Loading your companies…');
    await expect(other.getByRole('heading', { name: aliceCompany.name })).toHaveCount(0);
    await expect(other.getByRole('heading', { name: bobCompany.name })).toHaveCount(0);
  } finally {
    release();
  }
  await expect(other.getByRole('heading', { name: bobCompany.name })).toBeVisible();
  await expect(other.getByText('Signed in as Bob Example')).toBeVisible();
  expect(await observed.evaluate(({ messages, channel }) => {
    channel.close();
    return messages;
  })).toEqual(['session-changed']);
  await observed.dispose();
});

for (const delayed of [
  { path: mePath, url: '/api/v1/auth/me', body: alice, label: 'session' },
  { path: companiesPath, url: '/api/v1/companies', body: [aliceCompany], label: 'company list' },
]) {
  test(`a stale ${delayed.label} response cannot restore the old account`, async ({ context, page }) => {
    await mockSharedSession(context);
    await page.goto('/login');
    await expect(page.getByRole('heading', { name: aliceCompany.name })).toBeVisible();
    const other = await context.newPage();
    // Let the old response reach JavaScript even after cancellation to exercise the state guard.
    await other.addInitScript((url) => {
      const fetchWithSignal = window.fetch.bind(window);
      window.fetch = (input, init) => fetchWithSignal(input,
        input === url ? { ...init, signal: undefined } : init);
    }, delayed.url);
    let markStarted = () => {};
    let resolveDelivered = () => {};
    let release = () => {};
    const started = new Promise<void>((resolve) => { markStarted = resolve; });
    const delivered = new Promise<void>((resolve) => { resolveDelivered = resolve; });
    const gate = new Promise<void>((resolve) => { release = resolve; });
    await other.route(delayed.path, async (route) => {
      markStarted();
      await gate;
      try {
        await route.fulfill({ status: 200, json: delayed.body });
      } finally {
        resolveDelivered();
      }
    }, { times: 1 });
    try {
      await other.goto('/login');
      await started;
      await page.getByRole('button', { name: 'Logout' }).click();
      await expect(other.getByRole('button', { name: 'Log in', exact: true })).toBeVisible();
      await logInAsBob(page);
      await expect(other.getByRole('heading', { name: bobCompany.name })).toBeVisible();
      const stale = other.waitForResponse((response) =>
        new URL(response.url()).pathname === delayed.url && response.status() === 200);
      release();
      const response = await stale;
      await response.finished();
      await delivered;
      await expect(other.getByRole('heading', { name: bobCompany.name })).toBeVisible();
      await expect(other.getByRole('heading', { name: aliceCompany.name })).toHaveCount(0);
      await expect(other.getByText('Signed in as Alice Example')).toHaveCount(0);
    } finally {
      release();
    }
  });
}
