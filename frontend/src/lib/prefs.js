// Shared page preferences — persists across navigation via localStorage.
// SSR-safe: returns defaults when localStorage is unavailable.

const KEY = 'rill-prefs';

function isBrowser() {
  return typeof window !== 'undefined' && typeof localStorage !== 'undefined';
}

function load() {
  if (!isBrowser()) return {};
  try { return JSON.parse(localStorage.getItem(KEY)) || {}; }
  catch { return {}; }
}

function save(p) {
  if (!isBrowser()) return;
  localStorage.setItem(KEY, JSON.stringify(p));
}

export const prefs = {
  get projects() { return load().projects || []; },
  set projects(v) { const p = load(); p.projects = v; save(p); },

  get project() { return this.projects[0] || ''; },
  set project(v) { const p = load(); p.project = v; save(p); },

  get dense() { return load().dense ?? true; },
  set dense(v) { const p = load(); p.dense = v; save(p); },

  get sort() { return load().sort || 'recent'; },
  set sort(v) { const p = load(); p.sort = v; save(p); },

  get memoriesTypeFilter() { return load().memoriesTypeFilter || ''; },
  set memoriesTypeFilter(v) { const p = load(); p.memoriesTypeFilter = v; save(p); },

  // Live OKLCH theme (knobs object — see $lib/theme.js). null = use default.
  get theme() { return load().theme || null; },
  set theme(v) { const p = load(); p.theme = v; save(p); },
};
