package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
)

// --- Token generation helpers ---

func generateToken(prefix string, length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(b)
}

func generateClientID() string {
	return generateToken("", 16)
}

func generateClientSecret() string {
	return generateToken("", 32)
}

func generateAuthCode() string {
	return generateToken("", 30)
}

func generateMCPToken() string {
	return generateToken("rill_mcp_v1_", 32)
}

// pkceVerifier generates a random PKCE code verifier.
func pkceVerifier() string {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// pkceChallenge returns the S256 code challenge for a verifier.
func pkceChallengeS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// --- MCP OAuth Manager ---

// MCPOAuthManager handles the OAuth2 proxy server for MCP clients.
// Rill acts as the OAuth server to its MCP clients while delegating actual
// user auth to the upstream OIDC provider via the existing OIDCClient.
type MCPOAuthManager struct {
	db         *db.DB
	oidcClient *OIDCClient
	oidcCfg    OIDCConfig
	publicURL  string
}

// NewMCPOAuthManager creates a new MCP OAuth manager.
func NewMCPOAuthManager(d *db.DB, oidcClient *OIDCClient, oidcCfg OIDCConfig, publicURL string) *MCPOAuthManager {
	return &MCPOAuthManager{
		db:         d,
		oidcClient: oidcClient,
		oidcCfg:    oidcCfg,
		publicURL:  strings.TrimRight(publicURL, "/"),
	}
}

// --- Client Registration (RFC 7591) ---

// RegisterClientRequest is the RFC 7591 registration request body.
type RegisterClientRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// RegisterClientResponse is the RFC 7591 registration response.
type RegisterClientResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientSecretHash        string   `json:"-"` // hashed at rest, never serialized
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	RegistrationAccessToken string   `json:"registration_access_token,omitempty"`
	RegistrationClientURI   string   `json:"registration_client_uri,omitempty"`
}

// HandleRegister handles POST /oauth/register.
func (m *MCPOAuthManager) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var req RegisterClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}

	// Sanitize client_name: strip ASCII control chars (incl. CR/LF that
	// would otherwise inject newlines into the audit log) and cap length.
	// Reject the registration outright if it contained newlines so a caller
	// can't paper over a log-injection attempt with silent truncation.
	if strings.ContainsAny(req.ClientName, "\r\n") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_name must not contain newlines"})
		return
	}
	req.ClientName = stripControlChars(req.ClientName)
	if len(req.ClientName) > 100 {
		req.ClientName = req.ClientName[:100]
	}

	if len(req.RedirectURIs) == 0 {
		writeValidationError(w, "redirect_uris required", map[string]any{"field": "redirect_uris"})
		return
	}

	// Validate redirect URIs — HTTPS only, no localhost.
	for _, uri := range req.RedirectURIs {
		u, err := url.Parse(uri)
		if err != nil {
			writeValidationError(w, "invalid redirect_uri: "+uri, map[string]any{"field": "redirect_uris", "value": uri})
			return
		}
		if u.Scheme != "https" {
			writeValidationError(w, "redirect_uri must use HTTPS: "+uri, map[string]any{"field": "redirect_uris", "value": uri})
			return
		}
		if u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" {
			writeValidationError(w, "redirect_uri cannot target localhost: "+uri, map[string]any{"field": "redirect_uris", "value": uri})
			return
		}
	}

	// Cap total registered clients to prevent DCR spam.
	countRows, _ := m.db.Query(r.Context(), "SELECT count() AS cnt FROM oauth2_clients GROUP ALL", nil)
	if len(countRows) > 0 && db.IntField(countRows[0], "cnt") >= 50 {
		writeForbidden(w, "maximum number of registered clients reached")
		return
	}

	clientID := generateClientID()
	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "client_secret_post"
	}

	var clientSecret string
	var clientSecretHash string
	if authMethod != "none" {
		clientSecret = generateClientSecret()
		h := sha256.Sum256([]byte(clientSecret))
		clientSecretHash = hex.EncodeToString(h[:])
	}

	now := time.Now().UTC()
	data := map[string]any{
		"client_id":          clientID,
		"client_secret_hash": clientSecretHash,
		"redirect_uris":      req.RedirectURIs,
		"client_name":        req.ClientName,
		"created_at":         now,
	}
	if _, err := m.db.Create(r.Context(), "oauth2_clients", data); err != nil {
		writeInternalError(w, "oauth_register_create_client", err, "")
		return
	}

	resp := RegisterClientResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        now.Unix(),
		ClientSecretExpiresAt:   0, // never expires
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: authMethod,
	}

	rilllog.Logger().Info("oauth: registered client", "client_id", clientID, "client_name", req.ClientName)
	writeJSON(w, http.StatusCreated, resp)
}

