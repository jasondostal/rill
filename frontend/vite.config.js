import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// Allowed-hosts: defaults cover localhost only. Operator-specific public
// hostnames AND LAN IPs (e.g. rill.example.com behind SWAG, or a dev box's
// LAN IP) come from VITE_ALLOWED_HOSTS as a comma-separated list so the
// deployment config travels with the env, not the repo.
const extraHosts = (process.env.VITE_ALLOWED_HOSTS || '')
  .split(',').map(s => s.trim()).filter(Boolean);

const isProd = process.env.NODE_ENV === 'production';
const devUsername = process.env.RILL_DEV_USERNAME;

export default defineConfig({
  plugins: [
    sveltekit(),
    tailwindcss()
  ],
  server: {
    allowedHosts: ['localhost', ...extraHosts],
    proxy: {
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq, req) => {
            // Pass through the proxy auth username header from the incoming
            // request (a trusted reverse proxy sets it in prod). The header
            // name matches what the backend trusts via RILL_AUTH_PROXY_HEADER
            // (default X-Forwarded-User; override in your dev env if your
            // upstream proxy uses something else). In dev, only inject when
            // RILL_DEV_USERNAME is set — no implicit default identity.
            const headerName = (process.env.RILL_DEV_PROXY_HEADER || 'X-Forwarded-User');
            const incoming = req.headers && req.headers[headerName.toLowerCase()];
            if (incoming && String(incoming).trim() !== '') {
              proxyReq.setHeader(headerName, String(incoming));
            } else if (!isProd && devUsername && devUsername.trim() !== '') {
              proxyReq.setHeader(headerName, devUsername);
            }
          });
        },
      }
    }
  }
});
