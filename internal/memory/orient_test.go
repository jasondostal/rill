package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jasondostal/rill/internal/auth"
)

func TestCompactCardForOrient(t *testing.T) {
	longFact := strings.Repeat("alpha bravo charlie delta ", 20) // ~520 chars
	card := strings.Join([]string{
		"## Identity",
		"- " + longFact + " _(claude, 2026-06-18)_",
		"",
		"## Active edges",
		"- uses → **Claude Design** _(tool)_",
		"",
		"## Preferences",
		"- _(positive)_ **cave diving**",
		"",
		"## Facts",
		"- " + longFact + " _(claude, 2026-06-12)_",
		"- short fact stays whole _(claude, 2026-06-01)_",
		"",
		"## Decisions",
		"- " + longFact + " _(claude, 2026-06-15)_",
	}, "\n")

	got := compactCardForOrient(card, 200)

	// max <= 0 is a no-op.
	if compactCardForOrient(card, 0) != card {
		t.Fatalf("max=0 should return the card verbatim")
	}

	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 260 {
			t.Errorf("line not truncated (%d runes): %q", len([]rune(line)), line)
		}
	}

	// Attribution survives truncation on the prose sections.
	for _, suffix := range []string{"_(claude, 2026-06-18)_", "_(claude, 2026-06-12)_", "_(claude, 2026-06-15)_"} {
		if !strings.Contains(got, suffix) {
			t.Errorf("attribution %q dropped after truncation", suffix)
		}
	}

	// Terse sections pass through untouched.
	for _, keep := range []string{
		"- uses → **Claude Design** _(tool)_",
		"- _(positive)_ **cave diving**",
		"- short fact stays whole _(claude, 2026-06-01)_",
		"## Identity", "## Active edges", "## Preferences", "## Facts", "## Decisions",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("expected untouched line/header missing: %q", keep)
		}
	}
}

func TestSpliceDelta(t *testing.T) {
	body := "# orient — global\n_generated 2026-08-02T00:00:00Z_\n\n## Rules\n- a rule\n"
	delta := "## Since last orient (2d ago)\n\n**New memories:** 0\n\n"

	got := spliceDelta(body, delta)
	if !strings.Contains(got, delta) {
		t.Fatalf("delta text not present verbatim in spliced body:\n%s", got)
	}
	if !strings.HasPrefix(got, "# orient — global\n_generated 2026-08-02T00:00:00Z_\n\n## Since last orient") {
		t.Errorf("expected delta spliced immediately after the header block, got:\n%s", got)
	}
	idxDelta := strings.Index(got, "## Since last orient")
	idxRules := strings.Index(got, "## Rules")
	if idxDelta < 0 || idxRules < 0 || idxDelta > idxRules {
		t.Errorf("expected delta to land before Rules, got:\n%s", got)
	}

	// An empty delta (soft-failed or otherwise skipped) is a pure no-op —
	// the cached body must come back byte-for-byte unchanged.
	if got := spliceDelta(body, ""); got != body {
		t.Errorf("empty delta should return body unchanged, got:\n%s", got)
	}
}

func TestCallerKeyFromContext(t *testing.T) {
	if got := callerKeyFromContext(context.Background()); got != "anonymous" {
		t.Errorf("expected \"anonymous\" for a context with no identity, got %q", got)
	}
	ctx := auth.WithIdentity(context.Background(), auth.Identity{Type: "bearer", Name: "alice-token"})
	if got := callerKeyFromContext(ctx); got != "bearer/alice-token" {
		t.Errorf("expected \"bearer/alice-token\", got %q", got)
	}
}

func TestRenderOrientMapBody(t *testing.T) {
	data := orientMapData{
		DormantProjects: []mapProjectRow{
			{Name: "scorch-rs", Summary: "DOS Scorched Earth clone. Roadmap in PARITY.md."},
			{Name: "bolo-tui", DerivedCard: "## Facts\n- Faithful Bolo TUI remake in Rust/ratatui _(claude, 2026-06-01)_\n"},
			{Name: "no-hook-project"},
		},
		DocTotal:  3,
		DocSample: []docTitleRow{{Title: "Foundry lab writeup", DocType: "writeup"}},
		EntityCounts: map[string]int{
			"person": 2, "project": 3, "tool": 5,
		},
		TopEntities: []mapEntityRow{
			{Name: "rill", Type: "project", MentionCount: 40},
			{Name: "Jason", Type: "person", MentionCount: 30},
		},
	}

	got := renderOrientMapBody(data)

	if !strings.HasPrefix(got, "## Map\n\n") {
		t.Fatalf("expected Map section header first, got:\n%s", got)
	}

	// Dormant projects render by name, with a hook derived from summary (first
	// sentence) or derived_card (first Facts line), and bare when neither exists.
	for _, want := range []string{
		"- scorch-rs — DOS Scorched Earth clone.",
		"- bolo-tui — Faithful Bolo TUI remake in Rust/ratatui",
		"- no-hook-project\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected dormant-project line %q missing from:\n%s", want, got)
		}
	}

	// Documents: total count always shown, sample titles + doc_type, overflow noted.
	if !strings.Contains(got, "**Documents:** 3") {
		t.Errorf("expected document total count, got:\n%s", got)
	}
	if !strings.Contains(got, "- Foundry lab writeup (writeup)") {
		t.Errorf("expected document title line, got:\n%s", got)
	}
	if !strings.Contains(got, "_...and 2 more_") {
		t.Errorf("expected overflow note (3 total - 1 sampled = 2), got:\n%s", got)
	}

	// Entities: counts-by-type line, then top-mentioned names.
	if !strings.Contains(got, "person: 2") || !strings.Contains(got, "project: 3") || !strings.Contains(got, "tool: 5") {
		t.Errorf("expected entity type counts, got:\n%s", got)
	}
	if !strings.Contains(got, "Top: rill (project), Jason (person)") {
		t.Errorf("expected top-entities line, got:\n%s", got)
	}

	// Closing pointer line, always last.
	if !strings.Contains(got, "_pull: get_entity · doc_get · recall · orient(project=…)_") {
		t.Errorf("expected closing pointer line, got:\n%s", got)
	}
}

