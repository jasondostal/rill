package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// recomputeDerivedCard renders the system-maintained card for one entity
// from its current graph state and writes it to entity.derived_card.
//
// Deterministic. No LLM. Two calls with the same underlying graph produce
// identical text — that's the contract that makes diffing / orient
// rendering predictable.
//
// Sections (any empty section is omitted):
//   ## Identity        ← kind=identity memories mentioning this entity
//   ## Active edges    ← edges with valid_until IS NONE touching this entity
//   ## Facts           ← kind=fact memories mentioning this entity
//   ## Decisions       ← kind=decision memories mentioning this entity
//   ## Preferences     ← active `prefers` edges (grouped by valence)
//
// Memories are sorted newest-first; edges are sorted predicate then object.
func (s *Store) recomputeDerivedCard(ctx context.Context, recID string) error {
	if !strings.Contains(recID, ":") {
		return errs("recomputeDerivedCard: invalid record id %q", recID)
	}

	identityMems, err := s.fetchMemoriesForEntity(ctx, recID, KindIdentity, 5)
	if err != nil {
		return fmt.Errorf("fetch identity memories: %w", err)
	}
	factMems, err := s.fetchMemoriesForEntity(ctx, recID, KindFact, 10)
	if err != nil {
		return fmt.Errorf("fetch fact memories: %w", err)
	}
	decisionMems, err := s.fetchMemoriesForEntity(ctx, recID, KindDecision, 10)
	if err != nil {
		return fmt.Errorf("fetch decision memories: %w", err)
	}

	edges, err := s.fetchActiveEdgesTouching(ctx, recID)
	if err != nil {
		return fmt.Errorf("fetch edges: %w", err)
	}

	rendered := renderDerivedCard(identityMems, factMems, decisionMems, edges)

	// Write back. derived_card may be NONE (no sections produced) — that's fine.
	derivedExpr := "NONE"
	if rendered != "" {
		derivedExpr = EscapeStr(rendered)
	}

	// Sync mention_count to the number of ACTIVE memories mentioning this entity.
	// forget() keeps the mentions edge for provenance, so the raw edge count (and
	// the old += 1 counter) over-counts forgotten memories. Recomputing here — on
	// every remember and forget — keeps the badge honest. Best-effort: if the
	// count query fails, leave mention_count untouched rather than zeroing it.
	mentionClause := ""
	if mc, cerr := s.activeMentionCount(ctx, recID); cerr == nil {
		mentionClause = fmt.Sprintf(", mention_count = %d", mc)
	}

	stmt := fmt.Sprintf(`UPDATE %s SET derived_card = %s%s;`, recID, derivedExpr, mentionClause)
	_, err = s.db.SQL(ctx, stmt, true)
	return err
}

