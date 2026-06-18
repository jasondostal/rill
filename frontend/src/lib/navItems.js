// Single source of truth for section icons + sidebar nav. Both the sidebar and
// each page's <PageHeader> pull from here, so a section's glyph is identical
// everywhere — change it once and it updates the nav and the page header together.
import LayoutDashboard from '@lucide/svelte/icons/layout-dashboard';
import Network from '@lucide/svelte/icons/network';
import Brain from '@lucide/svelte/icons/brain';
import FileText from '@lucide/svelte/icons/file-text';
import Search from '@lucide/svelte/icons/search';
import Compass from '@lucide/svelte/icons/compass';
import Palette from '@lucide/svelte/icons/palette';
import Settings from '@lucide/svelte/icons/settings';

export const icons = {
  dashboard: LayoutDashboard,
  entities: Network,
  memories: Brain,
  documents: FileText,
  search: Search,
  orient: Compass,
  system: Palette,
  settings: Settings,
};

// Sidebar order + active-path matcher. `match(path)` decides the active
// highlight; Dashboard owns the root path.
export const navItems = [
  { href: '/', label: 'Dashboard', section: 'dashboard', match: (p) => p === '/' },
  { href: '/entities', label: 'Entities', section: 'entities', match: (p) => p.startsWith('/entities') },
  { href: '/memories', label: 'Memories', section: 'memories', match: (p) => p.startsWith('/memories') },
  { href: '/documents', label: 'Documents', section: 'documents', match: (p) => p.startsWith('/documents') },
  { href: '/search', label: 'Search', section: 'search', match: (p) => p.startsWith('/search') },
  { href: '/orient', label: 'Orient', section: 'orient', match: (p) => p.startsWith('/orient') },
  { href: '/system', label: 'Color System', section: 'system', match: (p) => p.startsWith('/system') },
  { href: '/settings', label: 'Settings', section: 'settings', match: (p) => p === '/settings' },
];
