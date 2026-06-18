// Package auth provides bearer token authentication for Rill's MCP server.
// Multi-mode: local (built-in login) or proxy (trusted reverse proxy).
// Bearer tokens (PATs) are always active for MCP agents and CLI.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
)

// Mode controls how humans authenticate to the web UI.
// Bearer tokens are always active regardless of mode.
type Mode string

const (
	ModeLocal Mode = "local"
	ModeProxy Mode = "proxy" // renamed from "sso"
	ModeOIDC  Mode = "oidc"
)

// Identity carries the authenticated caller's info on the request context.
type Identity struct {
	Type    string   // "bearer", "session", "proxy"
	Name    string   // token name, username, or proxy header value
	Source  string   // remote addr for audit
	Scopes  []string // token scopes (bearer only)
	TokenID string   // DB record ID of the auth_token row (bearer only)
}

type ctxKey struct{}

var identityCtxKey = ctxKey{}

// IdentityFromContext returns the auth identity from the request context,
// or zero-value Identity if unauthenticated.
func IdentityFromContext(ctx context.Context) Identity {
	if id, ok := ctx.Value(identityCtxKey).(Identity); ok {
		return id
	}
	return Identity{}
}

// WithIdentity injects an identity into a context for testing.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey, id)
}

// Manager handles token creation, validation, revocation, sessions, and auth middleware.
type Manager struct {
	db             *db.DB
	Mode           Mode
	trustedProxies []*net.IPNet
	proxyHeader    string
}

// NewManager creates a new auth manager for the given mode.
func NewManager(d *db.DB, mode Mode) *Manager {
	if mode != ModeLocal && mode != ModeProxy && mode != ModeOIDC {
		panic(fmt.Sprintf("invalid auth mode: %q", mode))
	}
	m := &Manager{
		db:          d,
		Mode:        mode,
		proxyHeader: "X-Forwarded-User",
	}
	if h := os.Getenv("RILL_AUTH_PROXY_HEADER"); h != "" {
		m.proxyHeader = h
	}
	for _, cidr := range strings.Split(os.Getenv("RILL_TRUSTED_PROXY_IPS"), ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			if ip := net.ParseIP(cidr); ip != nil {
				if ip4 := ip.To4(); ip4 != nil {
					ipnet = &net.IPNet{IP: ip4, Mask: net.CIDRMask(32, 32)}
				} else {
					ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
				}
			} else {
				rilllog.Logger().Warn("auth: ignoring invalid trusted proxy IP/CIDR", "cidr", cidr)
				continue
			}
		}
		m.trustedProxies = append(m.trustedProxies, ipnet)
	}
	if len(m.trustedProxies) > 0 {
		rilllog.Logger().Info("auth: trusting proxy header", "header", m.proxyHeader, "trusted_ip_count", len(m.trustedProxies))
	} else if os.Getenv("RILL_AUTH_PROXY_HEADER") != "" {
		rilllog.Logger().Warn("auth: RILL_AUTH_PROXY_HEADER set but RILL_TRUSTED_PROXY_IPS is empty; proxy auth is insecure")
	}
	if m.Mode == ModeProxy && len(m.trustedProxies) == 0 {
		rilllog.Logger().Warn("auth: proxy mode enabled but no trusted proxy IPs configured")
	}
	return m
}

// DB returns the underlying database connection.
func (m *Manager) DB() *db.DB { return m.db }

// ProxyEnabled reports whether trusted-proxy header auth is configured.
// True when both RILL_AUTH_PROXY_HEADER and RILL_TRUSTED_PROXY_IPS are set.
// Used by /api/auth/status to tell the UI whether to render an
// "authenticated via reverse proxy" banner.
func (m *Manager) ProxyEnabled() bool {
	return m.proxyHeader != "" && len(m.trustedProxies) > 0
}

// withIdentity stashes the identity on the request context.
func (m *Manager) withIdentity(r *http.Request, kind, name string, scopes []string, tokenID string) *http.Request {
	id := Identity{Type: kind, Name: name, Source: r.RemoteAddr, Scopes: scopes, TokenID: tokenID}
	return r.WithContext(context.WithValue(r.Context(), identityCtxKey, id))
}

