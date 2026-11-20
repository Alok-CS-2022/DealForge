import { expect, test } from '@playwright/test';

test('loads the DealForge home page', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'DealForge' })).toBeVisible();
});
