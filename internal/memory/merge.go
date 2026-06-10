package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// mergeableEdgeTables are the typed relation tables re-pointed during a merge.
// `mentions` is handled separately: it has no valid_until (can't be closed),
// and its `in` is a memory — only its `out` is ever an entity.
var mergeableEdgeTables = []string{
	"works_on", "uses", "prefers", "works_at", "depends_on", "part_of", "assertion",
}

// MergeResult summarizes what a merge moved.
type MergeResult struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	EdgesMoved    int    `json:"edges_moved"`
	MentionsMoved int    `json:"mentions_moved"`
	SelfLoops     int    `json:"self_loops_dropped"`
}

type mergeRow struct {
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	HandNotes    string   `json:"hand_notes"`
	MentionCount int      `json:"mention_count"`
	IsMerged     bool     `json:"is_merged"`
}

type mergeEdge struct {
	ID        string  `json:"id"`
	In        string  `json:"in_id"`
	Out       string  `json:"out_id"`
	Predicate string  `json:"predicate"`
	Valence   string  `json:"valence"`
	RoleTitle string  `json:"role_title"`
	Weight    float64 `json:"weight"`
}

type mergeMention struct {
	ID     string  `json:"id"`
	In     string  `json:"in_id"`
	Weight float64 `json:"weight"`
}

func recordTable(recID string) string {
	if i := strings.IndexByte(recID, ':'); i > 0 {
		return recID[:i]
	}
	return ""
}

