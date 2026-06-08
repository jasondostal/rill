package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCClient wraps go-oidc for Authorization Code flow with PKCE.
type OIDCClient struct {
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
}

// NewOIDCClient creates a client from an issuer URL (auto-discovery).
func NewOIDCClient(ctx context.Context, issuer, clientID, clientSecret string) (*OIDCClient, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	oc := &OIDCClient{
		provider: provider,
		oauth2Config: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: clientID}),
	}
	return oc, nil
}

// AuthorizationURL builds the authorization redirect URL with PKCE + nonce.
// The nonce is bound into the id_token by the IdP and verified by the
// callback against the per-flow value persisted in the state store.
func (c *OIDCClient) AuthorizationURL(state, codeVerifier, nonce, redirectURI string) string {
	c.oauth2Config.RedirectURL = redirectURI
	codeChallenge := pkceChallenge(codeVerifier)
	authURL := c.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	return authURL
}

// ExchangeCode exchanges an authorization code for tokens.
func (c *OIDCClient) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI string) (idToken, accessToken string, err error) {
	c.oauth2Config.RedirectURL = redirectURI
	token, err := c.oauth2Config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return "", "", fmt.Errorf("token exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return "", "", fmt.Errorf("no id_token in token response")
	}
	return rawIDToken, token.AccessToken, nil
}

// ValidateIDToken verifies and parses the ID token.
func (c *OIDCClient) ValidateIDToken(ctx context.Context, rawIDToken string) (*OIDCClaims, error) {
	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id token verification: %w", err)
	}

	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("claim extraction: %w", err)
	}
	return &claims, nil
}

// OIDCClaims holds the standard claims we care about.
type OIDCClaims struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
	Nonce             string   `json:"nonce"`
}

// pkceChallenge returns the S256 code challenge for a verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// HTTPClient returns the underlying HTTP client (for testing override).
func HTTPClient() *http.Client {
	return http.DefaultClient
}
