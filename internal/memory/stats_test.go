//go:build integration

package memory

import (
	"context"
	"testing"
)

// TestStats verifies the dashboard aggregates against a freshly-seeded store:
// KPI counts, the three breakdowns, cumulative growth by kind, the activity
// heatmap, and the recent feed.
func TestStats(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed: 3 facts (2 rill, 1 homelab), 2 decisions (rill), 1 rule (global).
	seed := []RememberPayload{
		{Summary: "fact one about rill", Kind: KindFact, Author: "claude", Project: "rill",
			Entities: []EntityDecl{{Name: "Rill", Type: EntityProject}}},
		{Summary: "fact two about rill", Kind: KindFact, Author: "claude", Project: "rill"},
		{Summary: "fact three on homelab", Kind: KindFact, Author: "claude", Project: "homelab"},
		{Summary: "a decision for rill", Kind: KindDecision, Author: "claude", Project: "rill"},
		{Summary: "another decision for rill", Kind: KindDecision, Author: "claude", Project: "rill"},
		{Summary: "a global rule", Kind: KindRule, Author: "admin"}, // no project -> __global__
	}
	for i, p := range seed {
		if _, err := s.Remember(ctx, p); err != nil {
			t.Fatalf("seed[%d] Remember: %v", i, err)
		}
	}

	res, err := s.Stats(ctx, StatsQuery{Days: 90})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	// ---- KPIs ----
	if res.KPIs.Memories != 6 {
		t.Errorf("memories KPI: want 6, got %d", res.KPIs.Memories)
	}
	if res.KPIs.Entities != 1 { // only "Rill" project entity
		t.Errorf("entities KPI: want 1, got %d", res.KPIs.Entities)
	}
	if res.KPIs.Projects != 2 { // rill + homelab (global is not a named project)
		t.Errorf("projects KPI: want 2, got %d", res.KPIs.Projects)
	}

	// ---- kind breakdown (canonical order, all 7 kinds present) ----
	if len(res.KindBreakdown) != len(ValidKinds) {
		t.Fatalf("kind breakdown len: want %d, got %d", len(ValidKinds), len(res.KindBreakdown))
	}
	byKind := map[string]int{}
	for _, c := range res.KindBreakdown {
		byKind[c.ID] = c.Count
	}
	if byKind["fact"] != 3 {
		t.Errorf("fact count: want 3, got %d", byKind["fact"])
	}
	if byKind["decision"] != 2 {
		t.Errorf("decision count: want 2, got %d", byKind["decision"])
	}
	if byKind["rule"] != 1 {
		t.Errorf("rule count: want 1, got %d", byKind["rule"])
	}
	if byKind["insight"] != 0 {
		t.Errorf("insight count: want 0, got %d", byKind["insight"])
	}

	// ---- project breakdown (sorted desc, global relabelled) ----
	byProj := map[string]int{}
	for _, c := range res.ProjectBreakdown {
		byProj[c.ID] = c.Count
	}
	if byProj["rill"] != 4 {
		t.Errorf("rill project count: want 4, got %d", byProj["rill"])
	}
	if byProj["homelab"] != 1 {
		t.Errorf("homelab project count: want 1, got %d", byProj["homelab"])
	}
	if byProj[globalProject] != 1 {
		t.Errorf("__global__ count: want 1, got %d", byProj[globalProject])
	}
	if len(res.ProjectBreakdown) > 0 && res.ProjectBreakdown[0].Count != 4 {
		t.Errorf("project breakdown should be sorted desc; first=%d", res.ProjectBreakdown[0].Count)
	}

	// ---- entity breakdown: all 7 types present, project=1 ----
	if len(res.EntityBreakdown) != len(ValidEntityTypes) {
		t.Errorf("entity breakdown len: want %d, got %d", len(ValidEntityTypes), len(res.EntityBreakdown))
	}
	for _, c := range res.EntityBreakdown {
		if c.ID == "project" && c.Count != 1 {
			t.Errorf("project entity count: want 1, got %d", c.Count)
		}
	}

	// ---- time series ----
	if len(res.Dates) != 90 {
		t.Errorf("dates len: want 90, got %d", len(res.Dates))
	}
	if len(res.Heatmap) != len(res.Dates) {
		t.Errorf("heatmap len %d != dates len %d", len(res.Heatmap), len(res.Dates))
	}
	// All 6 seeds land today, so the heatmap total over the window is 6.
	heatTotal := 0
	for _, h := range res.Heatmap {
		heatTotal += h.Count
	}
	if heatTotal != 6 {
		t.Errorf("heatmap total: want 6, got %d", heatTotal)
	}
	// Cumulative fact series ends at 3 (today, last index).
	fg := res.Growth["fact"]
	if len(fg) != len(res.Dates) || fg[len(fg)-1] != 3 {
		t.Errorf("fact growth should end at 3, got %v", fg[max(0, len(fg)-1):])
	}
	// Cumulative is monotonic non-decreasing.
	for kind, series := range res.Growth {
		for i := 1; i < len(series); i++ {
			if series[i] < series[i-1] {
				t.Errorf("growth[%s] not monotonic at %d: %d < %d", kind, i, series[i], series[i-1])
				break
			}
		}
	}

	// ---- recent feed ----
	if len(res.Recent) != 6 {
		t.Errorf("recent feed: want 6, got %d", len(res.Recent))
	}
}
