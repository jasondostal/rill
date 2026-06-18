package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/surrealdb/surrealdb.go/pkg/models"
)

const (
	sessionCookieName        = "rill_session"
	sessionDuration          = 7 * 24 * time.Hour
	sessionRefreshIfOlderThan = 1 * time.Hour
)

// CreateSession issues a new session for a user. Returns the cookie value
// to set on the client — the hash is what gets stored.
func (m *Manager) CreateSession(ctx context.Context, userID, userAgent, sourceIP string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	cookieValue := "rs_" + hex.EncodeToString(raw)
	h := sha256.Sum256([]byte(cookieValue))

	// SurrealDB v3 enforces the `record<auth_user>` type on the user field
	// strictly — passing the bare string "auth_user:abc123" fails. Wrap it
	// into a models.RecordID so the SDK serializes it as a record link
	// rather than a string literal.
	_, idPart, ok := db.SplitRecordID(userID)
	if !ok {
		return "", fmt.Errorf("session create: invalid user id %q", userID)
	}
	userRef := models.RecordID{Table: "auth_user", ID: idPart}

	data := map[string]any{
		"user":       userRef,
		"token_hash": hex.EncodeToString(h[:]),
		"created_at": time.Now().UTC(),
		"expires_at": time.Now().UTC().Add(sessionDuration),
		"user_agent": userAgent,
		"source_ip":  sourceIP,
	}
	if _, err := m.db.Create(ctx, "auth_session", data); err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}
	return cookieValue, nil
}

// ValidateSession reads the session cookie and returns the username if valid.
func (m *Manager) ValidateSession(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	h := sha256.Sum256([]byte(c.Value))
	rows, qErr := m.db.Query(r.Context(),
		`SELECT id, user.username AS username, expires_at, last_used
		 FROM auth_session WHERE token_hash = $hash LIMIT 1`,
		map[string]any{"hash": hex.EncodeToString(h[:])})
	if qErr != nil || len(rows) == 0 {
		return "", false
	}

	// Expiry check. db.TimeField handles all three shapes the SurrealDB
	// driver returns (CustomDateTime / time.Time / RFC3339 string) — the
	// previous .(string) cast silently passed for non-string types, which
	// would have granted access against an expired session.
	if exp := db.TimeField(rows[0], "expires_at"); !exp.IsZero() && time.Now().After(exp) {
		return "", false
	}

	username, _ := rows[0]["username"].(string)

	// Best-effort last_used refresh — skip if recent to avoid hot writes.
	if lu := db.TimeField(rows[0], "last_used"); !lu.IsZero() && time.Since(lu) > sessionRefreshIfOlderThan {
		sessionID := db.RecordID(rows[0])
		if _, err := m.db.QueryRecord(r.Context(), "UPDATE %s SET last_used = $now", sessionID,
			map[string]any{"now": time.Now().UTC()}); err != nil {
			rilllog.Logger().Warn("session last_used update failed", "error", err)
		}
	}

	return username, true
}

// RevokeSession deletes a session by cookie value.
func (m *Manager) RevokeSession(ctx context.Context, cookieValue string) error {
	h := sha256.Sum256([]byte(cookieValue))
	_, err := m.db.Query(ctx,
		"DELETE auth_session WHERE token_hash = $hash",
		map[string]any{"hash": hex.EncodeToString(h[:])})
	return err
}

// SetSessionCookie writes a properly-flagged session cookie on the response.
// Secure is conditional on isHTTPS(r): prod (behind TLS) gets Secure=true; local
// HTTP dev gets Secure=false because browsers reject Secure cookies over HTTP.
// HttpOnly + SameSite=Strict are unconditional. Strict (vs Lax) means the cookie
// is never sent on cross-site requests at all — defense-in-depth CSRF hardening
// for an admin surface. The OIDC login flow is unaffected: the cookie is *set*
// on the callback (Set-Cookie honored regardless of SameSite), and the callback
// then redirects same-site to the app, which sends it. The only behavior change
// is that a cold external deep-link arrives without the cookie and re-auths.
func SetSessionCookie(w http.ResponseWriter, value string, r *http.Request) {
	// #nosec G124 -- Secure conditional on TLS is intentional; HttpOnly+SameSite set.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// ClearSessionCookie expires the session cookie.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- Secure conditional on TLS is intentional; HttpOnly+SameSite set.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// isHTTPS detects if the original request was over TLS, accounting for
// reverse proxies that terminate TLS upstream.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
