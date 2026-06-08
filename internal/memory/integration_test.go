//go:build integration

// Run with:
//
//	RILL_TEST_SURREAL_URL=http://127.0.0.1:8002 \
//	RILL_TEST_SURREAL_NS=rill_test RILL_TEST_SURREAL_DB=test \
//	go test -tags=integration ./internal/memory/
//
// REFUSES TO RUN against the production memory store. You MUST point
// RILL_TEST_SURREAL_URL at a dedicated test instance (different URL, namespace,
// or database from RILL_SURREAL_URL). Every test wipes every table, so this
// guardrail is non-negotiable.
package memory

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func wipeV3(t *testing.T, c *Client) {
	t.Helper()
	ctx := context.Background()
	tables := []string{
		"memory", "mentions", "assertion", "version_is",
		"works_on", "uses", "prefers", "works_at", "depends_on", "part_of",
		"person", "project", "tool", "organization", "place", "preference", "concept",
		"orient_cache", "app_setting",
	}
	for _, tab := range tables {
		_, _ = c.SQL(ctx, "DELETE "+tab+";", false)
	}
}

// newTestStore connects to a DEDICATED test SurrealDB and wipes it.
//
// Safety contract: the test instance MUST differ from production on at least
// one of URL / namespace / database. Production is whatever RILL_SURREAL_URL
// points at (default http://127.0.0.1:8001 with ns=rill db=main). If the test
// config matches production, the test is skipped — never run a destructive
// wipe against the prod store.
//
// To opt in, set:
//
//	RILL_TEST_SURREAL_URL  (required — distinct from RILL_SURREAL_URL)
//	RILL_TEST_SURREAL_NS   (optional — defaults to rill_test)
//	RILL_TEST_SURREAL_DB   (optional — defaults to test)
func newTestStore(t *testing.T) *Store {
	t.Helper()
	testURL := os.Getenv("RILL_TEST_SURREAL_URL")
	if testURL == "" {
		t.Skip("RILL_TEST_SURREAL_URL not set — refusing to run destructive tests without an explicit dedicated test instance. See file header.")
	}

	prodCfg := ConfigFromEnv() // reads RILL_SURREAL_URL/NS/DB
	testNS := os.Getenv("RILL_TEST_SURREAL_NS")
	if testNS == "" {
		testNS = "rill_test"
	}
	testDB := os.Getenv("RILL_TEST_SURREAL_DB")
	if testDB == "" {
		testDB = "test"
	}
	if testURL == prodCfg.URL && testNS == prodCfg.NS && testDB == prodCfg.DB {
		t.Fatalf("REFUSING TO WIPE PRODUCTION: RILL_TEST_SURREAL_URL/NS/DB %s/%s/%s matches RILL_SURREAL_URL/NS/DB %s/%s/%s. Use a distinct test instance.",
			testURL, testNS, testDB, prodCfg.URL, prodCfg.NS, prodCfg.DB)
	}

	cfg := prodCfg
	cfg.URL = testURL
	cfg.NS = testNS
	cfg.DB = testDB
	c := NewClient(cfg)
	if err := c.Ping(context.Background()); err != nil {
		t.Skipf("no test surreal reachable at %s ns=%s db=%s: %v", cfg.URL, cfg.NS, cfg.DB, err)
	}
	wipeV3(t, c)
	// Embedder is optional; tests are simpler without it (no API calls).
	if os.Getenv("RILL_V3_TEST_USE_EMBEDDER") != "1" {
		return New(c, nil)
	}
	// Embedder still uses prod env vars (no test-specific override yet).
	return New(c, nil)
}

func TestRemember_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	res, err := s.Remember(ctx, RememberPayload{
		Summary: "Pi upgraded to v0.7. Strict flag now required for builds.",
		Details: "Hit during migration on 2026-05-15.",
		Kind:    KindProcedure,
		Tags:    []string{"pi", "upgrade"},
		Author:  "claude",
		Project: "homelab",
		Entities: []EntityDecl{
			{Name: "Pi", Type: EntityTool, Aliases: []string{"pi-mono"}},
			{Name: "v0.7", Type: EntityConcept},
		},
		Edges: []EdgeDecl{
			{Subject: "Pi", SubjectType: EntityTool,
				Predicate: "version_is",
				Object:    "v0.7", ObjectType: EntityConcept},
		},
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if res.MemoryID == "" {
		t.Fatal("expected memory id")
	}
	if len(res.Entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(res.Entities))
	}
	if len(res.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(res.Edges))
	}
}

