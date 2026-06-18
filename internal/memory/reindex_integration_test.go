//go:build integration

package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/jasondostal/rill/internal/embedding"
)

// End-to-end backfill: write rows with NO embedder (null vectors), then reindex
// with a real embedder and confirm vectors land (memory-id UPDATE round-trips)
// and semantic recall finds a paraphrase it would miss on keywords alone.
func TestReindexEmbeddings_Backfill(t *testing.T) {
	if embedding.LoadAPIKey() == "" {
		t.Skip("no embedding API key in env — skipping live reindex test")
	}

	plain := newTestStore(t) // nil embedder → rows written without vectors; wipes db
	ctx := context.Background()

	if _, err := plain.Remember(ctx, RememberPayload{
		Summary: "The team deployed the new memory system after three failed attempts at persistent context.",
		Kind:    KindFact, Author: "test-agent",
		Entities: []EntityDecl{{Name: "rill", Type: EntityProject}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Same DB handle, now WITH a real embedder.
	emb := New(plain.db, embedding.NewClientFromEnv())

	results, err := emb.ReindexEmbeddings(ctx, false)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	var embedded, failed int
	for _, r := range results {
		embedded += r.Embedded
		failed += r.Failed
	}
	if failed != 0 {
		t.Fatalf("reindex had %d failures", failed)
	}
	if embedded < 2 { // at least the memory + the rill entity
		t.Fatalf("expected ≥2 rows embedded, got %d", embedded)
	}

	// Semantic recall: a paraphrase with little keyword overlap should now hit.
	hits, err := emb.Recall(ctx, RecallQuery{Query: "rebuilding storage after context kept disappearing", K: 5})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	found := false
	for _, m := range hits.Memories {
		if strings.Contains(m.Summary, "persistent context") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("semantic recall did not surface the seeded memory; got %d hits", len(hits.Memories))
	}

	// Idempotent: a second missing-only pass embeds nothing.
	results2, err := emb.ReindexEmbeddings(ctx, false)
	if err != nil {
		t.Fatalf("reindex (2nd): %v", err)
	}
	for _, r := range results2 {
		if r.Embedded != 0 {
			t.Fatalf("second pass should embed nothing, %s embedded %d", r.Table, r.Embedded)
		}
	}
}
