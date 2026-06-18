<script>
  // Live OKLCH theme panel: curated presets + tuning sliders. Writes the theme
  // knobs back via onChange; the layout persists + applies them, so the whole
  // site retints instantly. Port of the Claude Design prototype's ThemePicker.
  import { ok, PRESETS, KIND_HUES, activePresetName } from '$lib/theme.js';
  import X from '@lucide/svelte/icons/x';

  let { value, open = false, onChange, onClose } = $props();

  let activeName = $derived(activePresetName(value));
  const set = (patch) => onChange({ ...value, ...patch });

  // Four representative kind dots for each preset's swatch.
  const dotKinds = ['procedure', 'fact', 'decision', 'preference'];
  function presetDot(p, id) {
    const h = KIND_HUES[id] + (p.hueShift || 0);
    const cl = p.catL ?? (p.mode === 'light' ? 0.55 : 0.745);
    return ok(cl, p.catC, h);
  }

  const sliders = [
    { key: 'bgH', label: 'Background hue', min: 0, max: 360, step: 1, unit: '°' },
    { key: 'bgL', label: 'Background light', min: () => (value.mode === 'light' ? 0.90 : 0.10), max: () => (value.mode === 'light' ? 0.99 : 0.30), step: 0.005 },
    { key: 'accentH', label: 'Accent hue', min: 0, max: 360, step: 1, unit: '°' },
    { key: 'accentC', label: 'Accent chroma', min: 0, max: 0.26, step: 0.005 },
    { key: 'catC', label: 'Palette chroma', min: 0, max: 0.24, step: 0.005 },
    { key: 'hueShift', label: 'Palette rotate', min: -180, max: 180, step: 1, unit: '°' },
  ];
  const v = (s) => (typeof s === 'function' ? s() : s);
  const fmt = (val, step) => Number(val).toFixed(step < 1 ? 3 : 0);
</script>

{#if open}
  <aside class="theme-panel" role="dialog" aria-label="Theme">
    <header class="tp-head">
      <div>
        <div class="tp-title">Theme</div>
        <div class="tp-sub mono">oklch · live</div>
      </div>
      <button class="icon-btn" onclick={onClose} aria-label="Close"><X size={16} /></button>
    </header>

    <div class="tp-section-label">Presets</div>
    <div class="tp-presets">
      {#each PRESETS as p (p.name)}
        <button class="tp-preset" class:active={activeName === p.name} onclick={() => onChange({ ...p })}>
          <span class="tp-dots">
            {#each dotKinds as id}<i style="background:{presetDot(p, id)}"></i>{/each}
          </span>
          <span class="tp-preset-name">{p.name}</span>
        </button>
      {/each}
    </div>

    <div class="tp-section-label">Tune</div>
    <div class="tp-tune">
      <div class="tp-mode">
        <button class:on={value.mode === 'dark'}
          onclick={() => set({ mode: 'dark', bgL: value.bgL < 0.5 ? value.bgL : 0.175 })}>Dark</button>
        <button class:on={value.mode === 'light'}
          onclick={() => set({ mode: 'light', bgL: value.bgL > 0.5 ? value.bgL : 0.965 })}>Light</button>
      </div>
      {#each sliders as s (s.key)}
        <label class="tp-slider">
          <span class="tp-slider-row">
            <span class="tp-slider-label">{s.label}</span>
            <span class="mono tp-slider-val">{fmt(value[s.key], s.step)}{s.unit || ''}</span>
          </span>
          <input type="range" min={v(s.min)} max={v(s.max)} step={s.step} value={value[s.key]}
            oninput={(e) => set({ [s.key]: parseFloat(e.currentTarget.value) })} />
        </label>
      {/each}
    </div>

    <div class="tp-foot mono">All views retint live · tokens only</div>
  </aside>
{/if}

<style>
  .mono { font-family: var(--font-mono); }
  .theme-panel {
    position: fixed; right: 22px; bottom: 80px; z-index: 41;
    width: 296px; background: var(--surface); border: 1px solid var(--border-strong);
    border-radius: 14px; box-shadow: var(--shadow); padding: 16px;
    display: flex; flex-direction: column; gap: 12px;
    max-height: calc(100vh - 120px); overflow-y: auto;
  }
  .tp-head { display: flex; align-items: flex-start; justify-content: space-between; }
  .tp-title { font-size: 15px; font-weight: 600; color: var(--text); }
  .tp-sub { font-size: 10.5px; color: var(--text-faint); letter-spacing: 0.04em; }
  .icon-btn { background: none; border: none; color: var(--text-faint); cursor: pointer; padding: 4px; display: flex; }
  .icon-btn:hover { color: var(--text); }
  .tp-section-label { font-size: 10.5px; color: var(--text-faint); text-transform: uppercase; letter-spacing: 0.1em; }
  .tp-presets { display: grid; grid-template-columns: 1fr 1fr; gap: 7px; }
  .tp-preset {
    display: flex; align-items: center; gap: 8px; background: var(--surface-2);
    border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 7px 9px;
    cursor: pointer; color: var(--text-dim); font: inherit;
  }
  .tp-preset:hover { border-color: var(--border-strong); color: var(--text); }
  .tp-preset.active { border-color: var(--accent); color: var(--text); background: var(--accent-bg); }
  .tp-dots { display: flex; gap: 2px; flex: none; }
  .tp-dots i { width: 8px; height: 8px; border-radius: 50%; }
  .tp-preset-name { font-size: 12px; white-space: nowrap; }
  .tp-tune { display: flex; flex-direction: column; gap: 11px; }
  .tp-mode { display: flex; gap: 4px; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 3px; }
  .tp-mode button { flex: 1; padding: 5px; border: none; background: none; border-radius: 5px; cursor: pointer; color: var(--text-faint); font: inherit; font-size: 12px; }
  .tp-mode button.on { background: var(--accent-bg); color: var(--accent); }
  .tp-slider { display: flex; flex-direction: column; gap: 5px; }
  .tp-slider-row { display: flex; justify-content: space-between; align-items: baseline; }
  .tp-slider-label { font-size: 12px; color: var(--text-dim); white-space: nowrap; }
  .tp-slider-val { font-size: 11px; color: var(--text-faint); flex: none; }
  .tp-slider input[type=range] { -webkit-appearance: none; appearance: none; width: 100%; height: 4px; border-radius: 4px; background: var(--surface-3); outline: none; }
  .tp-slider input[type=range]::-webkit-slider-thumb { -webkit-appearance: none; width: 14px; height: 14px; border-radius: 50%; background: var(--accent); cursor: pointer; border: 2px solid var(--surface); }
  .tp-slider input[type=range]::-moz-range-thumb { width: 14px; height: 14px; border-radius: 50%; background: var(--accent); cursor: pointer; border: 2px solid var(--surface); }
  .tp-foot { font-size: 10px; color: var(--text-faint); text-align: center; padding-top: 4px; border-top: 1px solid var(--border); }

  @media (max-width: 768px) {
    .theme-panel { right: 12px; left: 12px; width: auto; bottom: 72px; }
  }
</style>