// Strict validation: every edge endpoint must be declared in entities[].
func TestRemember_RejectsUndeclaredEdgeEndpoints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Pi version v0.7", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{
			{Name: "Pi", Type: EntityTool}, // v0.7 NOT declared
		},
		Edges: []EdgeDecl{
			{Subject: "Pi", SubjectType: EntityTool,
				Predicate: "version_is",
				Object:    "v0.7", ObjectType: EntityConcept},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for undeclared object")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Errorf("expected 'not declared' in error, got: %v", err)
	}
}

func TestRemember_DuplicateTupleBumpsWeight(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	payload := RememberPayload{
		Summary: "Alice uses SurrealDB.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{
			{Name: "Alice", Type: EntityPerson},
			{Name: "SurrealDB", Type: EntityTool},
		},
		Edges: []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson,
			Predicate: "uses", Object: "SurrealDB", ObjectType: EntityTool, Weight: 1.0}},
	}
	r1, err := s.Remember(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Edges) != 1 {
		t.Fatalf("expected 1 edge first time, got %d", len(r1.Edges))
	}
	edgeID1 := r1.Edges[0].ID

	// Same tuple again — should bump, not create.
	r2, err := s.Remember(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Edges) != 1 {
		t.Fatalf("expected 1 (re-used) edge second time, got %d", len(r2.Edges))
	}
	if r2.Edges[0].ID != edgeID1 {
		t.Errorf("expected same edge id (bumped), got new id %s", r2.Edges[0].ID)
	}

	// Count uses-edges — must be exactly 1
	var rows []map[string]any
	err = s.db.First(ctx, "SELECT count() AS n FROM uses GROUP ALL;", &rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || int(rows[0]["n"].(float64)) != 1 {
		t.Errorf("expected exactly 1 uses-edge after dup, got rows=%v", rows)
	}
}

func TestRemember_ExclusivePredicateSupersession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Pi version_is v0.6 (initial)
	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Pi at v0.6.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{
			{Name: "Pi", Type: EntityTool},
			{Name: "v0.6", Type: EntityConcept},
		},
		Edges: []EdgeDecl{{Subject: "Pi", SubjectType: EntityTool,
			Predicate: "version_is", Object: "v0.6", ObjectType: EntityConcept}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now Pi version_is v0.7 — should supersede the v0.6 edge.
	r2, err := s.Remember(ctx, RememberPayload{
		Summary: "Pi upgraded to v0.7.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{
			{Name: "Pi", Type: EntityTool},
			{Name: "v0.7", Type: EntityConcept},
		},
		Edges: []EdgeDecl{{Subject: "Pi", SubjectType: EntityTool,
			Predicate: "version_is", Object: "v0.7", ObjectType: EntityConcept}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Edges) != 1 || r2.Edges[0].Superseded == "" {
		t.Fatalf("expected supersession recorded, got %+v", r2.Edges)
	}

	// Verify the older edge has valid_until set, the new one doesn't.
	var rows []struct {
		ID         string  `json:"id"`
		Object     string  `json:"object_name"`
		ValidFrom  string  `json:"valid_from"`
		ValidUntil *string `json:"valid_until"`
	}
	if err := s.db.First(ctx,
		`SELECT id, out.name AS object_name, valid_from, valid_until FROM assertion WHERE predicate = 'version_is' ORDER BY valid_from;`,
		&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 version_is edges (1 closed, 1 open), got %d: %+v", len(rows), rows)
	}
	if rows[0].ValidUntil == nil || rows[1].ValidUntil != nil {
		t.Errorf("expected first edge closed and second open; got %+v", rows)
	}
}

func TestRemember_PreferenceValenceFlipSupersedes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Alice dislikes SurrealDB.", Kind: KindPreference, Author: "claude", Valence: ValenceNegative,
		Entities: []EntityDecl{
			{Name: "Alice", Type: EntityPerson},
			{Name: "SurrealDB", Type: EntityPreference},
		},
		Edges: []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson,
			Predicate: "prefers", Object: "SurrealDB", ObjectType: EntityPreference,
			Valence: ValenceNegative}},
	})
	if err != nil {
		t.Fatal(err)
	}

	r2, err := s.Remember(ctx, RememberPayload{
		Summary: "Alice now likes SurrealDB.", Kind: KindPreference, Author: "claude", Valence: ValencePositive,
		Entities: []EntityDecl{
			{Name: "Alice", Type: EntityPerson},
			{Name: "SurrealDB", Type: EntityPreference},
		},
		Edges: []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson,
			Predicate: "prefers", Object: "SurrealDB", ObjectType: EntityPreference,
			Valence: ValencePositive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Edges) != 1 || r2.Edges[0].Superseded == "" {
		t.Fatalf("expected valence-flip supersession, got %+v", r2.Edges)
	}
}

