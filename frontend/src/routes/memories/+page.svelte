<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import PageHeader from '$lib/PageHeader.svelte';
  import KindBadge from '$lib/components/KindBadge.svelte';
  import ProjectChip from '$lib/components/ProjectChip.svelte';
  import { KINDS } from '$lib/colors.js';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';

  const AUTHORS = [
    { value: '', label: 'all authors' },
    { value: 'claude', label: 'claude' },
  ];

  let project = $state(prefs.projects?.[0] || '');
  let kindFilter = $state('');
  let authorFilter = $state('');

  let memories = $state([]);
  let cursor = $state(null);
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state('');
  let isReady = $state(true);
  let heatmap = $state([]); // activity strip (best-effort, from /api/stats)

  let loadSeq = 0;
  async function loadFirst() {
    const mySeq = ++loadSeq;
    loading = true;
    error = '';
    cursor = null;
    memories = [];
    try {
      const r = await api.listMemories({
        kind: kindFilter || undefined,
        project: project || undefined,
        author: authorFilter || undefined,
        limit: 50,
      });
      if (mySeq !== loadSeq) return;
      memories = r.memories || [];
      cursor = r.next_cursor || null;
    } catch (e) {
      if (mySeq !== loadSeq) return;
      if (String(e.message || '').includes('404')) {
        isReady = false;
      } else {
        error = e.message || 'Failed to load memories';
      }
    } finally {
      if (mySeq === loadSeq) loading = false;
    }
  }

  async function loadMore() {
    if (!cursor || loadingMore) return;
    loadingMore = true;
    try {
      const r = await api.listMemories({
        kind: kindFilter || undefined,
        project: project || undefined,
        author: authorFilter || undefined,
        before: cursor,
        limit: 50,
      });
      memories = memories.concat(r.memories || []);
      cursor = r.next_cursor || null;
    } catch (e) {
      error = e.message || 'Failed to load more';
    }
    loadingMore = false;
  }

  function setKind(k) { kindFilter = k; loadFirst(); }
  function setAuthor(a) { authorFilter = a; loadFirst(); }
  function clearFilters() { kindFilter = ''; authorFilter = ''; loadFirst(); }

  onMount(async () => {
    isReady = await api.isReady();
    if (isReady) await loadFirst();
    else loading = false;

    // Activity strip — best effort; absence is non-fatal.
    api.stats('90d').then((s) => { heatmap = s?.heatmap || []; }).catch(() => {});

    const handler = (e) => {
      const sel = e.detail?.projects || [];
      project = sel.length === 1 ? sel[0] : '';
      if (isReady) loadFirst();
    };
    window.addEventListener('project-changed', handler);

    const onScroll = () => {
      if (!cursor || loadingMore) return;
      const remaining = document.documentElement.scrollHeight - window.scrollY - window.innerHeight;
      if (remaining < 600) loadMore();
    };
    window.addEventListener('scroll', onScroll);

    return () => {
      window.removeEventListener('project-changed', handler);
      window.removeEventListener('scroll', onScroll);
    };
  });

  function fmtAbs(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 16) + 'Z';
  }
  function fmtRel(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    const secs = Math.floor((Date.now() - d.getTime()) / 1000);
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
    if (secs < 86400 * 7) return `${Math.floor(secs / 86400)}d`;
    return d.toISOString().slice(0, 10);
  }
  function shortID(id) {
    if (!id) return '';
    return id.replace(/^memory:`?/, '').replace(/`?$/, '');
  }
  function dayBucket(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().slice(0, 10);
  }
  function detailHref(m) {
    return `/memories/${encodeURIComponent(shortID(m.id))}`;
  }

  let grouped = $derived.by(() => {
    const out = [];
    let currentDay = null;
    for (const m of memories) {
      const day = dayBucket(m.created_at);
      if (day !== currentDay) { out.push({ kind: 'header', day }); currentDay = day; }
      out.push({ kind: 'item', m });
    }
    return out;
  });

  // Activity strip levels.
  const op = [0, 0.3, 0.55, 0.78, 1];
  let heatMax = $derived(Math.max(...heatmap.map((d) => d.count), 1));
  let heatTotal = $derived(heatmap.reduce((a, d) => a + d.count, 0));
  function dotColor(c) {
    if (c === 0) return 'var(--surface-3)';
    const lvl = Math.min(4, Math.ceil((c / heatMax) * 4));
    return `oklch(from var(--accent) l c h / ${op[lvl]})`;
  }
</script>

<svelte:head>
  <title>Memories — Rill</title>
