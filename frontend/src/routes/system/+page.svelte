<script>
  // Color System reference — swatches, kind & entity palettes, the OKLCH hue
  // wheel. Every value is a live CSS custom property; the page re-reads computed
  // values whenever the theme changes, so it visibly retints.
  import { onMount } from 'svelte';
  import PageHeader from '$lib/PageHeader.svelte';
  import StatCard from '$lib/components/StatCard.svelte';
  import KindBadge from '$lib/components/KindBadge.svelte';
  import EntityBadge from '$lib/components/EntityBadge.svelte';
  import { KINDS, ENTITY_TYPES, KIND_HUES, ENTITY_HUES, loadTheme } from '$lib/theme.js';

  let tick = $state(0);
  let theme = $state(loadTheme());

  onMount(() => {
    const fn = (e) => { if (e.detail) theme = e.detail; tick++; };
    window.addEventListener('rill-theme', fn);
    tick++; // initial read after mount
    return () => window.removeEventListener('rill-theme', fn);
  });

  function computed(name) {
    if (typeof document === 'undefined') return '';
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }

  const neutrals = [
    ['--bg', 'Background'], ['--surface', 'Surface'], ['--surface-2', 'Surface 2'],
    ['--surface-3', 'Surface 3'], ['--border', 'Border'], ['--border-strong', 'Border strong'],
  ];
  const accents = [
    ['--accent', 'Accent'], ['--accent-hi', 'Accent hi'], ['--success', 'Success'],
    ['--warning', 'Warning / experimental'], ['--destructive', 'Destructive'], ['--muted', 'Muted'],
  ];

  // Hue wheel geometry.
  const size = 230, cx = size / 2, cy = size / 2, r = 86;
  const ring = Array.from({ length: 90 }, (_, i) => {
    const a = i * 4;
    const rad = (a - 90) * Math.PI / 180;
    return {
      x1: cx + Math.cos(rad) * (r - 14), y1: cy + Math.sin(rad) * (r - 14),
      x2: cx + Math.cos(rad) * (r + 14), y2: cy + Math.sin(rad) * (r + 14),
      stroke: `oklch(0.72 0.16 ${a})`,
    };
  });
  function dot(hue) {
    const a = (hue - 90) * Math.PI / 180;
    return { x: cx + Math.cos(a) * r, y: cy + Math.sin(a) * r };
  }
</script>

<PageHeader section="system" title="Color System">
  <span class="ph-meta mono">oklch · {theme.name || 'custom'} · live tokens</span>
</PageHeader>

<div class="sys-note">
  Every value below is a CSS custom property recomputed in OKLCH from the active theme.
  Components reference <code class="mono">var(--token)</code> only — never a literal.
  Open the <b>Theme</b> panel (bottom-right) and watch this page retint.
</div>

