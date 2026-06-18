<script>
  // data: [{ date: ISO string, count }] — GitHub-style activity grid, columns of 7.
  // colorVar: the CSS var the filled cells derive from (default --success).
  let { data = [], colorVar = '--success' } = $props();
  const op = [0, 0.28, 0.5, 0.74, 1];

  let max = $derived(Math.max(...data.map((d) => d.count), 1));
  let cols = $derived.by(() => {
    const out = [];
    for (let i = 0; i < data.length; i += 7) out.push(data.slice(i, i + 7));
    return out;
  });
  const level = (c) => (c === 0 ? 0 : Math.min(4, Math.ceil((c / max) * 4)));
  function cellColor(c) {
    return c === 0 ? 'var(--surface-3)' : `oklch(from var(${colorVar}) l c h / ${op[level(c)]})`;
  }
  function tip(d) {
    return `${d.count} on ${new Date(d.date).toLocaleDateString()}`;
  }
</script>

<div class="heatmap">
  {#each cols as col, ci (ci)}
    <div class="hm-col">
      {#each col as d, ri (ri)}
        <div class="hm-cell" style="background:{cellColor(d.count)}" title={tip(d)}></div>
      {/each}
    </div>
  {/each}
</div>

<style>
  .heatmap { display: flex; gap: 3px; }
  .hm-col { display: flex; flex-direction: column; gap: 3px; flex: 1; }
  .hm-cell { aspect-ratio: 1; border-radius: 2px; min-height: 12px; }
</style>