// Token represents a Personal Access Token.
type Token struct {
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name"`
	Token     string    `json:"token,omitempty"` // only returned on creation
	TokenHash string    `json:"-"`
	Scopes    []string  `json:"scopes,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Revoked   bool      `json:"revoked"`
}

func hashToken(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// CreateToken generates a new PAT and stores it. Optional ttl parses as a duration.
// Rejects nil or empty scopes — every token must have explicit permissions.
func (m *Manager) CreateToken(ctx context.Context, name string, scopes []string, ttl time.Duration) (*Token, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("token scopes cannot be empty")
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	tokenStr := "rill_v1_" + hex.EncodeToString(bytes)

	now := time.Now().UTC()
	data := map[string]any{
		"name":       name,
		"token_hash": hashToken(tokenStr),
		"created_at": now,
		"last_used":  now,
		"revoked":    false,
		"scopes":     scopes,
	}
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
		data["expires_at"] = expiresAt
	}

	record, err := m.db.Create(ctx, "auth_token", data)
	if err != nil {
		return nil, fmt.Errorf("store token: %w", err)
	}

	id := db.RecordID(record)
	rilllog.Logger().Info("auth: created token", "token_id", id, "name", name, "scopes", scopes)

	return &Token{
		ID:        id,
		Name:      name,
		Token:     tokenStr,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		Scopes:    scopes,
	}, nil
}

// ListTokens returns all non-revoked tokens.
func (m *Manager) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := m.db.Query(ctx,
		"SELECT * FROM auth_token WHERE revoked = false ORDER BY created_at DESC", nil)
	if err != nil {
		return nil, err
	}

	var tokens = make([]Token, 0)
	for _, r := range rows {
		t := mapToToken(r)
		t.Token = ""
		if t.ID != "" {
			tokens = append(tokens, *t)
		}
	}
	return tokens, nil
}

// RevokeToken marks a token as revoked.
func (m *Manager) RevokeToken(ctx context.Context, id string) error {
	if err := db.RequireTable(id, "auth_token"); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	_, err := m.db.QueryRecord(ctx, "UPDATE %s SET revoked = true", id, nil)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	rilllog.Logger().Info("auth: revoked token", "token_id", id)
	return nil
}

// OIDCConfig holds OIDC provider settings.
type OIDCConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	PublicURL    string
}

// LoadOIDCConfig reads OIDC settings from environment.
func LoadOIDCConfig() OIDCConfig {
	cfg := OIDCConfig{
		Enabled: os.Getenv("RILL_OIDC_ENABLED") == "true",
	}
	if !cfg.Enabled {
		return cfg
	}
	cfg.Issuer = os.Getenv("RILL_OIDC_ISSUER")
	cfg.ClientID = os.Getenv("RILL_OIDC_CLIENT_ID")
	cfg.ClientSecret = os.Getenv("RILL_OIDC_CLIENT_SECRET")
	cfg.PublicURL = os.Getenv("RILL_PUBLIC_URL")
	if cfg.PublicURL == "" {
		cfg.PublicURL = "http://localhost:8080"
	}
	return cfg
}

// ValidateToken checks if a bearer token is valid and non-revoked.
// Returns the token name, scopes, token ID, and whether valid.
// Handles both PATs (rill_v1_*) and MCP OAuth tokens (rill_mcp_v1_*).
func (m *Manager) ValidateToken(ctx context.Context, tokenStr string) (name string, scopes []string, tokenID string, valid bool) {
	if strings.HasPrefix(tokenStr, "rill_mcp_v1_") {
		userID, scopes, tokenID, ok := m.ValidateMCPToken(ctx, tokenStr)
		if ok {
			return userID, scopes, tokenID, true
		}
		return "", nil, "", false
	}
	if !strings.HasPrefix(tokenStr, "rill_v1_") {
		return "", nil, "", false
	}
	rows, err := m.db.Query(ctx,
		"SELECT id, name, last_used, expires_at, scopes FROM auth_token WHERE token_hash = $hash AND revoked = false LIMIT 1",
		map[string]any{"hash": hashToken(tokenStr)})
	if err != nil || len(rows) == 0 {
		return "", nil, "", false
	}

	// Expiry check. Use TimeField so we handle all three shapes the SurrealDB
	// Go driver returns: CustomDateTime, time.Time, and RFC3339 string.
	if exp := db.TimeField(rows[0], "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		return "", nil, "", false
	}

	id := db.RecordID(rows[0])
	if id != "" {
		// db.TimeField handles all three timestamp shapes the SurrealDB
		// driver returns; .(string) alone misses CustomDateTime.
		if lu := db.TimeField(rows[0], "last_used"); !lu.IsZero() && time.Since(lu) > time.Hour {
			if _, err := m.db.QueryRecord(ctx, "UPDATE %s SET last_used = $now", id,
				map[string]any{"now": time.Now().UTC()}); err != nil {
				rilllog.Logger().Warn("token last_used update failed", "error", err)
			}
		}
	}

	var nameVal string
	nameVal, _ = rows[0]["name"].(string)
	var scopeList []string
	if sl, ok := rows[0]["scopes"].([]any); ok {
		for _, s := range sl {
			if ss, ok := s.(string); ok {
				scopeList = append(scopeList, ss)
			}
		}
	}
	return nameVal, scopeList, id, true
}

// EnsureToken creates a default admin token if none exist.
// Returns (token, nil) when created, (nil, nil) when tokens already exist.
//
// The bootstrap token expires in 7 days. The operator is expected to log in,
// create a proper PAT, and revoke this one well before that. After 7 days
// the token stops working — by then the initial-admin-token file should be
// deleted from disk too.
func (m *Manager) EnsureToken(ctx context.Context) (*Token, error) {
	tokens, err := m.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 {
		return nil, nil
	}

	t, err := m.CreateToken(ctx, "default-admin", []string{"read", "write", "admin"}, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// SourceIP returns the real client IP for an incoming request, resolving
// X-Forwarded-For when (and only when) the request originated from a
// trusted reverse-proxy IP. Falls back to RemoteAddr's host portion when
// no trusted proxy is configured or the request didn't come through one.
//
// Use this for: audit-log source_ip, session source_ip, localhost-only
// gates (e.g. handleSetup). The raw r.RemoteAddr is wrong when behind a
// reverse proxy — it's the proxy's IP, not the real client.
func (m *Manager) SourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !m.isTrustedProxy(r) {
		return host
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF is a comma-separated list left-to-right: [client, proxy1, proxy2, ...].
		// We only honor the leftmost (real client) when the request came from
		// a trusted proxy. Multi-hop XFF chains beyond that are not validated.
		if comma := strings.Index(xff, ","); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	return host
}

// isTrustedProxy returns true if the request originates from a trusted reverse proxy IP.
func (m *Manager) isTrustedProxy(r *http.Request) bool {
	if len(m.trustedProxies) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range m.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Middleware returns an HTTP middleware that enforces authentication.
// Auth is always enabled — mode controls which auth methods are checked.
// Bearer tokens are checked first; an explicit Authorization header always wins.
func (m *Manager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Unauthenticated routes.
			if r.URL.Path == "/health" ||
				strings.HasPrefix(r.URL.Path, "/.well-known") ||
				r.URL.Path == "/api/auth/login" ||
				r.URL.Path == "/api/auth/logout" ||
				r.URL.Path == "/api/auth/setup" ||
				r.URL.Path == "/api/auth/oidc/login" ||
				r.URL.Path == "/api/auth/oidc/callback" ||
				r.URL.Path == "/api/auth/status" {
				next.ServeHTTP(w, r)
				return
			}

			// 1. Bearer first — when explicitly present, validate and stop.
			if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
				if name, scopes, tokenID, ok := m.ValidateToken(r.Context(), tokenStr); ok {
					next.ServeHTTP(w, m.withIdentity(r, "bearer", name, scopes, tokenID))
					return
				}
				// Failed bearer — log attempted identity for forensics.
				h := hashToken(tokenStr)
				m.recordFailedAuth(r, "bearer-failed", "bearer:"+h[:12])
				http.Error(w, `{"error":"invalid or revoked token"}`, http.StatusUnauthorized)
				return
			}

			// 2. Session cookie. Works in any mode — the cookie is set by
			// either local /api/auth/login or OIDC callback. Accept any valid
			// identity at runtime; Mode only controls which login UI elements
			// the frontend exposes.
			//
			// Session and proxy identities represent "an authenticated human
			// at the keyboard" — they implicitly get full scopes including
			// admin. The operator's settings page (token management, OAuth
			// client revocation) lives behind a session cookie and would 403
			// without admin here. PATs are still least-privilege: an agent
			// holding a bearer token only gets the scopes declared at
			// creation. When RBAC arrives, session/proxy scopes will be
			// derived from the user's role rather than blanket-granted.
			if user, ok := m.ValidateSession(r); ok {
				next.ServeHTTP(w, m.withIdentity(r, "session", user, []string{"read", "write", "admin"}, ""))
				return
			}

			// 3. Proxy header (browser-SSO fallback for the web UI + REST).
			// Works whenever a trusted-proxy header is configured, regardless of
			// Mode — lets a trusted reverse proxy (oauth2-proxy, Cloudflare
			// Access, Caddy forward_auth, Traefik forwardAuth, etc.)
			// authenticate users in parallel with local password / OIDC session.
			//
			// SECURITY: never honored on the MCP endpoints. /mcp and /api/mcp
			// deliberately bypass SSO at the reverse proxy so bearer-token
			// agents can reach them — which means a client-supplied proxy header
			// would otherwise be trusted here and mint full-admin access (see
			// the proxy-header-auth-bypass tracker). MCP callers must present a
			// bearer token or a session cookie. The reverse-proxy config also
			// strips these headers; this is the defense-in-depth half so a
			// proxy-conf regression cannot reopen the bypass.
			isMCPPath := strings.HasPrefix(r.URL.Path, "/mcp") || strings.HasPrefix(r.URL.Path, "/api/mcp")
			if !isMCPPath && m.proxyHeader != "" && len(m.trustedProxies) > 0 {
				if proxyUser := r.Header.Get(m.proxyHeader); proxyUser != "" && m.isTrustedProxy(r) {
					rilllog.Logger().Info("auth: trusted proxy identity", "user", proxyUser, "remote_addr", r.RemoteAddr)
					next.ServeHTTP(w, m.withIdentity(r, "proxy", proxyUser, []string{"read", "write", "admin"}, ""))
					return
				}
			}

			http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		})
	}
}

// --- Local mode session stubs (Phase 2 will fill these in) ---

// ValidateSession is implemented in session.go.

// recordFailedAuth writes a minimal audit row for failed-auth attempts.
// Synchronous — failed auth is rare; the perf cost is irrelevant.
func (m *Manager) recordFailedAuth(r *http.Request, kind, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = m.db.Create(ctx, "auth_audit", map[string]any{
		"at":            time.Now().UTC(),
		"identity_type": kind,
		"identity_name": name,
		"source_ip":     m.SourceIP(r),
		"method":        r.Method,
		"path":          r.URL.Path,
		"status":        401,
		"outcome":       "denied",
	})
}

// RecordFailedAuth is the exported form for callers outside the auth package
// (e.g. the /api/auth/login handler) that need to attribute a failed-auth
// attempt to a specific username, not just an IP.
func (m *Manager) RecordFailedAuth(r *http.Request, kind, name string) {
	m.recordFailedAuth(r, kind, name)
}

// User represents a local auth user.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	OIDCSubject  string
	CreatedAt    time.Time
}

// GetOrCreateOIDCUser looks up or creates a user based on OIDC claims.
// Lookup by oidc_subject first. If new, create with username from PreferredUsername.
// If existing user matches by username but has no oidc_subject, link them (one-time migration).
func (m *Manager) GetOrCreateOIDCUser(ctx context.Context, claims *OIDCClaims) (*User, error) {
	// 1. Lookup by oidc_subject.
	if claims.Subject != "" {
		rows, err := m.db.Query(ctx,
			"SELECT id, username, password_hash, oidc_subject FROM auth_user WHERE oidc_subject = $sub LIMIT 1",
			map[string]any{"sub": claims.Subject})
		if err == nil && len(rows) > 0 {
			return mapToUser(rows[0]), nil
		}
	}

	username := claims.PreferredUsername
	if username == "" {
		username = claims.Email
	}
	if username == "" {
		return nil, fmt.Errorf("oidc: no preferred_username or email in claims")
	}

	// 2. Lookup by username — maybe a local user that needs linking.
	// Tightened: only auto-link if the existing row is a placeholder (no
	// password hash AND no oidc_subject yet). If the row has a local
	// password, refuse — an attacker who provisions an OIDC account with
	// the same preferred_username as a local admin could otherwise hijack
	// the account. Operator can override via a future `rill admin link`.
	rows, err := m.db.Query(ctx,
		"SELECT id, username, password_hash, oidc_subject FROM auth_user WHERE username = $user LIMIT 1",
		map[string]any{"user": username})
	if err == nil && len(rows) > 0 {
		user := mapToUser(rows[0])
		if user.OIDCSubject != "" {
			// Already linked to a different subject — return as-is.
			return user, nil
		}
		if user.PasswordHash != "" {
			return nil, fmt.Errorf("oidc: refusing to link OIDC subject to local password user %q; run `rill admin link` to override", username)
		}
		// Safe to auto-link: local row is an OIDC-provisioned placeholder
		// (no password, no subject yet).
		if claims.Subject != "" {
			if _, err := m.db.QueryRecord(ctx,
				"UPDATE %s SET oidc_subject = $sub", user.ID,
				map[string]any{"sub": claims.Subject}); err != nil {
				rilllog.Logger().Error("auth: failed to link oidc_subject", "user_id", user.ID, "error", err)
			} else {
				user.OIDCSubject = claims.Subject
			}
		}
		return user, nil
	}

	// 3. Create new user.
	record, err := m.db.Create(ctx, "auth_user", map[string]any{
		"username":      username,
		"password_hash": "", // no local password for OIDC-only users
		"oidc_subject":  claims.Subject,
	})
	if err != nil {
		return nil, fmt.Errorf("create oidc user: %w", err)
	}
	return mapToUser(record), nil
}

func mapToUser(m map[string]any) *User {
	u := &User{
		Username: strField(m, "username"),
	}
	u.ID = db.RecordID(m)
	if v, ok := m["password_hash"].(string); ok {
		u.PasswordHash = v
	}
	if v, ok := m["oidc_subject"].(string); ok {
		u.OIDCSubject = v
	}
	if ca, ok := m["created_at"].(string); ok {
		u.CreatedAt, _ = time.Parse(time.RFC3339, ca)
	}
	return u
}

func mapToToken(m map[string]any) *Token {
	t := &Token{
		Name: strField(m, "name"),
	}
	t.ID = db.RecordID(m)
	if v, ok := m["token_hash"].(string); ok {
		t.TokenHash = v
	}
	if v, ok := m["revoked"].(bool); ok {
		t.Revoked = v
	}
	// SurrealDB returns datetime as models.CustomDateTime, not string —
	// the previous string-only cast failed silently, leaving CreatedAt at
	// Go zero time. Frontend then rendered "Dec 31, 1" after locale shift.
	t.CreatedAt = db.TimeField(m, "created_at")
	t.LastUsed = db.TimeField(m, "last_used")
	t.ExpiresAt = db.TimeField(m, "expires_at")
	if sl, ok := m["scopes"].([]any); ok {
		for _, s := range sl {
			if ss, ok := s.(string); ok {
				t.Scopes = append(t.Scopes, ss)
			}
		}
	}
	return t
}

func strField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
