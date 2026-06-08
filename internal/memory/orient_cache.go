package memory

import (
	"context"
	"fmt"
)

// markOrientStale flips the orient_cache row for the given scope to
// stale=true. If no row exists yet, creates one with stale=true.
func (s *Store) markOrientStale(ctx context.Context, scope string) error {
	stmt := fmt.Sprintf(`UPSERT orient_cache:`+"`%s`"+` SET
		scope = %s,
		stale = true;`,
		safeID(scope),
		EscapeStr(scope),
	)
	_, err := s.db.SQL(ctx, stmt, true)
	return err
}
