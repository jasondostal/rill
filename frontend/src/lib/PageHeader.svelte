<script>
  // Consistent page header: a section icon (accent-colored) + title on the left,
  // optional actions/meta on the right via the default snippet. Pass `section`
  // to reuse the sidebar's icon for that section, or `icon` for a one-off.
  import { icons } from '$lib/navItems.js';

  let { section = '', icon = null, title, children } = $props();
  const Icon = icon ?? (section ? icons[section] : null);
</script>

<header class="page-header">
  <h1>
    {#if Icon}<Icon size={22} strokeWidth={2.25} class="ph-icon" />{/if}
    <span>{title}</span>
  </h1>
  {#if children}<div class="ph-actions">{@render children()}</div>{/if}
</header>

<style>
  .page-header {
    display: flex; align-items: center; justify-content: space-between;
    gap: 1rem; margin-bottom: 0.75rem;
  }
  .page-header h1 {
    margin: 0; font-size: 1.4rem; color: var(--text);
    display: flex; align-items: center; gap: 0.5rem; min-width: 0;
  }
  .page-header :global(.ph-icon) { color: var(--accent); flex: none; }
  .ph-actions {
    display: flex; align-items: center; gap: 0.5rem;
    flex: none; flex-wrap: wrap; justify-content: flex-end;
  }
</style>