// MergeEntity folds source into target. Same-type by default; allowCrossType
// permits merging across entity types for the common real-world dupe where the
// SAME thing was recorded under two types (tool:fsevents vs concept:fsevents).
// Cross-type refs must be full record ids — a single `typ` can't resolve two
// bare names to different tables.
//
// Active edges and mentions on source are re-created on target — typed edges
// flow through writeEdge so they pick up the same dedup + exclusive-predicate
// consolidation as a normal write. The originals on source are closed (typed
// edges) or removed (mentions), so the retired source keeps only historical /
// closed edges. Source's name + aliases are folded into target's aliases (so
// old references still resolve to the survivor), hand_notes are appended, and
// mention_count is summed. Source is soft-retired via merged_into — archived,
// never deleted, and reversible.
//
// Requires admin scope at the tool layer (structural mutation).
func (s *Store) MergeEntity(ctx context.Context, sourceRef, targetRef string, typ EntityType, author string, allowCrossType bool) (*MergeResult, error) {
	srcID, err := s.resolveEntityRef(ctx, sourceRef, typ)
	if err != nil {
		return nil, err
	}
	tgtID, err := s.resolveEntityRef(ctx, targetRef, typ)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(srcID, ":") || !strings.Contains(tgtID, ":") {
		return nil, errs("merge requires fully-qualified entity ids (got %q, %q)", srcID, tgtID)
	}
	if srcID == tgtID {
		return nil, errs("cannot merge an entity into itself")
	}
	if recordTable(srcID) != recordTable(tgtID) && !allowCrossType {
		return nil, errs("source and target are different types (%s vs %s) — if they are genuinely the SAME thing recorded under two types, retry with allow_cross_type:true and full record ids", recordTable(srcID), recordTable(tgtID))
	}

	src, err := s.fetchMergeRow(ctx, srcID)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errs("source entity %s not found", srcID)
	}
	tgt, err := s.fetchMergeRow(ctx, tgtID)
	if err != nil {
		return nil, err
	}
	if tgt == nil {
		return nil, errs("target entity %s not found", tgtID)
	}
	if src.IsMerged {
		return nil, errs("source %s is already merged — pick a live entity", srcID)
	}
	if tgt.IsMerged {
		return nil, errs("target %s is itself merged into another entity — merge into the survivor instead", tgtID)
	}

	ts := now()
	result := &MergeResult{Source: srcID, Target: tgtID}

	// 1. Re-point typed edges: close the original on source, re-create on target.
	for _, table := range mergeableEdgeTables {
		edges, err := s.fetchActiveEdgesForMerge(ctx, table, srcID)
		if err != nil {
			return nil, fmt.Errorf("merge: fetch %s edges: %w", table, err)
		}
		for _, e := range edges {
			// Always retire the original edge attached to source (kept as history).
			closeStmt := fmt.Sprintf(`UPDATE %s SET valid_until = %s;`, e.ID, EscapeDatetime(ts))
			if _, err := s.db.SQL(ctx, closeStmt, true); err != nil {
				return nil, fmt.Errorf("merge: close edge %s: %w", e.ID, err)
			}
			newSubj, newObj := e.In, e.Out
			if newSubj == srcID {
				newSubj = tgtID
			}
			if newObj == srcID {
				newObj = tgtID
			}
			if newSubj == newObj {
				result.SelfLoops++ // degenerate self-loop: closed above, don't recreate
				continue
			}
			predicate := table
			if table == "assertion" {
				predicate = e.Predicate
			}
			decl := EdgeDecl{
				Predicate:   predicate,
				SubjectType: EntityType(recordTable(newSubj)),
				ObjectType:  EntityType(recordTable(newObj)),
				Valence:     Valence(e.Valence),
				RoleTitle:   e.RoleTitle,
				Weight:      e.Weight,
			}
			if _, err := s.writeEdge(ctx, decl, newSubj, newObj, "", ts); err != nil {
				return nil, fmt.Errorf("merge: rewrite %s edge onto target: %w", table, err)
			}
			result.EdgesMoved++
		}
	}

	// 2. Re-point mentions (memory -> entity). No valid_until, so delete + relate.
	mentions, err := s.fetchMentionsForMerge(ctx, srcID)
	if err != nil {
		return nil, fmt.Errorf("merge: fetch mentions: %w", err)
	}
	for _, m := range mentions {
		exists, err := s.mentionExists(ctx, m.In, tgtID)
		if err != nil {
			return nil, err
		}
		if !exists {
			relate := fmt.Sprintf(`RELATE %s->mentions->%s SET weight = %s, created_at = %s;`,
				m.In, tgtID, formatFloat(m.Weight), EscapeDatetime(ts))
			if _, err := s.db.SQL(ctx, relate, true); err != nil {
				return nil, fmt.Errorf("merge: relate mention onto target: %w", err)
			}
			result.MentionsMoved++
		}
		if _, err := s.db.SQL(ctx, fmt.Sprintf(`DELETE %s;`, m.ID), true); err != nil {
			return nil, fmt.Errorf("merge: delete old mention %s: %w", m.ID, err)
		}
	}

	// 2b. Re-point version_is rows (bi-temporal version attribute table), then
	// collapse to a single active version on the target (keep the newest).
	if _, err := s.db.SQL(ctx, fmt.Sprintf(`UPDATE version_is SET entity = %s WHERE entity = %s;`, tgtID, srcID), true); err != nil {
		return nil, fmt.Errorf("merge: re-point version_is: %w", err)
	}
	{
		res, err := s.db.SQL(ctx, fmt.Sprintf(`SELECT id, valid_from FROM version_is WHERE entity = %s AND valid_until IS NONE ORDER BY valid_from DESC;`, tgtID), true)
		if err != nil {
			return nil, fmt.Errorf("merge: list active versions: %w", err)
		}
		if len(res) > 0 && len(res[0].Result) > 0 {
			var vrows []struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(res[0].Result, &vrows); err != nil {
				return nil, fmt.Errorf("merge: decode versions: %w", err)
			}
			for i, vr := range vrows {
				if i == 0 {
					continue // keep the newest active version
				}
				if _, err := s.db.SQL(ctx, fmt.Sprintf(`UPDATE %s SET valid_until = %s;`, vr.ID, EscapeDatetime(ts)), true); err != nil {
					return nil, fmt.Errorf("merge: close dup version: %w", err)
				}
			}
		}
	}

	// 3. Fold identity into target: aliases (+ source name), notes, mention_count.
	aliases := append([]string{}, src.Aliases...)
	aliases = append(aliases, src.Name)
	aliasJSON, _ := json.Marshal(aliases)
	updTarget := fmt.Sprintf(`UPDATE %s SET
		aliases = array::distinct(array::concat(aliases, %s)),
		mention_count = mention_count + %d,
		last_edited_by = %s,
		last_edited_at = %s;`,
		tgtID, string(aliasJSON), src.MentionCount, EscapeStr(author), EscapeDatetime(ts))
	if _, err := s.db.SQL(ctx, updTarget, true); err != nil {
		return nil, fmt.Errorf("merge: fold identity into target: %w", err)
	}
	if strings.TrimSpace(src.HandNotes) != "" {
		note := fmt.Sprintf("_Merged from %s (`%s`) on %s._\n\n%s",
			src.Name, srcID, ts.UTC().Format("2006-01-02"), src.HandNotes)
		if err := s.editHandNotesAtRecID(ctx, tgtID, note, NotesAppend, author, ts); err != nil {
			return nil, fmt.Errorf("merge: append source notes to target: %w", err)
		}
	}

	// 4. Soft-retire source (archived, reversible). merged_into is the tombstone.
	retire := fmt.Sprintf(`UPDATE %s SET
		merged_into = %s,
		promoted = false,
		last_edited_by = %s,
		last_edited_at = %s;`,
		srcID, tgtID, EscapeStr(author), EscapeDatetime(ts))
	if _, err := s.db.SQL(ctx, retire, true); err != nil {
		return nil, fmt.Errorf("merge: retire source: %w", err)
	}

	// 5. Recompute the survivor's card; bust orient cache.
	if err := s.recomputeDerivedCard(ctx, tgtID); err != nil {
		return result, fmt.Errorf("merge: recompute target card: %w", err)
	}
	_ = s.markOrientStale(ctx, "global")
	return result, nil
}

