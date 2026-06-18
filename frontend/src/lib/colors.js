// Rill categorical color helpers. Colors themselves live as CSS custom
// properties on :root, written by the OKLCH theme engine ($lib/theme.js) — so
// everything retints live with the active theme. This module just maps a
// memory kind / entity type / project to the right var() reference, and owns
// the canonical display order.
//
// The 8 memory kinds and 7 entity types are the closed sets defined by the Go
// backend (internal/memory/types.go). Keep these in sync with that enum.

export const KINDS = ['decision', 'preference', 'insight', 'procedure', 'fact', 'identity', 'rule', 'idea'];
export const ENTITY_TYPES = ['person', 'project', 'tool', 'organization', 'place', 'preference', 'concept'];

/** CSS var for a memory kind's color (e.g. kindColor('fact') → 'var(--k-fact)'). */
export function kindColor(kind) {
  return KINDS.includes(kind) ? `var(--k-${kind})` : 'var(--muted)';
}
/** Translucent background variant for a kind badge fill. */
export function kindBg(kind) {
  return KINDS.includes(kind) ? `var(--k-${kind}-bg)` : 'var(--surface-2)';
}

/** CSS var for an entity type's color. */
export function entityColor(type) {
  return ENTITY_TYPES.includes(type) ? `var(--e-${type})` : 'var(--muted)';
}
export function entityBg(type) {
  return ENTITY_TYPES.includes(type) ? `var(--e-${type}-bg)` : 'var(--surface-2)';
}

// Projects don't have fixed hues (they're open-ended), so we borrow the entity
// palette by hashing the name to one of the 7 entity color vars — stable across
// sessions and theme changes.
const PROJECT_PALETTE = ENTITY_TYPES;
export function projectColor(name) {
  if (!name) return 'var(--muted)';
  let h = 0;
  for (let i = 0; i < name.length; i++) h = ((h << 5) - h + name.charCodeAt(i)) | 0;
  const idx = (((h % PROJECT_PALETTE.length) + PROJECT_PALETTE.length) % PROJECT_PALETTE.length);
  return `var(--e-${PROJECT_PALETTE[idx]})`;
}

export const GLOBAL_PROJECT = '__global__';
