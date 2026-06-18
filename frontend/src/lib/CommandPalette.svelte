<script>
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { api } from '$lib/api.js';
  import { navItems, icons } from '$lib/navItems.js';
  import Search from '@lucide/svelte/icons/search';

  let open = $state(false);
  let query = $state('');
  let results = $state([]);
  let searching = $state(false);
  let selectedIndex = $state(0);
  let inputEl;

  // Mirror the real sidebar nav (icons + routes) so the palette never drifts
  // from it — one source of truth in navItems.js.
  const pages = navItems.map((n) => ({ href: n.href, label: n.label, icon: icons[n.section] }));

  function onKeydown(e) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      open = !open;
      if (open) setTimeout(() => inputEl?.focus(), 50);
    }
    if (e.key === 'Escape' && open) { open = false; }
  }

  let debounce;
  function onInput() {
    clearTimeout(debounce);
    selectedIndex = 0;
    if (!query.trim()) { results = []; return; }
    searching = true;
    debounce = setTimeout(async () => {
      try {
        const r = await api.search(query, '', 8);
        results = Array.isArray(r) ? r : [];
      } catch { results = []; }
      searching = false;
    }, 200);
  }

  function navigate(href) {
    open = false;
    query = '';
    results = [];
    goto(href);
  }

  function selectResult(mem) {
    open = false;
    query = '';
    results = [];
    goto(`/memories?id=${mem.id}`);
  }

  function handleKeydown(e) {
    const total = filteredPages.length + results.length;
    if (e.key === 'ArrowDown') { e.preventDefault(); selectedIndex = (selectedIndex + 1) % Math.max(total, 1); }
    if (e.key === 'ArrowUp') { e.preventDefault(); selectedIndex = (selectedIndex - 1 + total) % Math.max(total, 1); }
    if (e.key === 'Enter') {
      e.preventDefault();
      if (selectedIndex < filteredPages.length) {
        navigate(filteredPages[selectedIndex].href);
      } else if (results.length) {
        selectResult(results[selectedIndex - filteredPages.length]);
      }
    }
  }

  const filteredPages = $derived(pages.filter(p =>
    !query || p.label.toLowerCase().includes(query.toLowerCase())
  ));

  onMount(() => {
    document.addEventListener('keydown', onKeydown);
    return () => document.removeEventListener('keydown', onKeydown);
  });
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="overlay" onclick={() => open = false}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="palette" onclick={(e) => e.stopPropagation()} role="dialog">
      <div class="search-wrap">
        <Search class="search-icon" size={16} />
        <input
          bind:this={inputEl}
          type="text"
          bind:value={query}
          oninput={onInput}
          onkeydown={handleKeydown}
          placeholder="Search memories or jump to page…"
          class="cmd-input"
        />
      </div>

      {#if filteredPages.length > 0}
        <div class="group">
          <div class="group-label">Pages</div>
          {#each filteredPages as page, i}
            {@const Icon = page.icon}
            <button class="item" class:selected={selectedIndex === i} onclick={() => navigate(page.href)}>
              <span class="item-icon"><Icon size={16} /></span>
              {page.label}
            </button>
          {/each}
        </div>
      {/if}

      {#if results.length > 0}
        <div class="group">
          <div class="group-label">Memories</div>
          {#each results as mem, i}
            <button
              class="item"
              class:selected={selectedIndex === i + filteredPages.length}
              onclick={() => selectResult(mem)}
            >
              <span class="item-type">{mem.memory_type}</span>
              <span class="item-text">{mem.summary || (mem.content || '').slice(0, 80)}</span>
            </button>
          {/each}
        </div>
      {:else if query && !searching}
        <div class="empty">No results for "{query}"</div>
      {/if}

      <div class="footer">
        <kbd>↑↓</kbd> Navigate <kbd>↵</kbd> Open <kbd>Esc</kbd> Close
      </div>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed; inset: 0; background: rgba(0,0,0,0.6); z-index: 200;
    display: flex; justify-content: center; padding-top: 15vh;
  }
  .palette {
    background: var(--surface); border: 1px solid var(--border); border-radius: 12px;
    width: 560px; max-height: 480px; overflow-y: auto; box-shadow: 0 20px 60px rgba(0,0,0,0.5);
  }
  .search-wrap {
    display: flex; align-items: center; padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }
  /* Icon lands on the lucide child's <svg>; reach it via :global so Svelte's
     scoped-CSS pass doesn't strip it as "unused". */
  .search-wrap :global(.search-icon) { color: var(--text-dim); margin-right: 0.5rem; flex-shrink: 0; }
  .cmd-input {
    flex: 1; background: none; border: none; color: var(--text);
    font-size: 1rem; outline: none; font-family: inherit;
  }
  .cmd-input::placeholder { color: var(--text-dim); }

  .group { padding: 0.5rem 0; }
  .group-label {
    padding: 0.25rem 1rem; font-size: 0.7rem; text-transform: uppercase;
    letter-spacing: 0.06em; color: var(--text-dim);
  }
  .item {
    display: flex; align-items: center; gap: 0.65rem; padding: 0.55rem 1rem;
    width: 100%; border: none; background: none; color: var(--text);
    font-size: 0.9rem; cursor: pointer; text-align: left; font-family: inherit;
  }
  .item:hover, .item.selected { background: var(--surface-2); }
  .item-icon { font-size: 1rem; width: 1.2rem; text-align: center; }
  .item-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .item-type {
    font-size: 0.7rem; padding: 0.1rem 0.4rem; background: var(--surface-2);
    border-radius: 4px; text-transform: uppercase; font-weight: 600;
    flex-shrink: 0; min-width: 50px; text-align: center;
  }

  .empty { padding: 1rem; text-align: center; color: var(--text-dim); font-size: 0.85rem; }

  .footer {
    display: flex; gap: 0.5rem; align-items: center; padding: 0.5rem 1rem;
    border-top: 1px solid var(--border); font-size: 0.7rem; color: var(--text-dim);
  }
  kbd {
    padding: 0.1rem 0.4rem; background: var(--surface-2); border-radius: 3px;
    font-size: 0.65rem; font-family: var(--font-mono, monospace);
  }
</style>