// fetchMergeRow pulls the merge-relevant fields for one entity. is_merged is
// derived (merged_into IS NOT NONE) so we never have to parse a record link.
func (s *Store) fetchMergeRow(ctx context.Context, recID string) (*mergeRow, error) {
	stmt := fmt.Sprintf(`SELECT
		name, aliases, hand_notes, mention_count,
		(merged_into IS NOT NONE) AS is_merged
		FROM %s;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []mergeRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// fetchActiveEdgesForMerge returns active edges in `table` where recID is
// either endpoint. predicate resolves to the table name for the dedicated
// tables and to the stored field for the generic `assertion` table.
func (s *Store) fetchActiveEdgesForMerge(ctx context.Context, table, recID string) ([]mergeEdge, error) {
	predExpr := EscapeStr(table)
	if table == "assertion" {
		predExpr = "predicate"
	}
	stmt := fmt.Sprintf(`SELECT
		id,
		in.id AS in_id,
		out.id AS out_id,
		%s AS predicate,
		valence,
		role_title,
		weight
		FROM %s
		WHERE (in = %s OR out = %s) AND valid_until IS NONE;`,
		predExpr, table, recID, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []mergeEdge
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) fetchMentionsForMerge(ctx context.Context, recID string) ([]mergeMention, error) {
	stmt := fmt.Sprintf(`SELECT id, in.id AS in_id, weight FROM mentions WHERE out = %s;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []mergeMention
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) mentionExists(ctx context.Context, memoryID, entityID string) (bool, error) {
	stmt := fmt.Sprintf(`SELECT id FROM mentions WHERE in = %s AND out = %s LIMIT 1;`, memoryID, entityID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return false, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return false, nil
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func formatFloat(f float64) string {
	if f == 0 {
		f = 1.0
	}
	return fmt.Sprintf("%g", f)
}
