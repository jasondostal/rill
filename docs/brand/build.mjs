// rill brand — pixel-art renderer.
// Authors pixel grids in code, emits crisp SVG (no AA), rasterizes with the
// app's own chromium (Playwright) so oklch() renders identically to the UI.
import { writeFileSync } from 'node:fs';
// Resolve playwright from the frontend workspace regardless of cwd.
const pw = await import(new URL('../../frontend/node_modules/playwright/index.js', import.meta.url).href);
const { chromium } = pw.default ?? pw;

// ── palette (exact app tokens where possible) ─────────────────────────────
const C = {
  bg:    'oklch(0.175 0.018 265)',   // app --bg, near-black blue
  tile:  'oklch(0.210 0.018 265)',   // app --surface
  deep:  'oklch(0.330 0.110 258)',   // streambed depth
  mid:   'oklch(0.520 0.150 252)',   // water body
  acc:   'oklch(0.700 0.190 255)',   // app --accent, electric blue
  foam:  'oklch(0.890 0.085 220)',   // crest highlight / spray
  vio:   'oklch(0.560 0.200 290)',   // logo violet
  stoneD:'oklch(0.330 0.016 265)',
  stoneL:'oklch(0.470 0.022 265)',
  stoneSh:'oklch(0.230 0.014 265)',
  // 7-kind spectrum motes (L0.745 C0.16, app hues)
  decision:'oklch(0.78 0.16 85)', preference:'oklch(0.74 0.16 330)',
  insight:'oklch(0.78 0.16 195)', procedure:'oklch(0.74 0.16 255)',
  fact:'oklch(0.76 0.16 150)', identity:'oklch(0.74 0.16 300)',
  rule:'oklch(0.74 0.16 30)',
};

// ── grid helpers ──────────────────────────────────────────────────────────
function grid(w, h) { return Array.from({ length: h }, () => Array(w).fill(null)); }
function px(g, x, y, c) { if (y >= 0 && y < g.length && x >= 0 && x < g[0].length) g[y][x] = c; }
// a flowing wave band: foam crest, accent body, mid, deep — sine-driven
function wave(g, { base, amp, period, phase = 0, thick = 4, xs = 0, xe }) {
  const W = g[0].length; xe = xe ?? W;
  const cols = [C.foam, C.acc, C.mid, C.deep];
  for (let x = xs; x < xe; x++) {
    const y = base + Math.round(amp * Math.sin((2 * Math.PI * x) / period + phase));
    for (let t = 0; t < thick; t++) px(g, x, y + t, cols[Math.min(t, cols.length - 1)]);
  }
}
function mote(g, x, y, c) { // 2x2 glowing mote with a foam glint
  px(g, x, y, c); px(g, x + 1, y, c); px(g, x, y + 1, c); px(g, x + 1, y + 1, c);
  px(g, x, y - 1, C.foam);
}

// ── SVG emit (1 cell = 1 unit; integer coords = no anti-aliasing) ─────────
function svg(g, { bg = null, pad = 0, round = 0 } = {}) {
  const h = g.length, w = g[0].length, W = w + pad * 2, H = h + pad * 2;
  let r = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" shape-rendering="crispEdges">`;
  if (bg) r += `<rect x="0" y="0" width="${W}" height="${H}" rx="${round}" fill="${bg}"/>`;
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    const c = g[y][x]; if (c) r += `<rect x="${x + pad}" y="${y + pad}" width="1" height="1" fill="${c}"/>`;
  }
  return r + '</svg>';
}

// ── compose: the 32×32 mark — three flowing currents carrying memory ──────
function mark() {
  const g = grid(32, 32);
  wave(g, { base: 6,  amp: 2, period: 18, phase: 0.0, thick: 4 });
  wave(g, { base: 14, amp: 2, period: 18, phase: 2.1, thick: 4 });
  wave(g, { base: 22, amp: 2, period: 18, phase: 4.2, thick: 4 });
  // motes riding the crests (spectrum sampling)
  mote(g, 7,  4, C.decision);
  mote(g, 21, 5, C.insight);
  mote(g, 12, 13, C.preference);
  mote(g, 24, 13, C.fact);
  mote(g, 5,  21, C.identity);
  mote(g, 18, 22, C.rule);
  return g;
}

