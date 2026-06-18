<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import X from '@lucide/svelte/icons/x';
  import Check from '@lucide/svelte/icons/check';

  let projects = $state([]);
  let selected = $state(prefs.projects);
  let open = $state(false);
  let search = $state('');
  let container;

  onMount(async () => {
    try {
      // Was: api.boot() — that endpoint never existed, so the sidebar was
      // always empty. /api/projects returns the distinct project list.
      const resp = await api.projects();
      if (resp?.projects) {
        projects = resp.projects.map(p => p.project).sort();
      }
    } catch { /* fallback */ }
  });

  function toggle(p) {
    if (selected.includes(p)) {
      selected = selected.filter(s => s !== p);
    } else {
      selected = [...selected, p];
    }
    prefs.projects = selected;
    window.dispatchEvent(new CustomEvent('project-changed', { detail: { projects: selected } }));
  }

  function clearAll() {
    selected = [];
    prefs.projects = [];
    window.dispatchEvent(new CustomEvent('project-changed', { detail: { projects: [] } }));
  }

  function remove(p, e) {
    e.stopPropagation();
    toggle(p);
  }

  let filtered = $derived(
    search
      ? projects.filter(p => p.toLowerCase().includes(search.toLowerCase()))
      : projects
  );

  function handleClick(e) {
    if (container && !container.contains(e.target)) {
      open = false;
      search = '';
    }
  }

  $effect(() => {
    if (open) document.addEventListener('click', handleClick);
    else document.removeEventListener('click', handleClick);
    return () => document.removeEventListener('click', handleClick);
  });

  function handleKeydown(e) {
    if (e.key === 'Escape') { open = false; search = ''; }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="selector" bind:this={container}>
  <!-- Trigger -->
  <button
    class="trigger"
    class:open
    onclick={() => open = !open}
  >
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" class="icon">
      <rect x="1" y="2" width="5" height="4" rx="1" stroke="currentColor" stroke-width="1.2"/>
      <rect x="8" y="2" width="5" height="4" rx="1" stroke="currentColor" stroke-width="1.2"/>
      <rect x="1" y="8" width="5" height="4" rx="1" stroke="currentColor" stroke-width="1.2"/>
      <rect x="8" y="8" width="5" height="4" rx="1" stroke="currentColor" stroke-width="1.2"/>
    </svg>

    {#if selected.length === 0}
      <span class="placeholder">All projects</span>
    {:else}
      <div class="chips">
        {#each selected as p}
          <span class="chip">
            {p}
            <button class="chip-x" onclick={(e) => remove(p, e)}><X size={12} /></button>
          </span>
        {/each}
      </div>
    {/if}

    <svg width="10" height="10" viewBox="0 0 10 10" fill="none" class="chevron" class:rotated={open}>
      <path d="M2 3.5L5 6.5L8 3.5" stroke="currentColor" stroke-width="1.2"/>
    </svg>
  </button>

  <!-- Dropdown -->
  {#if open}
    <div class="dropdown">
      <div class="search-wrap">
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" class="search-icon">
          <circle cx="6" cy="6" r="5" stroke="currentColor" stroke-width="1.2"/>
          <line x1="10" y1="10" x2="13" y2="13" stroke="currentColor" stroke-width="1.2"/>
        </svg>
        <input
          type="text"
          bind:value={search}
          placeholder="Filter projects…"
          class="search-input"
          autofocus
        />
        {#if selected.length > 0}
          <button class="clear-all" onclick={clearAll}>Clear all</button>
        {/if}
      </div>

      <div class="options">
        {#each filtered as p}
          {@const checked = selected.includes(p)}
          <button class="option" onclick={() => toggle(p)}>
            <span class="check">{#if checked}<Check size={13} />{/if}</span>
            <span class="opt-name">{p}</span>
          </button>
        {/each}
        {#if filtered.length === 0}
          <div class="empty">No projects match</div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .selector { position: relative; margin-bottom: 0.75rem; }

  .trigger {
    display: flex; align-items: center; gap: 0.5rem; width: 100%; min-height: 2rem;
    padding: 0.35rem 0.5rem; background: var(--surface-2); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text); font-size: 0.78rem;
    cursor: pointer; font-family: inherit; transition: border-color 0.15s;
  }
  .trigger:hover, .trigger.open { border-color: var(--accent); }
  .icon { flex-shrink: 0; opacity: 0.6; }
  .placeholder { color: var(--text-dim); }

  .chips {
    display: flex; flex-wrap: wrap; gap: 0.25rem; flex: 1; min-width: 0;
  }
  .chip {
    display: inline-flex; align-items: center; gap: 0.15rem;
    padding: 0.1rem 0.15rem 0.1rem 0.4rem;
    background: var(--primary); color: var(--primary-foreground);
    border-radius: 4px; font-size: 0.72rem; font-weight: 500;
    max-width: 120px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .chip-x {
    background: none; border: none; color: inherit; cursor: pointer;
    font-size: 0.85rem; padding: 0 0.15rem; opacity: 0.7; flex-shrink: 0;
  }
  .chip-x:hover { opacity: 1; }

  .chevron { flex-shrink: 0; opacity: 0.5; transition: transform 0.15s; }
  .chevron.rotated { transform: rotate(180deg); }

  .dropdown {
    position: absolute; top: 100%; left: 0; right: 0; z-index: 50; margin-top: 2px;
    background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0,0,0,0.4); max-height: 340px; display: flex; flex-direction: column;
  }
  .search-wrap {
    display: flex; align-items: center; padding: 0.4rem; border-bottom: 1px solid var(--border);
    gap: 0.35rem;
  }
  .search-icon { flex-shrink: 0; opacity: 0.5; }
  .search-input {
    flex: 1; background: none; border: none; color: var(--text);
    font-size: 0.8rem; outline: none; font-family: inherit;
  }
  .clear-all {
    flex-shrink: 0; background: none; border: none; color: var(--accent);
    font-size: 0.72rem; cursor: pointer; font-family: inherit;
  }
  .clear-all:hover { text-decoration: underline; }

  .options { overflow-y: auto; flex: 1; }
  .option {
    display: flex; align-items: center; gap: 0.5rem;
    width: 100%; padding: 0.4rem 0.6rem; background: none; border: none;
    color: var(--text); font-size: 0.8rem; cursor: pointer; text-align: left;
    font-family: inherit; transition: background 0.1s;
  }
  .option:hover { background: var(--surface-2); }
  .check { width: 14px; font-size: 0.75rem; color: var(--accent); flex-shrink: 0; }
  .opt-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .empty { padding: 0.75rem; text-align: center; color: var(--text-dim); font-size: 0.8rem; }
</style>