// activeMentionCount returns how many ACTIVE memories mention the entity. Used
// to keep mention_count honest (forgotten memories keep their mentions edge but
// must not inflate the count).
func (s *Store) activeMentionCount(ctx context.Context, recID string) (int, error) {
	stmt := fmt.Sprintf(`SELECT count() AS n FROM mentions WHERE out = %s AND in.is_active = true GROUP ALL;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return 0, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return 0, nil
	}
	var rows []struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].N, nil
}

// renderDerivedCard is the pure template renderer — split out for testability.
func renderDerivedCard(identity, facts, decisions []entityMemory, edges []derivedEdge) string {
	var b strings.Builder

	if len(identity) > 0 {
		b.WriteString("## Identity\n")
		for _, m := range identity {
			b.WriteString(fmt.Sprintf("- %s _(%s, %s)_\n", oneLiner(m.Summary), m.Author, fmtDay(m.CreatedAt)))
		}
		b.WriteString("\n")
	}

	if len(edges) > 0 {
		// Group active prefers edges into their own section; everything else in Active edges.
		var prefers, others []derivedEdge
		for _, e := range edges {
			if e.Predicate == "prefers" {
				prefers = append(prefers, e)
			} else {
				others = append(others, e)
			}
		}
		if len(others) > 0 {
			b.WriteString("## Active edges\n")
			for _, e := range others {
				extra := ""
				if e.RoleTitle != "" {
					extra = fmt.Sprintf(" (as %s)", e.RoleTitle)
				}
				arrow := "→"
				if e.Direction == "in" {
					arrow = "←"
				}
				b.WriteString(fmt.Sprintf("- %s %s **%s** _(%s)_%s\n",
					e.Predicate, arrow, e.OtherName, e.OtherType, extra))
			}
			b.WriteString("\n")
		}
		if len(prefers) > 0 {
			b.WriteString("## Preferences\n")
			for _, p := range prefers {
				val := p.Valence
				if val == "" {
					val = "neutral"
				}
				b.WriteString(fmt.Sprintf("- _(%s)_ **%s**\n", val, p.OtherName))
			}
			b.WriteString("\n")
		}
	}

	if len(facts) > 0 {
		b.WriteString("## Facts\n")
		for _, m := range facts {
			b.WriteString(fmt.Sprintf("- %s _(%s, %s)_\n", oneLiner(m.Summary), m.Author, fmtDay(m.CreatedAt)))
		}
		b.WriteString("\n")
	}

	if len(decisions) > 0 {
		b.WriteString("## Decisions\n")
		for _, m := range decisions {
			b.WriteString(fmt.Sprintf("- %s _(%s, %s)_\n", oneLiner(m.Summary), m.Author, fmtDay(m.CreatedAt)))
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func fmtDay(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// ============================================================
// Internal fetch helpers
// ============================================================

type entityMemory struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) fetchMemoriesForEntity(ctx context.Context, recID string, kind Kind, limit int) ([]entityMemory, error) {
	stmt := fmt.Sprintf(`SELECT
		in.id AS id,
		in.summary AS summary,
		in.author AS author,
		in.created_at AS created_at
		FROM mentions
		WHERE out = %s
		  AND in.kind = %s
		  AND in.is_active = true
		ORDER BY in.created_at DESC
		LIMIT %d;`,
		recID, EscapeStr(string(kind)), limit)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []entityMemory
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// derivedEdge is the per-edge projection used by renderDerivedCard.
type derivedEdge struct {
	Predicate string `json:"predicate"`
	Direction string `json:"direction"` // "out" if recID is subject, "in" if object
	OtherID   string `json:"other_id"`
	OtherName string `json:"other_name"`
	OtherType string `json:"other_type"`
	Valence   string `json:"valence,omitempty"`
	RoleTitle string `json:"role_title,omitempty"`
	// OtherLastEdited is the far entity's last activity timestamp — used to
	// recency-gate works_on/operates edges so dormant projects drop off the card.
	OtherLastEdited time.Time `json:"other_last_edited"`
}

// fetchActiveEdgesTouching collects all active edges (valid_until IS NONE)
// across the 6 dedicated relation tables AND the generic assertion table,
// in either direction. Results are sorted by predicate then other_name so
// the derived card stays diffable.
func (s *Store) fetchActiveEdgesTouching(ctx context.Context, recID string) ([]derivedEdge, error) {
	tables := []string{"works_on", "uses", "prefers", "works_at", "depends_on", "part_of", "assertion"}
	var out []derivedEdge
	for _, tbl := range tables {
		outRows, err := s.fetchActiveEdgeDirection(ctx, tbl, recID, "out")
		if err != nil {
			return nil, fmt.Errorf("%s out: %w", tbl, err)
		}
		out = append(out, outRows...)
		inRows, err := s.fetchActiveEdgeDirection(ctx, tbl, recID, "in")
		if err != nil {
			return nil, fmt.Errorf("%s in: %w", tbl, err)
		}
		out = append(out, inRows...)
	}
	// Recency gate: drop dormant works_on/operates edges from the card so it
	// reflects what's currently active. The edge stays valid in the graph
	// (valid_until untouched) — this only affects the rendered card. Recency
	// uses the far entity's last_edited_at (the project's last activity).
	cutoff := orientRecencyCutoff()
	kept := out[:0]
	for _, e := range out {
		if isRecencyGatedPredicate(e.Predicate) && !e.OtherLastEdited.IsZero() && e.OtherLastEdited.Before(cutoff) {
			continue
		}
		kept = append(kept, e)
	}
	out = kept

	// Sort: predicate asc, then other_name asc — deterministic.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Predicate != out[j].Predicate {
			return out[i].Predicate < out[j].Predicate
		}
		return strings.ToLower(out[i].OtherName) < strings.ToLower(out[j].OtherName)
	})
	return out, nil
}

func (s *Store) fetchActiveEdgeDirection(ctx context.Context, table, recID, dir string) ([]derivedEdge, error) {
	var filterCol, otherCol string
	if dir == "out" {
		filterCol = "in"
		otherCol = "out"
	} else {
		filterCol = "out"
		otherCol = "in"
	}
	// For the assertion table, predicate lives in a field. For the 6 dedicated
	// tables, the predicate IS the table name.
	predExpr := EscapeStr(table)
	if table == "assertion" {
		predExpr = "predicate"
	}
	stmt := fmt.Sprintf(`SELECT
		%[5]s AS predicate,
		%[3]q AS direction,
		%[1]s.id AS other_id,
		%[1]s.name AS other_name,
		meta::tb(%[1]s) AS other_type,
		%[1]s.last_edited_at AS other_last_edited,
		valence,
		role_title
		FROM %[4]s
		WHERE %[2]s = %[6]s
		  AND valid_until IS NONE;`,
		otherCol, filterCol, dir, table, predExpr, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		// Schema permits per-table missing fields (e.g. assertion has no role_title)
		// — SurrealDB returns NONE for them which decodes to "". Real errors bubble.
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []derivedEdge
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
