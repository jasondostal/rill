package auth

import (
	"context"
	"os"
	"testing"
)

func TestHashToken_DeterministicAndDistinct(t *testing.T) {
	a := hashToken("rill_v1_aaaa")
	b := hashToken("rill_v1_aaaa")
	c := hashToken("rill_v1_bbbb")

	if a != b {
		t.Errorf("hashToken not deterministic: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("hashToken collision for different inputs: %q", a)
	}
	if len(a) != 64 { // sha256 hex = 64 chars
		t.Errorf("hashToken length = %d, want 64 hex chars", len(a))
	}
}

func TestIdentityFromContext_RoundTrip(t *testing.T) {
	ctx := context.Background()
	want := Identity{
		Type:    "bearer",
		Name:    "test-token",
		Source:  "10.0.0.1:443",
		Scopes:  []string{"read", "write"},
		TokenID: "auth_token:abc",
	}
	ctx2 := WithIdentity(ctx, want)
	got := IdentityFromContext(ctx2)
	if got.Type != want.Type || got.Name != want.Name || got.TokenID != want.TokenID {
		t.Errorf("identity round-trip mismatch: got %+v want %+v", got, want)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "read" || got.Scopes[1] != "write" {
		t.Errorf("scopes lost in round-trip: %v", got.Scopes)
	}
}

func TestIdentityFromContext_EmptyOnMissing(t *testing.T) {
	id := IdentityFromContext(context.Background())
	if id.Type != "" || id.Name != "" || id.TokenID != "" || len(id.Scopes) != 0 {
		t.Errorf("expected zero-value Identity for unset context; got %+v", id)
	}
}

func TestProxyEnabled(t *testing.T) {
	// Empty trusted proxies → disabled.
	m1 := &Manager{proxyHeader: "X-Forwarded-User"}
	if m1.ProxyEnabled() {
		t.Errorf("ProxyEnabled with no trusted proxies should be false")
	}
	// Header set + at least one trusted proxy → enabled.
	m2 := newProxyManager(t, "10.0.0.0/8")
	m2.proxyHeader = "X-Forwarded-User"
	if !m2.ProxyEnabled() {
		t.Errorf("ProxyEnabled with trusted proxy + header should be true")
	}
	// Trusted proxies but empty header → disabled.
	m3 := newProxyManager(t, "10.0.0.0/8")
	m3.proxyHeader = ""
	if m3.ProxyEnabled() {
		t.Errorf("ProxyEnabled with empty header should be false")
	}
}

func TestStrField(t *testing.T) {
	m := map[string]any{
		"name":   "alice",
		"count":  42,
		"empty":  "",
		"nilval": nil,
	}
	if got := strField(m, "name"); got != "alice" {
		t.Errorf("strField name = %q, want alice", got)
	}
	if got := strField(m, "count"); got != "" {
		t.Errorf("strField on non-string should be empty, got %q", got)
	}
	if got := strField(m, "missing"); got != "" {
		t.Errorf("strField on missing key should be empty, got %q", got)
	}
	if got := strField(m, "empty"); got != "" {
		t.Errorf("strField on empty string should be empty, got %q", got)
	}
	if got := strField(m, "nilval"); got != "" {
		t.Errorf("strField on nil should be empty, got %q", got)
	}
}

func TestMapToToken_PopulatesAllFields(t *testing.T) {
	row := map[string]any{
		"id":         "auth_token:abc",
		"name":       "ci-bot",
		"token_hash": "deadbeef",
		"revoked":    true,
		"created_at": "2025-01-02T03:04:05Z",
		"last_used":  "2025-02-03T04:05:06Z",
		"scopes":     []any{"read", "write"},
	}
	tok := mapToToken(row)
	if tok.Name != "ci-bot" {
		t.Errorf("Name = %q, want ci-bot", tok.Name)
	}
	if tok.TokenHash != "deadbeef" {
		t.Errorf("TokenHash = %q, want deadbeef", tok.TokenHash)
	}
	if !tok.Revoked {
		t.Errorf("Revoked = false, want true")
	}
	if tok.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be parsed, got zero value")
	}
	if tok.LastUsed.IsZero() {
		t.Errorf("LastUsed should be parsed, got zero value")
	}
	if len(tok.Scopes) != 2 || tok.Scopes[0] != "read" || tok.Scopes[1] != "write" {
		t.Errorf("Scopes = %v, want [read write]", tok.Scopes)
	}
}

func TestMapToToken_HandlesMissingFields(t *testing.T) {
	tok := mapToToken(map[string]any{"name": "minimal"})
	if tok.Name != "minimal" {
		t.Errorf("Name = %q, want minimal", tok.Name)
	}
	if tok.Revoked {
		t.Errorf("Revoked should default to false")
	}
	if len(tok.Scopes) != 0 {
		t.Errorf("Scopes should be empty, got %v", tok.Scopes)
	}
}

func TestMapToToken_IgnoresWrongTypes(t *testing.T) {
	// Wrong types must not panic and must leave fields at zero values.
	row := map[string]any{
		"token_hash": 12345,                     // int, not string
		"revoked":    "yes",                     // string, not bool
		"created_at": 999,                       // int, not string
		"scopes":     []string{"read", "write"}, // []string, not []any
	}
	tok := mapToToken(row)
	if tok.TokenHash != "" {
		t.Errorf("TokenHash with non-string source should be empty, got %q", tok.TokenHash)
	}
	if tok.Revoked {
		t.Errorf("Revoked with non-bool source should be false")
	}
	if !tok.CreatedAt.IsZero() {
		t.Errorf("CreatedAt with non-string source should be zero")
	}
	if len(tok.Scopes) != 0 {
		t.Errorf("Scopes with []string source should be empty, got %v", tok.Scopes)
	}
}

func TestMapToUser_PopulatesFields(t *testing.T) {
	row := map[string]any{
		"username":      "alice",
		"password_hash": "$argon2id$...",
		"oidc_subject":  "sub-123",
		"created_at":    "2025-01-02T03:04:05Z",
	}
	u := mapToUser(row)
	if u.Username != "alice" {
		t.Errorf("Username = %q, want alice", u.Username)
	}
	if u.PasswordHash != "$argon2id$..." {
		t.Errorf("PasswordHash mismatch: %q", u.PasswordHash)
	}
	if u.OIDCSubject != "sub-123" {
		t.Errorf("OIDCSubject = %q, want sub-123", u.OIDCSubject)
	}
	if u.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be parsed")
	}
}

