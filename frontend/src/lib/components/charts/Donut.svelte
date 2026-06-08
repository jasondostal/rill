<script>
  // segments: [{ value, color }]. color is any CSS color (use var(--k-*)).
  let { segments = [], size = 168, thickness = 22, centerLabel = null, centerSub = '' } = $props();
  const cx = size / 2, cy = size / 2;
  const r = (size - thickness) / 2;
  const C = 2 * Math.PI * r;

  let arcs = $derived.by(() => {
    const total = segments.reduce((a, s) => a + s.value, 0) || 1;
    let offset = 0;
    return segments.map((s) => {
      const len = (s.value / total) * C;
      const arc = { color: s.color, len, offset };
      offset += len;
      return arc;
    });
  });
</script>

<svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} style="flex:none">
  <circle {cx} {cy} {r} fill="none" stroke="var(--surface-3)" stroke-width={thickness} opacity="0.5" />
  <g transform={`rotate(-90 ${cx} ${cy})`}>
    {#each arcs as a, i (i)}
      <circle {cx} {cy} {r} fill="none" stroke={a.color} stroke-width={thickness}
        stroke-dasharray={`${a.len} ${C - a.len}`} stroke-dashoffset={-a.offset} stroke-linecap="butt" />
    {/each}
  </g>
  {#if centerLabel != null}
    <text x={cx} y={cy - 2} text-anchor="middle" font-family="var(--font-mono)" font-weight="600"
      font-size="26" fill="var(--text)">{centerLabel}</text>
    <text x={cx} y={cy + 18} text-anchor="middle" font-family="var(--font-mono)"
      font-size="11" letter-spacing="0.08em" fill="var(--text-faint)">{centerSub}</text>
  {/if}
</svg>
