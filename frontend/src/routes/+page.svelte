<script>
  // Dashboard — the landing view. KPI tiles, cumulative growth by kind, by-kind
  // donut, by-project bars, a 90-day activity heatmap, and a recent feed. All
  // data comes from /api/stats; handles loading / empty / error / data.
  import { onMount } from 'svelte';
  import PageHeader from '$lib/PageHeader.svelte';
  import StatCard from '$lib/components/StatCard.svelte';
  import KindBadge from '$lib/components/KindBadge.svelte';
  import ProjectChip from '$lib/components/ProjectChip.svelte';
  import Legend from '$lib/components/Legend.svelte';
  import Donut from '$lib/components/charts/Donut.svelte';
  import StackedArea from '$lib/components/charts/StackedArea.svelte';
  import Bars from '$lib/components/charts/Bars.svelte';
  import Heatmap from '$lib/components/charts/Heatmap.svelte';
  import Sparkline from '$lib/components/charts/Sparkline.svelte';
  import { KINDS, projectColor } from '$lib/colors.js';
  import { api } from '$lib/api.js';
  import Brain from '@lucide/svelte/icons/brain';

  let range = $state('90d');
  let loading = $state(true);
  let error = $state('');
  let stats = $state(null);

  async function load(r) {
    loading = true; error = '';
    try {
      stats = await api.stats(r);
    } catch (e) {
      error = e?.message || 'Failed to load stats';
      stats = null;
    } finally {
      loading = false;
    }
  }
  onMount(() => load(range));
  function setRange(r) { range = r; load(r); }

  const kindColor = (id) => `var(--k-${id})`;

  // --- derived views ---
  let donutSegs = $derived((stats?.kind_breakdown || []).map((k) => ({ value: k.count, color: kindColor(k.id) })));
  let sortedKinds = $derived([...(stats?.kind_breakdown || [])].sort((a, b) => b.count - a.count));
  let areaSeries = $derived(KINDS.map((id) => ({ color: kindColor(id), values: stats?.growth?.[id] || [] })));
  let projItems = $derived((stats?.project_breakdown || []).map((p) => ({
    label: p.id, value: p.count, color: projectColor(p.id),
  })));
  let memSpark = $derived.by(() => {
    if (!stats?.dates) return [];
    const s = stats.dates.map((_, i) => KINDS.reduce((sum, id) => sum + (stats.growth?.[id]?.[i] || 0), 0));
    return s.slice(-24);
  });
  let totalMemories = $derived(stats?.kpis?.memories ?? 0);

  const kpiDefs = [
    { key: 'memories', label: 'Memories', spark: true },
    { key: 'entities', label: 'Entities' },
    { key: 'documents', label: 'Documents' },
    { key: 'relations', label: 'Relations' },
    { key: 'projects', label: 'Projects' },
    { key: 'sessions', label: 'Sessions' },
  ];

  function shortId(id) {
    return (id || '').replace(/^memory:/, '').replace(/`/g, '').slice(0, 8);
  }
  function ago(iso) {
    const then = new Date(iso).getTime();
    const s = Math.max(0, (Date.now() - then) / 1000);
    if (s < 3600) return Math.round(s / 60) + 'm';
    if (s < 86400) return Math.round(s / 3600) + 'h';
    return Math.round(s / 86400) + 'd';
  }
</script>

<PageHeader section="dashboard" title="Dashboard">
  {#if stats}
    <span class="ph-meta mono">{totalMemories.toLocaleString()} memories · {stats.kpis.entities} entities</span>
  {/if}
  <div class="seg">
    {#each ['7d', '30d', '90d', 'all'] as r (r)}
      <button class="seg-btn mono" class:on={range === r} onclick={() => setRange(r)}>{r}</button>
    {/each}
  </div>
</PageHeader>

{#if loading}
  <div class="dash-loading"><div class="spinner"></div></div>
{:else if error}
  <div class="state-box error">
    <div class="state-title">Couldn't load the dashboard</div>
    <div class="state-sub mono">{error}</div>
    <button class="btn" onclick={() => load(range)}>Retry</button>
  </div>
{:else if !stats || totalMemories === 0}
  <div class="state-box">
    <div class="state-mark"><Brain size={34} /></div>
    <div class="state-title">No memories yet</div>
    <div class="state-sub">The dashboard fills in as memories are captured. Start by storing one
      via the <code class="mono">remember</code> tool, or browse <a href="/memories">Memories</a>.</div>
  </div>
{:else}
  <div class="kpi-row">
    {#each kpiDefs as k (k.key)}
      <div class="kpi">
        <div class="kpi-top"><span class="kpi-label">{k.label}</span></div>
        <div class="kpi-value mono">{(stats.kpis[k.key] ?? 0).toLocaleString()}</div>
        {#if k.spark && memSpark.length > 1}
          <Sparkline values={memSpark} width={120} height={26} color="var(--accent)" />
        {/if}
      </div>
    {/each}
  </div>

  <div class="dash-grid">
    <div class="span-2">
      <StatCard title="Memory growth" sub="cumulative · by kind">
        {#snippet right()}
          <Legend columns={4} items={KINDS.map((id) => ({ label: id, color: kindColor(id) }))} />
        {/snippet}
        <StackedArea series={areaSeries} dates={stats.dates} width={760} height={300} />
      </StatCard>
    </div>

    <StatCard title="By kind" sub={`${totalMemories.toLocaleString()} total`}>
      <div class="donut-wrap">
        <Donut segments={donutSegs} centerLabel={totalMemories.toLocaleString()} centerSub="MEMORIES" />
        <ul class="donut-legend">
          {#each sortedKinds as k (k.id)}
            <li>
              <KindBadge kind={k.id} small />
              <span class="dl-val mono">{k.count}</span>
              <span class="dl-pct mono">{totalMemories ? Math.round((k.count / totalMemories) * 100) : 0}%</span>
            </li>
          {/each}
        </ul>
      </div>
    </StatCard>

    <StatCard title="By project" sub="memory volume">
      {#if projItems.length}
        <Bars items={projItems} />
      {:else}
        <p class="muted-note">No project-scoped memories yet.</p>
      {/if}
    </StatCard>

    <div class="span-2">
      <StatCard title="Activity" sub={`last ${stats.days || 90} days`}>
        <div class="hm-wrap">
          <Heatmap data={stats.heatmap} colorVar="--success" />
          <div class="hm-scale mono">
            <span>less</span>
            {#each [0, 1, 2, 3, 4] as l (l)}
              <i style="background:{l === 0 ? 'var(--surface-3)' : `oklch(from var(--success) l c h / ${[0, 0.28, 0.5, 0.74, 1][l]})`}"></i>
            {/each}
            <span>more</span>
          </div>
        </div>
      </StatCard>
    </div>

    <div class="span-3">
      <StatCard title="Recent" sub="latest captures" flush>
        {#snippet right()}<a class="link mono" href="/memories">view all →</a>{/snippet}
        <ul class="recent-list">
          {#each (stats.recent || []).slice(0, 6) as m (m.id)}
            <li class="recent-row">
              <span class="r-id mono">#{shortId(m.id)}</span>
              <KindBadge kind={m.kind} small />
              <span class="r-text">{m.summary}</span>
              <ProjectChip name={m.project} />
              <span class="r-ago mono">{ago(m.created_at)}</span>
            </li>
          {/each}
        </ul>
      </StatCard>
    </div>
  </div>
{/if}

<style>
  .mono { font-family: var(--font-mono); }
  .ph-meta { font-size: 12px; color: var(--text-faint); }
  .seg { display: flex; background: var(--surface-2); border: 1px solid var(--border); border-radius: var(--radius-sm); padding: 2px; }
  .seg-btn { padding: 5px 12px; border: none; background: none; cursor: pointer; color: var(--text-faint); border-radius: 5px; font-size: 12px; }
  .seg-btn:hover { color: var(--text); }
  .seg-btn.on { background: var(--accent-bg); color: var(--accent); }

  .dash-loading { display: flex; justify-content: center; padding: 5rem; }
  .state-box {
    text-align: center; padding: 48px 20px; display: flex; flex-direction: column; align-items: center; gap: 8px;
    border: 1px dashed var(--border); border-radius: var(--radius);
  }
  .state-box.error { border-color: color-mix(in oklab, var(--destructive) 40%, transparent); }
  .state-mark { color: var(--text-faint); opacity: 0.7; margin-bottom: 4px; }
  .state-title { font-size: 15px; font-weight: 600; color: var(--text); }
  .state-sub { font-size: 13px; color: var(--text-dim); max-width: 420px; }

  .kpi-row { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 16px; }
  .kpi { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 13px 15px; display: flex; flex-direction: column; gap: 6px; }
  .kpi-top { display: flex; align-items: center; justify-content: space-between; }
  .kpi-label { font-size: 11.5px; color: var(--text-dim); }
  .kpi-value { font-size: 27px; font-weight: 600; letter-spacing: -0.02em; line-height: 1.05; color: var(--text); }

  .dash-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; align-items: start; }
  .span-2 { grid-column: span 2; }
  .span-3 { grid-column: span 3; }

  .donut-wrap { display: flex; flex-direction: column; align-items: center; gap: 14px; }
  .donut-legend { list-style: none; display: flex; flex-direction: column; gap: 5px; width: 100%; margin: 0; padding: 0; }
  .donut-legend li { display: grid; grid-template-columns: 1fr auto auto; align-items: center; gap: 10px; }
  .dl-val { font-size: 12px; color: var(--text); }
  .dl-pct { font-size: 11px; color: var(--text-faint); min-width: 34px; text-align: right; }

  .hm-wrap { display: flex; flex-direction: column; gap: 10px; }
  .hm-scale { display: flex; align-items: center; gap: 4px; font-size: 11px; color: var(--text-faint); justify-content: flex-end; }
  .hm-scale i { width: 11px; height: 11px; border-radius: 2px; }

  .recent-list { list-style: none; margin: 0; padding: 0; }
  .recent-row { display: grid; grid-template-columns: auto auto 1fr auto auto; align-items: center; gap: 12px; padding: 10px 17px; border-top: 1px solid var(--border); }
  .recent-row:first-child { border-top: none; }
  .recent-row:hover { background: var(--surface-2); }
  .r-id { font-size: 11.5px; color: var(--text-faint); }
  .r-text { font-size: 13px; color: var(--text-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .r-ago { font-size: 11.5px; color: var(--text-faint); min-width: 26px; text-align: right; }

  .link { color: var(--accent); text-decoration: underline dotted var(--accent-line); text-underline-offset: 3px; font-size: 12px; }
  .link:hover { color: var(--accent-hi); }
  .muted-note { font-size: 12.5px; color: var(--text-faint); }

  @media (max-width: 1100px) {
    .kpi-row { grid-template-columns: repeat(3, 1fr); }
    .dash-grid { grid-template-columns: repeat(2, 1fr); }
    .span-2, .span-3 { grid-column: span 2; }
  }
  @media (max-width: 720px) {
    .kpi-row, .dash-grid { grid-template-columns: 1fr; }
    .span-2, .span-3 { grid-column: span 1; }
    .recent-row { grid-template-columns: auto 1fr auto; }
    .recent-row :global(.kind-badge), .r-id { display: none; }
  }
</style>