func TestLoadOIDCConfig_DisabledByDefault(t *testing.T) {
	// Snapshot env so we don't bleed into other tests.
	orig := os.Getenv("RILL_OIDC_ENABLED")
	defer os.Setenv("RILL_OIDC_ENABLED", orig)
	os.Unsetenv("RILL_OIDC_ENABLED")

	cfg := LoadOIDCConfig()
	if cfg.Enabled {
		t.Errorf("OIDC should be disabled when env unset")
	}
	// When disabled, other fields are NOT populated (early return).
	if cfg.Issuer != "" || cfg.ClientID != "" {
		t.Errorf("disabled config should not populate other fields, got %+v", cfg)
	}
}

func TestLoadOIDCConfig_EnabledReadsEnv(t *testing.T) {
	envSet := []string{"RILL_OIDC_ENABLED", "RILL_OIDC_ISSUER", "RILL_OIDC_CLIENT_ID",
		"RILL_OIDC_CLIENT_SECRET", "RILL_PUBLIC_URL"}
	saved := make(map[string]string, len(envSet))
	for _, k := range envSet {
		saved[k] = os.Getenv(k)
	}
	defer func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}()

	os.Setenv("RILL_OIDC_ENABLED", "true")
	os.Setenv("RILL_OIDC_ISSUER", "https://issuer.test")
	os.Setenv("RILL_OIDC_CLIENT_ID", "cid")
	os.Setenv("RILL_OIDC_CLIENT_SECRET", "csecret")
	os.Setenv("RILL_PUBLIC_URL", "https://rill.test")

	cfg := LoadOIDCConfig()
	if !cfg.Enabled {
		t.Fatalf("expected OIDC enabled")
	}
	if cfg.Issuer != "https://issuer.test" {
		t.Errorf("Issuer = %q", cfg.Issuer)
	}
	if cfg.ClientID != "cid" {
		t.Errorf("ClientID = %q", cfg.ClientID)
	}
	if cfg.ClientSecret != "csecret" {
		t.Errorf("ClientSecret = %q", cfg.ClientSecret)
	}
	if cfg.PublicURL != "https://rill.test" {
		t.Errorf("PublicURL = %q", cfg.PublicURL)
	}
}

func TestLoadOIDCConfig_PublicURLFallback(t *testing.T) {
	saved := map[string]string{
		"RILL_OIDC_ENABLED": os.Getenv("RILL_OIDC_ENABLED"),
		"RILL_PUBLIC_URL":   os.Getenv("RILL_PUBLIC_URL"),
	}
	defer func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}()
	os.Setenv("RILL_OIDC_ENABLED", "true")
	os.Unsetenv("RILL_PUBLIC_URL")

	cfg := LoadOIDCConfig()
	if cfg.PublicURL != "http://localhost:8080" {
		t.Errorf("default PublicURL = %q, want localhost fallback", cfg.PublicURL)
	}
}
