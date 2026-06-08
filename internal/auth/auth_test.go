package auth_test

import (
	"context"
	"testing"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
)

func TestCreateToken_RequiresScopes(t *testing.T) {
	cfg := db.Config{
		URL:  "ws://localhost:8000",
		User: "root",
		Pass: "root",
		NS:   "rill_auth_test",
		DB:   "rill_auth_test",
	}

	database, err := db.Connect(cfg)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}

	mgr := auth.NewManager(database, auth.ModeLocal)

	_, err = mgr.CreateToken(ctx, "test", nil, 0)
	if err == nil {
		t.Fatal("CreateToken with nil scopes should fail")
	}

	_, err = mgr.CreateToken(ctx, "test", []string{}, 0)
	if err == nil {
		t.Fatal("CreateToken with empty scopes should fail")
	}

	tok, err := mgr.CreateToken(ctx, "test", []string{"read"}, 0)
	if err != nil {
		t.Fatalf("CreateToken with valid scopes should succeed: %v", err)
	}
	if len(tok.Scopes) != 1 || tok.Scopes[0] != "read" {
		t.Errorf("token scopes = %v, want [read]", tok.Scopes)
	}
}

func TestEnsureToken_CreatesWithAdminScopes(t *testing.T) {
	cfg := db.Config{
		URL:  "ws://localhost:8000",
		User: "root",
		Pass: "root",
		NS:   "rill_auth_test_ensure",
		DB:   "rill_auth_test_ensure",
	}

	database, err := db.Connect(cfg)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	// Clean slate — remove any prior test data.
	_, _ = database.Query(ctx, "REMOVE TABLE auth_token", nil)
	if err := database.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}

	mgr := auth.NewManager(database, auth.ModeLocal)

	tok, err := mgr.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken failed: %v", err)
	}
	if tok == nil {
		t.Fatal("EnsureToken should create a token when none exist")
	}
	if len(tok.Scopes) != 3 {
		t.Errorf("token scopes = %v, want 3 scopes", tok.Scopes)
	}
}

func TestValidateToken_ReturnsScopes(t *testing.T) {
	cfg := db.Config{
		URL:  "ws://localhost:8000",
		User: "root",
		Pass: "root",
		NS:   "rill_auth_test3",
		DB:   "rill_auth_test3",
	}

	database, err := db.Connect(cfg)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}

	mgr := auth.NewManager(database, auth.ModeLocal)

	tok, err := mgr.CreateToken(ctx, "scoped", []string{"read", "write"}, 0)
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	name, scopes, _, valid := mgr.ValidateToken(ctx, tok.Token)
	if !valid {
		t.Fatal("ValidateToken should succeed for valid token")
	}
	if name != "scoped" {
		t.Errorf("name = %q, want scoped", name)
	}
	if len(scopes) != 2 {
		t.Errorf("scopes = %v, want 2", scopes)
	}
}
