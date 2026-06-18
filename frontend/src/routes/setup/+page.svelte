<script>
  import { api } from '$lib/api.js';
  import { goto } from '$app/navigation';

  let username = $state('');
  let password = $state('');
  let confirm = $state('');
  let error = $state('');
  let loading = $state(false);

  async function submit(e) {
    e.preventDefault();
    if (password !== confirm) {
      error = 'Passwords do not match';
      return;
    }
    if (password.length < 8) {
      error = 'Password must be at least 8 characters';
      return;
    }
    loading = true;
    error = '';
    try {
      await api.setup(username, password, confirm);
      goto('/login');
    } catch (e) {
      error = e.message || 'Setup failed';
    } finally {
      loading = false;
    }
  }
</script>

<svelte:head><title>Setup — Rill</title></svelte:head>

<div class="setup-page">
  <form class="setup-card" onsubmit={submit}>
    <h1>Welcome to Rill</h1>
    <p class="subtitle">Create your admin account</p>

    <label>
      <span>Username</span>
      <input type="text" bind:value={username} autocomplete="username" required autofocus />
    </label>

    <label>
      <span>Password</span>
      <input type="password" bind:value={password} autocomplete="new-password" required />
    </label>

    <label>
      <span>Confirm password</span>
      <input type="password" bind:value={confirm} autocomplete="new-password" required />
    </label>

    {#if error}
      <div class="error">{error}</div>
    {/if}

    <button type="submit" disabled={loading}>
      {loading ? 'Creating…' : 'Create account'}
    </button>
  </form>
</div>

<style>
  .setup-page {
    display: flex; align-items: center; justify-content: center;
    min-height: 100vh; background: var(--bg);
  }
  .setup-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 2rem; min-width: 340px;
    display: flex; flex-direction: column; gap: 1rem;
  }
  h1 { color: var(--text); margin: 0; font-size: 1.5rem; }
  .subtitle { color: var(--text-dim); margin: 0; font-size: 0.85rem; }
  label { display: flex; flex-direction: column; gap: 0.25rem; }
  label span { color: var(--text-dim); font-size: 0.8rem; }
  input {
    background: var(--bg); border: 1px solid var(--border);
    color: var(--text); padding: 0.5rem 0.75rem; border-radius: var(--radius);
  }
  .error {
    color: var(--destructive); background: var(--destructive-bg);
    padding: 0.5rem 0.75rem; border-radius: var(--radius); font-size: 0.85rem;
  }
  button {
    background: var(--accent); color: var(--bg);
    padding: 0.6rem 1rem; border: none; border-radius: var(--radius);
    cursor: pointer; font-weight: 600;
  }
  button:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