</svelte:head>

<PageHeader section="memories" title="Memories">
  <span class="muted-small mono">{memories.length} loaded{cursor ? ' · more available' : ''}</span>
</PageHeader>

{#if !isReady}
  <div class="state empty">
    <h2>Memory store unreachable</h2>
    <p>Server isn't running with <code>RILL_SURREAL_URL / RILL_SURREAL_NS</code>.</p>
  </div>
{:else}
  {#if heatmap.length}
    <div class="act-strip">
      <div class="act-head">
        <span class="act-title">Activity</span>
        <span class="act-count mono">{heatTotal} captures · 90d</span>
      </div>
      <div class="act-dots">
        {#each heatmap as d, i (i)}
          <i style="background:{dotColor(d.count)}" title={`${d.count} on ${new Date(d.date).toLocaleDateString()}`}></i>
        {/each}
      </div>
      <div class="act-scale mono">
        <span>less</span>
        {#each [0, 1, 2, 3, 4] as l (l)}<i style="background:{l === 0 ? 'var(--surface-3)' : `oklch(from var(--accent) l c h / ${op[l]})`}"></i>{/each}
        <span>more</span>
      </div>
    </div>
  {/if}

  <div class="mem-filters">
    <div class="filter-group">
      <span class="filter-label mono">kind</span>
      <div class="chips">
        <button class="fchip mono" class:on={kindFilter === ''} onclick={() => setKind('')}>all</button>
        {#each KINDS as k (k)}
          <button class="fchip mono kchip" class:on={kindFilter === k}
            style="--kc:var(--k-{k});--kcb:var(--k-{k}-bg)" onclick={() => setKind(k)}>
            <i class="kind-dot"></i>{k}
          </button>
        {/each}
      </div>
    </div>
    <div class="filter-group">
      <span class="filter-label mono">author</span>
      <div class="chips">
        {#each AUTHORS as a (a.value)}
          <button class="fchip mono" class:on={authorFilter === a.value} onclick={() => setAuthor(a.value)}>{a.label}</button>
        {/each}
        <button class="fchip mono refresh" onclick={loadFirst} disabled={loading} title="Refresh"><RotateCw size={13} /></button>
      </div>
    </div>
  </div>

  {#if loading}
    <div class="state loading"><div class="spinner"></div><p>Loading memories…</p></div>
  {:else if error}
    <div class="state error">
      <h2>Failed to load</h2>
      <p class="mono">{error}</p>
      <button onclick={loadFirst}>Try again</button>
    </div>
  {:else if memories.length === 0}
    <div class="state empty">
      <h2>No memories match</h2>
      {#if kindFilter || authorFilter}
        <p>Nothing captured for{#if kindFilter} <b>{kindFilter}</b>{/if}{#if authorFilter} by <b>{authorFilter}</b>{/if} yet.</p>
        <button onclick={clearFilters}>clear filters</button>
      {:else}
        <p>The store has no memories yet. Store one with the <code>remember</code> tool, or via <code>rill remember</code>.</p>
      {/if}
    </div>
  {:else}
    <div class="mem-list">
      {#each grouped as g (g.kind === 'header' ? 'h-' + g.day : 'm-' + g.m.id)}
        {#if g.kind === 'header'}
          <div class="mem-group-head mono">{g.day}<span class="day-line"></span></div>
        {:else}
          <a class="mem-row" href={detailHref(g.m)}>
            <span class="m-id mono">#{shortID(g.m.id).slice(0, 8)}</span>
            <KindBadge kind={g.m.kind} />
            <div class="m-body">
              <p class="m-text">{g.m.summary}</p>
              <div class="m-meta mono">
                <span class="m-author">{g.m.author}</span>
                {#if g.m.valence}<span class="v v-{g.m.valence}">{g.m.valence}</span>{/if}
              </div>
            </div>
            <div class="m-right">
              <ProjectChip name={g.m.project} />
              <span class="m-when mono" title={fmtAbs(g.m.created_at)}>{fmtRel(g.m.created_at)}</span>
            </div>
          </a>
        {/if}
      {/each}
    </div>
    {#if loadingMore}
      <div class="state loading"><div class="spinner"></div><p>Loading more…</p></div>
    {:else if cursor}
      <div class="more"><button onclick={loadMore}>Load more</button></div>
    {/if}
  {/if}
{/if}

<style>
  .mono { font-family: var(--font-mono); }
  .muted-small { color: var(--text-faint); font-size: 0.75rem; }

  .act-strip { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 14px 16px; margin-bottom: 18px; }
  .act-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 10px; }
  .act-title { font-size: 13px; font-weight: 600; color: var(--text); }
  .act-count { font-size: 11.5px; color: var(--text-faint); }
  .act-dots { display: flex; flex-wrap: wrap; gap: 4px; }
  .act-dots i { width: 11px; height: 11px; border-radius: 50%; flex: none; }
  .act-scale { display: flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-faint); justify-content: flex-end; margin-top: 10px; }
  .act-scale i { width: 10px; height: 10px; border-radius: 50%; }

  .mem-filters { display: flex; flex-direction: column; gap: 10px; margin-bottom: 16px; }
  .filter-group { display: flex; align-items: flex-start; gap: 12px; }
  .filter-label { font-size: 11px; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.08em; padding-top: 5px; min-width: 50px; }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .fchip {
    display: inline-flex; align-items: center; gap: 6px;
    background: var(--surface-2); border: 1px solid var(--border); color: var(--text-dim);
    border-radius: 20px; padding: 3px 11px; font-size: 12px; cursor: pointer; font-family: var(--font-mono);
  }
  .fchip:hover { border-color: var(--border-strong); color: var(--text); }
  .fchip.on { background: var(--accent-bg); border-color: var(--accent-line); color: var(--accent); }
  .fchip.kchip.on { background: var(--kcb); border-color: color-mix(in oklab, var(--kc) 40%, transparent); color: var(--kc); }
  .fchip .kind-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--kc); flex: none; }
  .fchip.refresh { padding: 3px 9px; }
  .fchip.refresh:disabled { opacity: 0.5; cursor: not-allowed; }

  .mem-list { display: flex; flex-direction: column; gap: 6px; }
  .mem-group-head { display: flex; align-items: center; gap: 10px; font-size: 11px; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.07em; margin: 14px 0 4px; }
  .day-line { flex: 1; height: 1px; background: var(--border); }
  .mem-row {
    display: grid; grid-template-columns: auto auto 1fr auto; align-items: start; gap: 12px;
    padding: 12px 14px; border: 1px solid var(--border); border-radius: var(--radius);
    background: var(--surface); text-decoration: none; color: var(--text);
  }
  .mem-row:hover { border-color: var(--border-strong); }
  .m-id { font-size: 11.5px; color: var(--text-faint); padding-top: 3px; }
  .m-body { min-width: 0; }
  .m-text { font-size: 13.5px; color: var(--text); line-height: 1.5; margin: 0; }
  .m-meta { margin-top: 4px; font-size: 11px; color: var(--text-faint); display: flex; gap: 0.6rem; align-items: baseline; }
  .v { padding: 0 0.3rem; border-radius: var(--radius-sm); font-size: 10px; }
  .v-positive { background: var(--success); color: var(--bg); }
  .v-negative { background: var(--destructive); color: var(--bg); }
  .v-neutral { background: var(--surface-2); color: var(--text-dim); }
  .m-right { display: flex; flex-direction: column; align-items: flex-end; gap: 7px; }
  .m-when { font-size: 11.5px; color: var(--text-faint); white-space: nowrap; }

  .more { display: flex; justify-content: center; padding: 1rem 0; }
  .more button { background: var(--surface-2); color: var(--text); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 0.4rem 1.2rem; cursor: pointer; font-size: 0.85rem; }
  .more button:hover { border-color: var(--accent); }

  .state { padding: 2.5rem 1rem; text-align: center; background: var(--surface); border: 1px dashed var(--border); border-radius: var(--radius); color: var(--text-dim); }
  .state h2 { margin: 0 0 0.4rem 0; color: var(--text); font-size: 1.1rem; }
  .state b { color: var(--text); }
  .state.error { border-style: solid; border-color: color-mix(in oklab, var(--destructive) 40%, transparent); }
  .state.error h2 { color: var(--destructive); }
  .state button { margin-top: 0.6rem; padding: 0.3rem 0.9rem; background: var(--surface-2); color: var(--text); border: 1px solid var(--border); border-radius: var(--radius-sm); cursor: pointer; }
  .spinner { width: 20px; height: 20px; border: 2px solid var(--border); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.6s linear infinite; margin: 0 auto 0.6rem; }
  @keyframes spin { to { transform: rotate(360deg); } }
  code { font-family: var(--font-mono); font-size: 0.85em; background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm); }

  @media (max-width: 720px) {
    .mem-row { grid-template-columns: auto 1fr; }
    .m-id, .m-right { display: none; }
  }
</style>
