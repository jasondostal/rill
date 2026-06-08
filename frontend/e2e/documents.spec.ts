import { test, expect } from '@playwright/test';

// Full document lifecycle through the UI: create → render markdown → edit →
// list → delete. Requires the backend running with a reachable store (proxy
// auth in dev). Confirm dialogs are auto-accepted.
test('document lifecycle: create, render, edit, list, delete', async ({ page }) => {
  page.on('dialog', (d) => d.accept());

  // --- Create ---
  await page.goto('/documents/new');
  await expect(page.locator('main h1')).toHaveText('New document');
  await page.locator('.editor input').first().fill('Playwright Doc');
  await page.locator('.editor textarea').fill(
    '# Heading One\n\n## Subsection\n\nSome **bold** text and `inline code`.\n\n- item a\n- item b\n'
  );
  await page.click('button:has-text("Create")');

  // Redirected to the reader.
  await expect(page).toHaveURL(/\/documents\/[^/]+$/);
  await expect(page.locator('.doc-title')).toHaveText('Playwright Doc');

  // --- Markdown actually rendered (not raw) ---
  await expect(page.locator('.markdown-body h2')).toHaveText('Subsection');
  await expect(page.locator('.markdown-body strong')).toHaveText('bold');
  await expect(page.locator('.markdown-body code').first()).toContainText('inline code');
  await expect(page.locator('.markdown-body li')).toHaveCount(2);

  // Download .md link points at the export endpoint.
  await expect(page.locator('a:has-text("Download .md")')).toHaveAttribute('href', /\/export\.md$/);

  // --- Edit (title) ---
  await page.click('button:has-text("Edit")');
  await page.locator('.editor input').first().fill('Playwright Doc v2');
  await page.click('button:has-text("Save")');
  await expect(page.locator('.doc-title')).toHaveText('Playwright Doc v2');

  // --- Appears in the list ---
  await page.goto('/documents');
  await expect(page.locator('main h1')).toHaveText('Documents');
  await expect(page.locator('.list')).toContainText('Playwright Doc v2');

  // --- Delete from the reader, then it's gone from the list ---
  await page.locator('.list a', { hasText: 'Playwright Doc v2' }).click();
  await expect(page.locator('.doc-title')).toHaveText('Playwright Doc v2');
  await page.click('button:has-text("Delete")');
  await expect(page).toHaveURL(/\/documents$/);
  // Count-based so it holds whether the list still renders or shows the empty state.
  await expect(page.locator('.list a', { hasText: 'Playwright Doc v2' })).toHaveCount(0);
});

// Quick-delete from the list view, with the undo safety net. The delete is a
// real soft-delete; Undo calls the restore endpoint and the row comes back.
test('list-view trash icon deletes with undo', async ({ page }) => {
  // Seed a doc through the create flow.
  await page.goto('/documents/new');
  await page.locator('.editor input').first().fill('Trash Me Doc');
  await page.locator('.editor textarea').fill('disposable');
  await page.click('button:has-text("Create")');
  await expect(page).toHaveURL(/\/documents\/[^/]+$/);

  // On the list, the row carries a delete button.
  await page.goto('/documents');
  const rowSel = page.locator('.row', { hasText: 'Trash Me Doc' });
  await expect(rowSel).toHaveCount(1);

  // Delete → row vanishes immediately, undo toast appears.
  await rowSel.locator('.del').click();
  await expect(page.locator('.row', { hasText: 'Trash Me Doc' })).toHaveCount(0);
  const undo = page.locator('[data-sonner-toast] button', { hasText: 'Undo' });
  await expect(undo).toBeVisible();

  // Undo → row comes back.
  await undo.click();
  await expect(page.locator('.row', { hasText: 'Trash Me Doc' })).toHaveCount(1);

  // Delete for real (don't undo) so it doesn't linger in the active list.
  await page.locator('.row', { hasText: 'Trash Me Doc' }).locator('.del').click();
  await expect(page.locator('.row', { hasText: 'Trash Me Doc' })).toHaveCount(0);
});

// Phase 3: entity association — link/unlink on the reader, surfaced on the
// entity page, and "New document about this entity" prefill.
test('document ↔ entity association', async ({ page }) => {
  page.on('dialog', (d) => d.accept());

  // Seed an entity to link against (via the authed API).
  const seed = await page.request.post('/api/remember', {
    data: { summary: 'pi is a small linux host', kind: 'fact', author: 'alice', entities: [{ name: 'pi', type: 'tool' }] },
  });
  expect(seed.ok()).toBe(true);

  // Create a doc, then link it to tool:pi from the reader.
  await page.goto('/documents/new');
  await page.locator('.editor input').first().fill('Assoc Doc');
  await page.locator('.editor textarea').fill('about pi');
  await page.click('button:has-text("Create")');
  await expect(page).toHaveURL(/\/documents\/[^/]+$/);

  await expect(page.locator('.no-assoc')).toBeVisible();
  await page.click('button:has-text("+ Link entity")');
  await page.locator('.link-form select').selectOption('tool');
  await page.locator('.link-form input').fill('pi');
  await page.locator('.link-form button:has-text("Add")').click();
  await expect(page.locator('.echip')).toContainText('pi');

  // It shows on the entity page.
  await page.goto('/entities/tool/pi');
  await expect(page.locator('.entity-docs')).toContainText('Assoc Doc');

  // "New document about this tool" prefills the entity and links on create.
  await page.locator('.ed-new').click();
  await expect(page).toHaveURL(/\/documents\/new\?entity=tool%3Api/);
  await expect(page.locator('.link-note')).toContainText('tool:pi');
  await page.locator('.editor input').first().fill('About Pi Doc');
  await page.click('button:has-text("Create")');
  await expect(page).toHaveURL(/\/documents\/[^/]+$/);
  await expect(page.locator('.echip')).toContainText('pi');

  // Unlink removes the chip.
  await page.locator('.echip', { hasText: 'pi' }).locator('.unlink').click();
  await expect(page.locator('.echip')).toHaveCount(0);
});

// Empty/loading/data states render and the route is reachable from the nav.
test('documents page reachable from sidebar', async ({ page }) => {
  await page.goto('/');
  await page.click('a[href="/documents"]');
  await expect(page).toHaveURL(/\/documents$/);
  await expect(page.locator('main h1')).toHaveText('Documents');
  await expect(page.locator('a:has-text("+ New document")').first()).toBeVisible();
});
