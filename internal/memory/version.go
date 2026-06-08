package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SetVersion records a new current version for an entity, bi-temporally. The
// version is a STRING ATTRIBUTE on the version_is table — never a graph node,
// so versions don't pollute the entity space. Exclusive/superseding: the prior
// active row (if any) is closed (valid_until = now); a fresh active row is
// inserted (valid_until = NONE). No-op if the entity is already at `version`.
//
//	current(X)  = SELECT version FROM version_is WHERE entity=X AND valid_until IS NONE
//	as-of(X,D)  = ... WHERE entity=X AND valid_from <= D AND (valid_until IS NONE OR valid_until > D)
func (s *Store) SetVersion(ctx context.Context, entityRef string, typ EntityType, version, author string) error {
	recID, err := s.resolveEntityRef(ctx, entityRef, typ)
	if err != nil {
		return err
	}
	ts := now()
	stmts, err := s.planSetVersion(ctx, recID, version, ts)
	if err != nil {
		return err
	}
	if stmts == nil {
		return nil // unchanged — no-op
	}
	for _, stmt := range stmts {
		if _, err := s.db.SQL(ctx, stmt, true); err != nil {
			return fmt.Errorf("set version: %w", err)
		}
	}
	_ = s.touchEntity(ctx, recID, author, ts)
	_ = s.recomputeDerivedCard(ctx, recID)
	_ = s.markOrientStale(ctx, "global")
	return nil
}

// planSetVersion returns the close-prior + create statements for a bi-temporal
// version change, or nil if the entity is already at `version` (no-op). The
// reads (validation + current-version check) happen here; the writes are
// returned for the caller to run — immediately (SetVersion) or batched into a
// Remember transaction.
func (s *Store) planSetVersion(ctx context.Context, recID, version string, ts time.Time) ([]string, error) {
	if !strings.Contains(recID, ":") {
		return nil, errs("invalid entity ref %q", recID)
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return nil, errs("version cannot be empty")
	}
	// No-op if unchanged — avoids churning the history with identical rows.
	if cur, _ := s.CurrentVersion(ctx, recID); cur == version {
		return nil, nil
	}
	closeStmt := fmt.Sprintf(
		`UPDATE version_is SET valid_until = %s WHERE entity = %s AND valid_until IS NONE;`,
		EscapeDatetime(ts), recID)
	createStmt := fmt.Sprintf(
		`CREATE version_is SET entity = %s, version = %s, valid_from = %s, created_at = %s;`,
		recID, EscapeStr(version), EscapeDatetime(ts), EscapeDatetime(ts))
	return []string{closeStmt, createStmt}, nil
}

// CurrentVersion returns the entity's current version string (the active row),
// or "" if none is set.
func (s *Store) CurrentVersion(ctx context.Context, recID string) (string, error) {
	// Supersession keeps at most one active row per entity, so no ORDER needed
	// (and SurrealDB 3.x requires ORDER fields to be in the projection anyway).
	stmt := fmt.Sprintf(
		`SELECT version FROM version_is WHERE entity = %s AND valid_until IS NONE LIMIT 1;`,
		recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return "", err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return "", nil
	}
	var rows []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].Version, nil
}
