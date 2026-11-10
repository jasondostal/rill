package auth

import "testing"

// The regression these guard: an X-Forwarded-For value the CALLER sent must
// never decide the source IP. Proxies append, so the caller's value sits on
// the left and our proxy's observation on the right.
func TestResolveClientIP(t *testing.T) {
	trusted := ParseTrustedProxies([]string{"10.20.0.0/16", "127.0.0.1", "::1", " ", "not-an-ip"})
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct, no proxy", "203.0.113.9:4000", "", "203.0.113.9"},
		{"untrusted peer's header is ignored", "203.0.113.9:4000", "127.0.0.1", "203.0.113.9"},
		{"proxy reports the client", "10.20.0.4:5000", "198.51.100.7", "198.51.100.7"},
		{"caller-supplied loopback on the left loses", "10.20.0.4:5000", "127.0.0.1, 198.51.100.7", "198.51.100.7"},
		{"caller-supplied junk on the left loses", "10.20.0.4:5000", "evil, 10.9.9.9, 198.51.100.7", "198.51.100.7"},
		{"two trusted hops", "10.20.0.4:5000", "198.51.100.7, 10.20.0.9", "198.51.100.7"},
		{"all trusted: nearest observation, not the leftmost claim", "10.20.0.4:5000", "127.0.0.1, 10.20.0.1", "10.20.0.1"},
		{"same-host proxy, genuinely local client", "127.0.0.1:5000", "127.0.0.1", "127.0.0.1"},
		{"same-host proxy, remote client", "127.0.0.1:5000", "198.51.100.7", "198.51.100.7"},
		{"garbage only", "10.20.0.4:5000", "not an ip", "10.20.0.4"},
		{"trusted proxy, no header", "10.20.0.4:5000", "", "10.20.0.4"},
		{"remote addr without port", "203.0.113.9", "", "203.0.113.9"},
	}
	for _, c := range cases {
		if got := ResolveClientIP(c.remote, c.xff, trusted); got != c.want {
			t.Errorf("%s: ResolveClientIP(%q, %q) = %q, want %q", c.name, c.remote, c.xff, got, c.want)
		}
	}
}

func TestRedirectHostAllowed(t *testing.T) {
	open := &MCPOAuthManager{}
	if !open.redirectHostAllowed("https://anything.example/cb") {
		t.Error("no allowlist configured should allow any host")
	}
	m := &MCPOAuthManager{redirectHosts: parseRedirectHosts(" Claude.ai , example.com,, ")}
	for uri, want := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback": true,
		"https://CLAUDE.AI/cb":                    true,
		"https://example.com:8443/cb":             true,
		"https://evil.example/cb":                 false,
		"https://claude.ai.evil.example/cb":       false,
		"https://sub.claude.ai/cb":                false,
		"https://evil.example/?x=claude.ai":       false,
		"https://claude.ai@evil.example/cb":       false,
		"::not a url":                             false,
	} {
		if got := m.redirectHostAllowed(uri); got != want {
			t.Errorf("redirectHostAllowed(%q) = %v, want %v", uri, got, want)
		}
	}
}