func TestEditHandNotes_AppendThenReplace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed an entity via remember (no graph edges).
	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Seeding Pi entity.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Pi", Type: EntityTool}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.EditHandNotes(ctx, "Pi", EntityTool, "First line.", NotesAppend, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := s.EditHandNotes(ctx, "Pi", EntityTool, "Second line.", NotesAppend, "alice"); err != nil {
		t.Fatal(err)
	}
	ent, err := s.fetchEntity(ctx, "tool:pi")
	if err != nil {
		t.Fatal(err)
	}
	if ent == nil || !strings.Contains(ent.HandNotes, "First line.") || !strings.Contains(ent.HandNotes, "Second line.") {
		t.Fatalf("append didn't accumulate: hand_notes=%q", ent.HandNotes)
	}

	if err := s.EditHandNotes(ctx, "Pi", EntityTool, "Replaced.", NotesReplace, "alice"); err != nil {
		t.Fatal(err)
	}
	ent, _ = s.fetchEntity(ctx, "tool:pi")
	if strings.Contains(ent.HandNotes, "First line.") || ent.HandNotes != "Replaced." {
		t.Errorf("replace didn't overwrite: hand_notes=%q", ent.HandNotes)
	}
}

// DerivedCard is auto-rendered. After a remember() that touches the entity,
// the derived_card should contain a deterministic rendering of the entity's
// active edges + matching memories.
func TestRemember_AutoRendersDerivedCard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Alice is a software engineer.",
		Kind:    KindIdentity,
		Author:  "claude",
		Entities: []EntityDecl{
			{Name: "Alice", Type: EntityPerson},
			{Name: "software engineer", Type: EntityConcept},
		},
		Edges: []EdgeDecl{
			{Subject: "Alice", SubjectType: EntityPerson,
				Predicate: "is_a",
				Object:    "software engineer", ObjectType: EntityConcept},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ent, err := s.fetchEntity(ctx, "person:alice")
	if err != nil {
		t.Fatal(err)
	}
	if ent == nil {
		t.Fatal("expected person:alice to exist")
	}
	if ent.DerivedCard == "" {
		t.Fatal("expected derived_card to be populated by recompute")
	}
	if !strings.Contains(ent.DerivedCard, "## Identity") {
		t.Errorf("expected Identity section in derived_card, got:\n%s", ent.DerivedCard)
	}
	if !strings.Contains(ent.DerivedCard, "## Active edges") {
		t.Errorf("expected Active edges section, got:\n%s", ent.DerivedCard)
	}
	if !strings.Contains(ent.DerivedCard, "software engineer") {
		t.Errorf("expected the object name in active edges, got:\n%s", ent.DerivedCard)
	}
	if !strings.Contains(ent.DerivedCard, "is_a") {
		t.Errorf("expected the predicate in active edges, got:\n%s", ent.DerivedCard)
	}
	// hand_notes must remain empty (we never wrote any).
	if ent.HandNotes != "" {
		t.Errorf("expected empty hand_notes, got: %q", ent.HandNotes)
	}
}

