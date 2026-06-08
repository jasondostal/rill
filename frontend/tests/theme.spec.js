import { test, expect } from '@playwright/test';

test('dark theme renders correctly', async ({ page }) => {
  await page.goto('http://localhost:5173');

  // Check background is dark.
  const bgColor = await page.evaluate(() => {
    return getComputedStyle(document.body).backgroundColor;
  });
  expect(bgColor).not.toBe('rgb(255, 255, 255)');

  // Check logo is visible.
  const logo = page.locator('.logo');
  await expect(logo).toBeVisible();
  await expect(logo).toHaveText('rill');
});

test('sidebar navigation links exist', async ({ page }) => {
  await page.goto('http://localhost:5173');
  await expect(page.locator('text=Dashboard')).toBeVisible();
  await expect(page.locator('text=Memories')).toBeVisible();
  await expect(page.locator('text=Documents')).toBeVisible();
});

test('OKLCH color consistency', async ({ page }) => {
  await page.goto('http://localhost:5173');
  // Verify accent color is set via OKLCH.
  const color = await page.evaluate(() => {
    return getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
  });
  expect(color).toContain('oklch');
});
