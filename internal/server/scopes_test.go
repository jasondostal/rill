package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jasondostal/rill/internal/auth"
)

// reqWithScopes builds a request carrying an identity with the given scopes.
func reqWithScopes(method, body string, scopes []string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, "/x", strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, "/x", nil)
	}
	return r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{Type: "bearer", Scopes: scopes}))
}

// TestRequireScope checks the additive scope model.
func TestRequireScope(t *testing.T) {
	h := &restHandler{}
	cases := []struct {
		have []string
		want string
		ok   bool
	}{
		{[]string{"read"}, "read", true},
		{[]string{"read"}, "write", false},
		{[]string{"read"}, "admin", false},
		{[]string{"read", "write"}, "write", true},
		{[]string{"read", "write"}, "admin", false},
		{[]string{"read", "write", "admin"}, "admin", true},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		got := h.requireScope(w, reqWithScopes("POST", "", c.have), c.want)
		if got != c.ok {
			t.Errorf("requireScope(have=%v, want=%s) = %v, want %v", c.have, c.want, got, c.ok)
		}
		if !c.ok && w.Code != http.StatusForbidden {
			t.Errorf("deny should write 403, got %d", w.Code)
		}
	}
}

// TestRESTScopeGates_Deny verifies that under-scoped identities are blocked at
// the gate (before any store access — so a nil store is never reached). This is
// the regression guard for H1: a read token must not write/forget, and a
// non-admin token must not touch token CRUD or settings.
func TestRESTScopeGates_Deny(t *testing.T) {
	h := &restHandler{}
	read := []string{"read"}
	write := []string{"read", "write"}

	writeGated := map[string]http.HandlerFunc{
		"remember":   h.handleRemember,
		"editMemory": h.handleEditMemory,
		"forget":     h.handleForget,
		"merge":      h.handleMergeEntity,
		"addEdge":    h.handleAddEdge,
		"closeEdge":  h.handleCloseEdge,
		"promote":    h.handlePromote,
		"demote":     h.handleDemote,
		"setVersion": h.handleSetVersion,
		"handNotes":  h.handleEditHandNotes,
		"putDoc":     h.handlePutDoc,
	}
	for name, fn := range writeGated {
		w := httptest.NewRecorder()
		fn(w, reqWithScopes("POST", "", read))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s with read scope: got %d, want 403", name, w.Code)
		}
	}

	adminGated := map[string]http.HandlerFunc{
		"createToken":   h.createToken,
		"revokeToken":   h.revokeToken,
		"listTokens":    h.listTokens,
		"updateSetting": h.handleUpdateSetting,
		"getSettings":   h.handleGetSettings,
	}
	for name, fn := range adminGated {
		w := httptest.NewRecorder()
		fn(w, reqWithScopes("POST", "", write)) // has write but not admin
		if w.Code != http.StatusForbidden {
			t.Errorf("%s with write scope: got %d, want 403", name, w.Code)
		}
	}
}

// TestCreateTokenRejectsUnknownScope verifies the scope allowlist on creation
// (admin identity passes the gate, then the bogus scope is rejected with 400).
func TestCreateTokenRejectsUnknownScope(t *testing.T) {
	h := &restHandler{}
	admin := []string{"read", "write", "admin"}
	w := httptest.NewRecorder()
	h.createToken(w, reqWithScopes("POST", `{"name":"x","scopes":["superuser"]}`, admin))
	if w.Code != http.StatusBadRequest {
		t.Errorf("bogus scope: got %d, want 400", w.Code)
	}
}