// a rounded, top-lit, dithered stone sprite (light from top-left)
const STONE = [
  '.LLDD.',
  'LLDDDd',
  'LDDDDd',
  'LDDDdd',
  '.Dddd.',
];
function stone(g, sx, sy) {
  const map = { L: C.stoneL, D: C.stoneD, d: C.stoneSh };
  STONE.forEach((row, dy) => [...row].forEach((ch, dx) => { if (map[ch]) px(g, sx + dx, sy + dy, map[ch]); }));
}
// a mote riding the current: 2x2 core, foam glint downstream, faint upstream wake
function flowMote(g, x, y, c) {
  px(g, x, y, c); px(g, x + 1, y, c); px(g, x, y + 1, c); px(g, x + 1, y + 1, c);
  px(g, x + 1, y - 1, C.foam);                 // glint ahead/down
  px(g, x - 1, y + 1, C.mid); px(g, x - 2, y + 2, C.deep); // upstream wake
}

// ── compose: candidate B (refined) — one stream carving an S downhill ─────
function brook({ stones: withStones = true } = {}) {
  const g = grid(32, 32);
  // a clean single S: ~1.05 cycles over the height
  const base = 16, amp = 7, period = 27, ph = -0.5, hw = 2;
  const cx = (y) => base + Math.round(amp * Math.sin((y / period) * 2 * Math.PI + ph));
  // streambed shadow → water body → glossy core; calm downstream chevrons
  for (let y = 2; y < 31; y++) {
    const c = cx(y);
    for (let x = c - hw - 1; x <= c + hw + 1; x++) px(g, x, y, C.deep);
    for (let x = c - hw; x <= c + hw; x++) px(g, x, y, C.mid);
    px(g, c, y, C.acc); px(g, c + 1, y, C.acc);                 // glossy core
  }
  for (let y = 4; y < 29; y += 5) {                              // ripple chevrons (point downstream)
    const c = cx(y);
    px(g, c, y, C.foam); px(g, c + 1, y, C.foam);
    px(g, c - 1, y - 1, C.foam); px(g, c + 2, y - 1, C.foam);
  }
  // source spring (top) and pool (bottom)
  const t = cx(2), b = cx(30);
  px(g, t, 1, C.foam); px(g, t + 1, 1, C.foam);
  for (let x = b - 4; x <= b + 5; x++) px(g, x, 30, C.deep);
  for (let x = b - 3; x <= b + 4; x++) px(g, x, 30, C.mid);
  for (let x = b - 1; x <= b + 2; x++) px(g, x, 30, C.foam);
  px(g, b - 5, 30, C.deep); px(g, b + 6, 30, C.deep);           // pool rim
  // stones straddling the outer bank of each bend; water foam breaks on them
  if (withStones) {
    const stones = [[cx(8) + 2, 6], [cx(16) - 8, 15], [cx(24) + 2, 23]];
    for (const [sx, sy] of stones) {
      stone(g, sx, sy);
      const inWater = sx < base;                     // which face meets the current
      const fx = inWater ? sx + 6 : sx - 1;
      px(g, fx, sy + 1, C.foam); px(g, fx, sy + 2, C.foam); px(g, fx, sy + 3, C.mid);
    }
  }
  // memory motes riding the current down the bends (spectrum)
  flowMote(g, cx(6),  6,  C.decision);
  flowMote(g, cx(13), 13, C.insight);
  flowMote(g, cx(20), 20, C.preference);
  flowMote(g, cx(27), 27, C.fact);
  return g;
}

