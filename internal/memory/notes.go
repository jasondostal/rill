package memory

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// NotesMode determines how an EditHandNotes call combines new text with existing.
type NotesMode string

const (
	NotesAppend  NotesMode = "append"
	NotesReplace NotesMode = "replace"
)

// EditHandNotes updates the markdown `hand_notes` field on the entity
// identified by entityRef (e.g. "tool:pi" or just "Pi" if `typ` is supplied).
//
//   - append (default): new text is appended, separated by a blank line
//   - replace: overwrites the field (replace-with-empty clears it)
//
// hand_notes is the human-edited surface. derived_card is system-maintained
// and not writable here — recomputeDerivedCard is the only thing that touches it.
//
// Records last_edited_by / last_edited_at and marks orient caches stale.
func (s *Store) EditHandNotes(ctx context.Context, entityRef string, typ EntityType, text string, mode NotesMode, author string) error {
	if mode == "" {
		mode = NotesAppend
	}
	if mode != NotesAppend && mode != NotesReplace {
		return errs("invalid notes mode %q (use append or replace)", mode)
	}
	recID, err := s.resolveEntityRef(ctx, entityRef, typ)
	if err != nil {
		return err
	}
	ts := now()
	if err := s.editHandNotesAtRecID(ctx, recID, text, mode, author, ts); err != nil {
		return err
	}
	// Mark global orient stale; hand_notes edits affect the rendered orient.
	_ = s.markOrientStale(ctx, "global")
	return nil
}

func (s *Store) editHandNotesAtRecID(ctx context.Context, recID, text string, mode NotesMode, author string, ts time.Time) error {
	var stmt string
	if mode == NotesReplace {
		valueExpr := EscapeStr(text)
		if text == "" {
			valueExpr = "NONE" // explicit clear
		}
		stmt = fmt.Sprintf(`UPDATE %s SET
			hand_notes = %s,
			last_edited_by = %s,
			last_edited_at = %s;`,
			recID, valueExpr, EscapeStr(author), EscapeDatetime(ts))
	} else {
		if text == "" {
			return nil // nothing to append
		}
		stmt = fmt.Sprintf(`UPDATE %s SET
			hand_notes = string::concat(IF hand_notes IS NONE THEN '' ELSE hand_notes + '\n\n' END, %s),
			last_edited_by = %s,
			last_edited_at = %s;`,
			recID, EscapeStr(text), EscapeStr(author), EscapeDatetime(ts))
	}
	_, err := s.db.SQL(ctx, stmt, true)
	return err
}

// resolveEntityRef accepts either a full record id ("tool:pi") or a bare name
// when the caller also supplies a type. Returns the record id.
func (s *Store) resolveEntityRef(ctx context.Context, ref string, typ EntityType) (string, error) {
	if strings.Contains(ref, ":") {
		// A full record id — validate before it reaches a query, since it's
		// interpolated raw downstream (SurrealQL injection guard).
		if err := safeRecordID(ref); err != nil {
			return "", err
		}
		return ref, nil
	}
	if typ == "" {
		return "", errs("entity ref %q has no table prefix and no type was supplied", ref)
	}
	return entityRecID(typ, ref), nil
}