{#key tick}
<div class="sys-grid">
  <StatCard title="Neutrals" sub="background ramp">
    <div class="sw-grid">
      {#each neutrals as [v, label] (v)}
        <div class="sw">
          <div class="sw-chip" style="background:var({v})"></div>
          <div class="sw-meta"><span class="sw-label">{label}</span><code class="sw-var mono">{v}</code></div>
        </div>
      {/each}
    </div>
  </StatCard>

  <StatCard title="Text" sub="on background">
    <div class="text-ramp">
      <div class="tr" style="color:var(--text)">Primary text <code class="mono">--text</code></div>
      <div class="tr" style="color:var(--text-dim)">Dimmed text <code class="mono">--text-dim</code></div>
      <div class="tr" style="color:var(--text-faint)">Faint / meta <code class="mono">--text-faint</code></div>
      <div class="tr" style="color:var(--accent)">Accent / link <code class="mono">--accent</code></div>
    </div>
  </StatCard>

  <StatCard title="Accent & status" sub="semantic">
    <div class="sw-grid">
      {#each accents as [v, label] (v)}
        <div class="sw">
          <div class="sw-chip" style="background:var({v})"></div>
          <div class="sw-meta"><span class="sw-label">{label}</span><code class="sw-var mono">{v}</code></div>
        </div>
      {/each}
    </div>
  </StatCard>

  <StatCard title="Memory kinds" sub="7 · the kind field">
    <div class="cat-list">
      {#each KINDS as id (id)}
        <div class="cat-row">
          <div class="cat-chip" style="background:var(--k-{id})"></div>
          <div class="cat-chip soft" style="background:var(--k-{id}-bg)"></div>
          <span class="cat-name">{id}</span>
          <code class="cat-val mono">{computed(`--k-${id}`) || `oklch(… ${KIND_HUES[id]}°)`}</code>
        </div>
      {/each}
    </div>
    <div class="badge-demo">{#each KINDS as id (id)}<KindBadge kind={id} />{/each}</div>
  </StatCard>

  <StatCard title="Entity types" sub="7 · the type field">
    <div class="cat-list">
      {#each ENTITY_TYPES as id (id)}
        <div class="cat-row">
          <div class="cat-chip" style="background:var(--e-{id})"></div>
          <div class="cat-chip soft" style="background:var(--e-{id}-bg)"></div>
          <span class="cat-name">{id}</span>
          <code class="cat-val mono">{computed(`--e-${id}`) || `oklch(… ${ENTITY_HUES[id]}°)`}</code>
        </div>
      {/each}
    </div>
    <div class="badge-demo">{#each ENTITY_TYPES as id (id)}<EntityBadge type={id} />{/each}</div>
  </StatCard>

  <StatCard title="The wheel" sub="hues on the OKLCH circle">
    <div class="wheel-wrap">
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        {#each ring as l, i (i)}
          <line x1={l.x1} y1={l.y1} x2={l.x2} y2={l.y2} stroke={l.stroke} stroke-width="5" />
        {/each}
        {#each KINDS as id (id)}
          {@const d = dot(KIND_HUES[id] + (theme.hueShift || 0))}
          <circle cx={d.x} cy={d.y} r="9" fill="var(--k-{id})" stroke="var(--bg)" stroke-width="2.5" />
        {/each}
        <circle {cx} {cy} r="44" fill="var(--surface)" stroke="var(--border)" />
        <text x={cx} y={cy - 4} text-anchor="middle" font-family="var(--font-mono)" font-size="12" fill="var(--text-dim)">OKLCH</text>
        <text x={cx} y={cy + 14} text-anchor="middle" font-family="var(--font-mono)" font-size="11" fill="var(--text-faint)">hue ring</text>
      </svg>
      <div class="wheel-note">
        <p>The 7 memory kinds sit at fixed hue anchors around the wheel. <b>Palette rotate</b> spins
          all of them together; <b>Palette chroma</b> pushes them in or out. Lightness stays constant
          so every kind keeps equal visual weight — no single color shouts.</p>
        <div class="badge-demo">{#each KINDS as id (id)}<KindBadge kind={id} small />{/each}</div>
      </div>
    </div>
  </StatCard>
</div>
{/key}

<style>
  .mono { font-family: var(--font-mono); }
  .ph-meta { font-size: 12px; color: var(--text-faint); }
  .sys-note {
    background: var(--surface); border: 1px solid var(--border); border-left: 2px solid var(--accent);
    border-radius: var(--radius-sm); padding: 12px 15px; font-size: 13px; color: var(--text-dim); margin-bottom: 18px;
  }
  .sys-note code, .sys-note b { color: var(--text); }
  .sys-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; align-items: start; }
  .sw-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .sw { display: flex; align-items: center; gap: 10px; }
  .sw-chip { width: 38px; height: 38px; border-radius: 8px; border: 1px solid var(--border); flex: none; }
  .sw-meta { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
  .sw-label { font-size: 12px; color: var(--text); }
  .sw-var { font-size: 10.5px; color: var(--text-faint); }
  .text-ramp { display: flex; flex-direction: column; gap: 9px; }
  .tr { font-size: 14px; display: flex; align-items: baseline; justify-content: space-between; gap: 10px; }
  .tr code { font-size: 10.5px; color: var(--text-faint); }
  .cat-list { display: flex; flex-direction: column; gap: 7px; }
  .cat-row { display: grid; grid-template-columns: auto auto 80px 1fr; align-items: center; gap: 9px; }
  .cat-chip { width: 26px; height: 26px; border-radius: 6px; flex: none; border: 1px solid var(--border); }
  .cat-chip.soft { width: 18px; }
  .cat-name { font-size: 12.5px; color: var(--text); font-family: var(--font-mono); }
  .cat-val { font-size: 10.5px; color: var(--text-faint); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .badge-demo { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border); }
  .wheel-wrap { display: flex; gap: 24px; align-items: center; flex-wrap: wrap; }
  .wheel-note { flex: 1; min-width: 240px; }
  .wheel-note p { font-size: 13px; color: var(--text-dim); line-height: 1.55; }
  .wheel-note b { color: var(--text); }
  @media (max-width: 1100px) { .sys-grid { grid-template-columns: repeat(2, 1fr); } }
  @media (max-width: 720px) { .sys-grid { grid-template-columns: 1fr; } }
</style>
