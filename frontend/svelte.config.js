import adapter from '@sveltejs/adapter-static';

// Static build: the SvelteKit app is a SPA fronted by Go's API. No SSR, no
// server endpoints, no per-request dynamic data at build time. Output is
// plain HTML/CSS/JS that the Go binary embeds via //go:embed.
//
// fallback: 'index.html' enables client-side routing — a direct hit to
// /memory/abc123 returns index.html, SvelteKit's router takes over, and
// the page fetches its data via /api/*. Without fallback, the file server
// would 404 on every deep link.
//
// strict: false because some pages aren't prerenderable as discrete HTML
// files (the SPA shell covers them at runtime); without it the static
// adapter complains.
/** @type {import('@sveltejs/kit').Config} */
const config = {
  kit: {
    adapter: adapter({
      fallback: 'index.html',
      strict: false
    }),
    alias: {
      '@/*': './src/*'
    },
    // Dynamic routes (/entities/[type]/[slug], /memories/[id]) are SPA-rendered
    // at runtime — they shouldn't be prerendered as discrete HTML files. The
    // fallback (index.html) serves the SPA shell; SvelteKit's router takes over.
    prerender: {
      handleUnseenRoutes: 'ignore'
    }
  }
};

export default config;
