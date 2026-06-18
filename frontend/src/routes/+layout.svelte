<script>
  import '../app.css';
  import '../lib/theme.css';
  import { onMount } from 'svelte';
  import { afterNavigate } from '$app/navigation';
  import { goto } from '$app/navigation';
  import { api } from '$lib/api.js';
  import CommandPalette from '$lib/CommandPalette.svelte';
  import ProjectSelector from '$lib/ProjectSelector.svelte';
  import { Toaster } from 'svelte-sonner';
  import { navItems, icons } from '$lib/navItems.js';
  import { applyTheme, loadTheme, saveTheme } from '$lib/theme.js';
  import { prefs } from '$lib/prefs.js';
  import ThemePicker from '$lib/ThemePicker.svelte';
  import Droplets from '@lucide/svelte/icons/droplets';
  import Menu from '@lucide/svelte/icons/menu';
  import Palette from '@lucide/svelte/icons/palette';
  import Sun from '@lucide/svelte/icons/sun';
  import Moon from '@lucide/svelte/icons/moon';

  let { children } = $props();
  let path = $state('');

  // Live OKLCH theme. The pre-paint script in app.html already applied it; we
  // re-apply on mount (SPA safety) and on every change via the picker.
  let theme = $state(loadTheme());
  let themePanelOpen = $state(false);
  function setTheme(t) { theme = t; saveTheme(t); }
  function toggleMode() {
    setTheme({ ...theme, mode: theme.mode === 'dark' ? 'light' : 'dark',
      bgL: theme.mode === 'dark' ? 0.965 : 0.175 });
  }

  let checking = $state(true);
  let identity = $state(null);
  let isAuthRoute = $derived(path === '/login' || path === '/setup');
  // Mobile-only: sidebar collapses by default and after route changes. On
  // desktop the sidebar is always-visible and this flag is ignored by CSS.
  let navOpen = $state(false);

  async function checkAuth() {
    // Auth-bypass routes — skip the probe, just clear checking.
    if (isAuthRoute) {
      identity = null;
      checking = false;
      return;
    }

    checking = true;
    try {
      identity = await api.me();
      if (!identity || identity.error) {
        try {
          const setupCheck = await fetch('/api/auth/setup', { method: 'GET' });
          const setupData = await setupCheck.json().catch(() => ({available: false}));
          await goto(setupData.available ? '/setup' : '/login');
        } catch {
          await goto('/login');
        }
        return;
      }
    } catch {
      await goto('/login');
    } finally {
      checking = false;
    }
  }

  // Pull the server-stored theme (set on any device or the sidecar) and apply
  // it. localStorage already painted instantly; this reconciles post-mount.
  // Best-effort: needs admin scope, no-ops otherwise.
  async function hydrateThemeFromServer() {
    try {
      const settings = await api.getSettings();
      const row = settings.find((s) => s.key === 'appearance.theme');
      if (row && row.value) {
        const t = JSON.parse(row.value);
        if (t && typeof t === 'object') {
          theme = { ...loadTheme(), ...t };
          prefs.theme = theme;
          applyTheme(theme);
        }
      }
    } catch { /* not admin / offline — keep local theme */ }
  }

  onMount(() => {
    if (typeof window === 'undefined') return;
    applyTheme(theme);
    path = window.location.pathname;
    checkAuth();
    hydrateThemeFromServer();
  });

  afterNavigate((nav) => {
    if (!nav.to) return;
    path = nav.to.url.pathname;
    navOpen = false;
    checkAuth();
  });

  async function handleLogout() {
    await api.logout();
    window.location.href = '/login';
  }
</script>

<CommandPalette />
<Toaster position="bottom-right" theme="dark" />

{#if checking}
  <div class="page-loading">
    <div class="spinner"></div>
  </div>
{:else if isAuthRoute}
  <!-- Auth routes render standalone — no sidebar, no main wrapper. -->
  {@render children()}
{:else}
  <!-- Mobile hamburger: visible only on narrow viewports via CSS. Toggles
       the sidebar slide-in. Desktop ignores this element entirely. -->
  <button
    class="nav-toggle"
    aria-label="Toggle navigation"
    aria-expanded={navOpen}
    onclick={() => (navOpen = !navOpen)}
  ><Menu size={20} /></button>
  {#if navOpen}
    <!-- Tap-outside backdrop for mobile only. Pointer-events isolates it
         from desktop where the sidebar is always present. -->
    <button class="nav-backdrop" aria-label="Close navigation" onclick={() => (navOpen = false)}></button>
  {/if}
  <nav class="sidebar" class:open={navOpen}>
    <h1 class="logo"><Droplets size={20} class="logo-icon" />rill</h1>
    <ProjectSelector />
    {#each navItems as item (item.href)}
      {@const Icon = icons[item.section]}
      <a href={item.href} class:active={item.match(path)}>
        <Icon size={16} class="nav-icon" />
        <span>{item.label}</span>
      </a>
    {/each}
    {#if identity}
      <button class="logout" onclick={handleLogout}>
        Logout {identity.username}
      </button>
    {/if}

    <div class="side-foot">
      <button class="side-foot-btn" onclick={toggleMode}
        title={theme.mode === 'dark' ? 'Switch to light' : 'Switch to dark'}
        aria-label="Toggle light / dark">
        {#if theme.mode === 'dark'}<Sun size={17} />{:else}<Moon size={17} />{/if}
      </button>
      <button class="side-foot-btn" class:on={themePanelOpen}
        onclick={() => (themePanelOpen = !themePanelOpen)} title="Theme" aria-label="Theme">
        <Palette size={17} />
      </button>
    </div>
  </nav>

  <main>
    {@render children()}
  </main>

  <ThemePicker
    value={theme}
    open={themePanelOpen}
    onChange={setTheme}
    onClose={() => (themePanelOpen = false)} />
{/if}

<style>
  .page-loading {
    display: flex; align-items: center; justify-content: center;
    min-height: 100vh; background: var(--bg);
  }
  .logout {
    margin-top: auto; padding: 0.35rem 0.6rem;
    background: var(--surface-2); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text-dim);
    font-size: 0.78rem; cursor: pointer; text-align: left; width: 100%;
  }
  .logout:hover { color: var(--destructive); border-color: var(--destructive); }
  .side-foot {
    margin-top: 0.5rem; padding-top: 0.5rem; display: flex; gap: 0.25rem;
    border-top: 1px solid var(--border);
  }
  .side-foot-btn {
    display: inline-flex; align-items: center; justify-content: center;
    padding: 0.4rem; border-radius: var(--radius-sm);
    background: none; border: none; cursor: pointer; color: var(--text-faint);
  }
  .side-foot-btn:hover { background: var(--surface-2); color: var(--text); }
  .side-foot-btn.on { color: var(--accent); background: var(--accent-bg); }
  .nav-section {
    margin-top: 0.6rem; padding: 0.3rem 0.4rem 0.1rem;
    font-size: 0.7rem; letter-spacing: 0.08em; text-transform: uppercase;
    color: var(--text-faint); border-top: 1px solid var(--border);
  }
</style>
