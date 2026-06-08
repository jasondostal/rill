<script>
  // Dashboard / color-system card: title + optional sub + right slot + body.
  // `flush` removes body padding (for edge-to-edge lists).
  let { title = '', sub = '', flush = false, right, children } = $props();
</script>

<section class="card">
  {#if title || right}
    <header class="card-head">
      <div class="card-head-l">
        {#if title}<h2 class="card-title">{title}</h2>{/if}
        {#if sub}<span class="card-sub mono">{sub}</span>{/if}
      </div>
      {#if right}<div class="card-head-r">{@render right()}</div>{/if}
    </header>
  {/if}
  <div class="card-body" class:flush>{@render children()}</div>
</section>

<style>
  .card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 15px 17px 0; }
  .card-head-l { display: flex; align-items: baseline; gap: 9px; flex-wrap: wrap; min-width: 0; }
  .card-head-r { flex: none; }
  .card-title { font-size: 14px; font-weight: 600; letter-spacing: -0.01em; white-space: nowrap; margin: 0; color: var(--text); }
  .card-sub { font-family: var(--font-mono); font-size: 11px; color: var(--text-faint); }
  .card-body { padding: 15px 17px 17px; }
  .card-body.flush { padding: 8px 0 0; }
</style>
