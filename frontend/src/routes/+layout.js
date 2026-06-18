// SPA mode. We're not using SvelteKit's server-side rendering — all dynamic
// data comes from Go's /api/* endpoints at runtime. Prerender produces a
// single index.html shell that's embedded in the Go binary; ssr off keeps
// the build deterministic and the runtime simple.
//
// Trust mode is the same as production: the user is authenticated by the
// reverse proxy / SSO before the SPA even loads, then bearer/session
// continues from there. No SvelteKit-side auth.
export const prerender = true;
export const ssr = false;
