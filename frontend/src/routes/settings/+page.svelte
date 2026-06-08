<script>
  import { api } from '$lib/api.js';
  import { prefs } from '$lib/prefs.js';
  import DenseToggle from '$lib/DenseToggle.svelte';
  import PageHeader from '$lib/PageHeader.svelte';
  import { onMount } from 'svelte';
  import { toast } from 'svelte-sonner';

  let tokens = $state([]);
  let loading = $state(true);
  let error = $state('');
  let newTokenName = $state('');
  let newTokenScope = $state('read,write'); // tier: read | read,write | read,write,admin
  let newToken = $state(null);
  let showTokenWarning = $state(false);
  let dense = $state(prefs.dense);
  let copied = $state(false);
  // Hide the freshly-minted PAT behind a reveal toggle so a shoulder-surfer
  // can't snapshot the value off a forgotten tab. Also auto-clear the value
  // from component state 60s after creation regardless.
  let revealed = $state(false);
  let autoClearTimer = null;

  // ---- runtime configuration (settings) ----
  let settings = $state([]);
  let drafts = $state({});        // key -> editable draft value
  let settingsError = $state('');
  let settingsLoading = $state(true);
  let savingKey = $state('');
  let personOptions = $state([]); // [{value: record id, label: name}] for entity:person dropdowns

  async function loadPersonOptions() {
    try {
      const people = await api.listEntities({ type: 'person', sort: 'name' });
      personOptions = (people || []).map((e) => ({ value: e.id, label: e.name }));
    } catch { personOptions = []; }
  }

  let groups = $derived.by(() => {
    const g = {};
    for (const s of settings) (g[s.group] ??= []).push(s);
    return Object.entries(g);
  });

  async function loadSettings() {
    settingsError = '';
    try {
      settings = await api.getSettings();
      const d = {};
      let needPeople = false;
      for (const s of settings) {
        if (s.editable && !s.locked) d[s.key] = s.value ?? '';
        if (s.options_source === 'entity:person') needPeople = true;
      }
      drafts = d;
      if (needPeople) loadPersonOptions();
    } catch (e) {
      settingsError = e?.message || 'Failed to load settings';
      settings = [];
    } finally {
      settingsLoading = false;
    }
  }

  const isDirty = (s) => s.editable && !s.locked && String(drafts[s.key] ?? '') !== String(s.value ?? '');

  async function saveSetting(s) {
    savingKey = s.key;
    try {
      const updated = await api.updateSetting(s.key, String(drafts[s.key] ?? ''));
      settings = settings.map((x) => (x.key === s.key ? updated : x));
      drafts[s.key] = updated.value ?? '';
      toast.success('Saved', { description: `${s.label} — ${s.hot ? 'applied live' : 'applies on restart'}` });
    } catch (e) {
      toast.error(`Couldn't save ${s.label}`, { description: e?.message || '' });
    }
    savingKey = '';
  }

  function badge(s) {
    if (s.locked) return { text: 'set via env', cls: 'env' };
    if (s.source === 'db') return { text: 'custom', cls: 'db' };
    return { text: 'default', cls: 'default' };
  }

  async function copyToken() {
    if (newToken?.token) {
      try {
        await navigator.clipboard.writeText(newToken.token);
        copied = true;
        setTimeout(() => copied = false, 2000);
      } catch {}
    }
  }

  function clearNewToken() {
    newToken = null;
    revealed = false;
    if (autoClearTimer) {
      clearTimeout(autoClearTimer);
      autoClearTimer = null;
    }
  }

  async function loadTokens() {
    error = '';
    try {
      tokens = await api.listTokens();
    } catch(e) {
      error = 'Failed to load tokens: ' + (e.message || e);
      tokens = [];
    }
  }

  async function createToken() {
    if (!newTokenName.trim()) return;
    try {
      newToken = await api.createToken(newTokenName.trim(), newTokenScope.split(','));
      newTokenName = '';
      revealed = false;
      // Auto-clear after 60s so the secret doesn't linger in the DOM on
      // an idle tab. The user has the copy-to-clipboard flow before then.
      if (autoClearTimer) clearTimeout(autoClearTimer);
      autoClearTimer = setTimeout(clearNewToken, 60_000);
      loadTokens();
      toast.success('Token created', { description: newToken.name });
    } catch(e) {
      toast.error('Failed to create token', { description: e.message });
    }
  }

  async function revokeToken(id, name) {
    loading = true;
    try {
      await api.revokeToken(id);
      await loadTokens();
      toast.success('Token revoked', { description: name });
    } catch(e) {
      toast.error('Failed to revoke token', { description: e.message });
    }
    loading = false;
  }

  function formatDate(d) {
    if (!d) return '';
    try { return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }); }
    catch { return d; }
  }

  onMount(async () => {
    try {
      await loadTokens();
    } finally {
      loading = false;
    }
    loadSettings();
  });
