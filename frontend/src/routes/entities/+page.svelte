<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import PageHeader from '$lib/PageHeader.svelte';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Star from '@lucide/svelte/icons/star';

  const ENTITY_TYPES = [
    { value: '', label: 'All' },
    { value: 'person', label: 'People' },
    { value: 'project', label: 'Projects' },
    { value: 'tool', label: 'Tools' },
    { value: 'organization', label: 'Organizations' },
    { value: 'place', label: 'Places' },
    { value: 'preference', label: 'Preferences' },
    { value: 'concept', label: 'Concepts' },
  ];

  const SORTS = [
    { value: 'mention_count', label: 'Most mentioned' },
    { value: 'recent', label: 'Most recent' },
    { value: 'name', label: 'Name' },
  ];

  let project = $state(prefs.projects?.[0] || '');
  let typeFilter = $state(''); // 'All' — show every type so newly-added entities don't get hidden
  let promotedOnly = $state(false);
  let sort = $state('mention_count');
  let entities = $state([]);
  let loading = $state(true);
  let error = $state('');
  let isReady = $state(true); // optimistic; we ping on mount

  let loadSeq = 0;
  async function load() {
    const mySeq = ++loadSeq;
    loading = true;
    error = '';
    try {
      const rows = await api.listEntities({
        type: typeFilter || undefined,
        promoted: promotedOnly ? true : undefined,
        sort,
        limit: 200,
      });
      if (mySeq !== loadSeq) return;
      entities = rows;
    } catch (e) {
      if (mySeq !== loadSeq) return;
      // 404 on /api/* means the flag is off.
      if (String(e.message || '').includes('404')) {
        isReady = false;
      } else {
        error = e.message || 'Failed to load entities';
      }
      entities = [];
    } finally {
      if (mySeq === loadSeq) loading = false;
    }
  }

  onMount(async () => {
    isReady = await api.isReady();
    if (isReady) await load();
    else loading = false;

    const handler = (e) => {
      const sel = e.detail?.projects || [];
      project = sel.length === 1 ? sel[0] : '';
      if (isReady) load();
    };
    window.addEventListener('project-changed', handler);
    return () => window.removeEventListener('project-changed', handler);
  });

  function fmtDate(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().slice(0, 10);
  }

  // Path segment from full record id ("tool:rill" → "rill") for href building.
  function slugOf(id) {
    if (!id) return '';
    const colon = id.indexOf(':');
    if (colon < 0) return id;
    let s = id.slice(colon + 1);
    // SurrealDB renders some IDs as `name` (backticked); strip them.
    if (s.startsWith('`') && s.endsWith('`')) s = s.slice(1, -1);
    return s;
  }

  function detailHref(row) {
    return `/entities/${encodeURIComponent(row.type)}/${encodeURIComponent(slugOf(row.id))}`;
  }
</script>

<svelte:head>
  <title>Entities — Rill</title>
</svelte:head>

<div class="page">
  <PageHeader section="entities" title="Entities" />

  {#if !isReady}
    <div class="state empty">
      <h2>Memory store unreachable</h2>
      <p>This server isn't running with <code>RILL_SURREAL_URL/RILL_SURREAL_NS</code>. Start the server with the flag and a reachable SurrealDB, then refresh.</p>
    </div>
  {:else}
    <div class="filters">
      <label>
        Type
        <select bind:value={typeFilter} onchange={load}>
          {#each ENTITY_TYPES as t}
            <option value={t.value}>{t.label}</option>
          {/each}
        </select>
      </label>
      <label>
        Sort
        <select bind:value={sort} onchange={load}>
          {#each SORTS as s}
            <option value={s.value}>{s.label}</option>
          {/each}
        </select>
      </label>
      <label class="checkbox">
        <input type="checkbox" bind:checked={promotedOnly} onchange={load} />
        Promoted only
      </label>
      <div class="spacer"></div>
      <button class="refresh" onclick={load} disabled={loading} aria-label="Refresh"><RotateCw size={15} /></button>
    </div>

    {#if loading}
      <div class="state loading">
        <div class="spinner"></div>
        <p>Loading entities…</p>
      </div>
    {:else if error}
      <div class="state error">
        <h2>Failed to load</h2>
        <p>{error}</p>
        <button onclick={load}>Try again</button>
      </div>
    {:else if entities.length === 0}
      <div class="state empty">
        <h2>No entities yet</h2>
        <p>
          {#if typeFilter && promotedOnly}
            No promoted entities of type <strong>{typeFilter}</strong>. Try clearing the filter, or use <code>remember</code> / <code>rill remember</code> to add one.
          {:else if typeFilter}
            No entities of type <strong>{typeFilter}</strong>. Use <code>remember</code> to create one with this type, or change the filter.
          {:else}
            The store has no entities. Run <code>rill remember</code> with a payload, then refresh.
          {/if}
        </p>
      </div>
    {:else}
      <ul class="list">
        {#each entities as row (row.id)}
          <li>
            <a class="row" href={detailHref(row)}>
              <span class="type-pill" data-type={row.type}>{row.type}</span>
              <span class="name">
                {row.name}
                {#if row.promoted}<span class="promoted" title="Promoted — appears in orient"><Star size={13} fill="currentColor" /></span>{/if}
              </span>
              {#if row.summary}<span class="summary">{row.summary}</span>{/if}
              <span class="meta">
                <span title="Mention count">{row.mention_count} ×</span>
                <span title="Last seen">{fmtDate(row.last_seen)}</span>
              </span>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 1100px; }

  .filters {
    display: flex; align-items: center; gap: 0.8rem;
    padding: 0.5rem 0.6rem; margin-bottom: 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .filters label {
    display: flex; align-items: center; gap: 0.4rem;
    color: var(--text-dim); font-size: 0.85rem;
  }
  .filters select {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.4rem; font-size: 0.85rem;
  }
  .filters .checkbox { gap: 0.3rem; }
  .filters .spacer { flex: 1; }
  .filters .refresh {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.5rem; cursor: pointer;
  }
  .filters .refresh:hover:not(:disabled) { color: var(--accent); }
  .filters .refresh:disabled { opacity: 0.5; cursor: not-allowed; }

  .state {
    padding: 2rem 1rem; text-align: center;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text-dim);
  }
  .state h2 { margin: 0 0 0.4rem 0; color: var(--text); font-size: 1.1rem; }
  .state.error { border-color: var(--destructive); background: var(--destructive-bg); }
  .state.error h2 { color: var(--destructive); }
  .state button {
    margin-top: 0.6rem; padding: 0.3rem 0.8rem;
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    cursor: pointer;
  }
  .spinner {
    width: 20px; height: 20px; border: 2px solid var(--border);
    border-top-color: var(--accent); border-radius: 50%;
    animation: spin 0.6s linear infinite; margin: 0 auto 0.6rem;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .row {
    display: grid;
    grid-template-columns: 8rem 1fr 2fr auto;
    align-items: baseline; gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text); text-decoration: none;
    font-size: 0.88rem;
  }
  .row:hover { border-color: var(--accent); }
  .type-pill {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
    text-align: center;
  }
  .row .name { font-weight: 500; }
  .promoted { color: var(--warning); margin-left: 0.3rem; }
  .summary { color: var(--text-dim); font-size: 0.82rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .meta {
    display: flex; gap: 0.7rem; color: var(--text-faint);
    font-family: var(--font-mono); font-size: 0.75rem;
  }

  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
