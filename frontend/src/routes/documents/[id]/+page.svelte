<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import { goto } from '$app/navigation';
  import { api } from '$lib/api.js';
  import { renderMarkdown } from '$lib/markdown.js';
  import { toast } from 'svelte-sonner';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';
  import X from '@lucide/svelte/icons/x';
  import Plus from '@lucide/svelte/icons/plus';

  let id = $derived(page.params.id);

  let doc = $state(null);
  let loading = $state(true);
  let error = $state('');
  let isReady = $state(true);

  let editing = $state(false);
  let saving = $state(false);
  // edit buffer
  let eTitle = $state('');
  let eType = $state('');
  let eProject = $state('');
  let eSource = $state('');
  let eContent = $state('');

  let rendered = $derived(doc ? renderMarkdown(doc.content) : '');

  // Entity association (read-mode inline link/unlink).
  const ENTITY_TYPES = ['person', 'project', 'tool', 'organization', 'place', 'preference', 'concept'];
  let showLink = $state(false);
  let linkType = $state('project');
  let linkName = $state('');
  let linking = $state(false);

  async function linkEntity() {
    if (!linkName.trim()) { toast.error('Entity name is required'); return; }
    linking = true;
    try {
      doc = await api.associateDoc(id, linkName.trim(), linkType);
      showLink = false; linkName = '';
      toast.success('Linked');
    } catch (e) {
      toast.error(e.message || 'Link failed');
    }
    linking = false;
  }

  async function unlinkEntity(e) {
    linking = true;
    try {
      doc = await api.unassociateDoc(id, e.type, slugOfEntity(e.id));
      toast.success('Unlinked');
    } catch (err) {
      toast.error(err.message || 'Unlink failed');
    }
    linking = false;
  }

  async function load() {
    loading = true;
    error = '';
    try {
      doc = await api.getDoc(id);
      if (!doc) error = 'Document not found';
    } catch (e) {
      const msg = String(e.message || '');
      if (msg.includes('404')) {
        if (!(await api.isReady())) isReady = false;
        else error = 'Document not found';
      } else {
        error = msg || 'Failed to load document';
      }
      doc = null;
    }
    loading = false;
  }

  onMount(load);

  function startEdit() {
    eTitle = doc.title;
    eType = doc.doc_type;
    eProject = doc.project || '';
    eSource = doc.source || '';
    eContent = doc.content;
    editing = true;
  }

  async function save() {
    if (!eTitle.trim()) { toast.error('Title is required'); return; }
    saving = true;
    try {
      // Omit entities so existing associations are preserved (managed separately).
      doc = await api.updateDoc(id, {
        title: eTitle,
        content: eContent,
        doc_type: eType,
        project: eProject,
        source: eSource,
      });
      editing = false;
      toast.success('Saved');
    } catch (e) {
      toast.error(e.message || 'Save failed');
    }
    saving = false;
  }

  async function remove() {
    // No confirm() — the undo toast is the safety net. Delete fires immediately
    // (real soft-delete); Undo calls restore and brings you back to the doc.
    const deletedId = id;
    const title = doc.title;
    try {
      await api.deleteDoc(deletedId);
    } catch (e) {
      const msg = String(e.message || '');
      toast.error(msg.includes('403') ? 'Delete requires admin scope' : (msg || 'Delete failed'));
      return;
    }
    await goto('/documents');
    toast.success(`Deleted “${title}”`, {
      duration: 10000,
      action: {
        label: 'Undo',
        onClick: async () => {
          try {
            await api.restoreDoc(deletedId);
            toast.success('Restored');
            await goto(`/documents/${encodeURIComponent(deletedId)}`);
          } catch (e) {
            toast.error(e.message || 'Restore failed');
          }
        },
      },
    });
  }

  function downloadPdf() { window.print(); }

  function fmtFull(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 19) + 'Z';
  }
  function slugOfEntity(eid) {
    if (!eid) return '';
    const colon = eid.indexOf(':');
    if (colon < 0) return eid;
    let s = eid.slice(colon + 1);
    if (s.startsWith('`') && s.endsWith('`')) s = s.slice(1, -1);
    return s;
  }
  function entityHref(e) {
    return `/entities/${encodeURIComponent(e.type)}/${encodeURIComponent(slugOfEntity(e.id))}`;
  }
</script>

<svelte:head>
  <title>{doc ? doc.title : 'Document'} — Rill</title>
</svelte:head>