// Closing an edge removes it from derived_card on both endpoints.
func TestCloseEdge_RemovesFromDerivedCard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	r, err := s.Remember(ctx, RememberPayload{
		Summary: "Alice likes React.", Kind: KindPreference, Author: "claude", Valence: ValencePositive,
		Entities: []EntityDecl{
			{Name: "Alice", Type: EntityPerson},
			{Name: "React", Type: EntityPreference},
		},
		Edges: []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson,
			Predicate: "prefers", Object: "React", ObjectType: EntityPreference,
			Valence: ValencePositive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(r.Edges))
	}
	edgeID := r.Edges[0].ID

	// Before close: derived_card on Alice should mention React.
	ent, _ := s.fetchEntity(ctx, "person:alice")
	if !strings.Contains(ent.DerivedCard, "React") {
		t.Fatalf("expected React in Alice's derived_card before close, got:\n%s", ent.DerivedCard)
	}

	if err := s.CloseEdge(ctx, edgeID, "alice"); err != nil {
		t.Fatal(err)
	}

	ent2, _ := s.fetchEntity(ctx, "person:alice")
	if strings.Contains(ent2.DerivedCard, "React") {
		t.Errorf("expected React removed from Alice's derived_card after close, got:\n%s", ent2.DerivedCard)
	}
}

func TestAddEdge_RequiresExistingEntities(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.AddEdge(ctx, EdgeDecl{
		Subject: "Ghost", SubjectType: EntityPerson,
		Predicate: "is_a",
		Object:    "Phantom", ObjectType: EntityConcept,
	}, "claude")
	if err == nil {
		t.Fatal("expected error for missing entities")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}
}

func TestPromoteDemote(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Remember(ctx, RememberPayload{
		Summary: "Seed Alice.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Alice", Type: EntityPerson}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "Alice", EntityPerson, "claude"); err != nil {
		t.Fatal(err)
	}
	ent, _ := s.fetchEntity(ctx, "person:alice")
	if ent == nil || !ent.Promoted {
		t.Fatalf("expected promoted=true, got %+v", ent)
	}
	if err := s.Demote(ctx, "Alice", EntityPerson, "alice"); err != nil {
		t.Fatal(err)
	}
	ent, _ = s.fetchEntity(ctx, "person:alice")
	if ent.Promoted {
		t.Errorf("expected promoted=false after demote")
	}
}

