<script>
  // series: [{ color, values:[] }] — cumulative-by-kind. dates: ISO strings.
  let { series = [], dates = [], width = 760, height = 300 } = $props();
  const pad = { t: 12, r: 8, b: 26, l: 40 };

  let model = $derived.by(() => {
    const n = dates.length;
    if (!n) return null;
    const innerW = width - pad.l - pad.r;
    const innerH = height - pad.t - pad.b;
    const stacks = [];
    let prev = new Array(n).fill(0);
    for (const s of series) {
      const top = (s.values || []).map((val, i) => prev[i] + (val || 0));
      stacks.push({ color: s.color, lower: prev, upper: top });
      prev = top;
    }
    const max = Math.max(...prev, 1);
    const niceMax = Math.ceil(max / 100) * 100 || 100;
    const x = (i) => pad.l + (i / Math.max(1, n - 1)) * innerW;
    const y = (val) => pad.t + innerH - (val / max) * innerH;
    const ticks = 4;
    const gridlines = Array.from({ length: ticks + 1 }, (_, t) => {
      const gv = (niceMax / ticks) * t;
      return { gv, gy: y(gv), label: gv >= 1000 ? gv / 1000 + 'k' : gv };
    });
    const polys = stacks.map((st) => {
      const up = st.upper.map((val, k) => `${x(k)},${y(val)}`);
      const lo = st.lower.map((val, k) => `${x(k)},${y(val)}`).reverse();
      return { color: st.color, poly: [...up, ...lo].join(' '), line: st.upper.map((val, k) => `${x(k)},${y(val)}`).join(' ') };
    });
    const xLabels = [0, Math.floor(n / 3), Math.floor((2 * n) / 3), n - 1].map((i) => ({
      x: x(i), anchor: i === 0 ? 'start' : i === n - 1 ? 'end' : 'middle',
      text: new Date(dates[i]).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
    }));
    return { gridlines, polys, xLabels };
  });
</script>

{#if model}
  <svg width="100%" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" style="display:block">
    {#each model.gridlines as g (g.gv)}
      <line x1={pad.l} x2={width - pad.r} y1={g.gy} y2={g.gy} stroke="var(--border)" stroke-width="1" opacity="0.5" stroke-dasharray="2 4" />
      <text x={pad.l - 8} y={g.gy + 4} text-anchor="end" font-family="var(--font-mono)" font-size="10" fill="var(--text-faint)">{g.label}</text>
    {/each}
    {#each model.polys as p, i (i)}
      <polygon points={p.poly} fill={p.color} fill-opacity="0.78" />
    {/each}
    {#each model.polys as p, i (i)}
      <polyline points={p.line} fill="none" stroke={p.color} stroke-width="1.4" />
    {/each}
    {#each model.xLabels as l, i (i)}
      <text x={l.x} y={height - 8} text-anchor={l.anchor} font-family="var(--font-mono)" font-size="10" fill="var(--text-faint)">{l.text}</text>
    {/each}
  </svg>
{/if}
