package memory

import (
	"context"
	"fmt"
	"strings"
)

// Promote sets the entity's promoted flag to true and records the actor.
// Promoted entities appear in the default orient render.
func (s *Store) Promote(ctx context.Context, entityRef string, typ EntityType, author string) error {
	recID, err := s.resolveEntityRef(ctx, entityRef, typ)
	if err != nil {
		return err
	}
	if !strings.Contains(recID, ":") {
		return errs("invalid entity ref %q", entityRef)
	}
	stmt := fmt.Sprintf(`UPDATE %s SET
		promoted = true,
		last_edited_by = %s,
		last_edited_at = %s;`,
		recID, EscapeStr(author), EscapeDatetime(now()))
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return err
	}
	return s.markOrientStale(ctx, "global")
}

// Demote flips promoted back to false. Used as the counter to autonomous
// agent promotions, exposed via UI and CLI.
func (s *Store) Demote(ctx context.Context, entityRef string, typ EntityType, author string) error {
	recID, err := s.resolveEntityRef(ctx, entityRef, typ)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf(`UPDATE %s SET
		promoted = false,
		last_edited_by = %s,
		last_edited_at = %s;`,
		recID, EscapeStr(author), EscapeDatetime(now()))
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return err
	}
	return s.markOrientStale(ctx, "global")
}
