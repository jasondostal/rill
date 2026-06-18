<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { beforeNavigate, goto } from '$app/navigation';
  import { api } from '$lib/api.js';
  import { toast } from 'svelte-sonner';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import AlertTriangle from '@lucide/svelte/icons/triangle-alert';
  import Check from '@lucide/svelte/icons/check';
  import X from '@lucide/svelte/icons/x';
  import ArrowRight from '@lucide/svelte/icons/arrow-right';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import Plus from '@lucide/svelte/icons/plus';
  import Star from '@lucide/svelte/icons/star';

  let type = $derived(page.params.type);
  let slug = $derived(page.params.slug);

  let detail = $state(null);
  let loading = $state(true);
  let error = $state('');
  let isReady = $state(true);

  // Hand-notes editor state. derived_card is system-rendered (view-only).
  let editText = $state('');
  let serverNotes = $state('');
  let dirty = $derived(editText !== serverNotes);
  let saving = $state(false);
  let lastSavedAt = $state(null);
  let saveError = $state('');
  let busy = $state(false); // promote/demote/edge ops

  // Documents associated with this entity via doc_about (Phase 3). Read-only
  // here — link/unlink happens on the document page.
  let entityDocs = $state([]);
  let docsLoading = $state(false);
  async function loadEntityDocs() {
    docsLoading = true;
    try {
      const r = await api.listDocs({ entity: `${type}:${slug}`, limit: 100 });
      entityDocs = r.documents || [];
    } catch {
      entityDocs = [];
    }
    docsLoading = false;
  }

  // Merge state — fold this entity into another of the same type.
  let showMerge = $state(false);
  let mergeTarget = $state('');
  let merging = $state(false);

  // Version state — set the entity's current version (bi-temporal).
  let showVersion = $state(false);
  let versionInput = $state('');
  let savingVersion = $state(false);

  let autosaveTimer = null;
  const AUTOSAVE_DELAY = 1200;

  // Add-edge form state
  let showAddEdge = $state(false);
  let newEdgePredicate = $state('uses');
  let newEdgeObjectName = $state('');
  let newEdgeObjectType = $state('tool');
  let newEdgeValence = $state('positive');
  let newEdgeRole = $state('');
  let addingEdge = $state(false);

  const KNOWN_PREDICATES = [
    { value: 'works_on', subject: 'person', object: 'project' },
    { value: 'uses',     subject: 'person', object: 'tool' },
    { value: 'prefers',  subject: 'person', object: 'preference' },
    { value: 'works_at', subject: 'person', object: 'organization' },
    { value: 'depends_on', subject: 'any',  object: 'any' },
    { value: 'part_of',  subject: 'any',    object: 'any' },
    { value: 'is_a',     subject: 'any',    object: 'concept' },
    { value: 'lives_in', subject: 'person', object: 'place' },
  ];
  const ENTITY_TYPES = ['person', 'project', 'tool', 'organization', 'place', 'preference', 'concept'];

  function scheduleAutosave() {
    if (autosaveTimer) clearTimeout(autosaveTimer);
    if (!dirty) return;
    autosaveTimer = setTimeout(() => {
      autosaveTimer = null;
      saveNotes({ silent: true });
    }, AUTOSAVE_DELAY);
  }

  async function load() {
    loading = true;
    error = '';
    try {
      detail = await api.getEntity(type, slug);
      serverNotes = detail?.hand_notes || '';
      editText = serverNotes;
      loadEntityDocs();
    } catch (e) {
      const msg = String(e.message || '');
      if (msg.includes('404')) {
        if (!(await api.isReady())) isReady = false;
        else error = 'Entity not found';
      } else {
        error = msg || 'Failed to load entity';
      }
      detail = null;
    }
    loading = false;
  }

  async function refresh({ keepEdits = true } = {}) {
    try {
      const next = await api.getEntity(type, slug);
      detail = next;
      const incoming = next?.hand_notes || '';
      if (!keepEdits || !dirty) {
        serverNotes = incoming;
        editText = incoming;
      } else {
        serverNotes = incoming;
      }
    } catch {/* best effort */}
  }

  onMount(() => {
    load();
    const handler = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        if (dirty && !saving) saveNotes();
      }
    };
    window.addEventListener('keydown', handler);
    return () => {
      window.removeEventListener('keydown', handler);
      if (autosaveTimer) clearTimeout(autosaveTimer);
    };
  });

  beforeNavigate(({ cancel }) => {
    if (dirty && !confirm('You have unsaved changes to hand notes. Leave anyway?')) cancel();
  });

  async function saveNotes({ silent = false } = {}) {
    if (!dirty || saving) return;
    if (autosaveTimer) { clearTimeout(autosaveTimer); autosaveTimer = null; }
    saving = true;
    saveError = '';
    const snapshot = editText;
    try {
      const updated = await api.editHandNotes(type, slug, snapshot, 'replace');
      detail = updated;
      serverNotes = updated?.hand_notes || '';
      lastSavedAt = new Date();
      if (!silent) toast.success('Hand notes saved');
    } catch (e) {
      saveError = e.message || 'Save failed';
      if (!silent) toast.error(saveError);
    }
    saving = false;
  }

  function discardEdits() {
    if (!dirty) return;
    if (!confirm('Discard unsaved edits and revert to the saved hand notes?')) return;
    if (autosaveTimer) { clearTimeout(autosaveTimer); autosaveTimer = null; }
    editText = serverNotes;
    saveError = '';
  }

  async function togglePromoted() {
    if (!detail) return;
    busy = true;
    try {
      if (detail.promoted) {
        await api.demote(type, slug);
        toast.success('Demoted');
      } else {
        await api.promote(type, slug);
        toast.success('Promoted');
      }
      await refresh({ keepEdits: true });
    } catch (e) {
      toast.error(e.message || 'Failed');
    }
    busy = false;
  }

  async function submitVersion() {
    const v = versionInput.trim();
    if (!v) { toast.error('Version is required'); return; }
    savingVersion = true;
    try {
      detail = await api.setVersion(type, slug, v);
      toast.success(`Version set to ${v}`);
      showVersion = false;
      versionInput = '';
    } catch (e) {
      toast.error(e.message || 'Set version failed');
    }
    savingVersion = false;
  }

  async function submitMerge() {
    const target = mergeTarget.trim();
    if (!target) { toast.error('Target entity is required'); return; }
    if (target === detail.name || target === `${detail.type}:${slug}`) {
      toast.error('Pick a different entity to merge into');
      return;
    }
    if (!confirm(
      `Merge "${detail.name}" INTO "${target}"?\n\n` +
      `All edges + mentions move to the target. "${detail.name}" is retired ` +
      `(soft / reversible). Proceed?`
    )) return;
    merging = true;
    try {
      const res = await api.mergeEntity(type, slug, target);
      toast.success(`Merged → ${res.target} (${res.edges_moved} edges, ${res.mentions_moved} mentions)`);
      const t = res.target || '';
      const colon = t.indexOf(':');
      if (colon > 0) {
        goto(`/entities/${encodeURIComponent(t.slice(0, colon))}/${encodeURIComponent(t.slice(colon + 1))}`);
      } else {
        showMerge = false;
        await refresh();
      }
    } catch (e) {
      toast.error(e.message || 'Merge failed');
    }
    merging = false;
  }

  async function closeEdge(edgeID, label) {
    if (!confirm(`Remove this edge?\n\n${label}\n\nThe edge will be soft-closed (provenance preserved). The derived card will update.`)) return;
    busy = true;
    try {
      await api.closeEdge(edgeID);
      toast.success('Edge closed');
      await refresh({ keepEdits: true });
    } catch (e) {
      toast.error(e.message || 'Close edge failed');
    }
    busy = false;
  }

  async function submitAddEdge() {
    if (!newEdgeObjectName.trim()) {
      toast.error('Object name is required');
      return;
    }
    addingEdge = true;
    try {
      const decl = {
        subject: detail.name,
        subject_type: detail.type,
        predicate: newEdgePredicate,
        object: newEdgeObjectName.trim(),
        object_type: newEdgeObjectType,
      };
      if (newEdgePredicate === 'prefers') decl.valence = newEdgeValence;
      if (newEdgePredicate === 'works_at' && newEdgeRole.trim()) decl.role_title = newEdgeRole.trim();
      await api.addEdge(decl);
      toast.success('Edge added');
      showAddEdge = false;
      newEdgeObjectName = '';
      newEdgeRole = '';
      await refresh({ keepEdits: true });
    } catch (e) {
      const msg = e.message || 'Add edge failed';
      if (msg.includes('does not exist')) {
        toast.error(`Object entity doesn't exist yet — declare it via a remember() first.`);
      } else {
        toast.error(msg);
      }
    }
    addingEdge = false;
  }

  async function forgetMemory(memoryID) {
    if (!confirm('Forget this memory? Soft-delete (provenance preserved, but excluded from orient/recall).')) return;
    try {
      await api.forget(memoryID);
      toast.success('Memory forgotten');
      await refresh({ keepEdits: true });
    } catch (e) {
      toast.error(e.message || 'Forget failed');
    }
  }

  function fmtDate(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 16) + 'Z';
  }

  function fmtAge(d) {
    if (!d) return '';
    const secs = Math.floor((Date.now() - d.getTime()) / 1000);
    if (secs < 5) return 'just now';
    if (secs < 60) return `${secs}s ago`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
    return d.toISOString().slice(11, 16) + ' UTC';
  }

  function shortMemId(id) {
    if (!id) return '';
    const colon = id.indexOf(':');
    return colon >= 0 ? id.slice(colon + 1).replace(/`/g, '') : id;
  }

  // Best-effort default object type when the predicate changes.
  function onPredicateChange() {
    const p = KNOWN_PREDICATES.find(p => p.value === newEdgePredicate);
    if (p && p.object !== 'any') newEdgeObjectType = p.object;
  }
</script>

<svelte:head>
  <title>{detail?.name || slug} — Rill</title>
</svelte:head>

<div class="page">
  <nav class="breadcrumb">
    <a href="/entities"><ArrowLeft size={14} /> Entities</a>
  </nav>

  {#if !isReady}
    <div class="state empty">
      <h2>Memory store unreachable</h2>
      <p>Server isn't running with <code>RILL_SURREAL_URL/RILL_SURREAL_NS</code>.</p>
    </div>
  {:else if loading}
    <div class="state loading">
      <div class="spinner"></div>
      <p>Loading entity…</p>
    </div>
  {:else if error}
    <div class="state error">
      <h2>{error}</h2>
      <p>Entity ref: <code>{type}:{slug}</code></p>
      <button onclick={load}>Try again</button>
    </div>
  {:else if !detail}
    <div class="state empty">
      <h2>Entity not found</h2>
      <p><code>{type}:{slug}</code> doesn't exist in the store yet.</p>
    </div>
  {:else}
    <header class="head">
      <div>
        <h1>
          {detail.name}
          {#if detail.promoted}<span class="promoted" title="Promoted — appears in orient"><Star size={15} fill="currentColor" /></span>{/if}
        </h1>
        <div class="meta">
          <span class="type-pill">{detail.type}</span>
          {#if detail.version}<span class="version-pill" title="Current version (bi-temporal)">v{detail.version}</span>{/if}
          {#if detail.aliases && detail.aliases.length > 1}
            <span>aliases: {detail.aliases.filter(a => a !== detail.name).join(', ')}</span>
          {/if}
          <span>{detail.mention_count} mentions</span>
          <span>first seen {fmtDate(detail.first_seen)}</span>
        </div>
        {#if detail.last_edited_by}
          <div class="audit">last edited by <strong>{detail.last_edited_by}</strong> at {fmtDate(detail.last_edited_at)}</div>
        {/if}
      </div>
      <div class="actions">
        <button
          class:promoted={detail.promoted}
          onclick={togglePromoted}
          disabled={busy}
          title={detail.promoted
            ? 'Demote — remove from the orient render (entity still exists, still mentioned by memories)'
            : 'Promote — include this entity in the orient render that agents see at session start'}
        >
          {detail.promoted ? 'Demote' : 'Promote'}
        </button>
        <button
          class:active={showVersion}
          onclick={() => { showVersion = !showVersion; versionInput = detail.version || ''; }}
          disabled={busy}
          title="Set this entity's current version (bi-temporal — keeps history)"
        >
          {showVersion ? 'Cancel' : (detail.version ? 'Version' : 'Set version')}
        </button>
        <button
          class:active={showMerge}
          onclick={() => showMerge = !showMerge}
          disabled={busy}
          title="Merge this entity into another of the same type (re-points edges + mentions, retires this one)"
        >
          {showMerge ? 'Cancel merge' : 'Merge'}
        </button>
        <button class="refresh" onclick={() => refresh()} title="Refetch from server (preserves your unsaved edits)"><RotateCw size={15} /></button>
      </div>
    </header>

    {#if showVersion}
      <div class="version-form">
        <label>
          Version of <strong>{detail.name}</strong>
          <input
            type="text"
            bind:value={versionInput}
            placeholder="e.g. K2.6, 3.6, 2.3.0"
            onkeydown={(e) => { if (e.key === 'Enter') submitVersion(); }}
          />
        </label>
        <button class="primary" onclick={submitVersion} disabled={savingVersion}>
          {savingVersion ? 'Saving…' : 'Set version'}
        </button>
        <p class="muted-small">
          Bi-temporal: setting a new version closes the prior one but keeps full history (as-of queryable). Stored as an attribute, not a graph node.
        </p>
      </div>
    {/if}

    {#if showMerge}
      <div class="merge-form">
        <label>
          Merge <strong>{detail.name}</strong> <span class="type-pill">{detail.type}</span> into
          <input
            type="text"
            bind:value={mergeTarget}
            placeholder={`another ${detail.type} — name or record id`}
            onkeydown={(e) => { if (e.key === 'Enter') submitMerge(); }}
          />
        </label>
        <button class="danger" onclick={submitMerge} disabled={merging}>
          {merging ? 'Merging…' : 'Merge & retire this entity'}
        </button>
        <p class="muted-small">
          Re-points all edges + mentions onto the target, folds this entity's name/aliases/notes in,
          sums mention counts, then soft-retires this one (reversible via <code>merged_into</code>).
          Same type only · admin only.
        </p>
      </div>
    {/if}

    <!-- DERIVED CARD — view-only, system-maintained from the graph -->
    <section class="card-section">
      <div class="card-header">
        <h2>Derived card <span class="muted-small">(auto, view-only)</span></h2>
      </div>
      <p class="card-help">
        Rendered by the system from this entity's active edges and matching memories. Updated automatically on every <code>remember()</code>, <code>add_edge</code>, or <code>close_edge</code>. Edit the graph (below + on memories) to change what shows here — not the text directly.
      </p>
      {#if detail.derived_card}
        <pre class="derived-view">{detail.derived_card}</pre>
      {:else}
        <div class="derived-empty">
          No graph data yet. Add edges below or write a memory mentioning this entity, and content will appear here.
        </div>
      {/if}
    </section>

    <!-- HAND NOTES — human-edited, free form, autosaved -->
    <section class="card-section">
      <div class="card-header">
        <h2>Hand notes <span class="muted-small">(human-curated)</span></h2>
        <div class="card-status" aria-live="polite">
          {#if saving}
            <span class="status saving"><span class="dot"></span> Saving…</span>
          {:else if saveError}
            <span class="status error"><AlertTriangle size={13} /> {saveError}</span>
          {:else if dirty}
            <span class="status dirty"><span class="dot"></span> Unsaved changes</span>
          {:else if lastSavedAt}
            <span class="status saved"><Check size={13} /> Saved {fmtAge(lastSavedAt)}</span>
          {:else}
            <span class="status idle">No changes</span>
          {/if}
        </div>
      </div>
      <p class="card-help">
        Your free-form commentary about this entity. Orient renders this <strong>above</strong> the derived card, so it's the first thing agents see. Autosaves 1.2s after you stop typing; ⌘S / Ctrl+S also works.
      </p>
      <div class="editor">
        <textarea
          bind:value={editText}
          oninput={scheduleAutosave}
          placeholder="Free-form markdown notes about this entity. Anything that doesn't fit a structured edge."
          rows="10"
          spellcheck="false"
        ></textarea>
        <div class="editor-toolbar">
          <span class="muted-small">
            {editText.length} chars · server: {serverNotes.length} chars
            {#if dirty}· <strong>{Math.abs(editText.length - serverNotes.length)}</strong> char Δ{/if}
          </span>
          <div class="spacer"></div>
          <button class="ghost" onclick={discardEdits} disabled={!dirty || saving}>Discard</button>
          <button class="primary" onclick={() => saveNotes()} disabled={!dirty || saving}>
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </section>

    <section class="related">
      <h2>
        Edges <span class="count">({detail.edges?.length || 0})</span>
        <button class="add-edge" onclick={() => showAddEdge = !showAddEdge}>{showAddEdge ? 'Cancel' : '+ Add edge'}</button>
      </h2>

      {#if showAddEdge}
        <div class="add-edge-form">
          <label>
            Predicate
            <select bind:value={newEdgePredicate} onchange={onPredicateChange}>
              {#each KNOWN_PREDICATES as p}
                <option value={p.value}>{p.value}</option>
              {/each}
            </select>
          </label>
          <label>
            Object name
            <input type="text" bind:value={newEdgeObjectName} placeholder="e.g. SurrealDB" />
          </label>
          <label>
            Object type
            <select bind:value={newEdgeObjectType}>
              {#each ENTITY_TYPES as t}
                <option value={t}>{t}</option>
              {/each}
            </select>
          </label>
          {#if newEdgePredicate === 'prefers'}
            <label>
              Valence
              <select bind:value={newEdgeValence}>
                <option value="positive">positive</option>
                <option value="negative">negative</option>
                <option value="neutral">neutral</option>
              </select>
            </label>
          {/if}
          {#if newEdgePredicate === 'works_at'}
            <label>
              Role title
              <input type="text" bind:value={newEdgeRole} placeholder="e.g. Engineer" />
            </label>
          {/if}
          <button class="primary" onclick={submitAddEdge} disabled={addingEdge}>
            {addingEdge ? 'Adding…' : 'Add edge'}
          </button>
          <p class="muted-small">
            Object entity must already exist. If it doesn't, declare it first via a <code>remember()</code> with the object in <code>entities[]</code>.
          </p>
        </div>
      {/if}

      {#if !detail.edges || detail.edges.length === 0}
        <p class="muted">No relationship edges yet.</p>
      {:else}
        <ul class="edges">
          {#each detail.edges as e}
            <li class:inactive={!e.active}>
              <span class="direction">{#if e.direction === 'out'}<ArrowRight size={13} />{:else}<ArrowLeft size={13} />{/if}</span>
              <span class="predicate">{e.predicate}</span>
              <a
                class="other"
                href={`/entities/${encodeURIComponent(e.other_type)}/${encodeURIComponent(e.other_id.includes(':') ? e.other_id.split(':').slice(1).join(':') : e.other_id)}`}
                title={`Open ${e.other_type}:${e.other_name}`}
              >
                {e.other_name}
                <span class="other-type">{e.other_type}</span>
              </a>
              {#if e.valence}<span class="valence v-{e.valence}">{e.valence}</span>{/if}
              {#if e.role_title}<span class="role">{e.role_title}</span>{/if}
              {#if !e.active}<span class="closed">closed {fmtDate(e.valid_until)}</span>{/if}
              {#if e.active}
                <button
                  class="close-edge"
                  onclick={() => closeEdge(e.id, `${e.predicate} ${e.direction === 'out' ? '→' : '←'} ${e.other_name}`)}
                  disabled={busy}
                  title="Soft-close this edge"
                ><X size={13} /></button>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="related">
      <h2>Mentions <span class="count">({detail.mentions?.length || 0})</span></h2>
      {#if !detail.mentions || detail.mentions.length === 0}
        <p class="muted">No memories mention this entity yet.</p>
      {:else}
        <ul class="mentions">
          {#each detail.mentions as m}
            <li>
              <div class="mention-head">
                <span class="kind">{m.kind}</span>
                {#if m.project}<span class="project">{m.project}</span>{/if}
                <span class="author">by {m.author}</span>
                <span class="when">{fmtDate(m.created_at)}</span>
                <button class="forget" onclick={() => forgetMemory(m.memory_id)} title="Soft-delete this memory">forget</button>
              </div>
              <div class="summary">{m.summary}</div>
              <div class="memid">{shortMemId(m.memory_id)}</div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="related">
      <h2>
        Documents <span class="count">({entityDocs.length})</span>
        <a class="ed-new" href={`/documents/new?entity=${encodeURIComponent(type + ':' + slug)}`}><Plus size={13} /> New document</a>
      </h2>
      {#if docsLoading}
        <p class="muted">Loading…</p>
      {:else if entityDocs.length === 0}
        <p class="muted">No documents about this {type} yet.</p>
      {:else}
        <ul class="entity-docs">
          {#each entityDocs as d}
            <li>
              <a href={`/documents/${encodeURIComponent(d.id.replace(/^document:/, ''))}`}>
                <span class="ed-type">{d.doc_type}</span>
                <span class="ed-title">{d.title}</span>
                {#if d.project}<span class="project">{d.project}</span>{/if}
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
    gap: 1rem; padding: 0.6rem 0.8rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); margin-bottom: 1rem;
  }
  .head h1 { margin: 0 0 0.3rem 0; font-size: 1.4rem; color: var(--text); }
  .meta {
    display: flex; gap: 0.7rem; flex-wrap: wrap;
    color: var(--text-dim); font-size: 0.82rem;
  }
  .audit {
    margin-top: 0.3rem; color: var(--text-faint); font-size: 0.75rem;
    font-style: italic;
  }
  .type-pill {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
  }
  .promoted { color: var(--warning); margin-left: 0.3rem; }
  .actions { display: flex; gap: 0.3rem; }
  .actions button {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.7rem; cursor: pointer;
  }
  .actions button.promoted { border-color: var(--warning); color: var(--warning); }
  .actions button:hover:not(:disabled) { border-color: var(--accent); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .actions .refresh { color: var(--text-faint); }

  .card-section, .related { margin-bottom: 1.5rem; }
  .card-section h2, .related h2 {
    margin: 0; font-size: 1rem; color: var(--text);
    display: flex; align-items: baseline; gap: 0.5rem;
  }
  .related h2 {
    border-bottom: 1px solid var(--border); padding-bottom: 0.3rem; margin-bottom: 0.5rem;
  }
  .related h2 .count { color: var(--text-faint); font-weight: normal; font-size: 0.85rem; }
  .related h2 .add-edge {
    margin-left: auto; background: var(--surface-2); color: var(--text-dim);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.6rem; font-size: 0.78rem; cursor: pointer;
  }
  .related h2 .add-edge:hover { color: var(--accent); border-color: var(--accent); }

  .card-header {
    display: flex; align-items: baseline; justify-content: space-between;
    gap: 0.6rem; border-bottom: 1px solid var(--border); padding-bottom: 0.3rem;
    margin-bottom: 0.4rem;
  }
  .card-status .status {
    font-size: 0.78rem; display: inline-flex; align-items: center; gap: 0.4rem;
    padding: 0.15rem 0.5rem; border-radius: var(--radius-sm);
  }
  .card-status .status.idle { color: var(--text-faint); }
  .card-status .status.saved { color: var(--success); }
  .card-status .status.dirty { color: var(--warning); background: var(--surface-2); }
  .card-status .status.saving { color: var(--accent); }
  .card-status .status.error { color: var(--destructive); background: var(--destructive-bg); }
  .card-status .dot {
    width: 7px; height: 7px; border-radius: 50%; background: currentColor;
    box-shadow: 0 0 6px currentColor;
  }
  .card-status .status.dirty .dot,
  .card-status .status.saving .dot { animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.3; } }

  .card-help {
    color: var(--text-dim); font-size: 0.82rem; margin: 0 0 0.6rem 0;
    line-height: 1.5;
  }

  .derived-view {
    background: var(--surface); border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm); padding: 0.8rem;
    font-family: var(--font-mono); font-size: 0.85rem;
    color: var(--text); white-space: pre-wrap; word-wrap: break-word;
    margin: 0;
  }
  .derived-empty {
    padding: 1rem; text-align: center; color: var(--text-faint);
    background: var(--surface); border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
  }

  .editor {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.6rem;
  }
  .editor textarea {
    width: 100%; box-sizing: border-box;
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.6rem; font-family: var(--font-mono); font-size: 0.88rem;
    line-height: 1.55; resize: vertical;
    margin-bottom: 0.5rem;
  }
  .editor textarea:focus { outline: none; border-color: var(--accent); }
  .editor-toolbar { display: flex; align-items: center; gap: 0.6rem; }
  .editor-toolbar .spacer { flex: 1; }
  .editor-toolbar button {
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.8rem; cursor: pointer; font-size: 0.85rem;
  }
  .editor-toolbar button.primary {
    background: var(--accent); color: white; border-color: var(--accent);
  }
  .editor-toolbar button.primary:hover:not(:disabled) { background: var(--accent-hi); border-color: var(--accent-hi); }
  .editor-toolbar button.ghost { background: transparent; color: var(--text-dim); }
  .editor-toolbar button.ghost:hover:not(:disabled) { color: var(--text); border-color: var(--text-faint); }
  .editor-toolbar button:disabled { opacity: 0.4; cursor: not-allowed; }

  .muted-small { color: var(--text-faint); font-size: 0.75rem; font-family: var(--font-mono); font-weight: normal; }

  .add-edge-form {
    display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: flex-end;
    background: var(--surface); border: 1px solid var(--accent);
    border-radius: var(--radius-sm); padding: 0.6rem; margin-bottom: 0.5rem;
  }
  .add-edge-form label {
    display: flex; flex-direction: column; gap: 0.2rem;
    color: var(--text-dim); font-size: 0.78rem;
  }
  .add-edge-form input, .add-edge-form select {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.5rem; font-size: 0.85rem;
    min-width: 8rem;
  }
  .add-edge-form button.primary {
    background: var(--accent); color: white; border: none;
    border-radius: var(--radius-sm); padding: 0.4rem 1rem;
    cursor: pointer; font-size: 0.85rem;
  }
  .add-edge-form button.primary:hover:not(:disabled) { background: var(--accent-hi); }
  .add-edge-form button.primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .add-edge-form > p { flex-basis: 100%; margin: 0; }

  .actions button.active { border-color: var(--destructive); color: var(--destructive); }
  .merge-form {
    display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: flex-end;
    background: var(--destructive-bg); border: 1px solid var(--destructive);
    border-radius: var(--radius-sm); padding: 0.6rem; margin-bottom: 1rem;
  }
  .merge-form label {
    display: flex; flex-direction: column; gap: 0.25rem;
    color: var(--text-dim); font-size: 0.82rem; flex: 1; min-width: 16rem;
  }
  .merge-form input {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.35rem 0.5rem; font-size: 0.9rem;
  }
  .merge-form input:focus { outline: none; border-color: var(--accent); }
  .merge-form button.danger {
    background: var(--destructive); color: white; border: none;
    border-radius: var(--radius-sm); padding: 0.45rem 1rem;
    cursor: pointer; font-size: 0.85rem; white-space: nowrap;
  }
  .merge-form button.danger:hover:not(:disabled) { filter: brightness(1.1); }
  .merge-form button.danger:disabled { opacity: 0.5; cursor: not-allowed; }
  .merge-form > p { flex-basis: 100%; margin: 0; }

  .version-pill {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: var(--accent); color: white; letter-spacing: 0.03em;
  }
  .version-form {
    display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: flex-end;
    background: var(--surface); border: 1px solid var(--accent);
    border-radius: var(--radius-sm); padding: 0.6rem; margin-bottom: 1rem;
  }
  .version-form label {
    display: flex; flex-direction: column; gap: 0.25rem;
    color: var(--text-dim); font-size: 0.82rem; flex: 1; min-width: 14rem;
  }
  .version-form input {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.35rem 0.5rem; font-size: 0.9rem;
  }
  .version-form input:focus { outline: none; border-color: var(--accent); }
  .version-form button.primary {
    background: var(--accent); color: white; border: none;
    border-radius: var(--radius-sm); padding: 0.45rem 1rem;
    cursor: pointer; font-size: 0.85rem; white-space: nowrap;
  }
  .version-form button.primary:hover:not(:disabled) { background: var(--accent-hi); }
  .version-form button.primary:disabled { opacity: 0.5; cursor: not-allowed; }
  .version-form > p { flex-basis: 100%; margin: 0; }

  .mentions, .edges { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.3rem; }
  .mentions li, .edges li {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.5rem 0.6rem;
    font-size: 0.85rem; color: var(--text);
  }
  .mention-head {
    display: flex; gap: 0.6rem; flex-wrap: wrap; align-items: baseline;
    color: var(--text-dim); font-size: 0.78rem; margin-bottom: 0.2rem;
  }
  .mention-head .kind {
    font-family: var(--font-mono); padding: 0.05rem 0.3rem;
    background: var(--surface-2); border-radius: var(--radius-sm);
    text-transform: uppercase; letter-spacing: 0.05em; font-size: 0.7rem;
  }
  .mention-head .project { color: var(--accent); }
  .mention-head .when { color: var(--text-faint); margin-left: auto; }
  .mention-head .forget {
    background: transparent; color: var(--text-faint);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0 0.4rem; font-size: 0.7rem; cursor: pointer;
  }
  .mention-head .forget:hover { color: var(--destructive); border-color: var(--destructive); }
  .summary { color: var(--text); margin-bottom: 0.2rem; }
  .memid { font-family: var(--font-mono); font-size: 0.7rem; color: var(--text-faint); }

  .edges li { display: flex; align-items: baseline; gap: 0.5rem; flex-wrap: wrap; }
  .edges .direction { color: var(--text-faint); font-family: var(--font-mono); }
  .edges .predicate { font-family: var(--font-mono); color: var(--accent); }
  .edges a.other {
    color: inherit;
    text-decoration: none;
    border-bottom: 1px dotted var(--text-faint);
    padding-bottom: 1px;
    transition: color 0.1s, border-color 0.1s;
  }
  .edges a.other:hover {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }
  .edges .other-type {
    font-family: var(--font-mono); font-size: 0.7rem;
    padding: 0.05rem 0.3rem; background: var(--surface-2);
    color: var(--text-dim); border-radius: var(--radius-sm); margin-left: 0.3rem;
  }
  .edges .valence { font-size: 0.75rem; padding: 0.05rem 0.3rem; border-radius: var(--radius-sm); }
  .edges .v-positive { background: var(--success); color: black; }
  .edges .v-negative { background: var(--destructive); color: white; }
  .edges .v-neutral { background: var(--surface-2); color: var(--text-dim); }
  .edges .role { color: var(--text-dim); font-style: italic; }
  .edges .closed { color: var(--text-faint); font-size: 0.75rem; }
  .edges li.inactive { opacity: 0.55; }
  .edges .close-edge {
    margin-left: auto; background: transparent; color: var(--text-faint);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0 0.45rem; font-size: 0.9rem; line-height: 1; cursor: pointer;
  }
  .edges .close-edge:hover:not(:disabled) { color: var(--destructive); border-color: var(--destructive); }
  .edges .close-edge:disabled { opacity: 0.4; cursor: not-allowed; }

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

  /* Documents section (Phase 3) */
  .ed-new {
    float: right; font-size: 0.78rem; font-weight: normal;
    background: var(--surface-2); color: var(--text-dim);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.15rem 0.5rem; text-decoration: none;
  }
  .ed-new:hover { border-color: var(--accent); color: var(--text); }
  .entity-docs { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 0.25rem; }
  .entity-docs a {
    display: flex; align-items: baseline; gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); text-decoration: none;
    font-size: 0.88rem;
  }
  .entity-docs a:hover { border-color: var(--accent); }
  .ed-type {
    font-family: var(--font-mono); font-size: 0.68rem;
    padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: oklch(0.30 0.08 280); color: oklch(0.88 0.05 280);
    text-transform: uppercase; letter-spacing: 0.05em;
  }
  .ed-title { color: var(--text); }
</style>
