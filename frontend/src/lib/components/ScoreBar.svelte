<script>
  // Recall-score bar. Hue ramps red→amber→green across the score via the
  // semantic status tokens.
  let { value } = $props();
  let pct = $derived(Math.round((value ?? 0) * 100));
  let col = $derived(value >= 0.8 ? 'var(--success)' : value >= 0.65 ? 'var(--warning)' : 'var(--destructive)');
</script>

<span class="score">
  <span class="score-track"><span class="score-fill" style="width:{pct}%;background:{col}"></span></span>
  <span class="score-num mono" style="color:{col}">{(value ?? 0).toFixed(2)}</span>
</span>

<style>
  .score { display: inline-flex; align-items: center; gap: 7px; }
  .score-track { width: 46px; height: 5px; background: var(--surface-3); border-radius: 10px; overflow: hidden; }
  .score-fill { height: 100%; border-radius: 10px; }
  .score-num { font-family: var(--font-mono); font-size: 11.5px; }
</style>
