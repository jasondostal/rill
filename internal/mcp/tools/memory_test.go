package tools

import (
	"encoding/json"
	"testing"

	"github.com/jasondostal/rill/internal/memory"
)

func TestParseRememberParams(t *testing.T) {
	t.Run("structured path decodes normally", func(t *testing.T) {
		in := json.RawMessage(`{
			"summary": "s", "kind": "fact", "author": "claude",
			"entities": [{"name":"Jason Dostal","type":"person"}]
		}`)
		p, err := parseRememberParams(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Kind != memory.KindFact || p.Summary != "s" || p.Author != "claude" {
			t.Fatalf("bad decode: %+v", p)
		}
		if len(p.Entities) != 1 || p.Entities[0].Name != "Jason Dostal" {
			t.Fatalf("entities not decoded: %+v", p.Entities)
		}
	})

	t.Run("payload escape hatch is parsed", func(t *testing.T) {
		inner := `{"summary":"s2","kind":"decision","author":"claude","entities":[{"name":"rill","type":"project"}]}`
		b, _ := json.Marshal(map[string]string{"payload": inner})
		p, err := parseRememberParams(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Kind != memory.KindDecision || p.Summary != "s2" {
			t.Fatalf("payload not honored: %+v", p)
		}
		if len(p.Entities) != 1 || p.Entities[0].Name != "rill" {
			t.Fatalf("payload entities not decoded: %+v", p.Entities)
		}
	})

	// The actual bug: a client mangles the entities array and drops the sibling
	// `kind` (arrives empty), but also passes the whole thing via payload. The
	// payload must win so validation sees a real kind.
	t.Run("payload wins over mangled top-level fields", func(t *testing.T) {
		inner := `{"summary":"real","kind":"insight","author":"claude"}`
		b, _ := json.Marshal(map[string]any{
			"kind":    "", // mangled/dropped by the client
			"summary": "",
			"payload": inner,
		})
		p, err := parseRememberParams(b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Kind != memory.KindInsight {
			t.Fatalf("expected payload kind to win, got %q", p.Kind)
		}
		if p.Summary != "real" {
			t.Fatalf("expected payload summary to win, got %q", p.Summary)
		}
	})

	t.Run("blank payload falls back to structured fields", func(t *testing.T) {
		in := json.RawMessage(`{"summary":"s","kind":"fact","author":"claude","payload":"   "}`)
		p, err := parseRememberParams(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Kind != memory.KindFact || p.Summary != "s" {
			t.Fatalf("blank payload should not override: %+v", p)
		}
	})
}
