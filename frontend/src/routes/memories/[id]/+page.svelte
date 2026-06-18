<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { api } from '$lib/api.js';
  import { toast } from 'svelte-sonner';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';

  let id = $derived(page.params.id);

  let detail = $state(null);
  let loading = $state(true);
  let error = $state('');
  let isReady = $state(true);

  async function load() {
    loading = true;
    error = '';
    try {
      detail = await api.getMemory(id);
    } catch (e) {
      const msg = String(e.message || '');
      if (msg.includes('404')) {
        if (!(await api.isReady())) isReady = false;
        else error = 'Memory not found';
      } else {
        error = msg || 'Failed to load memory';
      }
      detail = null;
    }
    loading = false;
  }

  onMount(load);

  async function forget() {
    if (!confirm('Forget this memory? Soft-delete (provenance preserved, but excluded from orient/recall + memory list).')) return;
    try {
      await api.forget(id);
      toast.success('Memory forgotten');
      await goto('/memories');
    } catch (e) {
      toast.error(e.message || 'Forget failed');
    }
  }

  function fmtFull(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 19) + 'Z';
  }

  function slugOfEntity(id) {
    if (!id) return '';
    const colon = id.indexOf(':');
    if (colon < 0) return id;
    let s = id.slice(colon + 1);
    if (s.startsWith('`') && s.endsWith('`')) s = s.slice(1, -1);
    return s;
  }

  function entityHref(e) {
    return `/entities/${encodeURIComponent(e.type)}/${encodeURIComponent(slugOfEntity(e.id))}`;
  }
</script>

<svelte:head>
  <title>Memory — Rill</title>
</svelte:head>

<div class="page">
  <nav class="breadcrumb">
    <a href="/memories"><ArrowLeft size={14} /> Memories</a>
  </nav>

  {#if !isReady}
    <div class="state empty">
      <h2>Memory store unreachable</h2>
    </div>
  {:else if loading}
    <div class="state loading">
      <div class="spinner"></div>
      <p>Loading…</p>
    </div>
  {:else if error}
    <div class="state error">
      <h2>{error}</h2>
      <p>Memory id: <code>{id}</code></p>
      <button onclick={load}>Try again</button>
    </div>
  {:else if !detail}
    <div class="state empty"><h2>Memory not found</h2></div>
  {:else}
    <header class="head">
      <div>
        <h1>
          <span class="kind kind-{detail.kind}">{detail.kind}</span>
          {detail.summary}
        </h1>
        <div class="meta">
          {#if detail.project}<span class="project">{detail.project}</span>{/if}
          {#if detail.valence}<span class="valence v-{detail.valence}">{detail.valence}</span>{/if}
          <span>by <strong>{detail.author}</strong></span>
          <span>{fmtFull(detail.created_at)}</span>
          {#if !detail.is_active}<span class="forgotten">forgotten</span>{/if}
        </div>
        <div class="memid">{detail.id}</div>
      </div>
      <div class="actions">
        <button class="forget" onclick={forget} disabled={!detail.is_active}>Forget</button>
      </div>
    </header>

    {#if detail.details}
      <section>
        <h2>Details</h2>
        <pre class="details">{detail.details}</pre>
      </section>
    {/if}

    {#if detail.tags && detail.tags.length}
      <section>
        <h2>Tags</h2>
        <div class="tags">
          {#each detail.tags as t}<span class="tag">{t}</span>{/each}
        </div>
      </section>
    {/if}

    <section>
      <h2>Mentioned entities <span class="count">({detail.mentioned_entities?.length || 0})</span></h2>
      {#if !detail.mentioned_entities || detail.mentioned_entities.length === 0}
        <p class="muted">No entities mentioned.</p>
      {:else}
        <ul class="entities">
          {#each detail.mentioned_entities as e}
            <li>
              <a href={entityHref(e)}>
                <span class="type-pill">{e.type}</span>
                <span class="name">{e.name}</span>
              </a>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 1100px; }
  .breadcrumb { margin-bottom: 0.5rem; font-size: 0.85rem; }
  .breadcrumb a { color: var(--accent); text-decoration: none; }
  .breadcrumb a:hover { color: var(--accent-hi); }

  .head {
    display: flex; justify-content: space-between; align-items: flex-start;
    gap: 1rem; padding: 0.6rem 0.8rem; margin-bottom: 1rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .head h1 {
    margin: 0 0 0.3rem 0; font-size: 1.1rem; color: var(--text);
    line-height: 1.4; display: flex; align-items: baseline; gap: 0.5rem; flex-wrap: wrap;
  }
  .meta {
    display: flex; gap: 0.7rem; flex-wrap: wrap;
    color: var(--text-dim); font-size: 0.82rem;
  }
  .meta .project { color: var(--accent); }
  .meta .valence { padding: 0 0.3rem; border-radius: var(--radius-sm); font-size: 0.75rem; }
  .meta .v-positive { background: var(--success); color: black; }
  .meta .v-negative { background: var(--destructive); color: white; }
  .meta .v-neutral  { background: var(--surface-2); color: var(--text-dim); }
  .meta .forgotten { color: var(--destructive); font-style: italic; }
  .memid { font-family: var(--font-mono); font-size: 0.7rem; color: var(--text-faint); margin-top: 0.4rem; }

  .actions .forget {
    background: var(--surface-2); color: var(--text-dim);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.7rem; cursor: pointer;
  }
  .actions .forget:hover:not(:disabled) { color: var(--destructive); border-color: var(--destructive); }
  .actions .forget:disabled { opacity: 0.4; cursor: not-allowed; }

  .kind {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
  }
  .kind-identity   { background: oklch(0.30 0.10 280); color: oklch(0.85 0.05 280); }
  .kind-fact       { background: oklch(0.28 0.06 200); color: oklch(0.85 0.04 200); }
  .kind-preference { background: oklch(0.28 0.08 145); color: oklch(0.85 0.05 145); }
  .kind-decision   { background: oklch(0.30 0.10 85);  color: oklch(0.85 0.05 85); }
  .kind-rule       { background: oklch(0.30 0.10 20);  color: oklch(0.85 0.05 20); }
  .kind-insight    { background: oklch(0.30 0.08 320); color: oklch(0.85 0.05 320); }
  .kind-procedure  { background: oklch(0.28 0.06 240); color: oklch(0.85 0.04 240); }

  section { margin-bottom: 1.2rem; }
  section h2 {
    margin: 0 0 0.4rem 0; font-size: 1rem; color: var(--text);
    border-bottom: 1px solid var(--border); padding-bottom: 0.3rem;
  }
  section h2 .count { color: var(--text-faint); font-weight: normal; font-size: 0.85rem; margin-left: 0.4rem; }

  .details {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.8rem;
    font-family: var(--font-mono); font-size: 0.85rem;
    color: var(--text); white-space: pre-wrap; word-wrap: break-word;
    margin: 0;
  }

  .tags { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  .tag {
    background: var(--surface-2); color: var(--text-dim);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.1rem 0.4rem; font-size: 0.78rem; font-family: var(--font-mono);
  }

  .entities { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .entities a {
    display: flex; align-items: baseline; gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text); text-decoration: none;
    font-size: 0.88rem;
  }
  .entities a:hover { border-color: var(--accent); }
  .type-pill {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
  }

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

  .muted { color: var(--text-faint); font-style: italic; padding: 0.5rem; }

  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
