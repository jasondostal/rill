package memory

import (
	"context"
	"fmt"
	"strings"
)

// Forget soft-deletes a memory by id. Sets is_active = false. The row stays
// in the database (and its mentions edges stay intact) so provenance is
// preserved, but Recall and Orient filter it out by default. Recomputes
// derived_card on every entity the memory mentioned so the forgotten line
// disappears from their rendered view.
func (s *Store) Forget(ctx context.Context, memoryID string) error {
	if !strings.HasPrefix(memoryID, "memory:") {
		return errs("memory id must start with 'memory:' (got %q)", memoryID)
	}
	memoryID = normalizeMemoryID(memoryID)
	if err := safeRecordID(memoryID); err != nil {
		return err
	}

	// Fetch the memory's mentioned entities BEFORE marking inactive so we
	// know which derived cards need re-rendering.
	detail, err := s.GetMemory(ctx, memoryID)
	if err != nil {
		return err
	}

	stmt := fmt.Sprintf(`UPDATE %s SET is_active = false, updated_at = time::now();`, memoryID)
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return err
	}

	// Recompute derived_card on each mentioned entity. The fetchMemoriesForEntity
	// query filters by is_active = true, so a forgotten memory drops out.
	if detail != nil {
		for _, e := range detail.MentionedEntities {
			_ = s.recomputeDerivedCard(ctx, e.ID)
		}
	}

	_ = s.markOrientStale(ctx, "global")
	return nil
}