func TestRenderOrientMapBody_EmptyIsStillWellFormed(t *testing.T) {
	got := renderOrientMapBody(orientMapData{EntityCounts: map[string]int{}})
	if !strings.Contains(got, "**Other projects (0):**") {
		t.Errorf("expected zero-count header even when empty, got:\n%s", got)
	}
	if !strings.Contains(got, "**Documents:** 0") {
		t.Errorf("expected zero document count, got:\n%s", got)
	}
	if !strings.Contains(got, "_pull: get_entity · doc_get · recall · orient(project=…)_") {
		t.Errorf("expected closing pointer line even when empty, got:\n%s", got)
	}
}

func TestMapHook(t *testing.T) {
	if got := mapHook("Short summary.", ""); got != "Short summary." {
		t.Errorf("expected summary-derived hook, got %q", got)
	}
	if got := mapHook("First sentence. Second sentence.", ""); got != "First sentence." {
		t.Errorf("expected only the first sentence, got %q", got)
	}
	card := "## Facts\n- has a Facts line _(claude, 2026-06-01)_\n"
	if got := mapHook("", card); got != "has a Facts line" {
		t.Errorf("expected Facts-line fallback with attribution stripped, got %q", got)
	}
	if got := mapHook("", ""); got != "" {
		t.Errorf("expected empty hook when neither summary nor Facts line exist, got %q", got)
	}
}

func TestRenderFocusBody(t *testing.T) {
	lastEdited := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	detail := &EntityDetail{
		EntityRow: EntityRow{
			ID:           "project:rill",
			Name:         "rill",
			Aliases:      []string{"rill", "the memory server"},
			HandNotes:    "Rill is Jason's SurrealDB-backed memory server.\n",
			DerivedCard:  "## Facts\n- ships via docker compose _(claude, 2026-07-01)_\n",
			LastEditedBy: "claude",
			LastEditedAt: &lastEdited,
		},
		Edges: []EntityEdge{
			{Predicate: "uses", Direction: "out", OtherID: "tool:surrealdb", OtherName: "SurrealDB", OtherType: "tool", Active: true},
			{Predicate: "works_at", Direction: "in", OtherID: "person:jason", OtherName: "Jason", OtherType: "person", Active: false},
		},
	}
	summaries := map[string]string{
		"tool:surrealdb": "Embedded multi-model database.",
	}
	docs := []docTitleRow{{Title: "rill design spec", DocType: "primer"}}
	loops := []openLoopRow{{Summary: "finish the orient v2 rollout", OpenedAt: "2026-08-01T00:00:00Z"}}
	recent := []recentMem{{Summary: "shipped open loops", Kind: "decision", Author: "claude", CreatedAt: "2026-08-02T00:00:00Z"}}

	got := renderFocusBody(detail, summaries, docs, loops, recent, 200)

	if !strings.Contains(got, "## Focus: rill _(the memory server)_") {
		t.Errorf("expected Focus header with non-canonical alias, got:\n%s", got)
	}
	// Full card — hand_notes AND derived_card, untruncated.
	if !strings.Contains(got, "Rill is Jason's SurrealDB-backed memory server.") {
		t.Errorf("expected hand_notes in full card, got:\n%s", got)
	}
	if !strings.Contains(got, "## Facts\n- ships via docker compose") {
		t.Errorf("expected derived_card in full card, got:\n%s", got)
	}
	// 1-hop edges, both directions, with neighbor type + one-line summary + closed marker.
	if !strings.Contains(got, "- uses → **SurrealDB** _(tool)_: Embedded multi-model database.") {
		t.Errorf("expected active out-edge with neighbor summary, got:\n%s", got)
	}
	if !strings.Contains(got, "- works_at ← **Jason** _(person)_ _(closed)_") {
		t.Errorf("expected closed in-edge marked, got:\n%s", got)
	}
	// Scoped docs, open loops, recent memories.
	if !strings.Contains(got, "- rill design spec (primer)") {
		t.Errorf("expected scoped document title, got:\n%s", got)
	}
	if !strings.Contains(got, "finish the orient v2 rollout") || !strings.Contains(got, "opened 2026-08-01") {
		t.Errorf("expected scoped open loop, got:\n%s", got)
	}
	if !strings.Contains(got, "shipped open loops") {
		t.Errorf("expected scoped recent memory, got:\n%s", got)
	}

	// Focus mode never renders the promoted-entity dumps — renderFocusBody
	// structurally can't (it never writes these headers), which is the
	// guarantee that matters since Focus mode replaces them entirely.
	for _, absent := range []string{"## Active topics", "## Active tools", "## Active organizations", "## Active places", "## Active projects"} {
		if strings.Contains(got, absent) {
			t.Errorf("Focus render must omit promoted-entity dumps, found %q in:\n%s", absent, got)
		}
	}
}

func TestRenderFocusBody_NoCard(t *testing.T) {
	detail := &EntityDetail{EntityRow: EntityRow{ID: "project:ghost", Name: "ghost"}}
	got := renderFocusBody(detail, nil, nil, nil, nil, 200)
	if !strings.Contains(got, "## Focus: ghost") {
		t.Errorf("expected Focus header even with no card content, got:\n%s", got)
	}
}
