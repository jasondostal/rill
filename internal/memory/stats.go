package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StatsQuery is the input to Stats(). Days bounds the time-series window
// (growth + heatmap); <= 0 means "all of recorded history".
type StatsQuery struct {
	Days int `json:"days,omitempty"`
}

// CountItem is one labelled count (kind, project, or entity type).
type CountItem struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

// HeatCell is one day's memory-creation count for the activity heatmap.
type HeatCell struct {
	Date  string `json:"date"` // YYYY-MM-DD (UTC)
	Count int    `json:"count"`
}

// StatsKPIs are the headline scalar counts for the dashboard tiles.
type StatsKPIs struct {
	Memories  int `json:"memories"`
	Entities  int `json:"entities"`
	Documents int `json:"documents"`
	Projects  int `json:"projects"`
	Relations int `json:"relations"`
	Sessions  int `json:"sessions"`
}

// StatsResult is the full dashboard payload: scalar KPIs, three breakdowns,
// a cumulative-by-kind growth series, an activity heatmap, and a recent feed.
type StatsResult struct {
	KPIs             StatsKPIs        `json:"kpis"`
	KindBreakdown    []CountItem      `json:"kind_breakdown"`
	ProjectBreakdown []CountItem      `json:"project_breakdown"`
	EntityBreakdown  []CountItem      `json:"entity_breakdown"`
	Dates            []string         `json:"dates"`  // one YYYY-MM-DD per day in the window
	Growth           map[string][]int `json:"growth"` // kind -> cumulative total at each date
	Heatmap          []HeatCell       `json:"heatmap"`
	Recent           []MemoryRow      `json:"recent"`
	Days             int              `json:"days"`
}

// globalProject is the label used for memories with no project scope.
const globalProject = "__global__"

// relationTables are the semantic graph-edge tables counted for the
// "Relations" KPI. `mentions` (memory→entity provenance) and `doc_about`
// are deliberately excluded — they're plumbing, not knowledge relations.
var relationTables = []string{
	"works_on", "uses", "prefers", "works_at", "depends_on", "part_of", "assertion",
}

// Stats computes the dashboard aggregates in a handful of GROUP BY queries.
// Everything is derived from live data; nothing is synthesised.
func (s *Store) Stats(ctx context.Context, q StatsQuery) (*StatsResult, error) {
	out := &StatsResult{Growth: map[string][]int{}}

	// ---- breakdown by kind (+ memories KPI) ----
	kindCounts, err := s.groupCount(ctx, "memory", "kind", "is_active = true")
	if err != nil {
		return nil, fmt.Errorf("kind breakdown: %w", err)
	}
	// Emit kinds in canonical order so the UI palette anchors stay stable.
	for _, k := range ValidKinds {
		out.KindBreakdown = append(out.KindBreakdown, CountItem{ID: string(k), Count: kindCounts[string(k)]})
		out.KPIs.Memories += kindCounts[string(k)]
	}

	// ---- breakdown by project (+ projects KPI) ----
	projCounts, err := s.groupCount(ctx, "memory", "project", "is_active = true")
	if err != nil {
		return nil, fmt.Errorf("project breakdown: %w", err)
	}
	for id, c := range projCounts {
		label := id
		if id == "" {
			label = globalProject
		} else {
			out.KPIs.Projects++ // count only real, named projects
		}
		out.ProjectBreakdown = append(out.ProjectBreakdown, CountItem{ID: label, Count: c})
	}
	sort.Slice(out.ProjectBreakdown, func(i, j int) bool {
		return out.ProjectBreakdown[i].Count > out.ProjectBreakdown[j].Count
	})

	// ---- breakdown by entity type (+ entities KPI) ----
	entTables := make([]string, len(ValidEntityTypes))
	for i, t := range ValidEntityTypes {
		entTables[i] = string(t)
	}
	entCounts, err := s.countTables(ctx, entTables, "merged_into IS NONE")
	if err != nil {
		return nil, fmt.Errorf("entity breakdown: %w", err)
	}
	for _, t := range ValidEntityTypes {
		c := entCounts[string(t)]
		out.EntityBreakdown = append(out.EntityBreakdown, CountItem{ID: string(t), Count: c})
		out.KPIs.Entities += c
	}

	// ---- relations KPI (active semantic edges) ----
	relCounts, err := s.countTables(ctx, relationTables, "valid_until IS NONE")
	if err != nil {
		return nil, fmt.Errorf("relations: %w", err)
	}
	for _, c := range relCounts {
		out.KPIs.Relations += c
	}

	// ---- documents + sessions KPIs ----
	docCounts, err := s.countTables(ctx, []string{"document"}, "deleted_at IS NONE")
	if err != nil {
		return nil, fmt.Errorf("documents: %w", err)
	}
	out.KPIs.Documents = docCounts["document"]
	sessCounts, err := s.countTables(ctx, []string{"auth_session"}, "1 = 1")
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	out.KPIs.Sessions = sessCounts["auth_session"]

	// ---- time series: cumulative growth by kind + activity heatmap ----
	if err := s.buildTimeSeries(ctx, q.Days, out); err != nil {
		return nil, fmt.Errorf("time series: %w", err)
	}

	// ---- recent feed ----
	recent, err := s.ListMemories(ctx, ListMemoriesQuery{Limit: 8})
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	out.Recent = recent

	return out, nil
}

