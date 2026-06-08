// Markdown rendering for the document reader.
//
// marked parses GFM markdown → HTML; DOMPurify sanitizes it before it hits
// {@html}. DOMPurify needs a DOM, so this is browser-only and returns '' during
// SSR (the document reader fetches via /api and renders client-side anyway).
//
// CSP note: the global middleware allows script-src 'self' (marked/DOMPurify
// ship in the app bundle), style-src 'unsafe-inline' (markdown inline styles ok),
// and img-src 'self' data: (external images won't load — acceptable hardening).
import { marked } from 'marked';
import DOMPurify from 'dompurify';

marked.setOptions({ gfm: true, breaks: false });

export function renderMarkdown(md) {
  if (typeof window === 'undefined' || !md) return '';
  const raw = marked.parse(md, { async: false });
  return DOMPurify.sanitize(raw);
}
