<script>
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import { toast } from 'svelte-sonner';
  import ArrowLeft from '@lucide/svelte/icons/arrow-left';

  // Optional prefills from the entity page: ?entity=type:slug&project=name
  const presetEntity = page.url.searchParams.get('entity') || '';
  const presetProject = page.url.searchParams.get('project') || '';

  let title = $state('');
  let docType = $state('writeup');
  let project = $state(presetProject || prefs.projects?.[0] || '');
  let source = $state('');
  let content = $state('');
  let saving = $state(false);

  function shortID(id) { return (id || '').replace(/^document:/, ''); }

  async function create() {
    if (!title.trim()) { toast.error('Title is required'); return; }
    saving = true;
    try {
      const doc = await api.createDoc({
        title,
        content,
        doc_type: docType || 'writeup',
        project: project || undefined,
        source: source || undefined,
      });
      // Link to the entity it was created "about", if any. The full record id
      // is passed as the name; the backend resolves the record-id form.
      if (presetEntity) {
        try { await api.associateDoc(doc.id, presetEntity, ''); }
        catch (e) { toast.error('Created, but linking failed: ' + (e.message || '')); }
      }
      toast.success('Document created');
      await goto(`/documents/${encodeURIComponent(shortID(doc.id))}`);
    } catch (e) {
      toast.error(e.message || 'Create failed');
      saving = false;
    }
  }
</script>

<svelte:head><title>New document — Rill</title></svelte:head>

<div class="page">
  <nav class="breadcrumb"><a href="/documents"><ArrowLeft size={14} /> Documents</a></nav>
  <h1>New document</h1>
  {#if presetEntity}
    <p class="link-note">Will be linked to <code>{presetEntity}</code> on create.</p>
  {/if}

  <div class="editor">
    <label class="fld">Title<input type="text" bind:value={title} placeholder="e.g. Rill Architecture Primer" /></label>
    <div class="row">
      <label class="fld">Type<input type="text" bind:value={docType} placeholder="writeup" /></label>
      <label class="fld">Project<input type="text" bind:value={project} placeholder="(global)" /></label>
      <label class="fld">Source<input type="text" bind:value={source} placeholder="optional" /></label>
    </div>
    <label class="fld">Content (markdown)
      <textarea bind:value={content} rows="22" spellcheck="false" placeholder="# Heading&#10;&#10;Write markdown here…"></textarea>
    </label>
    <div class="editor-actions">
      <button class="primary" onclick={create} disabled={saving}>{saving ? 'Creating…' : 'Create'}</button>
      <a class="cancel" href="/documents">Cancel</a>
    </div>
    <p class="hint">Associate with entities from the document page after creating (or via <code>doc_put</code>).</p>
  </div>
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 900px; }
  .breadcrumb { margin-bottom: 0.5rem; font-size: 0.85rem; }
  .breadcrumb a { color: var(--accent); text-decoration: none; }
  .breadcrumb a:hover { color: var(--accent-hi); }
  h1 { font-size: 1.4rem; color: var(--text); margin: 0 0 1rem 0; }

  .editor { display: flex; flex-direction: column; gap: 0.8rem; }
  .editor .row { display: flex; gap: 0.8rem; flex-wrap: wrap; }
  .fld { display: flex; flex-direction: column; gap: 0.3rem; color: var(--text-dim); font-size: 0.82rem; flex: 1; }
  .fld input, .fld textarea {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 0.5rem; font-size: 0.9rem;
  }
  .fld textarea { font-family: var(--font-mono); font-size: 0.85rem; line-height: 1.5; resize: vertical; }
  .editor-actions { display: flex; gap: 0.5rem; align-items: center; }
  .editor-actions button, .editor-actions .cancel {
    background: var(--surface-2); color: var(--text);
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 1rem; cursor: pointer; font-size: 0.85rem; text-decoration: none;
  }
  .editor-actions .primary { background: var(--accent); color: var(--accent-fg, #fff); border-color: var(--accent); }
  .editor-actions button:disabled { opacity: 0.5; cursor: not-allowed; }
  .hint { color: var(--text-faint); font-size: 0.8rem; }
  .link-note { color: var(--text-dim); font-size: 0.85rem; margin: 0 0 1rem 0; }
  code { font-family: var(--font-mono); font-size: 0.85em; background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm); }
</style>