// getClient looks up a registered OAuth2 client by ID.
func (m *MCPOAuthManager) getClient(ctx context.Context, clientID string) (*RegisterClientResponse, error) {
	rows, err := m.db.Query(ctx,
		"SELECT * FROM oauth2_clients WHERE client_id = $id LIMIT 1",
		map[string]any{"id": clientID})
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("client not found")
	}
	r := rows[0]
	var uris []string
	if u, ok := r["redirect_uris"].([]any); ok {
		for _, v := range u {
			if s, ok := v.(string); ok {
				uris = append(uris, s)
			}
		}
	}
	return &RegisterClientResponse{
		ClientID:                db.StrField(r, "client_id"),
		ClientSecretHash:        db.StrField(r, "client_secret_hash"),
		ClientName:              db.StrField(r, "client_name"),
		RedirectURIs:            uris,
		TokenEndpointAuthMethod: db.StrField(r, "token_endpoint_auth_method"),
	}, nil
}

// --- Well-Known Endpoints ---

// AuthorizationServerMetadata is RFC 8414 metadata.
type AuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// ProtectedResourceMetadata is RFC 9728 metadata.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// HandleWellKnownOAuthAuthServer handles GET /.well-known/oauth-authorization-server.
func (m *MCPOAuthManager) HandleWellKnownOAuthAuthServer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, AuthorizationServerMetadata{
		Issuer:                            m.publicURL,
		AuthorizationEndpoint:             m.publicURL + "/oauth/authorize",
		TokenEndpoint:                     m.publicURL + "/oauth/token",
		RegistrationEndpoint:              m.publicURL + "/oauth/register",
		RevocationEndpoint:                m.publicURL + "/oauth/revoke",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "none"},
		ScopesSupported:                   []string{"read", "write", "admin"},
	})
}

// HandleWellKnownOAuthProtectedResource handles GET /.well-known/oauth-protected-resource.
func (m *MCPOAuthManager) HandleWellKnownOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	// Resource must point to the actual MCP endpoint, not the public root.
	// MCP clients probe the resource URL with the bearer token after OAuth
	// completes; if we advertise the bare root URL, the request lands on the
	// SSO-gated UI and gets redirected into a login loop the agent
	// can't follow.
	writeJSON(w, http.StatusOK, ProtectedResourceMetadata{
		Resource:             m.publicURL + "/mcp",
		AuthorizationServers: []string{m.publicURL},
	})
}

// --- Authorization Endpoint ---

