<script>
  let { values = [], width = 96, height = 28, color = 'var(--accent)' } = $props();
  let pts = $derived.by(() => {
    if (!values.length) return '';
    const max = Math.max(...values, 1), min = Math.min(...values, 0);
    const x = (i) => (i / Math.max(1, values.length - 1)) * width;
    const y = (v) => height - ((v - min) / (max - min || 1)) * (height - 2) - 1;
    return values.map((v, i) => `${x(i)},${y(v)}`).join(' ');
  });
</script>

<svg {width} {height} style="display:block;overflow:visible">
  <polyline points={pts} fill="none" stroke={color} stroke-width="1.6"
    stroke-linecap="round" stroke-linejoin="round" />
</svg>
