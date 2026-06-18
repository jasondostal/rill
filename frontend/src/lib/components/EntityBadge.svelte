<script>
  // Colored badge for an entity type. Color flows from the --e-<type> tokens.
  let { type } = $props();
</script>

<span class="entity-badge" style="--kc:var(--e-{type});--kcb:var(--e-{type}-bg)">
  <i class="kind-dot"></i>{type}
</span>

<style>
  .entity-badge {
    display: inline-flex; align-items: center; gap: 6px;
    background: var(--kcb); color: var(--kc);
    border: 1px solid color-mix(in oklab, var(--kc) 35%, transparent);
    border-radius: 6px; padding: 2px 8px; font-size: 12px; font-weight: 500;
    font-family: var(--font-mono); white-space: nowrap; line-height: 1.5;
  }
  .kind-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--kc); flex: none; }
</style>
