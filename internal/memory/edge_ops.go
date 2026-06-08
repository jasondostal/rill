package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AddEdge writes a single relationship edge between two existing entities,
// applying the same consolidation rules as the Remember pipeline's edges step,
// then recomputes derived_card on both endpoints.
//
// Both endpoints must already exist — AddEdge does NOT auto-upsert entities.
// The caller is expected to have either declared them through a prior Remember
// or created them via some other intentional path.
//
// Use case: the UI lets a human add an edge ("Alice likes X") without writing
// a memory. The graph mutation alone is enough to update derived_card.
func (s *Store) AddEdge(ctx context.Context, edge EdgeDecl, author string) (*EdgeRef, error) {
	if edge.Subject == "" || edge.Object == "" || edge.Predicate == "" {
		return nil, errs("edge missing subject/object/predicate")
	}
	if !validEntityType(edge.SubjectType) || !validEntityType(edge.ObjectType) {
		return nil, errs("invalid entity type on edge")
	}
	if edge.Valence != "" && !validValence(edge.Valence) {
		return nil, errs("invalid valence %q", edge.Valence)
	}

	subjID := entityRecID(edge.SubjectType, edge.Subject)
	objID := entityRecID(edge.ObjectType, edge.Object)

	// Sanity check: both endpoints must exist (no auto-upsert).
	for _, recID := range []string{subjID, objID} {
		exists, err := s.entityExists(ctx, recID)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errs("entity %s does not exist — declare it via remember() before linking", recID)
		}
	}

	ts := now()
	ref, err := s.writeEdge(ctx, edge, subjID, objID, "" /* no source memory */, ts)
	if err != nil {
		return nil, fmt.Errorf("write edge: %w", err)
	}

	// Audit which user did this (writeEdge doesn't record actor on the entity,
	// but we do bump last_edited_by/at on both endpoints so it shows up in UI).
	_ = s.touchEntity(ctx, subjID, author, ts)
	_ = s.touchEntity(ctx, objID, author, ts)

	// Recompute derived card on both endpoints. Subject definitely changed;
	// object's set of incoming edges also changed.
	if err := s.recomputeDerivedCard(ctx, subjID); err != nil {
		return ref, fmt.Errorf("recompute subject card: %w", err)
	}
	if err := s.recomputeDerivedCard(ctx, objID); err != nil {
		return ref, fmt.Errorf("recompute object card: %w", err)
	}

	_ = s.markOrientStale(ctx, "global")
	return ref, nil
}

// CloseEdge soft-closes an active edge by setting valid_until = now.
// Provenance is preserved — the edge stays in the DB, just inactive.
// Recomputes derived_card on both endpoints so the closed edge disappears
// from the rendered view.
func (s *Store) CloseEdge(ctx context.Context, edgeID, author string) error {
	if err := safeRecordID(edgeID); err != nil {
		return err
	}

	// Pull subject + object before closing so we can recompute their cards.
	subjID, objID, err := s.fetchEdgeEndpoints(ctx, edgeID)
	if err != nil {
		return err
	}
	if subjID == "" || objID == "" {
		return errs("edge %s not found", edgeID)
	}

	ts := now()
	stmt := fmt.Sprintf(`UPDATE %s SET valid_until = %s;`, edgeID, EscapeDatetime(ts))
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return fmt.Errorf("close edge: %w", err)
	}

	_ = s.touchEntity(ctx, subjID, author, ts)
	_ = s.touchEntity(ctx, objID, author, ts)
	if err := s.recomputeDerivedCard(ctx, subjID); err != nil {
		return fmt.Errorf("recompute subject card: %w", err)
	}
	if err := s.recomputeDerivedCard(ctx, objID); err != nil {
		return fmt.Errorf("recompute object card: %w", err)
	}

	_ = s.markOrientStale(ctx, "global")
	return nil
}

// ============================================================
// Internal helpers
// ============================================================

func (s *Store) entityExists(ctx context.Context, recID string) (bool, error) {
	stmt := fmt.Sprintf(`SELECT id FROM %s LIMIT 1;`, recID)
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
	return len(rows) > 0 && rows[0].ID != "", nil
}

// fetchEdgeEndpoints returns the (in, out) record ids for an edge.
func (s *Store) fetchEdgeEndpoints(ctx context.Context, edgeID string) (string, string, error) {
	stmt := fmt.Sprintf(`SELECT in.id AS in_id, out.id AS out_id FROM %s LIMIT 1;`, edgeID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return "", "", err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return "", "", nil
	}
	var rows []struct {
		In  string `json:"in_id"`
		Out string `json:"out_id"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return "", "", err
	}
	if len(rows) == 0 {
		return "", "", nil
	}
	return rows[0].In, rows[0].Out, nil
}

// touchEntity bumps last_edited_by/at on the given entity record. Used so
// UI surfaces show "edited by claude at X" after graph mutations that
// otherwise wouldn't touch the entity row directly.
func (s *Store) touchEntity(ctx context.Context, recID, author string, ts time.Time) error {
	if !strings.Contains(recID, ":") {
		return errs("touchEntity: invalid record id %q", recID)
	}
	stmt := fmt.Sprintf(`UPDATE %s SET last_edited_by = %s, last_edited_at = %s;`,
		recID, EscapeStr(author), EscapeDatetime(ts))
	_, err := s.db.SQL(ctx, stmt, true)
	return err
}
