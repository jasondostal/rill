package server

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// frontendFS embeds the production SvelteKit build. The Dockerfile copies
// frontend/build/ over this directory's contents before `go build`, so
// the embedded files are the real production bundle. For local development,
// the committed placeholder index.html serves as a stub — the operator runs
// `vite dev` in frontend/ for HMR-driven UI work and this static handler is
// effectively ignored.
//
// `all:` prefix is required so files starting with `_` (SvelteKit's
// internal asset directory `_app/`) are included.
//
//go:embed all:webui
var frontendFS embed.FS

// staticHandler returns an http.Handler that serves the embedded SvelteKit
// SPA. Unknown paths fall back to index.html so client-side routing works
// (a direct hit to /memories returns the SPA shell; the router takes over).
//
// Caching:
//   - /_app/immutable/* are content-hashed by Vite — safe to cache forever.
//   - Everything else (index.html, *.html) is no-cache so deploys land
//     immediately for users with stale tabs.
//
// CSP: the global middleware sets a strict default-src/script-src 'self',
// which would block SvelteKit's inline bootstrap <script> in index.html
// (sets __sveltekit_*, then import()s entry chunks). On HTML responses we
// override the CSP with one that allows that exact inline script via a
// SHA-256 hash computed from the embedded shell at startup. API/JSON
// responses keep the strict middleware CSP.
func staticHandler() (http.Handler, error) {
	sub, err := fs.Sub(frontendFS, "webui")
	if err != nil {
		return nil, err
	}
	server := http.FileServer(http.FS(sub))

	htmlCSP, err := buildHTMLCSP(sub)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Aggressive cache for immutable chunks.
		if strings.HasPrefix(r.URL.Path, "/_app/immutable/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		// Look up the requested path. Existing files go through FileServer
		// (handles Range, Content-Type, ETag). Missing paths serve the SPA
		// shell directly — NOT through FileServer, because FileServer would
		// issue a canonical-URL redirect (e.g. /a/b/ → /a/b/) creating a
		// loop. Reading the embedded index.html and writing it ourselves
		// keeps the response a clean 200 with the SPA shell body.
		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		if reqPath != "" {
			if _, err := fs.Stat(sub, reqPath); err == nil {
				server.ServeHTTP(w, r)
				return
			}
		}
		// Unknown /api/* and /mcp paths shouldn't fall through to the SPA
		// shell — operators hitting a misspelled endpoint should see 404,
		// not HTML. SPA routes don't live under these prefixes.
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mcp") {
			http.NotFound(w, r)
			return
		}
		f, err := sub.Open("index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Security-Policy", htmlCSP)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
	}), nil
}

// buildHTMLCSP reads the embedded index.html, computes the SHA-256 hash of
// each inline <script>...</script> body, and returns a CSP string that
// allows exactly those scripts (plus 'self' for the chunk imports). The
// hashes are deterministic per-build: SvelteKit content-hashes the entry
// chunk filenames into the inline bootstrap, so any change to the bundle
// produces a new hash here too — no manual sync required.
func buildHTMLCSP(sub fs.FS) (string, error) {
	b, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return "", err
	}
	hashes := extractScriptHashes(string(b))
	scriptSrc := "'self'"
	for _, h := range hashes {
		scriptSrc += " '" + h + "'"
	}
	return "default-src 'self'; " +
		"script-src " + scriptSrc + "; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"object-src 'none'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'", nil
}

// extractScriptHashes returns CSP-formatted SHA-256 hashes (sha256-<b64>) for
// each inline <script>...</script> body in the input HTML. Only matches
// inline scripts — `<script src="...">` is skipped because its bytes aren't
// the script body. Naive parsing is fine here: the input is SvelteKit's own
// generated shell, not arbitrary HTML.
func extractScriptHashes(html string) []string {
	var hashes []string
	s := html
	for {
		i := strings.Index(s, "<script")
		if i < 0 {
			return hashes
		}
		s = s[i+len("<script"):]
		end := strings.Index(s, ">")
		if end < 0 {
			return hashes
		}
		// Skip <script src=...> tags — they're external loads, not inline.
		if strings.Contains(s[:end], " src=") {
			s = s[end+1:]
			continue
		}
		s = s[end+1:]
		j := strings.Index(s, "</script>")
		if j < 0 {
			return hashes
		}
		body := s[:j]
		sum := sha256.Sum256([]byte(body))
		hashes = append(hashes, "sha256-"+base64.StdEncoding.EncodeToString(sum[:]))
		s = s[j+len("</script>"):]
	}
}
