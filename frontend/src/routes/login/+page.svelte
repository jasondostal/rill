<script>
  import { api } from '$lib/api.js';
  import { goto } from '$app/navigation';

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let loading = $state(false);
  let oidcEnabled = $state(false);
  let proxyEnabled = $state(false);
  let authStatusLoading = $state(true);

  $effect(() => {
    // If we're already authed (proxy header, valid session, bearer in cookie),
    // skip the login page so users already authed by an upstream reverse
    // proxy never see a form they don't need.
    api.me().then(u => {
      if (u && u.name) {
        goto('/');
        return;
      }
      return api.authStatus().then(s => {
        oidcEnabled = s.oidc_enabled;
        proxyEnabled = s.proxy_enabled;
        authStatusLoading = false;
      });
    }).catch(() => {
      authStatusLoading = false;
    });
  });

  async function submit(e) {
    e.preventDefault();
    loading = true;
    error = '';
    try {
      await api.login(username, password);
      goto('/');
    } catch (e) {
      error = 'Invalid username or password';
      password = '';
    } finally {
      loading = false;
    }
  }

  function oidcLogin() {
    window.location.href = '/api/auth/oidc/login';
  }
</script>

<svelte:head><title>Login — Rill</title></svelte:head>

<div class="login-page">
  <form class="login-card" onsubmit={submit}>
    <h1>Rill</h1>
    <p class="subtitle">Sign in to continue</p>

    {#if !authStatusLoading && oidcEnabled}
      <button type="button" class="sso-button" onclick={oidcLogin}>
        Sign in with SSO
      </button>
      <div class="divider">
        <span>or</span>
      </div>
    {/if}

    {#if !authStatusLoading && proxyEnabled && !oidcEnabled}
      <div class="proxy-note">
        Reverse-proxy auth is configured. If you're going through your
        SSO proxy, you should be signed in automatically — try refreshing.
        Use the form below for direct/console access.
      </div>
    {/if}

    <label>
      <span>Username</span>
      <input type="text" bind:value={username} autocomplete="username" required autofocus={!oidcEnabled} />
    </label>

    <label>
      <span>Password</span>
      <input type="password" bind:value={password} autocomplete="current-password" required />
    </label>

    {#if error}
      <div class="error">{error}</div>
    {/if}

    <button type="submit" disabled={loading}>
      {loading ? 'Signing in…' : 'Sign in'}
    </button>
  </form>
</div>

<style>
  .login-page {
    display: flex; align-items: center; justify-content: center;
    min-height: 100vh; background: var(--bg);
  }
  .login-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 2rem; min-width: 320px;
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
  .sso-button {
    background: var(--bg); color: var(--text);
    border: 1px solid var(--accent);
  }
  .proxy-note {
    background: var(--bg); border: 1px solid var(--border);
    color: var(--text-dim); font-size: 0.8rem; line-height: 1.4;
    padding: 0.6rem 0.75rem; border-radius: var(--radius);
  }
  .divider {
    display: flex; align-items: center; text-align: center; color: var(--text-dim);
    font-size: 0.8rem;
  }
  .divider::before, .divider::after {
    content: ''; flex: 1; border-bottom: 1px solid var(--border);
  }
  .divider span { padding: 0 0.5rem; }
</style>