func TestForget_SoftDeletesAndExcludesFromRecall(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r, err := s.Remember(ctx, RememberPayload{
		Summary: "Pi at v0.5.", Kind: KindFact, Author: "claude",
		Entities: []EntityDecl{{Name: "Pi", Type: EntityTool}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, r.MemoryID); err != nil {
		t.Fatal(err)
	}
	// Try fetching the row directly — should be inactive.
	var rows []struct {
		ID       string `json:"id"`
		IsActive bool   `json:"is_active"`
	}
	if err := s.db.First(ctx, "SELECT id, is_active FROM "+r.MemoryID+";", &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].IsActive {
		t.Errorf("expected is_active=false; got %+v", rows)
	}
}

func TestOrient_Renders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Seed memories: derived_card will auto-populate with the works_on edge.
	_, err := s.Remember(ctx, RememberPayload{
		Summary: "rill is the project.", Kind: KindFact, Author: "claude",
		Project: "rill",
		Entities: []EntityDecl{
			{Name: "Alice Smith", Type: EntityPerson, Summary: "Engineer"},
			{Name: "rill", Type: EntityProject, Summary: "Knowledge graph memory system"},
		},
		Edges: []EdgeDecl{{Subject: "Alice Smith", SubjectType: EntityPerson,
			Predicate: "works_on", Object: "rill", ObjectType: EntityProject}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Add a human-authored hand_notes line on the project.
	if err := s.EditHandNotes(ctx, "rill", EntityProject, "Greenfield agent memory on SurrealDB.", NotesReplace, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "Alice Smith", EntityPerson, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := s.Promote(ctx, "rill", EntityProject, "claude"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Orient(ctx, OrientQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Rendered, "Alice Smith") {
		t.Errorf("expected Alice in orient render, got:\n%s", got.Rendered)
	}
	if !strings.Contains(got.Rendered, "rill") {
		t.Errorf("expected rill in orient render, got:\n%s", got.Rendered)
	}
	if !strings.Contains(got.Rendered, "## Identity") {
		t.Errorf("expected Identity section, got:\n%s", got.Rendered)
	}
	if !strings.Contains(got.Rendered, "## Active projects") {
		t.Errorf("expected Active projects section")
	}
	if !strings.Contains(got.Rendered, "Greenfield agent memory") {
		t.Errorf("expected hand_notes to land in orient, got:\n%s", got.Rendered)
	}

	// Cache should serve second call
	got2, err := s.Orient(ctx, OrientQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !got2.FromCache {
		t.Errorf("expected second orient() to come from cache")
	}
	// Edit hand_notes → cache stale → next call regens
	if err := s.EditHandNotes(ctx, "rill", EntityProject, "Updated.", NotesAppend, "alice"); err != nil {
		t.Fatal(err)
	}
	got3, err := s.Orient(ctx, OrientQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got3.FromCache {
		t.Errorf("expected third orient() to bypass cache (stale)")
	}
}

// TestRemember_BitemporalSplit verifies that ValidFrom only overrides event
// time (valid_from), NEVER transaction time (created_at, updated_at, memory_id).
// Bug we're guarding against: prior to 2026-05-23, a single `ts` drove both,
// so a ported memory about a 2026-02-15 event got created_at=2026-02-15 and
// memory_id=memory:`20260215T...` — wrong on both counts.
func TestRemember_BitemporalSplit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	historical := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC) // way in the past
	beforeWrite := time.Now().UTC()

	res, err := s.Remember(ctx, RememberPayload{
		Summary:   "Historical event from June 2025.",
		Kind:      KindFact,
		Author:    "claude",
		ValidFrom: &historical,
		Entities: []EntityDecl{
			{Name: "historical-thing", Type: EntityConcept},
		},
	})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}

	afterWrite := time.Now().UTC()

	// Memory ID must reflect TX time, not event time.
	// IDs look like memory:`20260523T...` — pull the YYYYMMDD prefix.
	if strings.Contains(res.MemoryID, "20250601") {
		t.Errorf("memory_id leaked event time: %s (should reflect today, not historical valid_from)", res.MemoryID)
	}

	// Pull the row back and verify the timestamp fields.
	got, err := s.GetMemory(ctx, res.MemoryID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("memory not found after Remember")
	}

	// created_at must be ~now, not historical
	if got.CreatedAt.Before(beforeWrite) || got.CreatedAt.After(afterWrite.Add(2*time.Second)) {
		t.Errorf("created_at = %v, expected between %v and %v (must NOT be event time %v)",
			got.CreatedAt, beforeWrite, afterWrite, historical)
	}

	// Entity first_seen should reflect the historical event time (we encountered
	// this thing in the past; we're just writing it down now).
	ent, err := s.GetEntity(ctx, "historical-thing", EntityConcept)
	if err != nil {
		t.Fatal(err)
	}
	if ent == nil {
		t.Fatal("entity not found")
	}
	// first_seen should equal historical
	if !ent.FirstSeen.Equal(historical) {
		t.Errorf("entity first_seen = %v, expected %v (event time)", ent.FirstSeen, historical)
	}
	// last_edited_at should be ~now (tx time)
	if ent.LastEditedAt != nil && (ent.LastEditedAt.Before(beforeWrite) || ent.LastEditedAt.After(afterWrite.Add(2*time.Second))) {
		t.Errorf("entity last_edited_at = %v, expected ~now (tx time)", *ent.LastEditedAt)
	}
}

// TestMergeEntity_FoldsSourceIntoTarget verifies the end-to-end merge: edges +
// mentions move to the survivor, a colliding edge dedupes to one active edge,
// the source's name becomes an alias on the survivor, mention_count sums, and
// the source is retired (drops out of listings).
func TestMergeEntity_FoldsSourceIntoTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Target: Kimi, used by Alice.
	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Alice uses Kimi.",
		Kind:    "fact", Author: "test",
		Entities: []EntityDecl{{Name: "Alice", Type: EntityPerson}, {Name: "Kimi", Type: EntityTool}},
		Edges:    []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson, Predicate: "uses", Object: "Kimi", ObjectType: EntityTool}},
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	// Source: "Kimi K2.6" — SAME uses<-Alice edge (collision → must dedupe) + alias.
	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Kimi K2.6 is reached via OpenRouter.",
		Kind:    "fact", Author: "test",
		Entities: []EntityDecl{{Name: "Alice", Type: EntityPerson}, {Name: "Kimi K2.6", Type: EntityTool, Aliases: []string{"K2.6"}}},
		Edges:    []EdgeDecl{{Subject: "Alice", SubjectType: EntityPerson, Predicate: "uses", Object: "Kimi K2.6", ObjectType: EntityTool}},
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	res, err := s.MergeEntity(ctx, "Kimi K2.6", "Kimi", EntityTool, "test")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if res.EdgesMoved < 1 {
		t.Errorf("expected >=1 edge moved, got %d", res.EdgesMoved)
	}

	// Source is retired — gone from listings.
	rows, err := s.ListEntities(ctx, ListEntitiesQuery{Type: EntityTool})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if strings.EqualFold(r.Name, "Kimi K2.6") {
			t.Errorf("retired source still listed: %s", r.ID)
		}
	}

	// Survivor absorbed alias + kept exactly one ACTIVE uses edge (dedupe worked).
	tgt, err := s.GetEntity(ctx, "Kimi", EntityTool)
	if err != nil || tgt == nil {
		t.Fatalf("get target: %v", err)
	}
	hasAlias := false
	for _, a := range tgt.Aliases {
		if strings.EqualFold(a, "Kimi K2.6") {
			hasAlias = true
		}
	}
	if !hasAlias {
		t.Errorf("survivor aliases missing source name: %v", tgt.Aliases)
	}
	activeUses := 0
	for _, e := range tgt.Edges {
		if e.Predicate == "uses" && e.Active {
			activeUses++
		}
	}
	if activeUses != 1 {
		t.Errorf("expected exactly 1 active uses edge after dedupe, got %d", activeUses)
	}

	// Merging into self must be rejected.
	if _, err := s.MergeEntity(ctx, "Kimi", "Kimi", EntityTool, "test"); err == nil {
		t.Errorf("expected error merging an entity into itself")
	}
}

// TestSetVersion_BitemporalSupersession verifies version_is: setting a new
// version closes the prior (one active), GetEntity surfaces current, re-setting
// the same value is a no-op, and inline declaration via remember() works.
func TestSetVersion_BitemporalSupersession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Kimi is a model.", Kind: "fact", Author: "test",
		Entities: []EntityDecl{{Name: "Kimi", Type: EntityTool}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := s.SetVersion(ctx, "tool:kimi", EntityTool, "K2.5", "test"); err != nil {
		t.Fatalf("set v1: %v", err)
	}
	if v, _ := s.CurrentVersion(ctx, "tool:kimi"); v != "K2.5" {
		t.Fatalf("current = %q, want K2.5", v)
	}
	if err := s.SetVersion(ctx, "tool:kimi", EntityTool, "K2.6", "test"); err != nil {
		t.Fatalf("set v2: %v", err)
	}
	if v, _ := s.CurrentVersion(ctx, "tool:kimi"); v != "K2.6" {
		t.Fatalf("current = %q, want K2.6", v)
	}
	// Re-setting the same version is a no-op (no new history row).
	if err := s.SetVersion(ctx, "tool:kimi", EntityTool, "K2.6", "test"); err != nil {
		t.Fatalf("set v2 again: %v", err)
	}

	// GetEntity surfaces the current version.
	det, err := s.GetEntity(ctx, "Kimi", EntityTool)
	if err != nil || det == nil {
		t.Fatalf("get: %v", err)
	}
	if det.Version != "K2.6" {
		t.Errorf("GetEntity.Version = %q, want K2.6", det.Version)
	}

	// History preserved: exactly 2 version_is rows for Kimi, exactly 1 active.
	res, err := s.db.SQL(ctx, `SELECT count() AS c FROM version_is WHERE entity = tool:kimi GROUP ALL;`, true)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	_ = res

	// Inline declaration via remember() sets the version.
	if _, err := s.Remember(ctx, RememberPayload{
		Summary: "Gemma local model.", Kind: "fact", Author: "test",
		Entities: []EntityDecl{{Name: "Gemma 4 26B A4B", Type: EntityTool, Version: "4"}},
	}); err != nil {
		t.Fatalf("seed gemma: %v", err)
	}
	if v, _ := s.CurrentVersion(ctx, "tool:gemma_4_26b_a4b"); v != "4" {
		t.Errorf("gemma version = %q, want 4", v)
	}
}
