<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import { toast } from 'svelte-sonner';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import PageHeader from '$lib/PageHeader.svelte';
  import Plus from '@lucide/svelte/icons/plus';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';

  // Per-row delete with an undo window. The delete is real (soft-delete) and
  // fires immediately; Undo calls the restore endpoint, so closing the tab can
  // never leave a "deleted" doc silently alive. `deleting` guards double-clicks.
  let deleting = $state({});
  async function removeDoc(d) {
    if (deleting[d.id]) return;
    const idx = docs.findIndex((x) => x.id === d.id);
    deleting = { ...deleting, [d.id]: true };
    try {
      await api.deleteDoc(d.id);
    } catch (e) {
      const msg = String(e.message || '');
      toast.error(msg.includes('403') ? 'Delete requires admin scope' : (msg || 'Delete failed'));
      deleting = { ...deleting, [d.id]: false };
      return;
    }
    docs = docs.slice(0, idx).concat(docs.slice(idx + 1));
    deleting = { ...deleting, [d.id]: false };
    toast.success(`Deleted “${d.title}”`, {
      duration: 10000,
      action: {
        label: 'Undo',
        onClick: async () => {
          try {
            await api.restoreDoc(d.id);
            docs = docs.slice(0, idx).concat([d], docs.slice(idx));
            toast.success('Restored');
          } catch (e) {
            toast.error(e.message || 'Restore failed');
          }
        },
      },
    });
  }

  let project = $state(prefs.projects?.[0] || '');
  let docTypeFilter = $state('');

  let docs = $state([]);
  let cursor = $state(null);
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state('');
  let isReady = $state(true);

  let loadSeq = 0;
  async function loadFirst() {
    const mySeq = ++loadSeq;
    loading = true;
    error = '';
    cursor = null;
    docs = [];
    try {
      const r = await api.listDocs({
        project: project || undefined,
        doc_type: docTypeFilter || undefined,
        limit: 50,
      });
      if (mySeq !== loadSeq) return;
      docs = r.documents || [];
      cursor = r.next_cursor || null;
    } catch (e) {
      if (mySeq !== loadSeq) return;
      if (String(e.message || '').includes('404')) isReady = false;
      else error = e.message || 'Failed to load documents';
    } finally {
      if (mySeq === loadSeq) loading = false;
    }
  }

  async function loadMore() {
    if (!cursor || loadingMore) return;
    loadingMore = true;
    try {
      const r = await api.listDocs({
        project: project || undefined,
        doc_type: docTypeFilter || undefined,
        before: cursor,
        limit: 50,
      });
      docs = docs.concat(r.documents || []);
      cursor = r.next_cursor || null;
    } catch (e) {
      error = e.message || 'Failed to load more';
    }
    loadingMore = false;
  }

  onMount(async () => {
    isReady = await api.isReady();
    if (isReady) await loadFirst();
    else loading = false;

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

  function fmtRel(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    const secs = Math.floor((Date.now() - d.getTime()) / 1000);
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
    if (secs < 86400 * 7) return `${Math.floor(secs / 86400)}d ago`;
    return d.toISOString().slice(0, 10);
  }
  function fmtAbs(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 16) + 'Z';
  }
  function shortID(id) {
    return (id || '').replace(/^document:/, '');
  }
  function detailHref(d) {
    return `/documents/${encodeURIComponent(shortID(d.id))}`;
  }
</script>

<svelte:head>
  <title>Documents — Rill</title>
</svelte:head>

<div class="page">
  <PageHeader section="documents" title="Documents">
    <span class="muted-small">{docs.length} loaded{cursor ? ', more available' : ''}</span>
  </PageHeader>

  {#if !isReady}
    <div class="state empty">
      <h2>Store unreachable</h2>
      <p>Server isn't running with <code>RILL_SURREAL_URL/RILL_SURREAL_NS</code>.</p>
    </div>
  {:else}
    <div class="filters">
      <label>
        Type
        <input
          type="text"
          placeholder="any (primer, review…)"
          bind:value={docTypeFilter}
          onchange={loadFirst}
        />
      </label>
      <div class="spacer"></div>
      <a class="new-btn" href="/documents/new"><Plus size={14} /> New document</a>
      <button class="refresh" onclick={loadFirst} disabled={loading} aria-label="Refresh"><RotateCw size={15} /></button>
    </div>

    {#if loading}
      <div class="state loading"><div class="spinner"></div><p>Loading documents…</p></div>
    {:else if error}
      <div class="state error">
        <h2>Failed to load</h2>
        <p>{error}</p>
        <button onclick={loadFirst}>Try again</button>
      </div>
    {:else if docs.length === 0}
      <div class="state empty">
        <h2>No documents</h2>
        <p>
          {#if docTypeFilter || project}
            No documents match these filters.
          {:else}
            No documents yet. Create one with <strong>+ New document</strong>, or via the <code>doc_put</code> MCP tool.
          {/if}
        </p>
        <a class="new-btn" href="/documents/new"><Plus size={14} /> New document</a>
      </div>
    {:else}
      <ul class="list">
        {#each docs as d}
          <li class="row">
            <a class="row-link" href={detailHref(d)}>
              <span class="doc-type">{d.doc_type}</span>
              <span class="title">{d.title}</span>
              <span class="meta">
                {#if d.project}<span class="project">{d.project}</span>{/if}
                {#if d.source}<span class="source">{d.source}</span>{/if}
                <span class="when" title={fmtAbs(d.updated_at)}>{fmtRel(d.updated_at)}</span>
              </span>
            </a>
            <button
              class="del"
              title="Delete document"
              aria-label="Delete {d.title}"
              disabled={deleting[d.id]}
              onclick={() => removeDoc(d)}
            >
              <Trash2 size={15} aria-hidden="true" />
            </button>
          </li>
        {/each}
      </ul>
      {#if loadingMore}
        <div class="state loading"><div class="spinner"></div><p>Loading more…</p></div>
      {:else if cursor}
        <div class="more"><button onclick={loadMore}>Load more</button></div>
      {/if}
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
  .filters label { display: flex; align-items: center; gap: 0.4rem; color: var(--text-dim); font-size: 0.85rem; }
  .filters input {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.4rem; font-size: 0.85rem; min-width: 12rem;
  }
  .filters .spacer { flex: 1; }
  .filters .refresh {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.5rem; cursor: pointer;
  }
  .filters .refresh:disabled { opacity: 0.5; cursor: not-allowed; }

  .new-btn {
    background: var(--accent); color: var(--accent-fg, #fff);
    border: 1px solid var(--accent); border-radius: var(--radius-sm);
    padding: 0.25rem 0.7rem; font-size: 0.85rem; text-decoration: none; white-space: nowrap;
  }
  .new-btn:hover { background: var(--accent-hi, var(--accent)); }

  .list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .row {
    display: flex; align-items: stretch;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .row:hover { border-color: var(--accent); }
  .row-link {
    flex: 1; min-width: 0;
    display: grid; grid-template-columns: 6rem 1fr auto;
    align-items: baseline; gap: 0.6rem;
    padding: 0.5rem 0.7rem;
    color: var(--text); text-decoration: none;
    font-size: 0.9rem;
  }
  .del {
    flex: none; display: flex; align-items: center;
    padding: 0 0.7rem; margin: 0;
    background: none; border: none; border-left: 1px solid transparent;
    color: var(--text-faint); cursor: pointer;
  }
  .row:hover .del { border-left-color: var(--border); }
  .del:hover { color: var(--destructive); }
  .del:disabled { opacity: 0.4; cursor: not-allowed; }

  .doc-type {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: oklch(0.30 0.08 280); color: oklch(0.88 0.05 280);
    text-transform: uppercase; letter-spacing: 0.05em; text-align: center;
  }
  .title { color: var(--text); font-weight: 500; }
  .meta { display: flex; gap: 0.6rem; align-items: baseline; color: var(--text-faint); font-size: 0.75rem; font-family: var(--font-mono); }
  .meta .project { color: var(--accent); }
  .meta .when { white-space: nowrap; }

  .more { display: flex; justify-content: center; padding: 1rem 0; }
  .more button {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 1.2rem; cursor: pointer; font-size: 0.85rem;
  }
  .more button:hover { border-color: var(--accent); }

  .state {
    padding: 2rem 1rem; text-align: center;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text-dim);
  }
  .state h2 { margin: 0 0 0.4rem 0; color: var(--text); font-size: 1.1rem; }
  .state.error { border-color: var(--destructive); background: var(--destructive-bg); }
  .state.error h2 { color: var(--destructive); }
  .state .new-btn { display: inline-block; margin-top: 0.8rem; }
  .state button {
    margin-top: 0.6rem; padding: 0.3rem 0.8rem;
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm); cursor: pointer;
  }
  .spinner {
    width: 20px; height: 20px; border: 2px solid var(--border);
    border-top-color: var(--accent); border-radius: 50%;
    animation: spin 0.6s linear infinite; margin: 0 auto 0.6rem;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .muted-small { color: var(--text-faint); font-size: 0.75rem; font-family: var(--font-mono); }
  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