// HandleAuthorize handles GET /oauth/authorize.
// Validates client, stores pending auth state, redirects to the OIDC provider.
func (m *MCPOAuthManager) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	scope := q.Get("scope")
	state := q.Get("state")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")

	if responseType != "code" {
		writeBadRequest(w, "unsupported response_type")
		return
	}

	client, err := m.getClient(r.Context(), clientID)
	if err != nil {
		writeBadRequest(w, "invalid client_id")
		return
	}

	// Validate redirect_uri matches a registered URI.
	validRedirect := false
	for _, ru := range client.RedirectURIs {
		if ru == redirectURI {
			validRedirect = true
			break
		}
	}
	if !validRedirect {
		writeBadRequest(w, "invalid redirect_uri")
		return
	}

	if codeChallengeMethod != "S256" {
		writeBadRequest(w, "code_challenge_method must be S256")
		return
	}
	if codeChallenge == "" {
		writeBadRequest(w, "code_challenge required")
		return
	}

	// Generate Rill's own PKCE verifier, state, and nonce for the upstream-OIDC leg.
	oidcVerifier := pkceVerifier()
	rillState := generateToken("", 32)
	oidcNonce := generateToken("", 32)

	scopes := []string{"read", "write", "admin"}
	if scope != "" {
		scopes = strings.Split(scope, " ")
	}

	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	data := map[string]any{
		"rill_state":                   rillState,
		"client_state":                 state,
		"client_id":                    clientID,
		"client_redirect_uri":          redirectURI,
		"client_code_challenge":        codeChallenge,
		"client_code_challenge_method": codeChallengeMethod,
		"oidc_code_verifier":           oidcVerifier,
		"oidc_nonce":                   oidcNonce,
		"scopes":                       scopes,
		"expires_at":                   expiresAt,
	}
	if _, err := m.db.Create(r.Context(), "oauth2_pending_auths", data); err != nil {
		writeInternalError(w, "oauth_authorize_store_pending", err, "")
		return
	}

	// Redirect to the OIDC provider with Rill's callback as the redirect_uri.
	callbackURI := m.publicURL + "/oauth/callback"
	authURL := m.oidcClient.AuthorizationURL(rillState, oidcVerifier, oidcNonce, callbackURI)

	rilllog.Logger().Info("oauth authorize: redirecting to OIDC provider", "client_id", clientID)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// --- Callback Endpoint ---

// HandleCallback handles GET /oauth/callback from the OIDC provider.
// Exchanges the upstream code, validates ID token, creates/links user,
// generates Rill auth code, redirects to client's callback.
func (m *MCPOAuthManager) HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	errParam := q.Get("error")

	if errParam != "" {
		rilllog.Logger().Warn("oauth callback: OIDC provider returned error", "error", errParam)
		writeBadRequest(w, "authentication failed: "+errParam)
		return
	}

	if code == "" || state == "" {
		writeBadRequest(w, "missing code or state")
		return
	}

	// Look up pending auth by rill_state.
	rows, err := m.db.Query(r.Context(),
		"SELECT * FROM oauth2_pending_auths WHERE rill_state = $state LIMIT 1",
		map[string]any{"state": state})
	if err != nil || len(rows) == 0 {
		writeBadRequest(w, "invalid or expired state")
		return
	}

	pending := rows[0]
	// Check expiry. db.TimeField handles CustomDateTime / time.Time / RFC3339
	// — the previous .(string) cast silently passed for non-string types,
	// which would have let an expired state be consumed.
	if exp := db.TimeField(pending, "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		writeBadRequest(w, "expired state")
		return
	}

	// Delete pending auth (one-time use).
	defer func() {
		if rid := db.RecordID(pending); rid != "" {
			_, _ = m.db.QueryRecord(r.Context(), "DELETE %s", rid, nil)
		}
	}()

	oidcVerifier := db.StrField(pending, "oidc_code_verifier")
	callbackURI := m.publicURL + "/oauth/callback"

	ctx := r.Context()
	rawIDToken, _, err := m.oidcClient.ExchangeCode(ctx, code, oidcVerifier, callbackURI)
	if err != nil {
		rilllog.Logger().Warn("oauth callback: token exchange failed", "error", err)
		writeBadRequest(w, "token exchange failed")
		return
	}

	claims, err := m.oidcClient.ValidateIDToken(ctx, rawIDToken)
	if err != nil {
		rilllog.Logger().Warn("oauth callback: id token validation failed", "error", err)
		writeBadRequest(w, "token validation failed")
		return
	}

	// Nonce binding: reject id_tokens that don't echo back the nonce we
	// generated when we built the auth URL. Protects against id_token replay.
	expectedNonce := db.StrField(pending, "oidc_nonce")
	if expectedNonce == "" || claims.Nonce != expectedNonce {
		rilllog.Logger().Warn("oauth callback: nonce mismatch (rejected)")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nonce mismatch"})
		return
	}

	// Get or create Rill user from OIDC claims.
	authMgr := &Manager{db: m.db}
	user, err := authMgr.GetOrCreateOIDCUser(ctx, claims)
	if err != nil {
		writeInternalError(w, "oauth_callback_user_lookup", err, "")
		return
	}

	// Generate Rill authorization code.
	rillCode := generateAuthCode()
	clientID := db.StrField(pending, "client_id")
	clientRedirectURI := db.StrField(pending, "client_redirect_uri")
	clientState := db.StrField(pending, "client_state")
	clientCodeChallenge := db.StrField(pending, "client_code_challenge")
	clientCodeChallengeMethod := db.StrField(pending, "client_code_challenge_method")
	scopes := []string{"read", "write", "admin"}
	if s, ok := pending["scopes"].([]any); ok {
		scopes = nil
		for _, v := range s {
			if ss, ok := v.(string); ok {
				scopes = append(scopes, ss)
			}
		}
	}

	codeData := map[string]any{
		"code":                  rillCode,
		"client_id":             clientID,
		"user_id":               user.ID,
		"redirect_uri":          clientRedirectURI,
		"code_challenge":        clientCodeChallenge,
		"code_challenge_method": clientCodeChallengeMethod,
		"scopes":                scopes,
		"expires_at":            time.Now().UTC().Add(10 * time.Minute),
	}
	if _, err := m.db.Create(r.Context(), "oauth2_auth_codes", codeData); err != nil {
		writeInternalError(w, "oauth_callback_store_auth_code", err, "")
		return
	}

	// Redirect to client's callback with the auth code.
	redirectURL, err := url.Parse(clientRedirectURI)
	if err != nil {
		writeInternalError(w, "oauth_callback_parse_redirect", err, "")
		return
	}
	q2 := redirectURL.Query()
	q2.Set("code", rillCode)
	q2.Set("state", clientState)
	redirectURL.RawQuery = q2.Encode()

	rilllog.Logger().Info("oauth callback: redirecting to client callback", "user", user.Username, "client_id", clientID)
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// --- Token Endpoint ---

