package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
)

// AuditMiddleware logs an audit record for every authenticated request.
// Writes are async/best-effort — DB hiccups don't block the request.
// Uses a buffered channel + small worker pool instead of per-request goroutines.
type auditLogger struct {
	db             *db.DB
	queue          chan auditSnapshot
	dropped        atomic.Int64 // overflow counter, lock-free
	workers        int
	trustedProxies []string
	closeOnce      sync.Once
}

func newAuditLogger(ctx context.Context, d *db.DB, trustedProxies []string) *auditLogger {
	// Queue capacity = 1000. A burst of audit entries shouldn't drop forensic
	// data, and the writer pool drains continuously. If we ever see this
	// queue fill, the upstream signal is bad — an agent in a tight retry loop
	// or a DoS attempt — and the dropped-counter Warn fires loudly enough
	// to investigate. Bumped from 100 because 100 was too tight under any
	// realistic burst from a misbehaving client.
	a := &auditLogger{
		db:             d,
		queue:          make(chan auditSnapshot, 1000),
		workers:        2,
		trustedProxies: trustedProxies,
	}
	for i := 0; i < a.workers; i++ {
		go a.worker()
	}
	// Lifecycle: when ctx is cancelled, close the queue so workers exit.
	if ctx != nil {
		go func() {
			<-ctx.Done()
			a.Close()
		}()
	}
	return a
}

// Close drains the queue (closing it lets workers exit the `range` loop).
// Safe to call multiple times — sync.Once guards the close.
func (a *auditLogger) Close() {
	a.closeOnce.Do(func() {
		close(a.queue)
	})
}

// auditSnapshot captures everything the audit log needs from the request
// BEFORE ServeHTTP returns. Go cancels r.Context() at that point, so we
// must snapshot and use a fresh context inside the goroutine.
type auditSnapshot struct {
	identityType string
	identityName string
	tokenID      string
	sourceIP     string
	method       string
	path         string
	status       int
	agentHarness string
	llmModel     string
}

func (a *auditLogger) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aw := &auditWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(aw, r)

		// Snapshot everything before pushing to queue —
		// r.Context() is cancelled when ServeHTTP returns.
		id := auth.IdentityFromContext(r.Context())
		path := r.URL.Path
		if len(path) > 512 {
			path = path[:512]
		}
		agentHarness := r.Header.Get("X-Agent-Harness")
		if agentHarness == "" {
			agentHarness = detectHarnessFromUA(r.Header.Get("User-Agent"))
		}
		snap := auditSnapshot{
			identityType: id.Type,
			identityName: id.Name,
			tokenID:      id.TokenID,
			sourceIP:     sourceIP(r, a.trustedProxies),
			method:       r.Method,
			path:         path,
			status:       aw.status,
			agentHarness: agentHarness,
			llmModel:     r.Header.Get("X-LLM-Model"),
		}

		select {
		case a.queue <- snap:
		default:
			// Channel full — drop and log counter.
			n := a.dropped.Add(1)
			rilllog.Logger().Warn("audit: dropped entry (queue full)", "total_dropped", n)
		}
	})
}

func (a *auditLogger) worker() {
	for snap := range a.queue {
		a.log(snap)
	}
}

func (a *auditLogger) log(s auditSnapshot) {
	// Tests build the audit logger with a nil DB; don't crash.
	if a.db == nil || a.db.DB() == nil {
		return
	}
	// Detached context — the request is long gone.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var toolName string
	if s.path == "/mcp" || s.path == "/api/mcp" {
		toolName = "mcp"
	}

	outcome := "ok"
	switch {
	case s.status == http.StatusUnauthorized, s.status == http.StatusForbidden, s.status == http.StatusTooManyRequests:
		outcome = "denied"
	case s.status >= 500:
		outcome = "error"
	}

	data := map[string]any{
		"at":            time.Now().UTC(),
		"identity_type": s.identityType,
		"identity_name": s.identityName,
		"source_ip":     s.sourceIP,
		"method":        s.method,
		"path":          s.path,
		"tool":          toolName,
		"token_id":      s.tokenID,
		"status":        s.status,
		"outcome":       outcome,
		"agent_harness": s.agentHarness,
		"llm_model":     s.llmModel,
	}

	defer func() {
		if rec := recover(); rec != nil {
			rilllog.Logger().Error("audit: panic in log goroutine", "recover", rec)
		}
	}()

	if _, err := a.db.Create(ctx, "auth_audit", data); err != nil {
		rilllog.Logger().Error("audit: write failed", "error", err)
	}
}

// detectHarnessFromUA guesses the agent harness from User-Agent string.
func detectHarnessFromUA(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "pi-coding-agent"):
		return "pi"
	case strings.Contains(ua, "claude-desktop"):
		return "claude-desktop"
	case strings.Contains(ua, "claude-code"):
		return "claude-code"
	default:
		return "unknown"
	}
}

// auditWriter captures the HTTP status code from the response.
type auditWriter struct {
	http.ResponseWriter
	status int
}

func (aw *auditWriter) WriteHeader(status int) {
	aw.status = status
	aw.ResponseWriter.WriteHeader(status)
}