// buildTimeSeries pulls every active memory's (created_at, kind), buckets by
// UTC day, and produces a per-day date axis with cumulative-by-kind totals and
// a daily activity count. Cumulative carries forward true totals, so the curve
// reflects the real graph even when the window starts mid-history.
func (s *Store) buildTimeSeries(ctx context.Context, days int, out *StatsResult) error {
	stmt := `SELECT created_at, kind FROM memory WHERE is_active = true ORDER BY created_at ASC;`
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return err
	}
	type rowT struct {
		CreatedAt time.Time `json:"created_at"`
		Kind      string    `json:"kind"`
	}
	var rows []rowT
	if len(res) > 0 && len(res[0].Result) > 0 {
		if err := json.Unmarshal(res[0].Result, &rows); err != nil {
			return err
		}
	}

	const dayFmt = "2006-01-02"
	today := now().Truncate(24 * time.Hour)

	// Determine the window start. days<=0 → from the earliest memory.
	var start time.Time
	if days > 0 {
		start = today.AddDate(0, 0, -(days - 1))
	} else if len(rows) > 0 {
		start = rows[0].CreatedAt.UTC().Truncate(24 * time.Hour)
	} else {
		start = today
	}
	out.Days = days

	// Build the date axis (inclusive of today).
	var dates []string
	dateIdx := map[string]int{}
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format(dayFmt)
		dateIdx[key] = len(dates)
		dates = append(dates, key)
	}
	out.Dates = dates
	n := len(dates)

	// Per-day, per-kind increments + per-day total; plus pre-window baselines
	// so cumulative starts from the true total at the window's left edge.
	perDay := map[string][]int{}        // kind -> daily increments within window
	baseline := map[string]int{}        // kind -> count strictly before the window
	heat := make([]int, n)              // daily total within window
	for _, k := range ValidKinds {
		perDay[string(k)] = make([]int, n)
	}
	for _, r := range rows {
		day := r.CreatedAt.UTC().Truncate(24 * time.Hour)
		key := day.Format(dayFmt)
		idx, in := dateIdx[key]
		if !in {
			if day.Before(start) {
				baseline[r.Kind]++ // counts toward cumulative baseline
			}
			continue
		}
		if _, ok := perDay[r.Kind]; !ok {
			perDay[r.Kind] = make([]int, n) // tolerate any unexpected kind
		}
		perDay[r.Kind][idx]++
		heat[idx]++
	}

	// Cumulative per kind, carrying the baseline forward.
	for kind, inc := range perDay {
		series := make([]int, n)
		running := baseline[kind]
		for i := 0; i < n; i++ {
			running += inc[i]
			series[i] = running
		}
		out.Growth[kind] = series
	}

	out.Heatmap = make([]HeatCell, n)
	for i := 0; i < n; i++ {
		out.Heatmap[i] = HeatCell{Date: dates[i], Count: heat[i]}
	}
	return nil
}

// groupCount runs `SELECT <field>, count() AS c FROM <table> WHERE <where>
// GROUP BY <field>` and returns field-value -> count. A null/absent field
// value maps to the empty string.
func (s *Store) groupCount(ctx context.Context, table, field, where string) (map[string]int, error) {
	stmt := fmt.Sprintf("SELECT %s, count() AS c FROM %s WHERE %s GROUP BY %s;", field, table, where, field)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return out, nil
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(res[0].Result, &raw); err != nil {
		return nil, err
	}
	for _, row := range raw {
		var c int
		if v, ok := row["c"]; ok {
			_ = json.Unmarshal(v, &c)
		}
		var key string
		if v, ok := row[field]; ok && string(v) != "null" {
			_ = json.Unmarshal(v, &key)
		}
		out[key] = c
	}
	return out, nil
}

// countTables runs one `SELECT count() ... GROUP ALL` per table in a single
// multi-statement round trip and returns table -> count. Tables with no
// matching rows map to 0.
func (s *Store) countTables(ctx context.Context, tables []string, where string) (map[string]int, error) {
	if len(tables) == 0 {
		return map[string]int{}, nil
	}
	var sb strings.Builder
	for _, t := range tables {
		fmt.Fprintf(&sb, "SELECT count() AS c FROM %s WHERE %s GROUP ALL;", t, where)
	}
	res, err := s.db.SQL(ctx, sb.String(), true)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for i, t := range tables {
		out[t] = 0
		if i >= len(res) || len(res[i].Result) == 0 {
			continue
		}
		var rows []struct {
			C int `json:"c"`
		}
		if err := json.Unmarshal(res[i].Result, &rows); err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			out[t] = rows[0].C
		}
	}
	return out, nil
}
