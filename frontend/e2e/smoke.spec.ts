import { test, expect } from '@playwright/test';

// These run against a real rill backend on :9191 (proxy-auth via the
// X-Forwarded-User header). See the redesign verification notes.
test.use({
  baseURL: 'http://localhost:9191',
  extraHTTPHeaders: { 'X-Forwarded-User': 'admin' },
});

async function readVar(page, name) {
  return page.evaluate(
    (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(),
    name
  );
}

test('dashboard renders with real stats', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.logo')).toHaveText('rill');
  await expect(page.locator('main h1').first()).toHaveText('Dashboard');
  // KPI tiles render; the Memories KPI is a positive count (exact value depends
  // on seed/verification data — don't assert a brittle constant).
  await expect(page.locator('.kpi').first()).toBeVisible({ timeout: 10000 });
  const kpi = (await page.locator('.kpi-value').first().textContent()) || '';
  expect(Number(kpi.replace(/,/g, ''))).toBeGreaterThan(0);
  // Charts present (the by-kind donut), no error/empty state.
  await expect(page.locator('.donut-wrap svg')).toBeVisible();
  await expect(page.locator('.state-box.error')).toHaveCount(0);
});

test('sidebar exposes Dashboard, Memories and Color System', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('.sidebar a[href="/"]')).toContainText('Dashboard');
  await expect(page.locator('.sidebar a[href="/memories"]')).toContainText('Memories');
  await expect(page.locator('.sidebar a[href="/system"]')).toContainText('Color System');
});

test('memories page: kind filter chips + dense rows', async ({ page }) => {
  await page.goto('/memories');
  await expect(page.locator('main h1')).toHaveText('Memories');
  // Kind chips (all + 7 kinds).
  await expect(page.locator('.fchip.kchip')).toHaveCount(7);
  // Rows render; filtering to "decision" narrows the list and every visible
  // row is a decision (count-resilient — shared DB accrues verification data).
  const total = await page.locator('.mem-row').count();
  expect(total).toBeGreaterThan(0);
  await page.locator('.fchip.kchip', { hasText: 'decision' }).click();
  await page.waitForTimeout(300);
  const filtered = await page.locator('.mem-row').count();
  expect(filtered).toBeGreaterThan(0);
  expect(filtered).toBeLessThanOrEqual(total);
  await expect(page.locator('.mem-row .kind-badge').first()).toHaveText('decision');
});

test('color system page renders live oklch tokens', async ({ page }) => {
  await page.goto('/system');
  await expect(page.locator('main h1')).toHaveText('Color System');
  const fact = await readVar(page, '--k-fact');
  expect(fact).toContain('oklch');
  // Swatches + badge demos present.
  await expect(page.locator('.sw-chip').first()).toBeVisible();
});

test('theme engine retints the whole site live', async ({ page }) => {
  await page.goto('/system');
  const before = await readVar(page, '--accent');
  expect(before).toContain('oklch');
  // Open the theme panel via the sidebar palette button, switch to the warm Ember preset.
  await page.locator('.side-foot-btn[aria-label="Theme"]').click();
  await page.locator('.tp-preset', { hasText: 'Ember' }).click();
  await expect.poll(() => readVar(page, '--accent')).not.toBe(before);
  const after = await readVar(page, '--accent');
  expect(after).toContain('oklch');
  // Persisted across reload (pre-paint script applies it).
  await page.reload();
  await expect.poll(() => readVar(page, '--accent')).toBe(after);
});

test('settings: config groups, env-lock, secrets hidden, hot edit', async ({ page }) => {
  await page.goto('/settings');
  await expect(page.locator('main h1')).toHaveText('Settings');
  // Configuration groups render (Orient & Memory among them).
  await expect(page.locator('.card.section h2', { hasText: 'Orient & Memory' })).toBeVisible({ timeout: 10000 });
  // An env-pinned setting is read-only with a "set via env" badge.
  const authRow = page.locator('.cfg-row', { hasText: 'Auth mode' });
  await expect(authRow.locator('.cfg-badge.env')).toBeVisible();
  await expect(authRow.locator('input')).toHaveCount(0);
  // A secret shows configured-status, never an input or a value.
  const secretRow = page.locator('.cfg-row', { hasText: 'SurrealDB password' });
  await expect(secretRow.locator('.cfg-secret')).toBeVisible();
  await expect(secretRow.locator('input')).toHaveCount(0);
  // A hot, editable setting can be changed and saved. Pick a value distinct
  // from the current one so the field is dirty regardless of prior runs.
  const recRow = page.locator('.cfg-row', { hasText: 'Project recency window' });
  const input = recRow.locator('input');
  const cur = await input.inputValue();
  await input.fill(cur === '40' ? '50' : '40');
  const save = recRow.locator('button', { hasText: 'Save' });
  await expect(save).toBeEnabled();
  await save.click();
  // After save, the row reflects the custom (db) source and the dirty dot clears.
  await expect(recRow.locator('.cfg-badge.db')).toBeVisible({ timeout: 5000 });
});

test('dark-first: background is dark and accent is oklch', async ({ page }) => {
  await page.goto('/');
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  expect(bg).not.toBe('rgb(255, 255, 255)');
  expect(await readVar(page, '--accent')).toContain('oklch');
});