// ── pixel wordmark "rill" ─────────────────────────────────────────────────
// 9 rows tall; lowercase, 2px stems. '#'=letter, 'o'=the i-dot mote, '.'=gap.
const GLYPHS = {
  r: ['....', '....', '....', '####', '####', '##..', '##..', '##..', '##..'],
  i: ['oo', 'oo', '..', '##', '##', '##', '##', '##', '##'],
  l: ['##', '##', '##', '##', '##', '##', '##', '##', '##'],
};
// blue→violet across the word, per-column (linear oklch lerp)
function gradAt(t) {
  const L = 0.70 + (0.56 - 0.70) * t, Ch = 0.19 + (0.20 - 0.19) * t, H = 255 + (290 - 255) * t;
  return `oklch(${L.toFixed(3)} ${Ch.toFixed(3)} ${H.toFixed(1)})`;
}
function wordmark(text = 'rill', gap = 2) {
  const glyphs = [...text].map((ch) => GLYPHS[ch]);
  const h = 9, width = glyphs.reduce((a, g) => a + g[0].length + gap, -gap);
  const g = grid(width, h);
  let ox = 0;
  for (const gl of glyphs) {
    const gw = gl[0].length;
    gl.forEach((row, y) => [...row].forEach((ch, x) => {
      if (ch === '#') px(g, ox + x, y, gradAt((ox + x) / (width - 1)));
      else if (ch === 'o') px(g, ox + x, y, C.decision);          // mote dot on the i
    }));
    ox += gw + gap;
  }
  return g;
}

// grid → scaled <rect> string for composing into a larger SVG (no AA)
function gridToRects(g, ox, oy, scale) {
  let r = '';
  for (let y = 0; y < g.length; y++) for (let x = 0; x < g[0].length; x++) {
    const c = g[y][x];
    if (c) r += `<rect x="${ox + x * scale}" y="${oy + y * scale}" width="${scale}" height="${scale}" fill="${c}"/>`;
  }
  return r;
}

// ── hero banner: stream mark + wordmark + tagline ─────────────────────────
function banner() {
  const W = 216, H = 72;
  const m = brook({ stones: true });           // 32×32
  const wm = wordmark('rill');                  // 13×9
  const tile = 50, tx = 12, ty = (H - tile) / 2;
  const mx = tx + (tile - 32) / 2, my = ty + (tile - 32) / 2;
  const ws = 3, wx = 80, wy = 16;               // wordmark ×3
  let s = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" shape-rendering="crispEdges">`;
  s += `<rect x="0" y="0" width="${W}" height="${H}" rx="12" fill="${C.bg}"/>`;
  s += `<rect x="0.5" y="0.5" width="${W - 1}" height="${H - 1}" rx="11.5" fill="none" stroke="${C.deep}" stroke-width="1"/>`;
  // icon tile + mark
  s += `<rect x="${tx}" y="${ty}" width="${tile}" height="${tile}" rx="11" fill="${C.tile}" stroke="${C.deep}" stroke-width="1"/>`;
  s += gridToRects(m, mx, my, 1);
  // wordmark + tagline
  s += gridToRects(wm, wx, wy, ws);
  s += `<text x="${wx + 1}" y="${wy + 9 * ws + 12}" shape-rendering="auto"
      font-family="'JetBrains Mono','Fira Code',ui-monospace,monospace" font-size="7"
      letter-spacing="0.2" fill="oklch(0.745 0.012 265)">a tiny stream, flowing memory</text>`;
  return s;
}

// ── favicon: a bolder stream tuned to survive 16px ────────────────────────
function favMark(N) {
  const g = grid(N, N);
  const base = N / 2, amp = N * 0.24, period = N * 0.92, ph = -0.5;
  const hw = Math.max(1, Math.round(N / 14));
  const cx = (y) => Math.round(base + amp * Math.sin((y / period) * 2 * Math.PI + ph));
  for (let y = 1; y < N - 1; y++) {
    const c = cx(y);
    for (let x = c - hw - (N >= 24 ? 1 : 0); x <= c + hw + (N >= 24 ? 1 : 0); x++) px(g, x, y, C.deep);
    for (let x = c - hw; x <= c + hw; x++) px(g, x, y, C.mid);
    px(g, c, y, C.acc); px(g, c + 1, y, C.acc);
  }
  const step = Math.max(3, Math.round(N / 5));
  for (let y = 2; y < N - 1; y += step) { px(g, cx(y), y, C.foam); if (N >= 24) px(g, cx(y) + 1, y, C.foam); }
  // 2 oversized motes that stay visible when tiny
  const big = (fy, col) => {
    const y = Math.round(fy * N), c = cx(y), s = N >= 24 ? 2 : 1;
    for (let dy = 0; dy < s; dy++) for (let dx = 0; dx < s; dx++) px(g, c + dx, y + dy, col);
  };
  big(0.28, C.decision); big(0.68, C.insight);
  return g;
}

