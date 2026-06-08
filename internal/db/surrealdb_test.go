package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/jasondostal/rill/internal/db"
)

// liveSurrealURL returns the URL for a live SurrealDB to test against, or
// skips the test cleanly when RILL_TEST_SURREAL_URL is not set. CI sets it
// to the workflow's in-memory SurrealDB; local devs opt in by exporting it.
// Matches the convention used by internal/memory/integration_test.go.
func liveSurrealURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("RILL_TEST_SURREAL_URL")
	if url == "" {
		t.Skip("RILL_TEST_SURREAL_URL not set; skipping live-DB test")
	}
	return url
}

func TestConnectAndPing(t *testing.T) {
	cfg := db.Config{
		URL:  liveSurrealURL(t),
		User: "root",
		Pass: "root",
		NS:   "rill_test",
		DB:   "rill_test",
	}

	d, err := db.Connect(cfg)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer d.Close()

	if err := d.Ping(context.Background()); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestSetupSchema(t *testing.T) {
	cfg := db.Config{
		URL:  liveSurrealURL(t),
		User: "root",
		Pass: "root",
		NS:   "rill_schema_test",
		DB:   "rill_schema_test",
	}

	d, err := db.Connect(cfg)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	defer d.Close()

	ctx := context.Background()
	if err := d.SetupSchema(ctx); err != nil {
		t.Fatalf("schema setup failed: %v", err)
	}

	// Verify the canonical schema applied by round-tripping a v3 memory row
	// (summary + kind from the closed set + author are all required).
	_, err = d.Create(ctx, "memory", map[string]any{
		"summary": "test memory",
		"kind":    "fact",
		"author":  "test",
		"project": "test",
	})
	if err != nil {
		t.Fatalf("insert memory failed: %v", err)
	}

	rows, err := d.Query(ctx, "SELECT * FROM memory WHERE project = $project", map[string]any{"project": "test"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least one row")
	}
}

func TestSplitRecordID_Valid(t *testing.T) {
	tests := []struct {
		input     string
		wantTable string
		wantID    string
		wantOK    bool
	}{
		{"memory:abc123", "memory", "abc123", true},
		{"memory:abc_123", "memory", "abc_123", true},
		{"memory:abc-123", "memory", "abc-123", true},
		{"auth_token:89fr1pri0xjw496vs785", "auth_token", "89fr1pri0xjw496vs785", true},
		{"", "", "", false},
		{"no-colon", "", "", false},
		{":empty-table", "", "", false},
		{"empty-id:", "", "", false},
		{"bad-table!:123", "", "", false},
		{"memory:bad;id", "", "", false},
		{"memory:bad'id", "", "", false},
		{"memory:bad\"id", "", "", false},
		{"memory:bad`id", "", "", false},
		{"memory:bad$id", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			table, id, ok := db.SplitRecordID(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("SplitRecordID(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if table != tt.wantTable || id != tt.wantID {
				t.Errorf("SplitRecordID(%q) = (%q, %q), want (%q, %q)",
					tt.input, table, id, tt.wantTable, tt.wantID)
			}
		})
	}
}

func TestSplitRecordID_InjectionPayloads(t *testing.T) {
	payloads := []string{
		"memory:1; DROP TABLE memory",
		"memory:1' OR '1'='1",
		"memory:1\" OR \"1\"=\"1",
		"memory:1`",
		"memory:1$",
		"memory:1@",
		"memory:1#",
		"memory:1%",
		"memory:1&",
		"memory:1*",
		"memory:1(",
		"memory:1+",
		"memory:1=",
		"memory:1[",
		"memory:1{",
		"memory:1<",
		"memory:1>",
		"memory:1/",
		"memory:1\\",
		"memory:1|",
	}
	for _, p := range payloads {
		t.Run(p, func(t *testing.T) {
			_, _, ok := db.SplitRecordID(p)
			if ok {
				t.Errorf("SplitRecordID(%q) should reject injection payload", p)
			}
		})
	}
}

func TestRequireTable(t *testing.T) {
	if err := db.RequireTable("memory:abc123", "memory"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := db.RequireTable("entity:abc123", "memory"); err == nil {
		t.Error("expected error for wrong table")
	}
	if err := db.RequireTable("bad-id", "memory"); err == nil {
		t.Error("expected error for invalid ID")
	}
}
