<script>
  import LayoutGrid from '@lucide/svelte/icons/layout-grid';
  import LayoutList from '@lucide/svelte/icons/layout-list';
  import { prefs } from '$lib/prefs.js';

  let { dense = prefs.dense, ontoggle } = $props();
</script>

<button
  class="dense-toggle"
  onclick={ontoggle}
  title={dense ? 'Card view' : 'Dense view'}
>
  {#if dense}
    <LayoutGrid size={16} />
  {:else}
    <LayoutList size={16} />
  {/if}
</button>

<style>
  .dense-toggle {
    display: inline-flex; align-items: center; justify-content: center;
    width: 28px; height: 28px; padding: 0;
    background: none; border: none; border-radius: var(--radius-sm);
    color: var(--text-dim); cursor: pointer;
    font-family: inherit; flex-shrink: 0;
  }
  .dense-toggle:hover { background: var(--surface-2); color: var(--text); }
</style>
