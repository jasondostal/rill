// Rill theme engine — every color is computed in OKLCH from a handful of knobs
// and written as CSS custom properties on :root. Components reference
// var(--token) only; never a literal. This is the SvelteKit port of the
// Claude Design prototype's theme.jsx — same tokens, same math.

import { prefs } from '$lib/prefs.js';

/** Build an oklch() string. h wraps to [0,360); a is optional alpha. */
export function ok(l, c, h, a) {
  const L = Math.max(0, Math.min(1, l)).toFixed(4);
  const C = Math.max(0, c).toFixed(4);
  const H = (((h % 360) + 360) % 360).toFixed(1);
  return a == null ? `oklch(${L} ${C} ${H})` : `oklch(${L} ${C} ${H} / ${a})`;
}

// Categorical hue anchors around the OKLCH wheel. The 7 memory kinds and 7
// entity types each get a fixed hue; palette-rotate spins them together.
export const KIND_HUES = {
  decision: 85, preference: 330, insight: 195, procedure: 255,
  fact: 150, identity: 300, rule: 30, idea: 110,
};
export const ENTITY_HUES = {
  person: 25, project: 255, tool: 150, organization: 300,
  place: 85, preference: 330, concept: 195,
};

// Canonical display order (matches the backend's ValidKinds / ValidEntityTypes).
export const KINDS = ['decision', 'preference', 'insight', 'procedure', 'fact', 'identity', 'rule', 'idea'];
export const ENTITY_TYPES = ['person', 'project', 'tool', 'organization', 'place', 'preference', 'concept'];

/** Write a fully-normalized theme param object onto :root. */
export function applyTheme(p) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  const set = (k, v) => root.style.setProperty(k, v);
  const light = p.mode === 'light';
  const bgH = p.bgH, bgC = p.bgC;

  // ----- neutral ramp -----
  if (light) {
    set('--bg', ok(p.bgL, bgC * 0.6, bgH));
    set('--surface', ok(Math.min(1, p.bgL + 0.03), bgC * 0.5, bgH));
    set('--surface-2', ok(Math.min(1, p.bgL + 0.05), bgC * 0.5, bgH));
    set('--surface-3', ok(p.bgL - 0.04, bgC * 0.7, bgH));
    set('--border', ok(p.bgL - 0.10, bgC * 0.8, bgH));
    set('--border-strong', ok(p.bgL - 0.22, bgC, bgH));
    set('--text', ok(0.26, 0.02, bgH));
    set('--text-dim', ok(0.44, 0.018, bgH));
    set('--text-faint', ok(0.58, 0.016, bgH));
  } else {
    set('--bg', ok(p.bgL, bgC, bgH));
    set('--surface', ok(p.bgL + 0.035, bgC, bgH));
    set('--surface-2', ok(p.bgL + 0.065, bgC, bgH));
    set('--surface-3', ok(p.bgL + 0.105, bgC * 1.1, bgH));
    set('--border', ok(p.bgL + 0.095, bgC * 0.9, bgH));
    set('--border-strong', ok(p.bgL + 0.185, bgC, bgH));
    set('--text', ok(0.965, 0.007, bgH));
    set('--text-dim', ok(0.745, 0.012, bgH));
    set('--text-faint', ok(0.555, 0.016, bgH));
  }

  // ----- accent -----
  set('--accent', ok(p.accentL, p.accentC, p.accentH));
  set('--accent-hi', ok(Math.min(0.92, p.accentL + 0.12), p.accentC * 0.9, p.accentH));
  set('--accent-fg', ok(0.985, 0.012, p.accentH));
  set('--accent-bg', ok(p.accentL, p.accentC, p.accentH, light ? 0.12 : 0.16));
  set('--accent-line', ok(p.accentL, p.accentC, p.accentH, 0.35));

  // ----- semantic status -----
  set('--destructive', ok(light ? 0.55 : 0.66, 0.20, 32));
  set('--destructive-bg', ok(0.66, 0.20, 32, 0.16));
  set('--warning', ok(light ? 0.62 : 0.80, 0.155, 80));
  set('--warning-bg', ok(0.80, 0.155, 80, 0.16));
  set('--success', ok(light ? 0.55 : 0.74, 0.15, 150));
  set('--muted', ok(light ? 0.62 : 0.60, 0.012, bgH));

  // ----- categorical: memory kinds + entity types -----
  // catL is the categorical lightness; presets may override it (the dark-neutral
  // preset uses a richer/darker 0.62 to match its palette). Defaults preserve prior behavior.
  const catL = p.catL ?? (light ? 0.55 : 0.745);
  const catC = p.catC;
  const apply = (prefix, map) => {
    for (const id in map) {
      const h = map[id] + p.hueShift;
      set(`--${prefix}-${id}`, ok(catL, catC, h));
      set(`--${prefix}-${id}-bg`, ok(catL, catC, h, light ? 0.13 : 0.16));
      set(`--${prefix}-${id}-soft`, ok(catL, catC, h, 0.30));
    }
  };
  apply('k', KIND_HUES);
  apply('e', ENTITY_HUES);

  set('--shadow', light
    ? '0 1px 2px oklch(0 0 0 / 0.08), 0 8px 24px oklch(0 0 0 / 0.06)'
    : '0 1px 2px oklch(0 0 0 / 0.4), 0 16px 40px oklch(0 0 0 / 0.45)');
  root.style.colorScheme = light ? 'light' : 'dark';
  root.setAttribute('data-theme', light ? 'light' : 'dark');

  // Let live views (e.g. the Color System page) re-read computed token values.
  try { window.dispatchEvent(new CustomEvent('rill-theme', { detail: p })); } catch (e) {}
}

