import { expect, test } from '@playwright/test';

test('loads the DealForge home page', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByText('DealForge', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Create an account' })).toBeVisible();
});
