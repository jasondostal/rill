package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/jasondostal/rill/internal/db"
)

// consentFixture wires an MCPOAuthManager to a live SurrealDB and a mock OIDC
// provider, with one registered client. Skips when no database is reachable.
func consentFixture(t *testing.T) (*MCPOAuthManager, *db.DB, string, string) {
	t.Helper()
	database, err := db.Connect(db.Config{URL: "ws://localhost:8000", User: "root", Pass: "root", NS: "rill_consent_test", DB: "rill_consent_test"})
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()
	// Simulate a database from before the authentik_* -> oidc_* rename: the
	// leftover required field must be dropped by SetupSchema, or every
	// authorize request fails at the insert.
	if _, err := database.Query(ctx, "DEFINE TABLE IF NOT EXISTS oauth2_pending_auths SCHEMAFULL; DEFINE FIELD OVERWRITE authentik_code_verifier ON oauth2_pending_auths TYPE string;", nil); err != nil {
		t.Fatalf("seed legacy field: %v", err)
	}
	if err := database.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}
	for _, tbl := range []string{"oauth2_clients", "oauth2_pending_auths", "oauth2_auth_codes"} {
		if _, err := database.Query(ctx, "DELETE "+tbl, nil); err != nil {
			t.Fatalf("clean %s: %v", tbl, err)
		}
	}
	provider := mockOIDCProvider(t)
	t.Cleanup(provider.Close)
	oidcClient, err := NewOIDCClient(ctx, provider.URL, "upstream-client", "upstream-secret")
	if err != nil {
		t.Fatalf("oidc client: %v", err)
	}
	m := NewMCPOAuthManager(database, oidcClient, OIDCConfig{}, "https://rill.example")

	const redirect = "https://client.example/cb"
	w := httptest.NewRecorder()
	m.HandleRegister(w, httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"client_name":"Test <b>Client</b>","redirect_uris":["`+redirect+`"],"token_endpoint_auth_method":"none"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	clientID := regexp.MustCompile(`"client_id":"([0-9a-f]+)"`).FindStringSubmatch(w.Body.String())[1]
	return m, database, clientID, redirect
}

func authorizeReq(clientID, redirect string) *http.Request {
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"state": {"client-state"}, "code_challenge": {"abc"}, "code_challenge_method": {"S256"},
	}
	return httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
}