</script>

<svelte:head><title>Settings — Rill</title></svelte:head>

<div class="settings-page">
  <PageHeader section="settings" title="Settings">
    <DenseToggle dense={dense} ontoggle={() => { dense = !dense; prefs.dense = dense; }} />
  </PageHeader>

  <!-- PATs -->
  <div class="card section">
    <h2>Personal Access Tokens</h2>
    <p class="desc">Tokens authenticate CLI tools, MCP agents, and API clients. Keep them secret.</p>

    <div class="create-row">
      <input type="text" bind:value={newTokenName} placeholder="Token name…" class="field" />
      <select bind:value={newTokenScope} class="field scope-select" title="Token permissions">
        <option value="read">Read-only</option>
        <option value="read,write">Read / write</option>
        <option value="read,write,admin">Admin</option>
      </select>
      <button class="btn btn-primary" onclick={createToken} disabled={!newTokenName.trim()}>Generate</button>
    </div>
    <p class="scope-hint desc">
      <b>Read-only</b>: recall, orient, browse — can't mutate. <b>Read/write</b>: also remember, edit,
      forget, merge. <b>Admin</b>: also token + settings management.
    </p>

    {#if newToken}
      <div class="token-reveal">
        <p class="token-warn">Copy this token now — it won't be shown again. (Auto-clears in 60s.)</p>
        <div class="token-value-row">
          <code class="token-value">{revealed ? newToken.token : '•'.repeat(Math.min(newToken.token.length, 48))}</code>
          <button class="btn copy-btn" onclick={() => revealed = !revealed}>
            {revealed ? 'Hide' : 'Reveal'}
          </button>
          <button class="btn copy-btn" onclick={copyToken}>
            {#if copied}Copied{:else}Copy{/if}
          </button>
        </div>
        <p class="token-usage">Use: Authorization: Bearer &lt;token&gt;</p>
        <button class="btn" onclick={clearNewToken}>Dismiss</button>
      </div>
    {/if}

    {#if loading}
      <div class="loading"><div class="spinner"></div></div>
    {:else if error}
      <div class="error-banner">{error} <button onclick={loadTokens}>Retry</button></div>
    {:else if tokens.length > 0}
      <div class="token-list">
        {#each tokens as t}
          <div class="token-row">
            <div class="token-info">
              <span class="token-name">
                {t.name}
                {#each (t.scopes || []) as sc}<span class="scope-tag" class:admin={sc === 'admin'}>{sc}</span>{/each}
              </span>
              <span class="token-date">Created {formatDate(t.created_at)}</span>
            </div>
            <button class="btn btn-danger" onclick={() => revokeToken(t.id, t.name)}>Revoke</button>
          </div>
        {/each}
      </div>
    {:else}
      <p class="empty">No tokens yet. Generate one above.</p>
    {/if}
  </div>

  <!-- Preferences -->
  <div class="card section">
    <h2>Preferences</h2>
    <div class="pref-row">
      <span>Default view</span>
      <select bind:value={prefs.dense} onchange={() => { dense = prefs.dense; }} class="filter-select">
        <option value={true}>Dense</option>
        <option value={false}>Card</option>
      </select>
    </div>
    <div class="pref-row">
      <span>Selected projects</span>
      <span class="muted">{prefs.projects.length ? prefs.projects.join(', ') : 'All projects'}</span>
    </div>
  </div>

  <!-- Configuration -->
  {#if settingsLoading}
    <div class="card section"><div class="loading"><div class="spinner"></div></div></div>
  {:else if settingsError}
    <div class="card section">
      <h2>Configuration</h2>
      <div class="error-banner">{settingsError} <button onclick={loadSettings}>Retry</button></div>
    </div>
  {:else}
    <p class="config-intro desc">
      Precedence is <b>env&nbsp;&gt;&nbsp;saved&nbsp;&gt;&nbsp;default</b>. A value pinned by an
      environment variable is shown read-only. Editable values persist in the database;
      hot ones apply immediately, others on restart. Secrets are never displayed.
    </p>
    {#each groups as [group, items] (group)}
      <div class="card section">
        <h2>{group}</h2>
        <div class="cfg-list">
          {#each items as s (s.key)}
            {@const b = badge(s)}
            <div class="cfg-row">
              <div class="cfg-meta">
                <div class="cfg-label-row">
                  <span class="cfg-label">{s.label}</span>
                  <span class="cfg-badge {b.cls}">{b.text}</span>
                  {#if s.editable && !s.locked && !s.hot}<span class="cfg-badge restart">restart</span>{/if}
                  {#if isDirty(s)}<span class="cfg-dot" title="Unsaved change"></span>{/if}
                </div>
                {#if s.desc}<p class="cfg-desc">{s.desc}</p>{/if}
                <code class="cfg-key mono">{s.env || s.key}</code>
              </div>

              <div class="cfg-control">
                {#if s.secret}
                  <span class="cfg-secret {s.configured ? 'on' : 'off'}">{s.configured ? '✓ configured' : 'not set'}</span>
                {:else if !s.editable || s.locked}
                  <span class="cfg-readonly mono">{s.value === '' ? '—' : s.value}{s.unit ? ' ' + s.unit : ''}</span>
                {:else}
                  {#if s.options_source === 'entity:person'}
                    <select class="field cfg-input" bind:value={drafts[s.key]}>
                      <option value="">— none —</option>
                      {#each personOptions as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
                    </select>
                  {:else if s.kind === 'enum'}
                    <select class="field cfg-input" bind:value={drafts[s.key]}>
                      {#each s.options as opt}<option value={opt}>{opt}</option>{/each}
                    </select>
                  {:else if s.kind === 'bool'}
                    <select class="field cfg-input" bind:value={drafts[s.key]}>
                      <option value="true">true</option>
                      <option value="false">false</option>
                    </select>
                  {:else if s.kind === 'int'}
                    <input class="field cfg-input" type="number" min={s.min} max={s.max} bind:value={drafts[s.key]} />
                  {:else}
                    <input class="field cfg-input" type="text" bind:value={drafts[s.key]} placeholder={s.default || '—'} />
                  {/if}
                  <button class="btn btn-primary cfg-save" disabled={!isDirty(s) || savingKey === s.key}
                    onclick={() => saveSetting(s)}>
                    {savingKey === s.key ? '…' : 'Save'}
                  </button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/each}
  {/if}
</div>

<style>
  .settings-page { max-width: 800px; }
  .section { margin-bottom: 1.5rem; }
  .desc { color: var(--text-dim); font-size: 0.85rem; margin-bottom: 1rem; }

  .create-row { display: flex; gap: 0.5rem; margin-bottom: 0.4rem; }
  .scope-select { flex: 0 0 auto; min-width: 130px; cursor: pointer; }
  .scope-hint { margin-bottom: 1rem; font-size: 0.78rem; }
  .scope-hint b { color: var(--text); }
  .scope-tag {
    font-family: var(--font-mono); font-size: 0.62rem; text-transform: uppercase; letter-spacing: 0.04em;
    padding: 0.05rem 0.4rem; margin-left: 0.35rem; border-radius: 20px;
    background: var(--surface-2); border: 1px solid var(--border); color: var(--text-faint);
  }
  .scope-tag.admin { color: var(--warning); border-color: color-mix(in oklab, var(--warning) 40%, transparent); }
  .field {
    padding: 0.5rem 0.75rem; background: var(--surface-2); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text); font-size: 0.85rem; flex: 1;
  }

  /* ---- configuration section ---- */
  .config-intro { max-width: 800px; margin: -0.25rem 0 1rem; line-height: 1.5; }
  .config-intro b { color: var(--text); }
  .cfg-list { display: flex; flex-direction: column; }
  .cfg-row {
    display: flex; align-items: flex-start; justify-content: space-between; gap: 1.5rem;
    padding: 0.85rem 0; border-top: 1px solid var(--border);
  }
  .cfg-row:first-child { border-top: none; }
  .cfg-meta { min-width: 0; flex: 1; }
  .cfg-label-row { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .cfg-label { font-size: 0.9rem; font-weight: 500; color: var(--text); }
  .cfg-desc { font-size: 0.8rem; color: var(--text-dim); margin: 0.25rem 0 0.3rem; line-height: 1.45; }
  .cfg-key { font-size: 0.72rem; color: var(--text-faint); }
  .cfg-badge {
    font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.05em; font-weight: 600;
    padding: 0.1rem 0.45rem; border-radius: 20px; font-family: var(--font-mono);
    border: 1px solid var(--border); color: var(--text-faint);
  }
  .cfg-badge.env { color: var(--warning); border-color: color-mix(in oklab, var(--warning) 40%, transparent); }
  .cfg-badge.db { color: var(--accent); border-color: var(--accent-line); }
  .cfg-badge.restart { color: var(--text-dim); }
  .cfg-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--accent); flex: none; }
  .cfg-control { display: flex; align-items: center; gap: 0.5rem; flex: none; min-width: 220px; justify-content: flex-end; }
  .cfg-input { flex: 0 1 150px; min-width: 90px; }
  .cfg-save { flex: none; padding: 0.45rem 0.85rem; font-size: 0.8rem; }
  .cfg-save:disabled { opacity: 0.4; cursor: not-allowed; }
  .cfg-readonly { font-size: 0.82rem; color: var(--text-dim); text-align: right; word-break: break-all; }
  .cfg-secret { font-size: 0.78rem; font-family: var(--font-mono); }
  .cfg-secret.on { color: var(--success); }
  .cfg-secret.off { color: var(--text-faint); }
  @media (max-width: 640px) {
    .cfg-row { flex-direction: column; gap: 0.6rem; }
    .cfg-control { min-width: 0; width: 100%; justify-content: flex-start; }
  }

  .token-reveal {
    background: var(--warning-bg); border: 1px solid color-mix(in oklab, var(--warning) 40%, transparent);
    border-radius: var(--radius); padding: 1rem; margin-bottom: 1rem;
  }
  .token-warn { font-size: 0.85rem; color: var(--warning); font-weight: 600; margin-bottom: 0.5rem; }
  .token-value {
    display: block; padding: 0.5rem; background: var(--bg); border-radius: 4px;
    font-family: var(--font-mono); font-size: 0.8rem; word-break: break-all;
    color: var(--warning); flex: 1;
  }
  .token-value-row {
    display: flex; gap: 0.5rem; align-items: flex-start; margin-bottom: 0.5rem;
  }
  .copy-btn {
    flex-shrink: 0; padding: 0.4rem 0.75rem; font-size: 0.8rem;
  }
  .token-usage { font-size: 0.8rem; color: var(--text-dim); margin-bottom: 0.75rem; }

  .token-list { display: flex; flex-direction: column; gap: 0.5rem; margin-top: 1rem; }
  .token-row {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.65rem 0.75rem; background: var(--surface-2); border-radius: var(--radius);
  }
  .token-info { display: flex; flex-direction: column; gap: 0.15rem; }
  .token-name { font-weight: 600; font-size: 0.9rem; }
  .token-date { font-size: 0.75rem; color: var(--text-dim); }
  .btn-danger { background: oklch(0.18 0.06 20); color: oklch(0.75 0.1 20); border-color: oklch(0.3 0.08 20); }
  .btn-danger:hover { background: oklch(0.22 0.08 20); }

  .pref-row {
    display: flex; justify-content: space-between; align-items: center;
    padding: 0.5rem 0; border-bottom: 1px solid var(--border);
  }
  .pref-row:last-child { border-bottom: none; }

  .env-row {
    display: flex; align-items: center; gap: 1rem; padding: 0.5rem 0;
    border-bottom: 1px solid var(--border); font-size: 0.85rem;
  }
  .env-row:last-child { border-bottom: none; }
  .env-label {
    font-family: var(--font-mono); font-size: 0.8rem; color: var(--accent); min-width: 130px;
  }
  .env-value { color: var(--text); min-width: 120px; }
  .env-desc { color: var(--text-dim); flex: 1; }

  .filter-select {
    padding: 0.35rem 0.5rem; background: var(--surface-2); border: 1px solid var(--border);
    border-radius: var(--radius); color: var(--text); font-size: 0.85rem;
  }

  .loading { display: flex; justify-content: center; padding: 2rem; }
  .spinner {
    width: 24px; height: 24px; border: 2px solid var(--border);
    border-top: 2px solid var(--accent); border-radius: 50%; animation: spin 0.6s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  .empty { color: var(--text-dim); font-size: 0.85rem; padding: 1rem 0; }
  .error-banner {
    text-align: center;
    padding: 1rem;
    color: var(--destructive, oklch(0.65 0.2 25));
  }
  .error-banner button {
    margin-left: 0.5rem;
    padding: 0.35rem 0.75rem;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    color: var(--text);
    cursor: pointer;
  }
</style>
