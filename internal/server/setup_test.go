package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
)

// Setup makes the first account and then closes for good. The localhost gate
// was once bypassable with a caller-supplied X-Forwarded-For, and POST never
// looked at whether an account already existed — together, anyone who could
// reach the server could add themselves an admin.
func TestSetup_OnlyCreatesTheFirstUser_AndIgnoresSpoofedForwardedFor(t *testing.T) {
	database, err := db.Connect(db.Config{URL: "ws://localhost:8000", User: "root", Pass: "root", NS: "rill_setup_test", DB: "rill_setup_test"})
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer database.Close()
	ctx := context.Background()
	if err := database.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}
	if _, err := database.Query(ctx, "DELETE auth_user", nil); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RILL_TRUSTED_PROXY_IPS", "10.20.0.0/16,127.0.0.1")
	t.Setenv("RILL_ALLOW_REMOTE_SETUP", "")
	s := &Server{authMgr: auth.NewManager(database, auth.ModeLocal)}

	post := func(remote, xff, user string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/setup",
			strings.NewReader(`{"username":"`+user+`","password":"longenough1","confirm":"longenough1"}`))
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		s.handleSetup(w, r)
		return w.Code
	}

	// An internet client behind the proxy claiming to be loopback: the proxy
	// appended the real address on the right, and that is the one that counts.
	if code := post("10.20.0.4:5000", "127.0.0.1, 198.51.100.7", "mallory"); code != http.StatusForbidden {
		t.Fatalf("spoofed X-Forwarded-For got %d, want 403", code)
	}
	if code := post("127.0.0.1:5000", "", "owner"); code != http.StatusOK {
		t.Fatalf("first local setup got %d, want 200", code)
	}
	// Even from localhost, setup is finished once a user exists.
	if code := post("127.0.0.1:5000", "", "second"); code != http.StatusConflict {
		t.Fatalf("second setup got %d, want 409", code)
	}
	rows, _ := database.Query(ctx, "SELECT username FROM auth_user", nil)
	if len(rows) != 1 {
		t.Fatalf("want exactly one user, got %v", rows)
	}
}
