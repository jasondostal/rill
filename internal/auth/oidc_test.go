package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func mockOIDCProvider(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration"):
			issuer := "http://" + r.Host
			fmt.Fprintf(w, `{
				"issuer": %q,
				"authorization_endpoint": %q,
				"token_endpoint": %q,
				"jwks_uri": %q,
				"userinfo_endpoint": %q
			}`,
				issuer,
				issuer+"/authorize",
				issuer+"/token",
				issuer+"/jwks",
				issuer+"/userinfo",
			)
		case strings.HasSuffix(r.URL.Path, "/token"):
			fmt.Fprint(w, `{"access_token":"mock_access","id_token":"mock_id_token"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// mockOIDCProviderWithJWT spins up a minimal OIDC provider that signs real JWTs
// with a test RSA key so go-oidc can validate them via JWKS.
func mockOIDCProviderWithJWT(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	publicKey := &privateKey.PublicKey

	kid := "test-key-1"
	jwk := jose.JSONWebKey{Key: publicKey, KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"}
	jwkSet := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration"):
			issuer := "http://" + r.Host
			fmt.Fprintf(w, `{
				"issuer": %q,
				"authorization_endpoint": %q,
				"token_endpoint": %q,
				"jwks_uri": %q,
				"userinfo_endpoint": %q,
				"id_token_signing_alg_values_supported": ["RS256"]
			}`,
				issuer,
				issuer+"/authorize",
				issuer+"/token",
				issuer+"/jwks",
				issuer+"/userinfo",
			)
		case strings.HasSuffix(r.URL.Path, "/jwks"):
			json.NewEncoder(w).Encode(jwkSet)
		case strings.HasSuffix(r.URL.Path, "/token"):
			// Sign a mock ID token.
			issuer := "http://" + r.Host
			now := time.Now()
			claims := jwt.Claims{
				Issuer:   issuer,
				Subject:  "test-user-123",
				Audience: jwt.Audience{"test-client"},
				Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
				IssuedAt: jwt.NewNumericDate(now),
			}
			customClaims := map[string]any{
				"preferred_username": "testuser",
				"email":              "test@example.com",
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateKey}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
			rawJWT, err := jwt.Signed(signer).Claims(claims).Claims(customClaims).Serialize()
			if err != nil {
				t.Fatalf("sign mock jwt: %v", err)
			}
			fmt.Fprintf(w, `{"access_token":"mock_access","id_token":%q}`, rawJWT)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, privateKey
}

func TestOIDCClient_DiscoversProvider(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}
	if client.provider == nil {
		t.Fatal("provider is nil")
	}
	if client.oauth2Config.ClientID != "test-client" {
		t.Fatalf("expected client_id 'test-client', got %q", client.oauth2Config.ClientID)
	}
}

func TestAuthorizationURL_IncludesPKCEChallenge(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}

	url := client.AuthorizationURL("test-state", "my-verifier", "my-nonce", "http://localhost/callback")
	if !strings.Contains(url, "code_challenge=") {
		t.Fatal("auth URL missing code_challenge")
	}
	if !strings.Contains(url, "code_challenge_method=S256") {
		t.Fatal("auth URL missing code_challenge_method=S256")
	}
	if !strings.Contains(url, "state=test-state") {
		t.Fatal("auth URL missing state")
	}

	// Verify the challenge is the correct S256 hash.
	expectedChallenge := pkceChallenge("my-verifier")
	if !strings.Contains(url, "code_challenge="+expectedChallenge) {
		t.Fatalf("auth URL has wrong code_challenge; expected %s", expectedChallenge)
	}
}

func TestExchangeCode_RoundTrips(t *testing.T) {
	server := mockOIDCProvider(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}

	idToken, accessToken, err := client.ExchangeCode(ctx, "mock-code", "verifier", "http://localhost/callback")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}
	if idToken != "mock_id_token" {
		t.Fatalf("expected id_token 'mock_id_token', got %q", idToken)
	}
	if accessToken != "mock_access" {
		t.Fatalf("expected access_token 'mock_access', got %q", accessToken)
	}
}

func TestValidateIDToken_AcceptsValidToken(t *testing.T) {
	server, _ := mockOIDCProviderWithJWT(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}

	// Exchange code to get a real signed JWT.
	idToken, _, err := client.ExchangeCode(ctx, "mock-code", "verifier", "http://localhost/callback")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}

	claims, err := client.ValidateIDToken(ctx, idToken)
	if err != nil {
		t.Fatalf("ValidateIDToken failed: %v", err)
	}
	if claims.Subject != "test-user-123" {
		t.Fatalf("expected subject 'test-user-123', got %q", claims.Subject)
	}
	if claims.PreferredUsername != "testuser" {
		t.Fatalf("expected preferred_username 'testuser', got %q", claims.PreferredUsername)
	}
}

func TestValidateIDToken_RejectsExpired(t *testing.T) {
	server, privateKey := mockOIDCProviderWithJWT(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}

	// Sign an expired token directly.
	now := time.Now()
	claims := jwt.Claims{
		Issuer:   server.URL,
		Subject:  "test-user-123",
		Audience: jwt.Audience{"test-client"},
		Expiry:   jwt.NewNumericDate(now.Add(-time.Hour)), // expired 1 hour ago
		IssuedAt: jwt.NewNumericDate(now.Add(-2 * time.Hour)),
	}
	kid := "test-key-1"
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateKey}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	expiredJWT, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("sign expired jwt: %v", err)
	}

	_, err = client.ValidateIDToken(ctx, expiredJWT)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
	if !strings.Contains(err.Error(), "token is expired") {
		t.Fatalf("expected 'token is expired' error, got: %v", err)
	}
}

func TestValidateIDToken_RejectsWrongIssuer(t *testing.T) {
	server, privateKey := mockOIDCProviderWithJWT(t)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewOIDCClient(ctx, server.URL, "test-client", "test-secret")
	if err != nil {
		t.Fatalf("NewOIDCClient failed: %v", err)
	}

	// Sign a token with wrong issuer.
	now := time.Now()
	claims := jwt.Claims{
		Issuer:   "https://evil.com", // wrong issuer
		Subject:  "test-user-123",
		Audience: jwt.Audience{"test-client"},
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(now),
	}
	kid := "test-key-1"
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privateKey}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	wrongIssuerJWT, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	_, err = client.ValidateIDToken(ctx, wrongIssuerJWT)
	if err == nil {
		t.Fatal("expected error for wrong issuer, got nil")
	}
}

func TestStateStore_TTLExpiry(t *testing.T) {
	store := NewOIDCStateStore(100 * time.Millisecond)
	defer store.Close()
	state, verifier, nonce := store.Create("/")
	if state == "" || verifier == "" || nonce == "" {
		t.Fatal("Create returned empty values")
	}

	// Should exist immediately.
	entry := store.Consume(state)
	if entry == nil {
		t.Fatal("entry missing immediately after create")
	}
	if entry.CodeVerifier != verifier {
		t.Fatalf("verifier mismatch: expected %q, got %q", verifier, entry.CodeVerifier)
	}
	if entry.Nonce != nonce {
		t.Fatalf("nonce mismatch: expected %q, got %q", nonce, entry.Nonce)
	}

	// Create another and let it expire.
	state2, _, _ := store.Create("/")
	time.Sleep(150 * time.Millisecond)
	entry2 := store.Consume(state2)
	if entry2 != nil {
		t.Fatal("expired entry should be nil")
	}
}

func TestStateStore_ConsumeOncePolicy(t *testing.T) {
	store := NewOIDCStateStore(time.Minute)
	defer store.Close()
	state, _, _ := store.Create("/")

	// First consume succeeds.
	if store.Consume(state) == nil {
		t.Fatal("first consume should succeed")
	}
	// Second consume returns nil (one-time use).
	if store.Consume(state) != nil {
		t.Fatal("second consume should fail (one-time use)")
	}
}

func TestStateStore_PreservesRedirectAfter(t *testing.T) {
	store := NewOIDCStateStore(time.Minute)
	defer store.Close()
	state, _, _ := store.Create("/dashboard")
	entry := store.Consume(state)
	if entry == nil {
		t.Fatal("entry missing")
	}
	if entry.RedirectAfter != "/dashboard" {
		t.Fatalf("expected redirect '/dashboard', got %q", entry.RedirectAfter)
	}
}