<div class="page">
  <nav class="breadcrumb no-print"><a href="/documents"><ArrowLeft size={14} /> Documents</a></nav>

  {#if !isReady}
    <div class="state empty"><h2>Store unreachable</h2></div>
  {:else if loading}
    <div class="state loading"><div class="spinner"></div><p>Loading…</p></div>
  {:else if error}
    <div class="state error">
      <h2>{error}</h2>
      <p>Document id: <code>{id}</code></p>
      <button onclick={load}>Try again</button>
    </div>
  {:else if !doc}
    <div class="state empty"><h2>Document not found</h2></div>
  {:else if editing}
    <!-- Edit mode -->
    <div class="editor">
      <label class="fld">Title<input type="text" bind:value={eTitle} /></label>
      <div class="row">
        <label class="fld">Type<input type="text" bind:value={eType} placeholder="writeup" /></label>
        <label class="fld">Project<input type="text" bind:value={eProject} placeholder="(global)" /></label>
        <label class="fld">Source<input type="text" bind:value={eSource} placeholder="optional" /></label>
      </div>
      <label class="fld">Content (markdown)
        <textarea bind:value={eContent} rows="24" spellcheck="false"></textarea>
      </label>
      <div class="editor-actions">
        <button class="primary" onclick={save} disabled={saving}>{saving ? 'Saving…' : 'Save'}</button>
        <button onclick={() => (editing = false)} disabled={saving}>Cancel</button>
      </div>
    </div>
  {:else}
    <!-- Read mode -->
    <header class="head no-print">
      <div class="meta">
        <span class="doc-type">{doc.doc_type}</span>
        {#if doc.project}<span class="project">{doc.project}</span>{/if}
        {#if doc.source}<span class="source">src: {doc.source}</span>{/if}
        <span class="when">updated {fmtFull(doc.updated_at)}</span>
      </div>
      <div class="actions">
        <button onclick={startEdit}>Edit</button>
        <a class="btn" href={api.exportDocMarkdownUrl(doc.id)} download>Download .md</a>
        <button onclick={downloadPdf}>Download PDF</button>
        <button class="danger" onclick={remove}>Delete</button>
      </div>
    </header>

    <div class="entities no-print">
      <span class="lbl">About:</span>
      {#each doc.entities as e}
        <span class="echip">
          <a href={entityHref(e)}><span class="type-pill">{e.type}</span>{e.name}</a>
          <button class="unlink" title="Remove association" onclick={() => unlinkEntity(e)} disabled={linking}><X size={13} /></button>
        </span>
      {/each}
      {#if showLink}
        <span class="link-form">
          <select bind:value={linkType}>
            {#each ENTITY_TYPES as t}<option value={t}>{t}</option>{/each}
          </select>
          <input type="text" bind:value={linkName} placeholder="entity name" />
          <button class="primary" onclick={linkEntity} disabled={linking}>{linking ? '…' : 'Add'}</button>
          <button onclick={() => (showLink = false)}>Cancel</button>
        </span>
      {:else}
        <button class="add-link" onclick={() => (showLink = true)}><Plus size={13} /> Link entity</button>
      {/if}
    </div>
    {#if doc.entities.length === 0 && !showLink}
      <p class="no-assoc no-print">Not linked to any entity yet.</p>
    {/if}

    <article class="doc-print-area">
      <h1 class="doc-title">{doc.title}</h1>
      <div class="markdown-body">{@html rendered}</div>
    </article>
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 900px; }
  .breadcrumb { margin-bottom: 0.5rem; font-size: 0.85rem; }
  .breadcrumb a { color: var(--accent); text-decoration: none; }
  .breadcrumb a:hover { color: var(--accent-hi); }

  .head {
    display: flex; justify-content: space-between; align-items: center;
    gap: 1rem; flex-wrap: wrap; padding: 0.6rem 0.8rem; margin-bottom: 0.6rem;
    background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-sm);
  }
  .meta { display: flex; gap: 0.7rem; flex-wrap: wrap; align-items: center; color: var(--text-dim); font-size: 0.8rem; font-family: var(--font-mono); }
  .meta .project { color: var(--accent); }
  .doc-type {
    font-size: 0.7rem; padding: 0.1rem 0.4rem; border-radius: var(--radius-sm);
    background: oklch(0.30 0.08 280); color: oklch(0.88 0.05 280);
    text-transform: uppercase; letter-spacing: 0.05em;
  }
  .actions { display: flex; gap: 0.4rem; flex-wrap: wrap; }
  .actions button, .actions .btn {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.3rem 0.7rem; cursor: pointer; font-size: 0.82rem; text-decoration: none;
  }
  .actions button:hover, .actions .btn:hover { border-color: var(--accent); }
  .actions .danger:hover { color: var(--destructive); border-color: var(--destructive); }

  .entities {
    display: flex; gap: 0.4rem; flex-wrap: wrap; align-items: center;
    margin-bottom: 1rem; font-size: 0.82rem;
  }
  .entities .lbl { color: var(--text-faint); font-family: var(--font-mono); }
  .entities a {
    display: inline-flex; align-items: center; gap: 0.35rem;
    padding: 0.15rem 0.5rem; background: var(--surface);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    color: var(--text); text-decoration: none;
  }
  .entities a:hover { border-color: var(--accent); }
  .echip { display: inline-flex; align-items: center; }
  .echip .unlink {
    margin-left: -1px; padding: 0.15rem 0.4rem;
    background: var(--surface); color: var(--text-faint);
    border: 1px solid var(--border); border-left: none;
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0; cursor: pointer; line-height: 1;
  }
  .echip a { border-radius: var(--radius-sm) 0 0 var(--radius-sm); }
  .echip .unlink:hover { color: var(--destructive); border-color: var(--destructive); }
  .add-link {
    background: var(--surface-2); color: var(--text-dim);
    border: 1px dashed var(--border); border-radius: var(--radius-sm);
    padding: 0.15rem 0.5rem; cursor: pointer; font-size: 0.8rem;
  }
  .add-link:hover { border-color: var(--accent); color: var(--text); }
  .link-form { display: inline-flex; gap: 0.3rem; align-items: center; }
  .link-form select, .link-form input {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.35rem; font-size: 0.8rem;
  }
  .link-form button {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.2rem 0.5rem; cursor: pointer; font-size: 0.8rem;
  }
  .link-form .primary { background: var(--accent); color: var(--accent-fg, #fff); border-color: var(--accent); }
  .no-assoc { color: var(--text-faint); font-size: 0.8rem; margin: 0 0 1rem 0; }
  .type-pill {
    font-family: var(--font-mono); font-size: 0.68rem;
    padding: 0.05rem 0.3rem; border-radius: var(--radius-sm);
    background: var(--surface-2); color: var(--text-dim);
    text-transform: uppercase; letter-spacing: 0.05em;
  }

  .doc-title { font-size: 1.6rem; color: var(--text); margin: 0 0 1rem 0; line-height: 1.25; }

  /* Markdown body — rendered HTML from {@html}. Styles target generated tags. */
  .markdown-body { color: var(--text); line-height: 1.7; font-size: 0.95rem; }
  .markdown-body :global(h1),
  .markdown-body :global(h2),
  .markdown-body :global(h3),
  .markdown-body :global(h4) { color: var(--text); line-height: 1.3; margin: 1.4em 0 0.5em; }
  .markdown-body :global(h1) { font-size: 1.5rem; border-bottom: 1px solid var(--border); padding-bottom: 0.3rem; }
  .markdown-body :global(h2) { font-size: 1.25rem; border-bottom: 1px solid var(--border); padding-bottom: 0.2rem; }
  .markdown-body :global(h3) { font-size: 1.08rem; }
  .markdown-body :global(p) { margin: 0.7em 0; }
  .markdown-body :global(a) { color: var(--accent); }
  .markdown-body :global(ul),
  .markdown-body :global(ol) { padding-left: 1.5rem; margin: 0.6em 0; }
  .markdown-body :global(li) { margin: 0.25em 0; }
  .markdown-body :global(blockquote) {
    border-left: 3px solid var(--border); margin: 0.8em 0; padding: 0.2em 0 0.2em 1em;
    color: var(--text-dim);
  }
  .markdown-body :global(code) {
    font-family: var(--font-mono); font-size: 0.88em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
  .markdown-body :global(pre) {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.8rem; overflow-x: auto; margin: 0.8em 0;
  }
  .markdown-body :global(pre code) { background: none; padding: 0; }
  .markdown-body :global(table) { border-collapse: collapse; margin: 0.8em 0; width: 100%; }
  .markdown-body :global(th),
  .markdown-body :global(td) { border: 1px solid var(--border); padding: 0.35rem 0.6rem; text-align: left; }
  .markdown-body :global(th) { background: var(--surface); }
  .markdown-body :global(img) { max-width: 100%; }
  .markdown-body :global(hr) { border: none; border-top: 1px solid var(--border); margin: 1.5em 0; }

  /* Editor */
  .editor { display: flex; flex-direction: column; gap: 0.8rem; }
  .editor .row { display: flex; gap: 0.8rem; flex-wrap: wrap; }
  .fld { display: flex; flex-direction: column; gap: 0.3rem; color: var(--text-dim); font-size: 0.82rem; flex: 1; }
  .fld input, .fld textarea {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 0.5rem; font-size: 0.9rem;
  }
  .fld textarea { font-family: var(--font-mono); font-size: 0.85rem; line-height: 1.5; resize: vertical; }
  .editor-actions { display: flex; gap: 0.5rem; }
  .editor-actions button {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 1rem; cursor: pointer; font-size: 0.85rem;
  }
  .editor-actions .primary { background: var(--accent); color: var(--accent-fg, #fff); border-color: var(--accent); }
  .editor-actions button:disabled { opacity: 0.5; cursor: not-allowed; }

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
    border: 1px solid var(--border); border-radius: var(--radius-sm); cursor: pointer;
  }
  .spinner {
    width: 20px; height: 20px; border: 2px solid var(--border);
    border-top-color: var(--accent); border-radius: 50%;
    animation: spin 0.6s linear infinite; margin: 0 auto 0.6rem;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
