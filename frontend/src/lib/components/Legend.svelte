<script>
  // items: [{ label, color, value? }]
  let { items = [], columns = 2 } = $props();
</script>

<ul class="legend" style="grid-template-columns:repeat({columns},minmax(0,1fr))">
  {#each items as it (it.label)}
    <li>
      <i style="background:{it.color}"></i>
      <span class="legend-label">{it.label}</span>
      {#if it.value != null}<span class="legend-val mono">{it.value}</span>{/if}
    </li>
  {/each}
</ul>

<style>
  .legend { list-style: none; display: grid; gap: 4px 14px; margin: 0; padding: 0; }
  .legend li { display: flex; align-items: center; gap: 6px; font-size: 11.5px; }
  .legend i { width: 9px; height: 9px; border-radius: 2px; flex: none; }
  .legend-label { color: var(--text-dim); }
  .legend-val { font-family: var(--font-mono); color: var(--text-faint); margin-left: auto; }
</style>
