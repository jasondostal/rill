<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import PageHeader from '$lib/PageHeader.svelte';
  import Star from '@lucide/svelte/icons/star';

  const KINDS = [
    { value: '', label: 'All kinds' },
    { value: 'decision', label: 'Decision' },
    { value: 'preference', label: 'Preference' },
    { value: 'insight', label: 'Insight' },
    { value: 'procedure', label: 'Procedure' },
    { value: 'fact', label: 'Fact' },
    { value: 'identity', label: 'Identity' },
    { value: 'rule', label: 'Rule' },
  ];

  let query = $state('');
  let kind = $state('');
  let author = $state('');
  let k = $state(10);
  let project = $state(prefs.projects?.[0] || '');

  let result = $state(null);
  let loading = $state(false);
  let error = $state('');
  let lastQuery = $state('');

  let searchSeq = 0;
  async function runSearch() {
    if (!query.trim()) return;
    const mySeq = ++searchSeq;
    loading = true;
    error = '';
    try {
      const args = { k };
      if (kind) args.kind = kind;
      if (author) args.author = author;
      if (project) args.project = project;
      const r = await api.recall(query.trim(), args);
      if (mySeq !== searchSeq) return;
      result = r;
      lastQuery = query.trim();
    } catch (e) {
      if (mySeq !== searchSeq) return;
      error = e.message || 'Search failed';
      result = null;
    } finally {
      if (mySeq === searchSeq) loading = false;
    }
  }

  function onSubmit(e) {
    e.preventDefault();
    runSearch();
  }

  onMount(() => {
    const handler = (e) => {
      const sel = e.detail?.projects || [];
      project = sel.length === 1 ? sel[0] : '';
      if (query.trim()) runSearch();
    };
    window.addEventListener('project-changed', handler);
    return () => window.removeEventListener('project-changed', handler);
  });

  function shortID(id) {
    if (!id) return '';
    return id.replace(/^memory:`?/, '').replace(/`?$/, '');
  }

  function entityHref(e) {
    let slug = e.id;
    const colon = slug.indexOf(':');
    if (colon >= 0) slug = slug.slice(colon + 1).replace(/`/g, '');
    return `/entities/${encodeURIComponent(e.type)}/${encodeURIComponent(slug)}`;
  }

  function memoryHref(id) {
    return `/memories/${encodeURIComponent(shortID(id))}`;
  }
</script>

<svelte:head>
  <title>Search — Rill</title>
</svelte:head>

<div class="page">
  <PageHeader section="search" title="Search">
    <span class="muted-small">Hybrid recall — vector + FTS</span>
  </PageHeader>

  <form class="filters" onsubmit={onSubmit}>
    <input
      type="text"
      bind:value={query}
      placeholder="Search memories — vector matches the summary, FTS fallback on details…"
      class="query"
      autofocus
    />
    <select bind:value={kind}>
      {#each KINDS as opt}<option value={opt.value}>{opt.label}</option>{/each}
    </select>
    <select bind:value={author}>
      <option value="">All authors</option>
      <option value="claude">Claude</option>
    </select>
    <label class="k-label">
      k
      <input type="number" bind:value={k} min="1" max="50" class="k-input" />
    </label>
    <button type="submit" class="primary" disabled={loading || !query.trim()}>
      {loading ? 'Searching…' : 'Search'}
    </button>
  </form>

  {#if error}
    <div class="state error">
      <h2>Search failed</h2>
      <p>{error}</p>
    </div>
  {:else if !result}
    <div class="state empty">
      <p>Type a query above — vector embedding hits the <code>summary</code> field, FTS fallback covers <code>details</code>. Use filters to narrow by <code>kind</code> or <code>author</code>.</p>
      <p class="muted-small">Recall returns memories <em>plus</em> the entities they mention, so you can hop into related cards.</p>
    </div>
  {:else if (!result.memories || result.memories.length === 0) && (!result.entities || result.entities.length === 0)}
    <div class="state empty">
      <h2>No hits for "{lastQuery}"</h2>
      <p>Try a broader query, or clear the filters above.</p>
    </div>
  {:else}
    {#if result.memories?.length}
      <section>
        <h2>Memories <span class="count">({result.memories.length})</span></h2>
        <ol class="hits">
          {#each result.memories as m}
            <li>
              <a href={memoryHref(m.id)}>
                <span class="kind kind-{m.kind}">{m.kind}</span>
                <span class="summary">{m.summary}</span>
                <span class="meta">
                  {#if m.project}<span class="project">{m.project}</span>{/if}
                  <span class="author">by {m.author}</span>
                  <span class="dist">cosine {m.distance?.toFixed(3) ?? '—'}</span>
                </span>
              </a>
            </li>
          {/each}
        </ol>
      </section>
    {/if}

    {#if result.entities?.length}
      <section>
        <h2>Linked entities <span class="count">({result.entities.length})</span></h2>
        <ul class="entities">
          {#each result.entities as e}
            <li>
              <a href={entityHref(e)}>
                <span class="type-pill">{e.type}</span>
                <span class="name">
                  {e.name}
                  {#if e.promoted}<span class="promoted" title="Promoted"><Star size={13} fill="currentColor" /></span>{/if}
                </span>
                <span class="meta">{e.mention_count} mentions</span>
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 1100px; }
  .muted-small { color: var(--text-faint); font-size: 0.75rem; font-family: var(--font-mono); }

  .filters {
    display: flex; gap: 0.5rem; align-items: center;
    padding: 0.5rem 0.6rem; margin-bottom: 0.8rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .query {
    flex: 1; background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 0.6rem; font-size: 0.9rem;
  }
  .query:focus { outline: none; border-color: var(--accent); }
  .filters select, .filters .k-input {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.5rem; font-size: 0.85rem;
  }
  .filters .k-label { color: var(--text-dim); font-size: 0.78rem; display: flex; align-items: center; gap: 0.3rem; }
  .filters .k-input { width: 3rem; }
  .filters .primary {
    background: var(--accent); color: white; border: none;
    border-radius: var(--radius-sm); padding: 0.4rem 1rem;
    cursor: pointer; font-size: 0.9rem;
  }
  .filters .primary:hover:not(:disabled) { background: var(--accent-hi); }
  .filters .primary:disabled { opacity: 0.5; cursor: not-allowed; }

  section { margin-bottom: 1.5rem; }
  section h2 {
    margin: 0 0 0.5rem 0; font-size: 1rem; color: var(--text);
    border-bottom: 1px solid var(--border); padding-bottom: 0.3rem;
  }
  section h2 .count { color: var(--text-faint); font-weight: normal; font-size: 0.85rem; margin-left: 0.4rem; }

  .hits, .entities { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .hits a, .entities a {
    display: grid; align-items: baseline; gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text); text-decoration: none;
    font-size: 0.88rem;
  }
  .hits a { grid-template-columns: 5rem 1fr auto; }
  .entities a { grid-template-columns: 8rem 1fr auto; }
  .hits a:hover, .entities a:hover { border-color: var(--accent); }

  .kind {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
    text-align: center;
  }
  .kind-identity   { background: oklch(0.30 0.10 280); color: oklch(0.85 0.05 280); }
  .kind-fact       { background: oklch(0.28 0.06 200); color: oklch(0.85 0.04 200); }
  .kind-preference { background: oklch(0.28 0.08 145); color: oklch(0.85 0.05 145); }
  .kind-decision   { background: oklch(0.30 0.10 85);  color: oklch(0.85 0.05 85); }
  .kind-rule       { background: oklch(0.30 0.10 20);  color: oklch(0.85 0.05 20); }
  .kind-insight    { background: oklch(0.30 0.08 320); color: oklch(0.85 0.05 320); }
  .kind-procedure  { background: oklch(0.28 0.06 240); color: oklch(0.85 0.04 240); }

  .meta {
    display: flex; gap: 0.6rem; align-items: baseline;
    color: var(--text-faint); font-size: 0.75rem; font-family: var(--font-mono);
  }
  .meta .project { color: var(--accent); }
  .meta .dist { white-space: nowrap; }

  .type-pill {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
    text-align: center;
  }
  .promoted { color: var(--warning); margin-left: 0.3rem; }

  .state {
    padding: 1.5rem 1rem; text-align: center;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text-dim);
  }
  .state h2 { margin: 0 0 0.4rem 0; color: var(--text); font-size: 1rem; }
  .state.error { border-color: var(--destructive); background: var(--destructive-bg); }
  .state.error h2 { color: var(--destructive); }
  .state p { margin: 0.3rem 0; }

  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