func confirmReq(token, action string, cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/oauth/authorize/confirm",
		strings.NewReader(url.Values{"consent_token": {token}, "action": {action}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

// startAuthorize runs GET /oauth/authorize and returns the consent token from
// the page plus the browser-binding cookie.
func startAuthorize(t *testing.T, m *MCPOAuthManager, clientID, redirect string) (string, *http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	w := httptest.NewRecorder()
	m.HandleAuthorize(w, authorizeReq(clientID, redirect))
	match := regexp.MustCompile(`name="consent_token" value="([0-9a-f]+)"`).FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || match == nil {
		t.Fatalf("authorize: %d, no consent token in body: %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == consentCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("authorize did not set the consent cookie")
	}
	return match[1], cookie, w
}

// The heart of the fix: /oauth/authorize must stop and ask. It used to 302
// straight to the OIDC provider, so a live SSO session completed the whole
// flow unseen and the code went to whatever redirect_uri the client chose.
func TestAuthorize_ShowsConsentInsteadOfRedirecting(t *testing.T) {
	m, _, clientID, redirect := consentFixture(t)
	_, cookie, w := startAuthorize(t, m, clientID, redirect)

	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("authorize must not redirect anywhere, got Location %q", loc)
	}
	body := w.Body.String()
	if !strings.Contains(body, "client.example") {
		t.Error("consent page must show the redirect host")
	}
	if strings.Contains(body, "<b>Client</b>") || !strings.Contains(body, "Test &lt;b&gt;Client&lt;/b&gt;") {
		t.Error("client name must be HTML-escaped")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("consent cookie should be HttpOnly + SameSite=Strict, got %+v", cookie)
	}
}

func TestConfirm_RequiresTheBrowserThatWasAsked(t *testing.T) {
	m, database, clientID, redirect := consentFixture(t)
	token, cookie, _ := startAuthorize(t, m, clientID, redirect)

	// Someone who only has the page's token (fetched it themselves, then got
	// another browser to post it) has no cookie, or the wrong one.
	for name, c := range map[string]*http.Cookie{"no cookie": nil, "wrong cookie": {Name: consentCookieName, Value: "deadbeef"}} {
		w := httptest.NewRecorder()
		m.HandleAuthorizeConfirm(w, confirmReq(token, "approve", c))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: confirm = %d, want 403", name, w.Code)
		}
	}
	rows, _ := database.Query(context.Background(), "SELECT consented, rill_state FROM oauth2_pending_auths", nil)
	if len(rows) != 1 || rows[0]["consented"] == true {
		t.Fatalf("pending auth must still be unapproved, got %v", rows)
	}

	// The callback refuses a request nobody approved, even with a valid state.
	w := httptest.NewRecorder()
	state := db.StrField(rows[0], "rill_state")
	m.HandleCallback(w, httptest.NewRequest(http.MethodGet, "/oauth/callback?code=x&state="+state, nil))
	if w.Code != http.StatusForbidden {
		t.Errorf("callback for unapproved request = %d, want 403", w.Code)
	}

	// The right browser approves: now, and only now, the upstream URL appears.
	w = httptest.NewRecorder()
	m.HandleAuthorizeConfirm(w, confirmReq(token, "approve", cookie))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/authorize?") || !strings.Contains(w.Body.String(), "state="+state) {
		t.Fatalf("approve = %d, want a page forwarding to the OIDC provider: %s", w.Code, w.Body.String())
	}
	rows, _ = database.Query(context.Background(), "SELECT consented FROM oauth2_pending_auths", nil)
	if len(rows) != 1 || rows[0]["consented"] != true {
		t.Fatalf("pending auth should be approved, got %v", rows)
	}

	// One answer per request.
	w = httptest.NewRecorder()
	m.HandleAuthorizeConfirm(w, confirmReq(token, "approve", cookie))
	if w.Code != http.StatusBadRequest {
		t.Errorf("second confirm = %d, want 400", w.Code)
	}
}

func TestConfirm_DenySendsAccessDeniedAndForgetsTheRequest(t *testing.T) {
	m, database, clientID, redirect := consentFixture(t)
	token, cookie, _ := startAuthorize(t, m, clientID, redirect)

	w := httptest.NewRecorder()
	m.HandleAuthorizeConfirm(w, confirmReq(token, "deny", cookie))
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "https://client.example/cb?error=access_denied") {
		t.Fatalf("deny = %d, want a page returning access_denied to the client: %s", w.Code, body)
	}
	rows, _ := database.Query(context.Background(), "SELECT id FROM oauth2_pending_auths", nil)
	if len(rows) != 0 {
		t.Errorf("denied request should be deleted, %d left", len(rows))
	}
}

func TestRedirectAllowlist_BlocksRegistrationAndOldClients(t *testing.T) {
	m, _, clientID, redirect := consentFixture(t) // client.example registered while open
	m.redirectHosts = []string{"claude.ai"}

	w := httptest.NewRecorder()
	m.HandleRegister(w, httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"redirect_uris":["https://evil.example/cb"]}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("register with off-list host = %d, want 400", w.Code)
	}
	w = httptest.NewRecorder()
	m.HandleRegister(w, httptest.NewRequest(http.MethodPost, "/oauth/register",
		strings.NewReader(`{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`)))
	if w.Code != http.StatusCreated {
		t.Errorf("register with listed host = %d, want 201", w.Code)
	}
	// A client registered before the allowlist can no longer start a flow.
	w = httptest.NewRecorder()
	m.HandleAuthorize(w, authorizeReq(clientID, redirect))
	if w.Code != http.StatusBadRequest {
		t.Errorf("authorize for off-list pre-existing client = %d, want 400", w.Code)
	}
}