// ── GitHub social card (1280×640 at ×4) ───────────────────────────────────
function ogcard() {
  const W = 320, H = 160;
  const m = brook({ stones: true });
  const wm = wordmark('rill');
  const tile = 64, ws = 4;
  const lock = tile + 18 + wm[0].length * ws;
  const lx = Math.round((W - lock) / 2), cy = 64;
  const tx = lx, ty = cy - tile / 2;
  const mx = tx + (tile - 32) / 2, my = ty + (tile - 32) / 2;
  const wx = tx + tile + 18, wy = cy - (9 * ws) / 2;
  let s = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" shape-rendering="crispEdges">`;
  s += `<rect width="${W}" height="${H}" fill="${C.bg}"/>`;
  // faint stream flowing across the foot, carrying the full spectrum
  const fbase = 134, amp = 3, per = 46;
  const fy = (x) => fbase + Math.round(amp * Math.sin((x / per) * 2 * Math.PI));
  let foot = '';
  for (let x = 8; x < W - 8; x++) {
    const y = fy(x);
    foot += `<rect x="${x}" y="${y}" width="1" height="2" fill="${C.deep}"/>`;
    foot += `<rect x="${x}" y="${y}" width="1" height="1" fill="${C.mid}"/>`;
  }
  const kinds = [C.decision, C.preference, C.insight, C.procedure, C.fact, C.identity, C.rule];
  kinds.forEach((col, i) => { const x = 30 + i * 38, y = fy(x) - 1; foot += `<rect x="${x}" y="${y}" width="2" height="2" fill="${col}"/><rect x="${x + 1}" y="${y - 1}" width="1" height="1" fill="${C.foam}"/>`; });
  s += foot;
  s += `<rect x="${tx}" y="${ty}" width="${tile}" height="${tile}" rx="14" fill="${C.tile}" stroke="${C.deep}" stroke-width="1"/>`;
  s += gridToRects(m, mx, my, 1);
  s += gridToRects(wm, wx, wy, ws);
  // tagline centered on the whole card, below the lockup
  s += `<text x="${W / 2}" y="${ty + tile + 22}" text-anchor="middle" shape-rendering="auto"
      font-family="'JetBrains Mono','Fira Code',ui-monospace,monospace" font-size="8"
      letter-spacing="0.6" fill="oklch(0.745 0.012 265)">a tiny stream, flowing memory</text>`;
  return s + '</svg>';
}

// ── render ────────────────────────────────────────────────────────────────
const SCALE = 20;
async function render(name, markup, w, h, scale = SCALE) {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: w * scale, height: h * scale }, deviceScaleFactor: 1 });
  const html = `<!doctype html><meta charset=utf8>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@500;700&display=swap" rel="stylesheet">
    <style>*{margin:0}html,body{background:transparent}
    svg{width:${w * scale}px;height:${h * scale}px;image-rendering:pixelated;display:block}
    text{image-rendering:auto}</style>${markup}`;
  await page.setContent(html);
  await page.evaluate(() => document.fonts && document.fonts.ready).catch(() => {});
  await page.locator('svg').screenshot({ path: `docs/brand/${name}.png`, omitBackground: true });
  await browser.close();
  writeFileSync(`docs/brand/${name}.svg`, markup);
  console.log('wrote', name);
}

await render('mark-clean', svg(brook({ stones: false }), { pad: 6, round: 12, bg: C.tile }), 44, 44);
await render('mark-stones', svg(brook({ stones: true }),  { pad: 6, round: 12, bg: C.tile }), 44, 44);
const wm = wordmark('rill');
await render('wordmark', svg(wm, { pad: 4, round: 4, bg: C.tile }), wm[0].length + 8, 9 + 8);
await render('banner', banner(), 216, 72, 6);
// favicons: native crisp at each size on the app tile
await render('favicon-32', svg(favMark(32), { pad: 0, round: 7, bg: C.tile }), 32, 32, 16);
await render('favicon-16', svg(favMark(16), { pad: 0, round: 3, bg: C.tile }), 16, 16, 32);
await render('og-card', ogcard(), 320, 160, 4);
