# Rill Frontend Standards

> Every page. Every time. No exceptions.

## 1. Project Selection — Single Source of Truth

The sidebar `ProjectSelector` is the **only** project selection mechanism in the app. No page may have its own project dropdown, input, or selector.

**Every page MUST:**
- Read initial state from `prefs.projects` (array), NOT `prefs.project` (deprecated singular)
- Listen to `window.addEventListener('project-changed', ...)` with `e.detail.projects` (array)
- Show ALL data when `projects` is empty; filtered data when projects are selected
- Never have its own `<select>`, `<input>`, or tab-based project picker

**Pattern:**
```js
let project = $state(prefs.projects?.[0] || '');

onMount(() => {
  loadData();
  const handler = (e) => {
    const sel = e.detail.projects || [];
    project = sel.length === 1 ? sel[0] : '';
    loadData();
  };
  window.addEventListener('project-changed', handler);
  return () => window.removeEventListener('project-changed', handler);
});
```

## 2. State Handling — Four States Everywhere

Every data-fetching page MUST handle exactly four states:

| State | Visual | When |
|-------|--------|------|
| **Loading** | Spinner or skeleton | Data being fetched |
| **Empty** | Helpful message + suggested action | No data, explain why |
| **Error** | Error banner with message | Fetch/parse failure |
| **Data** | Normal content | Data available |

**Pattern:**
```svelte
{#if loading}
  <div class="loading"><div class="spinner"></div></div>
{:else if error}
  <div class="error-banner">{error}</div>
{:else if results.length === 0}
  <div class="empty-state"><p>No results. Try adjusting filters.</p></div>
{:else}
  <!-- data content -->
{/if}
```

**Empty states must explain WHY and suggest NEXT ACTION:**
- "No documents in this project. Create one to get started."
- "No clusters found. Dream runs hourly — check back later."
- "Select a project to view documents." (but prefer showing ALL when none selected)

## 3. CSS Variables — Use the System

All colors come from CSS variables defined in `theme.css`. Never hardcode hex or oklch values.

```css
/* ✅ DO */
color: var(--text-primary);
background: var(--bg-secondary);
border: 1px solid var(--border);
color: var(--accent);

/* ❌ DON'T */
color: oklch(0.65 0.22 260);
background: #1a1a2e;
```

Exception: component-scoped `<style>` blocks may use oklch for component-specific color variants that aren't in the global palette.

## 4. Data Access — Through the API Module

All backend calls go through `$lib/api.js`. Never use raw `fetch` or `curl`-equivalent.

```js
// ✅ DO
import { api } from '$lib/api.js';
const results = await api.search(query, project);

// ❌ DON'T
const resp = await fetch('/api/mcp', { method: 'POST', ... });
```

## 5. Component Architecture

- **Shared components** live in `$lib/` (e.g., `ProjectSelector.svelte`, `CommandPalette.svelte`)
- **Page components** live in `routes/<page>/+page.svelte`
- **Shared styles** in `lib/theme.css` (global) and `src/app.css` (Tailwind)
- **No duplicated logic** between pages. If two pages do the same thing, extract a shared component or utility

## 6. Naming Conventions

- CSS classes: kebab-case (`.dense-row`, `.memory-card`)
- Component files: PascalCase (`ProjectSelector.svelte`)
- JavaScript variables: camelCase (`project`, `loadData`)
- Svelte stores: camelCase (`prefs.projects`)

## 7. Sidebar Navigation

The sidebar in `+layout.svelte` contains:
1. Logo
2. ProjectSelector
3. Navigation links (Dashboard, Memories, Explore, Clusters, Documents, Settings)

No page may modify or override the sidebar. It is the frame.

## 8. Keyboard & Accessibility

- Tab order must flow logically
- Interactive elements must be keyboard-accessible (Enter/Space to activate)
- Focus states must be visible
- Color is not the only indicator (use icons, text alongside color)

## 9. Responsive Design

- Mobile: sidebar collapses, cards stack vertically
- All touch targets ≥ 44x44px on mobile
- Test at 320px, 768px, 1280px

## 10. Playwright Gate

Before reporting a change as "done":
1. `npx playwright test` must pass all tests
2. Manual check of the affected page
3. Verify the page works with: no project selected, one project selected, nav back/forward

## 11. Dense/Card Toggle

Every data-display page includes a `DenseToggle` component inline with the page title. Uses Lucide `LayoutGrid`/`LayoutList` icons. Shared component at `$lib/DenseToggle.svelte`.

**Pattern:**
```svelte
import DenseToggle from '$lib/DenseToggle.svelte';
// ...
<h1>Memories <DenseToggle dense={dense} ontoggle={() => { dense = !dense; prefs.dense = dense; }} /></h1>
```
