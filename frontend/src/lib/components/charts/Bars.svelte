<script>
  // items: [{ label, value, color }]
  let { items = [], max = null } = $props();
  let m = $derived(max || Math.max(...items.map((i) => i.value), 1));
</script>

<div class="bars">
  {#each items as it (it.label)}
    <div class="bar-row">
      <div class="bar-label">{it.label}</div>
      <div class="bar-track"><div class="bar-fill" style="width:{(it.value / m) * 100}%;background:{it.color}"></div></div>
      <div class="bar-val mono">{it.value}</div>
    </div>
  {/each}
</div>

<style>
  .bars { display: flex; flex-direction: column; gap: 11px; }
  .bar-row { display: grid; grid-template-columns: 84px 1fr 38px; align-items: center; gap: 10px; }
  .bar-label { font-size: 12.5px; color: var(--text-dim); font-family: var(--font-mono); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .bar-track { height: 9px; background: var(--surface-3); border-radius: 20px; overflow: hidden; }
  .bar-fill { height: 100%; border-radius: 20px; }
  .bar-val { font-family: var(--font-mono); font-size: 12px; color: var(--text); text-align: right; }
</style>
