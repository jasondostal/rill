<script>
  import { onMount } from 'svelte';
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import { toast } from 'svelte-sonner';
  import PageHeader from '$lib/PageHeader.svelte';
  import Copy from '@lucide/svelte/icons/copy';
  import RotateCw from '@lucide/svelte/icons/rotate-cw';
  import Check from '@lucide/svelte/icons/check';
  import Zap from '@lucide/svelte/icons/zap';
  import Star from '@lucide/svelte/icons/star';

  let project = $state(prefs.projects?.[0] || '');
  let result = $state(null);
  let loading = $state(true);
  let error = $state('');
  let regenerating = $state(false);

  async function load() {
    loading = true;
    error = '';
    try {
      result = await api.orient({ project: project || undefined });
    } catch (e) {
      error = e.message || 'Failed to load orient';
      result = null;
    }
    loading = false;
  }

  async function regen() {
    regenerating = true;
    error = '';
    try {
      result = await api.orientRegen(project || '');
      toast.success('Re-rendered from current graph');
    } catch (e) {
      error = e.message || 'Regen failed';
    }
    regenerating = false;
  }

  async function copyBlob() {
    if (!result?.rendered) return;
    try {
      await navigator.clipboard.writeText(result.rendered);
      toast.success('Copied to clipboard');
    } catch (e) {
      toast.error('Clipboard not available');
    }
  }

  onMount(() => {
    load();
    const handler = (e) => {
      const sel = e.detail?.projects || [];
      project = sel.length === 1 ? sel[0] : '';
      load();
    };
    window.addEventListener('project-changed', handler);
    return () => window.removeEventListener('project-changed', handler);
  });

  function fmtTime(iso) {
    if (!iso) return '';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    return d.toISOString().replace('T', ' ').slice(0, 19) + 'Z';
  }
</script>

<svelte:head>
  <title>Orient — Rill</title>
</svelte:head>

<div class="page">
  <PageHeader section="orient" title="Orient">
    <div class="actions">
      <button class="ghost" onclick={copyBlob} disabled={!result?.rendered}><Copy size={15} /> Copy</button>
      <button class="primary" onclick={regen} disabled={regenerating}>
        <RotateCw size={15} /> {regenerating ? 'Regen…' : 'Force regen'}
      </button>
    </div>
  </PageHeader>
  <p class="muted-small intro">
    The blob agents see at session start. Renders Rules, Identity, Active projects, Active edges, Recent intentional memories. Pulled from cache; force re-render to pick up new entities / edges / hand_notes.
  </p>

  {#if loading}
    <div class="state loading">
      <div class="spinner"></div>
      <p>Loading orient blob…</p>
    </div>
  {:else if error}
    <div class="state error">
      <h2>Failed to load</h2>
      <p>{error}</p>
      <button onclick={load}>Try again</button>
    </div>
  {:else if !result || !result.rendered}
    <div class="state empty">
      <h2>Empty orient</h2>
      <p>
        Nothing to render yet — no promoted entities, no rules, no recent memories. Promote an entity (<Star size={12} fill="currentColor" />) on its detail page, or write some memories with <code>rill remember</code>, then come back.
      </p>
    </div>
  {:else}
    <div class="meta-bar">
      <span class="scope">scope: <strong>{result.scope}</strong></span>
      <span class="cache" class:fresh={!result.from_cache}>
        {#if result.from_cache}<Check size={13} /> from cache{:else}<Zap size={13} /> freshly rendered{/if}
      </span>
      <span class="when">rendered {fmtTime(result.rendered_at)}</span>
      <span class="chars muted-small">{result.rendered.length} chars</span>
    </div>
    <pre class="blob">{result.rendered}</pre>
  {/if}
</div>

<style>
  .page { padding: 1rem 1.5rem; max-width: 1100px; }
  .muted-small { color: var(--text-faint); font-size: 0.78rem; max-width: 60ch; margin: 0; line-height: 1.5; }
  .intro { margin: -0.2rem 0 0.8rem 0; }

  .actions { display: flex; gap: 0.5rem; flex-shrink: 0; }
  .actions button {
    border: 1px solid var(--border); border-radius: var(--radius-sm);
    padding: 0.4rem 0.8rem; cursor: pointer; font-size: 0.85rem;
  }
  .actions button.primary { background: var(--accent); color: white; border-color: var(--accent); }
  .actions button.primary:hover:not(:disabled) { background: var(--accent-hi); border-color: var(--accent-hi); }
  .actions button.ghost { background: var(--surface-2); color: var(--text-dim); }
  .actions button.ghost:hover:not(:disabled) { color: var(--text); border-color: var(--text-faint); }
  .actions button:disabled { opacity: 0.5; cursor: not-allowed; }

  .meta-bar {
    display: flex; gap: 1rem; align-items: baseline; flex-wrap: wrap;
    padding: 0.4rem 0.6rem; margin-bottom: 0.6rem;
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-dim); font-size: 0.78rem; font-family: var(--font-mono);
  }
  .cache { color: var(--text-faint); }
  .cache.fresh { color: var(--success); }
  .chars { margin-left: auto; }

  .blob {
    background: var(--surface); border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm); padding: 1rem 1.2rem;
    font-family: var(--font-mono); font-size: 0.88rem; line-height: 1.5;
    color: var(--text); white-space: pre-wrap; word-wrap: break-word;
    margin: 0;
    max-height: 75vh; overflow-y: auto;
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

  code {
    font-family: var(--font-mono); font-size: 0.85em;
    background: var(--surface-2); padding: 0.1rem 0.3rem; border-radius: var(--radius-sm);
  }
</style>
