<script>
  // Colored badge for a memory kind. Color flows from the --k-<kind> tokens so
  // it retints with the theme. `small` is the dense list/legend variant.
  let { kind, small = false } = $props();
</script>

<span class="kind-badge" class:sm={small}
  style="--kc:var(--k-{kind});--kcb:var(--k-{kind}-bg)">
  <i class="kind-dot"></i>{kind}
</span>

<style>
  .kind-badge {
    display: inline-flex; align-items: center; gap: 6px;
    background: var(--kcb); color: var(--kc);
    border: 1px solid color-mix(in oklab, var(--kc) 35%, transparent);
    border-radius: 6px; padding: 2px 8px; font-size: 12px; font-weight: 500;
    font-family: var(--font-mono); white-space: nowrap; line-height: 1.5;
  }
  .kind-badge.sm { font-size: 11px; padding: 1px 7px; }
  .kind-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--kc); flex: none; }
</style>
