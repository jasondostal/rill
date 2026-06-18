package memory

import "testing"

func f64(v float64) *float64 { return &v }

// fuseRRF must combine the vector and FTS result lists by Reciprocal Rank
// Fusion: an item that appears in BOTH lists should outrank items that appear
// in only one, and ordering within a single list is preserved by rank.
func TestFuseRRF_AgreementWins(t *testing.T) {
	vector := []MemoryHit{
		{ID: "A", Distance: f64(0.1)},
		{ID: "B", Distance: f64(0.2)},
	}
	fts := []MemoryHit{
		{ID: "B"},
		{ID: "C"},
	}

	got := fuseRRF(vector, fts, 5)

	if len(got) != 3 {
		t.Fatalf("want 3 fused hits, got %d", len(got))
	}
	// B is in both lists → highest RRF score → first.
	if got[0].ID != "B" {
		t.Errorf("want B first (appears in both lists), got %q", got[0].ID)
	}
	// A (vector rank 0) beats C (fts rank 1).
	if got[1].ID != "A" || got[2].ID != "C" {
		t.Errorf("want order [B A C], got [%s %s %s]", got[0].ID, got[1].ID, got[2].ID)
	}
}

// #4: an FTS-only hit must NOT report a fabricated distance of 0 (which reads
// as "perfectly similar"). Vector hits keep their real cosine distance.
func TestFuseRRF_DistanceProvenance(t *testing.T) {
	vector := []MemoryHit{{ID: "A", Distance: f64(0.1)}, {ID: "B", Distance: f64(0.2)}}
	fts := []MemoryHit{{ID: "B"}, {ID: "C"}}

	got := fuseRRF(vector, fts, 5)
	byID := map[string]MemoryHit{}
	for _, h := range got {
		byID[h.ID] = h
	}

	if byID["A"].Distance == nil || *byID["A"].Distance != 0.1 {
		t.Errorf("vector-only hit A should keep distance 0.1, got %v", byID["A"].Distance)
	}
	if byID["B"].Distance == nil || *byID["B"].Distance != 0.2 {
		t.Errorf("both-list hit B should keep its vector distance 0.2, got %v", byID["B"].Distance)
	}
	if byID["C"].Distance != nil {
		t.Errorf("fts-only hit C must have nil distance (not a fabricated 0), got %v", *byID["C"].Distance)
	}
	// Every fused hit should carry a positive RRF score.
	for _, h := range got {
		if h.Score <= 0 {
			t.Errorf("hit %q has non-positive RRF score %v", h.ID, h.Score)
		}
	}
}

func TestFuseRRF_DedupAndTruncate(t *testing.T) {
	vector := []MemoryHit{{ID: "A", Distance: f64(0.1)}, {ID: "B", Distance: f64(0.2)}, {ID: "C", Distance: f64(0.3)}}
	fts := []MemoryHit{{ID: "B"}, {ID: "A"}, {ID: "D"}}

	got := fuseRRF(vector, fts, 2)
	if len(got) != 2 {
		t.Fatalf("want truncation to k=2, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, h := range got {
		if seen[h.ID] {
			t.Fatalf("duplicate id %q in fused output", h.ID)
		}
		seen[h.ID] = true
	}
}

// With no embedder, the vector list is empty — fusion degrades to the FTS
// ordering, with no fabricated distances.
func TestFuseRRF_FTSOnly(t *testing.T) {
	fts := []MemoryHit{{ID: "X"}, {ID: "Y"}, {ID: "Z"}}
	got := fuseRRF(nil, fts, 5)
	if len(got) != 3 || got[0].ID != "X" || got[2].ID != "Z" {
		t.Fatalf("fts-only fusion should preserve order [X Y Z], got %v", got)
	}
	for _, h := range got {
		if h.Distance != nil {
			t.Errorf("fts-only hit %q should have nil distance", h.ID)
		}
	}
}