// TokenResponse is the OAuth2 token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// HandleToken handles POST /oauth/token.
func (m *MCPOAuthManager) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeBadRequest(w, "invalid form data")
		return
	}

	grantType := r.PostFormValue("grant_type")
	clientID := r.PostFormValue("client_id")
	clientSecret := r.PostFormValue("client_secret")

	// Validate client.
	client, err := m.getClient(r.Context(), clientID)
	if err != nil {
		writeBadRequest(w, "invalid client")
		return
	}

	// Validate client secret if required. Use a constant-time comparison so
	// an attacker can't probe the secret hash byte-by-byte via timing.
	if client.ClientSecretHash != "" {
		h := sha256.Sum256([]byte(clientSecret))
		want, decErr := hex.DecodeString(client.ClientSecretHash)
		if decErr != nil || subtle.ConstantTimeCompare(h[:], want) != 1 {
			writeUnauthorized(w, "invalid client credentials")
			return
		}
	}

	switch grantType {
	case "authorization_code":
		m.handleAuthorizationCodeGrant(w, r, client)
	case "refresh_token":
		m.handleRefreshTokenGrant(w, r, client)
	default:
		writeBadRequest(w, "unsupported grant_type")
	}
}

func (m *MCPOAuthManager) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request, client *RegisterClientResponse) {
	code := r.PostFormValue("code")
	codeVerifier := r.PostFormValue("code_verifier")
	redirectURI := r.PostFormValue("redirect_uri")

	if code == "" {
		writeBadRequest(w, "code required")
		return
	}

	// Look up auth code.
	rows, err := m.db.Query(r.Context(),
		"SELECT * FROM oauth2_auth_codes WHERE code = $code LIMIT 1",
		map[string]any{"code": code})
	if err != nil || len(rows) == 0 {
		writeBadRequest(w, "invalid code")
		return
	}

	codeRow := rows[0]
	// Check expiry. db.TimeField handles all three timestamp shapes the
	// SurrealDB driver returns (see note in session expiry check).
	if exp := db.TimeField(codeRow, "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		writeBadRequest(w, "expired code")
		return
	}

	// Validate client_id matches.
	if db.StrField(codeRow, "client_id") != client.ClientID {
		writeBadRequest(w, "client_id mismatch")
		return
	}

	// Validate redirect_uri matches.
	if db.StrField(codeRow, "redirect_uri") != redirectURI {
		writeBadRequest(w, "redirect_uri mismatch")
		return
	}

	// Validate PKCE code_verifier. HandleAuthorize already rejects anything
	// other than S256, so the stored method should always be S256 here.
	// Anything else means stored state was tampered with — fail closed
	// rather than skip the PKCE check.
	storedChallenge := db.StrField(codeRow, "code_challenge")
	storedMethod := db.StrField(codeRow, "code_challenge_method")
	if storedMethod != "S256" {
		writeBadRequest(w, "unsupported code_challenge_method on stored auth code")
		return
	}
	expectedChallenge := pkceChallengeS256(codeVerifier)
	// Constant-time compare on the PKCE challenge to avoid leaking how many
	// leading bytes matched via response timing.
	if subtle.ConstantTimeCompare([]byte(expectedChallenge), []byte(storedChallenge)) != 1 {
		writeBadRequest(w, "invalid code_verifier")
		return
	}

	// Delete auth code (one-time use).
	if rid := db.RecordID(codeRow); rid != "" {
		_, _ = m.db.QueryRecord(r.Context(), "DELETE %s", rid, nil)
	}

	// Issue tokens.
	userID := db.RecordID(codeRow)
	if uid, ok := codeRow["user_id"].(string); ok && uid != "" {
		userID = uid
	}
	scopes := []string{"read", "write", "admin"}
	if s, ok := codeRow["scopes"].([]any); ok {
		scopes = nil
		for _, v := range s {
			if ss, ok := v.(string); ok {
				scopes = append(scopes, ss)
			}
		}
	}

	resp, err := m.issueTokens(r.Context(), client.ClientID, userID, scopes)
	if err != nil {
		writeInternalError(w, "oauth_authcode_issue_tokens", err, "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (m *MCPOAuthManager) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request, client *RegisterClientResponse) {
	refreshToken := r.PostFormValue("refresh_token")
	if refreshToken == "" {
		writeBadRequest(w, "refresh_token required")
		return
	}

	// Look up refresh token by hash.
	rows, err := m.db.Query(r.Context(),
		"SELECT * FROM oauth2_refresh_tokens WHERE token_hash = $hash AND revoked = false LIMIT 1",
		map[string]any{"hash": hashToken(refreshToken)})
	if err != nil || len(rows) == 0 {
		writeBadRequest(w, "invalid refresh_token")
		return
	}

	tokRow := rows[0]
	// Check expiry. db.TimeField handles all three timestamp shapes
	// (see note in session expiry check).
	if exp := db.TimeField(tokRow, "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		writeBadRequest(w, "expired refresh_token")
		return
	}

	// Validate client_id matches.
	if db.StrField(tokRow, "client_id") != client.ClientID {
		writeBadRequest(w, "client_id mismatch")
		return
	}

	userID := db.RecordID(tokRow)
	if uid, ok := tokRow["user_id"].(string); ok && uid != "" {
		userID = uid
	}
	scopes := []string{"read", "write", "admin"}
	if s, ok := tokRow["scopes"].([]any); ok {
		scopes = nil
		for _, v := range s {
			if ss, ok := v.(string); ok {
				scopes = append(scopes, ss)
			}
		}
	}

	// Rotate refresh token: delete old, create new.
	if rid := db.RecordID(tokRow); rid != "" {
		_, _ = m.db.QueryRecord(r.Context(), "DELETE %s", rid, nil)
	}

	resp, err := m.issueTokens(r.Context(), client.ClientID, userID, scopes)
	if err != nil {
		writeInternalError(w, "oauth_refresh_issue_tokens", err, "")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// issueTokens mints a new access + refresh token pair, persists them, and
// returns the response. An error here MUST surface to the caller: previously
// the Create calls silently discarded errors with `_, _ =`, which meant a
// failed write returned a valid-looking TokenResponse for a token that was
// never stored — the client got a token that 401'd on first use.
func (m *MCPOAuthManager) issueTokens(ctx context.Context, clientID, userID string, scopes []string) (TokenResponse, error) {
	accessToken := generateMCPToken()
	refreshToken := generateMCPToken()
	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour) // 7 days

	// Store access token.
	if _, err := m.db.Create(ctx, "oauth2_access_tokens", map[string]any{
		"token_hash": hashToken(accessToken),
		"client_id":  clientID,
		"user_id":    userID,
		"scopes":     scopes,
		"expires_at": expiresAt,
		"created_at": now,
		"revoked":    false,
	}); err != nil {
		return TokenResponse{}, fmt.Errorf("store access token: %w", err)
	}

	// Store refresh token.
	if _, err := m.db.Create(ctx, "oauth2_refresh_tokens", map[string]any{
		"token_hash": hashToken(refreshToken),
		"client_id":  clientID,
		"user_id":    userID,
		"scopes":     scopes,
		"expires_at": now.Add(30 * 24 * time.Hour), // 30 days
		"revoked":    false,
	}); err != nil {
		return TokenResponse{}, fmt.Errorf("store refresh token: %w", err)
	}

	rilllog.Logger().Info("oauth: issued tokens", "user_id", userID, "client_id", clientID)
	return TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(7 * 24 * time.Hour.Seconds()),
		RefreshToken: refreshToken,
		Scope:        strings.Join(scopes, " "),
	}, nil
}

// --- Revocation Endpoint (RFC 7009) ---

// HandleRevoke handles POST /oauth/revoke.
func (m *MCPOAuthManager) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeBadRequest(w, "invalid form data")
		return
	}

	token := r.PostFormValue("token")
	tokenTypeHint := r.PostFormValue("token_type_hint")
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true}) // idempotent
		return
	}

	ctx := r.Context()
	tokenHash := hashToken(token)

	// Try access token first.
	if tokenTypeHint == "" || tokenTypeHint == "access_token" {
		_, _ = m.db.Query(ctx,
			"UPDATE oauth2_access_tokens SET revoked = true WHERE token_hash = $hash",
			map[string]any{"hash": tokenHash})
	}

	// Try refresh token.
	if tokenTypeHint == "" || tokenTypeHint == "refresh_token" {
		_, _ = m.db.Query(ctx,
			"UPDATE oauth2_refresh_tokens SET revoked = true WHERE token_hash = $hash",
			map[string]any{"hash": tokenHash})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- Token Validation for Auth Middleware ---

// ValidateMCPToken checks if an MCP OAuth token is valid and non-revoked.
// Returns the user ID, scopes, token ID, and whether valid.
func (m *Manager) ValidateMCPToken(ctx context.Context, tokenStr string) (userID string, scopes []string, tokenID string, valid bool) {
	if !strings.HasPrefix(tokenStr, "rill_mcp_v1_") {
		return "", nil, "", false
	}
	rows, err := m.db.Query(ctx,
		"SELECT id, user_id, expires_at, scopes FROM oauth2_access_tokens WHERE token_hash = $hash AND revoked = false LIMIT 1",
		map[string]any{"hash": hashToken(tokenStr)})
	if err != nil || len(rows) == 0 {
		return "", nil, "", false
	}

	// Expiry check. db.TimeField handles all three timestamp shapes
	// (see note in session expiry check).
	if exp := db.TimeField(rows[0], "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		return "", nil, "", false
	}

	id := db.RecordID(rows[0])
	var uid string
	if u, ok := rows[0]["user_id"].(string); ok {
		uid = u
	}
	var scopeList []string
	if sl, ok := rows[0]["scopes"].([]any); ok {
		for _, s := range sl {
			if ss, ok := s.(string); ok {
				scopeList = append(scopeList, ss)
			}
		}
	}
	return uid, scopeList, id, true
}

// --- Helper ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// stripControlChars removes ASCII control characters from a string. Used to
// sanitize fields (e.g. DCR client_name) before they hit the audit log so a
// caller can't smuggle newlines, terminal escapes, etc. into operator-facing
// output. Non-ASCII runes pass through unchanged.
func stripControlChars(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Allow tab + printable ASCII (>= 0x20, != DEL) and any non-ASCII
		// rune. Drop CR/LF/NUL/etc.
		if r == '\t' || (r >= 0x20 && r != 0x7F) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// StrField extracts a string field from a map (moved from db package for local use).
// The db package already has this, but we use db.StrField directly.