// ----- curated presets ("color-tuned themes") -----
const DARK = { mode: 'dark', bgL: 0.175, bgC: 0.018, catC: 0.16, hueShift: 0 };
export const PRESETS = [
  { ...DARK, name: 'Electric', bgH: 265, accentL: 0.70, accentC: 0.19, accentH: 255 },
  { ...DARK, name: 'Deep Water', bgH: 220, bgC: 0.024, accentL: 0.74, accentC: 0.135, accentH: 196, catC: 0.155 },
  { ...DARK, name: 'Aurora', bgH: 165, bgC: 0.020, accentL: 0.78, accentC: 0.16, accentH: 158, catC: 0.17, hueShift: 24 },
  { ...DARK, name: 'Ember', bgH: 45, bgC: 0.020, accentL: 0.72, accentC: 0.165, accentH: 48, catC: 0.165, hueShift: -18 },
  // Pure-neutral near-black background (oklch 0.145 0 0, zero tint) with
  // high-chroma categoricals popping against it (--type-* run chroma 0.18-0.24
  // at lightness ~0.60), and a signature violet-magenta accent (hue 304).
  { ...DARK, name: 'Cairn', bgH: 0, bgL: 0.145, bgC: 0.0, accentL: 0.65, accentC: 0.22, accentH: 304, catC: 0.20, catL: 0.62 },
  { ...DARK, name: 'Violet', bgH: 292, bgC: 0.022, accentL: 0.66, accentC: 0.205, accentH: 300, catC: 0.165 },
  { ...DARK, name: 'Mono', bgH: 255, bgC: 0.010, accentL: 0.74, accentC: 0.045, accentH: 255, catC: 0.052 },
  { name: 'Daylight', mode: 'light', bgH: 255, bgL: 0.965, bgC: 0.012, accentL: 0.55, accentC: 0.18, accentH: 255, catC: 0.16, hueShift: 0 },
];

export const DEFAULT_THEME = { ...PRESETS[0] };

/** Find the preset whose knobs match a theme object (for active-state UI). */
export function activePresetName(t) {
  const p = PRESETS.find((p) =>
    p.bgH === t.bgH && p.accentH === t.accentH && p.mode === t.mode &&
    Math.abs(p.catC - t.catC) < 0.001 && (p.hueShift || 0) === (t.hueShift || 0));
  return p ? p.name : null;
}

/** Load the persisted theme (or the default). SSR-safe. */
export function loadTheme() {
  const stored = prefs.theme;
  return stored && typeof stored === 'object' ? { ...DEFAULT_THEME, ...stored } : { ...DEFAULT_THEME };
}

/** Persist + apply a theme. Also mirror it to the server so the theme follows
 *  the user across devices and the macOS sidecar. localStorage stays the
 *  instant/pre-paint source; the server write is best-effort (needs admin). */
export function saveTheme(t) {
  prefs.theme = t;
  applyTheme(t);
  if (typeof window !== 'undefined') {
    import('$lib/api.js')
      .then(({ api }) => api.updateSetting('appearance.theme', JSON.stringify(t)))
      .catch(() => {});
  }
}
