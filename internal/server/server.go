package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
	"github.com/jasondostal/rill/internal/document"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/jasondostal/rill/internal/memory"
)

// Server holds the HTTP server and routes.
type Server struct {
	mux         *http.ServeMux
	authMgr     *auth.Manager
	memStore    *memory.Store
	docStore    *document.Store
	oidcClient  *auth.OIDCClient
	oidcStore   *auth.OIDCStateStore
	oidcCfg     auth.OIDCConfig
	mcpOAuthMgr *auth.MCPOAuthManager
	audit       *auditLogger
}

func maxBodyBytes() int64 {
	if v := os.Getenv("RILL_MAX_BODY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 10 << 20
}

// New creates a new Server with configured routes.
// MCP routes are protected by: body size limit → auth → panic recovery.
// All routes get security headers.
func New(mcpHandler http.Handler, authMgr *auth.Manager, database *db.DB, memStore *memory.Store, docStore *document.Store, oidcClient *auth.OIDCClient, oidcStore *auth.OIDCStateStore, oidcCfg auth.OIDCConfig, mcpOAuthMgr *auth.MCPOAuthManager) *Server {
	s := &Server{
		mux:         http.NewServeMux(),
		authMgr:     authMgr,
		memStore:    memStore,
		docStore:    docStore,
		oidcClient:  oidcClient,
		oidcStore:   oidcStore,
		oidcCfg:     oidcCfg,
		mcpOAuthMgr: mcpOAuthMgr,
	}
	s.mux.HandleFunc("/health", s.handleHealth)

	// Auth endpoints (unauthenticated — excluded by middleware).
	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("/api/auth/setup", s.handleSetup)
	s.mux.HandleFunc("GET /api/auth/oidc/login", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/auth/oidc/callback", s.handleOIDCCallback)
	s.mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)

	if mcpOAuthMgr != nil {
		s.mux.HandleFunc("GET /.well-known/oauth-authorization-server", mcpOAuthMgr.HandleWellKnownOAuthAuthServer)
		s.mux.HandleFunc("GET /.well-known/oauth-protected-resource", mcpOAuthMgr.HandleWellKnownOAuthProtectedResource)
		s.mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", mcpOAuthMgr.HandleWellKnownOAuthProtectedResource)
		s.mux.HandleFunc("POST /oauth/register", mcpOAuthMgr.HandleRegister)
		s.mux.HandleFunc("GET /oauth/authorize", mcpOAuthMgr.HandleAuthorize)
		s.mux.HandleFunc("GET /oauth/callback", mcpOAuthMgr.HandleCallback)
		s.mux.HandleFunc("POST /oauth/token", mcpOAuthMgr.HandleToken)
		s.mux.HandleFunc("POST /oauth/revoke", mcpOAuthMgr.HandleRevoke)
	}

	meHandler := authMgr.Middleware()(http.HandlerFunc(s.handleMe))
	s.mux.Handle("/api/auth/me", meHandler)

	limited := bodyLimitMiddleware(recoverMiddleware(mcpHandler))
	authMux := authMgr.Middleware()(limited)
	s.mux.Handle("/mcp", authMux)
	s.mux.Handle("/api/mcp", authMux)

	// Memory + document REST surface (authenticated).
	if memStore != nil {
		restH := newRestHandler(authMgr, database, memStore, docStore)
		restMux := authMgr.Middleware()(restH)
		s.mux.Handle("/api/", restMux)
	}

	// Static frontend at root. Mounted last so /api/* and /mcp take precedence.
	if static, err := staticHandler(); err == nil {
		s.mux.Handle("/", static)
	} else {
		rilllog.Logger().Error("static frontend embed init failed", "error", err)
	}

	return s
}

func bodyLimitMiddleware(next http.Handler) http.Handler {
	limit := maxBodyBytes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				rilllog.Logger().Error("panic in MCP handler", "recover", rec)
				http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal server error"}}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler(lifecycleCtx context.Context, trustedProxies []string) http.Handler {
	mcpRL := NewRateLimiter(lifecycleCtx, 10, 30)
	restRL := NewRateLimiter(lifecycleCtx, 10, 30)
	loginRL := NewRateLimiter(lifecycleCtx, 1, 5)
	rlMiddleware := RateLimitMiddleware(mcpRL, loginRL, restRL, trustedProxies)
	var chain http.Handler = s.mux
	if s.authMgr != nil && s.authMgr.DB() != nil {
		s.audit = newAuditLogger(lifecycleCtx, s.authMgr.DB(), trustedProxies)
		chain = s.audit.middleware(chain)
	}
	return securityHeadersMiddleware(rlMiddleware(chain))
}

func (s *Server) Close() error {
	if s.audit != nil {
		s.audit.Close()
	}
	return nil
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"object-src 'none'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbHealthy := false
	if s.authMgr != nil && s.authMgr.DB() != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.authMgr.DB().Ping(ctx); err == nil {
			dbHealthy = true
		}
	}
	status := http.StatusOK
	if !dbHealthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status":    "ok",
		"db_status": map[bool]string{true: "connected", false: "disconnected"}[dbHealthy],
		"version":   "0.2.0",
		"name":      "rill",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.authMgr.Mode != auth.ModeLocal {
		writeBadRequest(w, "login is only available in local auth mode")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeBadRequest(w, "username and password are required")
		return
	}

	rows, err := s.authMgr.DB().Query(r.Context(),
		"SELECT id, password_hash FROM auth_user WHERE username = $user LIMIT 1",
		map[string]any{"user": req.Username})
	if err != nil {
		writeInternalError(w, "login_user_query", err, "")
		return
	}

	passwordHash := auth.DummyHash()
	userFound := false
	var userID string
	if len(rows) > 0 {
		if h, ok := rows[0]["password_hash"].(string); ok && h != "" {
			passwordHash = h
			userFound = true
			userID = db.RecordID(rows[0])
		}
	}

	ok, _ := auth.VerifyPassword(req.Password, passwordHash)
	if !ok || !userFound {
		writeUnauthorized(w, "invalid credentials")
		return
	}
	userAgent := r.Header.Get("User-Agent")
	sourceIP := s.authMgr.SourceIP(r)

	cookieValue, err := s.authMgr.CreateSession(r.Context(), userID, userAgent, sourceIP)
	if err != nil {
		writeInternalError(w, "login_session_create", err, "")
		return
	}

	if _, err := s.authMgr.DB().QueryRecord(r.Context(),
		"UPDATE %s SET last_login = $now", userID,
		map[string]any{"now": time.Now().UTC()}); err != nil {
		rilllog.Logger().Error("login: last_login update failed", "user_id", userID, "error", err)
	}

	auth.SetSessionCookie(w, cookieValue, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	c, err := r.Cookie("rill_session")
	if err == nil && c.Value != "" {
		if revokeErr := s.authMgr.RevokeSession(r.Context(), c.Value); revokeErr != nil {
			rilllog.Logger().Error("logout: revoke session failed", "error", revokeErr)
		}
	}
	auth.ClearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "logged out"})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, _ := s.authMgr.DB().Query(r.Context(),
			"SELECT count() AS cnt FROM auth_user GROUP ALL", nil)
		available := len(rows) > 0 && db.IntField(rows[0], "cnt") == 0
		writeJSON(w, http.StatusOK, map[string]any{"available": available})
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if os.Getenv("RILL_ALLOW_REMOTE_SETUP") != "1" {
		// SourceIP resolves X-Forwarded-For when behind a configured trusted
		// proxy; falls back to RemoteAddr's host otherwise. Important: a raw
		// r.RemoteAddr check would let a same-host reverse proxy bypass this
		// gate (RemoteAddr would be 127.0.0.1 even for a request from the
		// internet).
		host := s.authMgr.SourceIP(r)
		if host != "127.0.0.1" && host != "::1" {
			writeForbidden(w, "setup from remote networks is disabled. Set RILL_ALLOW_REMOTE_SETUP=1 to enable")
			return
		}
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeBadRequest(w, "username and password are required")
		return
	}
	if req.Password != req.Confirm {
		writeValidationError(w, "passwords do not match", map[string]any{"field": "confirm"})
		return
	}
	if len(req.Password) < 8 {
		writeValidationError(w, "password must be at least 8 characters", map[string]any{"field": "password"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeInternalError(w, "setup_hash_password", err, "")
		return
	}

	if _, err := s.authMgr.DB().Create(r.Context(), "auth_user", map[string]any{
		"username":      req.Username,
		"password_hash": hash,
	}); err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "UNIQUE") {
			writeConflict(w, "setup is not available — user already exists")
			return
		}
		writeInternalError(w, "setup_create_user", err, "")
		return
	}

	rilllog.Logger().Info("setup: admin user created", "username", req.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFromContext(r.Context())
	if id.Type == "" {
		writeUnauthorized(w, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username": id.Name,
		"type":     id.Type,
		"mode":     string(s.authMgr.Mode),
	})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidcClient == nil || !s.oidcCfg.Enabled {
		writeNotFound(w, "OIDC not configured")
		return
	}
	callbackURI := s.oidcCfg.PublicURL + "/api/auth/oidc/callback"
	state, codeVerifier, nonce := s.oidcStore.Create("/")
	authURL := s.oidcClient.AuthorizationURL(state, codeVerifier, nonce, callbackURI)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidcClient == nil || !s.oidcCfg.Enabled {
		writeNotFound(w, "OIDC not configured")
		return
	}
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		writeBadRequest(w, "missing code or state")
		return
	}
	entry := s.oidcStore.Consume(state)
	if entry == nil {
		writeBadRequest(w, "invalid or expired state")
		return
	}
	callbackURI := s.oidcCfg.PublicURL + "/api/auth/oidc/callback"
	ctx := r.Context()
	rawIDToken, _, err := s.oidcClient.ExchangeCode(ctx, code, entry.CodeVerifier, callbackURI)
	if err != nil {
		writeInternalError(w, "oidc_callback_token_exchange", err, "")
		return
	}
	claims, err := s.oidcClient.ValidateIDToken(ctx, rawIDToken)
	if err != nil {
		writeInternalError(w, "oidc_callback_id_token_validation", err, "")
		return
	}
	if entry.Nonce == "" || claims.Nonce != entry.Nonce {
		reqID := requestID()
		rilllog.Logger().Warn("oidc callback: nonce mismatch", "req_id", reqID)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oidc nonce mismatch"})
		return
	}
	user, err := s.authMgr.GetOrCreateOIDCUser(ctx, claims)
	if err != nil {
		writeInternalError(w, "oidc_callback_user_lookup", err, "")
		return
	}
	userAgent := r.Header.Get("User-Agent")
	sourceIP := s.authMgr.SourceIP(r)
	cookieValue, err := s.authMgr.CreateSession(ctx, user.ID, userAgent, sourceIP)
	if err != nil {
		writeInternalError(w, "oidc_callback_session_create", err, "")
		return
	}
	auth.SetSessionCookie(w, cookieValue, r)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var hasUsers bool
	if s.authMgr != nil && s.authMgr.DB() != nil {
		rows, _ := s.authMgr.DB().Query(ctx, "SELECT count() AS cnt FROM auth_user GROUP ALL", nil)
		hasUsers = len(rows) > 0 && db.IntField(rows[0], "cnt") > 0
	}
	providers := []string{"local"}
	if s.oidcCfg.Enabled {
		providers = append(providers, "oidc")
	}
	if s.authMgr != nil && s.authMgr.ProxyEnabled() {
		providers = append(providers, "proxy")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"has_users":     hasUsers,
		"oidc_enabled":  s.oidcCfg.Enabled,
		"proxy_enabled": s.authMgr != nil && s.authMgr.ProxyEnabled(),
		"providers":     providers,
	})
}

func firstRow(rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}
