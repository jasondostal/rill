package auth

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newProxyManager builds a *Manager with the given trusted CIDRs/IPs without
// touching the database — isTrustedProxy is pure on the trustedProxies slice.
// Mirrors the parsing NewManager does for RILL_TRUSTED_PROXY_IPS so the tests
// exercise the same kinds of *net.IPNet entries that production builds.
func newProxyManager(t *testing.T, entries ...string) *Manager {
	t.Helper()
	m := &Manager{Mode: ModeLocal, proxyHeader: "X-Forwarded-User"}
	for _, e := range entries {
		_, ipnet, err := net.ParseCIDR(e)
		if err != nil {
			ip := net.ParseIP(e)
			if ip == nil {
				t.Fatalf("invalid trusted proxy fixture %q: %v", e, err)
			}
			if ip4 := ip.To4(); ip4 != nil {
				ipnet = &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
			} else {
				ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
			}
		}
		m.trustedProxies = append(m.trustedProxies, ipnet)
	}
	return m
}

func mkReq(remoteAddr string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	return r
}

func TestIsTrustedProxy_EmptyListAlwaysFalse(t *testing.T) {
	m := newProxyManager(t)
	if m.isTrustedProxy(mkReq("127.0.0.1:1234")) {
		t.Errorf("empty trustedProxies must never trust any source")
	}
	if m.isTrustedProxy(mkReq("10.0.0.5:80")) {
		t.Errorf("empty trustedProxies must never trust any source")
	}
}

func TestIsTrustedProxy_CIDRMatch(t *testing.T) {
	m := newProxyManager(t, "10.0.0.0/8")
	cases := []struct {
		remote string
		want   bool
	}{
		{"10.0.0.5:443", true},
		{"10.255.255.254:9090", true},
		{"11.0.0.1:443", false},
		{"192.0.2.5:443", false},
	}
	for _, c := range cases {
		got := m.isTrustedProxy(mkReq(c.remote))
		if got != c.want {
			t.Errorf("isTrustedProxy(%q) = %v, want %v", c.remote, got, c.want)
		}
	}
}

func TestIsTrustedProxy_SingleIPEntry(t *testing.T) {
	m := newProxyManager(t, "192.0.2.32")
	if !m.isTrustedProxy(mkReq("192.0.2.32:55555")) {
		t.Errorf("exact single-IP match must be trusted")
	}
	if m.isTrustedProxy(mkReq("192.0.2.33:55555")) {
		t.Errorf("neighbor IP must NOT be trusted by a single-IP entry")
	}
}

func TestIsTrustedProxy_MixedCIDRAndSingle(t *testing.T) {
	m := newProxyManager(t, "10.0.0.0/8", "192.0.2.32")
	cases := []struct {
		remote string
		want   bool
	}{
		{"10.5.5.5:80", true},      // CIDR hit
		{"192.0.2.32:80", true},    // single hit
		{"192.0.2.31:80", false},   // off by one
		{"172.16.0.1:80", false},
	}
	for _, c := range cases {
		got := m.isTrustedProxy(mkReq(c.remote))
		if got != c.want {
			t.Errorf("mixed-list isTrustedProxy(%q) = %v, want %v", c.remote, got, c.want)
		}
	}
}

func TestIsTrustedProxy_MalformedRemoteAddr_HostOnly(t *testing.T) {
	// `host` without `:port` should still parse as IP when valid — that's the
	// safety net (the SplitHostPort branch fall-through).
	m := newProxyManager(t, "10.0.0.0/8")
	if !m.isTrustedProxy(mkReq("10.0.0.1")) {
		t.Errorf("RemoteAddr without port should still resolve when IP is valid")
	}
}

func TestIsTrustedProxy_MalformedRemoteAddr_Garbage(t *testing.T) {
	m := newProxyManager(t, "10.0.0.0/8")
	// Garbage that is neither host:port nor a valid IP must return false —
	// this is the spoof-prevention guard.
	if m.isTrustedProxy(mkReq("not-an-ip")) {
		t.Errorf("garbage RemoteAddr must NOT be trusted")
	}
	if m.isTrustedProxy(mkReq("")) {
		t.Errorf("empty RemoteAddr must NOT be trusted")
	}
}

func TestIsTrustedProxy_IPv6(t *testing.T) {
	m := newProxyManager(t, "::1/128", "2001:db8::/32")
	cases := []struct {
		remote string
		want   bool
	}{
		{"[::1]:443", true},
		{"[2001:db8::5]:443", true},
		{"[2001:dead::1]:443", false},
	}
	for _, c := range cases {
		got := m.isTrustedProxy(mkReq(c.remote))
		if got != c.want {
			t.Errorf("IPv6 isTrustedProxy(%q) = %v, want %v", c.remote, got, c.want)
		}
	}
}

func TestIsTrustedProxy_IPv4MappedIPv6(t *testing.T) {
	// An IPv4-mapped IPv6 address (::ffff:10.0.0.5) should match an IPv4 CIDR.
	// This is the spoof vector to be aware of: net.ParseIP normalizes the
	// address, and net.IPNet.Contains handles the mapping.
	m := newProxyManager(t, "10.0.0.0/8")
	r := mkReq("[::ffff:10.0.0.5]:443")
	if !m.isTrustedProxy(r) {
		t.Errorf("IPv4-mapped IPv6 ::ffff:10.0.0.5 should match 10.0.0.0/8")
	}
}

// TestMiddleware_ProxyHeaderNotHonoredOnMCP is the regression guard for the
// proxy-header-auth-bypass incident: even from a trusted proxy IP, a forged
// proxy identity header must NOT authenticate a request to the MCP endpoints
// (those bypass SSO at the edge and must require bearer or session).
func TestMiddleware_ProxyHeaderNotHonoredOnMCP(t *testing.T) {
	m := newProxyManager(t, "192.0.2.32")
	m.proxyHeader = "X-Forwarded-User"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(IdentityFromContext(r.Context()).Type))
	})
	h := m.Middleware()(next)

	for _, path := range []string{"/mcp", "/api/mcp"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "192.0.2.32:55555" // trusted proxy source
		req.Header.Set("X-Forwarded-User", "spoofed-admin")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with forged proxy header: status = %d, want 401 (proxy auth must not apply to MCP)", path, rec.Code)
		}
	}
}

// TestMiddleware_ProxyHeaderHonoredOnNonMCP confirms the legitimate browser-SSO
// path still works on the web UI / REST surface after the MCP carve-out.
func TestMiddleware_ProxyHeaderHonoredOnNonMCP(t *testing.T) {
	m := newProxyManager(t, "192.0.2.32")
	m.proxyHeader = "X-Forwarded-User"

	var gotType string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = IdentityFromContext(r.Context()).Type
		w.WriteHeader(http.StatusOK)
	})
	h := m.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/api/tokens", nil)
	req.RemoteAddr = "192.0.2.32:55555"
	req.Header.Set("X-Forwarded-User", "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/api/tokens with trusted proxy header: status = %d, want 200", rec.Code)
	}
	if gotType != "proxy" {
		t.Errorf("identity type = %q, want proxy (browser-SSO path must still work on REST)", gotType)
	}
}
