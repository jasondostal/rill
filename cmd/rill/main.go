package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
	"github.com/jasondostal/rill/internal/document"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/jasondostal/rill/internal/mcp"
	mcptools "github.com/jasondostal/rill/internal/mcp/tools"
	"github.com/jasondostal/rill/internal/memory"
	"github.com/jasondostal/rill/internal/server"
	"github.com/jasondostal/rill/internal/settings"
)

func main() {
	// Server-side maintenance op with direct Store access (not a REST client
	// command), so it's intercepted before Cobra — like "serve". Run inside the
	// container where the SurrealDB + embedder env live.
	if len(os.Args) > 1 && os.Args[1] == "reindex-embeddings" {
		runReindex(os.Args[2:])
		return
	}

	// Server mode: only when "serve" is the first arg. Everything else
	// (no args, subcommands, --help, etc.) goes through Cobra.
	if len(os.Args) <= 1 || os.Args[1] != "serve" {
		root := BuildCommands()
		if err := root.Execute(); err != nil {
			fmt.Fprintf(os.Stderr, "rill: %v\n", err)
			os.Exit(1)
		}
		return
	}

	port := os.Getenv("RILL_PORT")
	if port == "" {
		port = "8080"
	}
	// RILL_BIND restricts the listen interface (e.g. 127.0.0.1 or a LAN IP).
	// Empty = all interfaces. This env var was previously read into the launch
	// environment but never honored; wiring it in lets the listener be narrowed
	// where the topology allows. Behind a reverse proxy on a separate host the
	// listener must stay reachable, so a host firewall remains the primary
	// network control there.
	bindHost := os.Getenv("RILL_BIND")

	// Process-lifetime context — cancelled on SIGINT/SIGTERM.
	rootCtx, cancelRoot := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancelRoot()

	// Auth DB connection (SurrealDB WebSocket). Owns the auth tables only.
	cfg := db.ConfigFromEnv()
	database, err := db.Connect(cfg)
	if err != nil {
		rilllog.Logger().Error("auth db connect", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := database.SetupSchema(rootCtx); err != nil {
		rilllog.Logger().Error("auth schema", "error", err)
		os.Exit(1)
	}

	// Memory store — the intentional-memory backbone. Talks to the same
	// SurrealDB the auth tables live in (RILL_SURREAL_URL, default
	// http://127.0.0.1:8001, ns=rill, db=main). The single canonical schema
	// (internal/db/schema.surql, embedded) is applied idempotently on boot by
	// SetupSchema — memory, entities, edges, version_is, and auth/oauth.
	// Runtime settings service — resolves config from env > DB > default and
	// backs the settings UI. Init loads any stored overrides (best-effort).
	settings.Init(rootCtx)

	memStore := memory.NewFromEnv()
	if err := memStore.Ping(rootCtx); err != nil {
		rilllog.Logger().Error("memory store unreachable", "error", err,
			"url", os.Getenv("RILL_SURREAL_URL"))
		os.Exit(1)
	}
	rilllog.Logger().Info("memory store connected")

	// Document store — standalone markdown docs. Shares the WebSocket DB handle
	// (not memory's HTTP /sql client) so large bodies ride as bound params past
	// the /sql size limit. Separate table (document + doc_about); deliberately
	// NOT wired into the memory/embedding/orient pipeline — a sibling, not a memory.
	docStore := document.New(database)

	// Auth mode: local (default) / proxy / oidc.
	mode := os.Getenv("RILL_AUTH_MODE")
	if mode == "" {
		mode = "local"
	}
	if mode == "sso" {
		mode = "proxy"
	}
	if mode != "local" && mode != "proxy" && mode != "oidc" {
		rilllog.Logger().Error("rill: invalid RILL_AUTH_MODE — must be 'local', 'proxy', or 'oidc'", "mode", mode)
		os.Exit(1)
	}
	authMgr := auth.NewManager(database, auth.Mode(mode))

	// Footgun guard for misconfigured local mode on non-loopback bind.
	if mode == "local" && os.Getenv("RILL_BIND") != "127.0.0.1" && os.Getenv("RILL_BIND") != "localhost" {
		rilllog.Logger().Warn("auth: RILL_AUTH_MODE=local but server is bound to all interfaces — /setup will be unusable from remote clients without RILL_AUTH_MODE=sso + proxy headers")
	}

	// OIDC (optional).
	oidcCfg := auth.LoadOIDCConfig()
	var oidcClient *auth.OIDCClient
	var oidcStore *auth.OIDCStateStore
	if oidcCfg.Enabled {
		if oidcCfg.Issuer == "" || oidcCfg.ClientID == "" || oidcCfg.ClientSecret == "" {
			rilllog.Logger().Error("rill: RILL_AUTH_MODE=oidc requires RILL_OIDC_ISSUER, RILL_OIDC_CLIENT_ID, and RILL_OIDC_CLIENT_SECRET")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		oidcClient, err = auth.NewOIDCClient(ctx, oidcCfg.Issuer, oidcCfg.ClientID, oidcCfg.ClientSecret)
		if err != nil {
			rilllog.Logger().Error("rill: OIDC client init failed", "error", err)
			os.Exit(1)
		}
		oidcStore = auth.NewOIDCStateStoreWithContext(rootCtx, 10*time.Minute)
		rilllog.Logger().Info("auth: OIDC enabled", "issuer", oidcCfg.Issuer)
	}

	// Bootstrap token for first-time admin setup.
	defaultToken, err := authMgr.EnsureToken(rootCtx)
	if err != nil {
		rilllog.Logger().Error("auth: failed to ensure default token", "error", err)
	} else if defaultToken != nil {
		dataDir := os.Getenv("RILL_DATA_DIR")
		if dataDir == "" {
			dataDir = "."
		}
		tokenPath := filepath.Join(dataDir, "initial-admin-token")
		// #nosec G703 -- dataDir is operator-controlled via RILL_DATA_DIR; filename is fixed.
		if writeErr := os.WriteFile(tokenPath, []byte(defaultToken.Token+"\n"), 0600); writeErr != nil {
			rilllog.Logger().Error("auth: failed to write initial token", "path", tokenPath, "error", writeErr)
			os.Exit(1)
		}
		rilllog.Logger().Info("auth: initial admin token written (mode 0600)", "path", tokenPath)
		rilllog.Logger().Info("auth: bootstrap token expires in 7 days — save it then delete the file")
	}

	if mode == "local" || (mode == "oidc" && !oidcCfg.Enabled) {
		rows, _ := database.Query(rootCtx, "SELECT count() AS cnt FROM auth_user GROUP ALL", nil)
		if len(rows) > 0 && db.IntField(rows[0], "cnt") == 0 {
			rilllog.Logger().Info("auth: NO USERS YET — visit /setup to create the admin account", "port", port)
		}
	}

	// MCP tool registry — memory tools only (plus the discover meta-tool).
	registry := mcp.NewRegistry()
	registry.Register(mcptools.NewDiscoverTool(registry))
	registry.Register(mcptools.NewLoadTool(registry))
	mcptools.RegisterMemoryTools(registry, memStore)
	rilllog.Logger().Info("mcp: memory tools registered (remember, recall, orient, edit_notes, add_edge, close_edge, list_entities, get_entity, list_memories, get_memory, promote, demote, forget, merge_entity, set_version)")
	mcptools.RegisterDocumentTools(registry, docStore)
	rilllog.Logger().Info("mcp: document tools registered (doc_put, doc_get, doc_list, doc_delete)")

	// MCP handler options.
	mcpOpts := mcp.HandlerOpts{}
	switch os.Getenv("RILL_COMPACT_TOOLS") {
	case "1", "true":
		mcpOpts.CompactTools = true
	case "names":
		mcpOpts.NamesOnly = true
	}
	mcpHandler := mcp.NewHandler(registry, mcpOpts)

	// MCP OAuth manager (for `claude mcp add rill` flow).
	var mcpOAuthMgr *auth.MCPOAuthManager
	if oidcCfg.Enabled && oidcClient != nil {
		mcpOAuthMgr = auth.NewMCPOAuthManager(database, oidcClient, oidcCfg, oidcCfg.PublicURL)
	}

	srv := server.New(mcpHandler, authMgr, database, memStore, docStore, oidcClient, oidcStore, oidcCfg, mcpOAuthMgr)

	var trustedProxies []string
	if v := strings.TrimSpace(os.Getenv("RILL_TRUSTED_PROXY_IPS")); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				trustedProxies = append(trustedProxies, p)
			}
		}
	}
	if (mode == "proxy" || mode == "oidc") && len(trustedProxies) == 0 {
		rilllog.Logger().Error("rill: RILL_TRUSTED_PROXY_IPS is required",
			"mode", mode,
			"reason", "without it the proxy header can be forged by any client; refusing to start")
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              bindHost + ":" + port,
		Handler:           srv.Handler(rootCtx, trustedProxies),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	shutdownErr := make(chan error, 1)
	go func() {
		<-rootCtx.Done()
		rilllog.Logger().Info("rill: shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shutdownErr <- httpSrv.Shutdown(shutdownCtx)
		_ = srv.Close()
	}()

	rilllog.Logger().Info("rill starting", "port", port)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		rilllog.Logger().Error("server stopped", "error", err)
		os.Exit(1)
	}
	if err := <-shutdownErr; err != nil {
		rilllog.Logger().Error("rill: shutdown error", "error", err)
	}
	rilllog.Logger().Info("rill: shutdown complete")
}
