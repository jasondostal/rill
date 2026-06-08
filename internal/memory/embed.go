package memory

import (
	"context"
	"os"
	"time"

	rilllog "github.com/jasondostal/rill/internal/log"
)

// writeEmbedTimeout bounds a single best-effort embed on the write path. It's a
// var (not a const) so tests can shrink it. Embeddings are best-effort —
// reindex backfills anything skipped — so a slow/hung embedder must never hold
// a write open for minutes. Overridable via RILL_WRITE_EMBED_TIMEOUT (Go
// duration, e.g. "8s").
var writeEmbedTimeout = func() time.Duration {
	if v := os.Getenv("RILL_WRITE_EMBED_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 8 * time.Second
}()

// embedForWrite embeds a single text for the write path: bounded by
// writeEmbedTimeout and best-effort. On nil embedder, timeout, or any error it
// returns nil (the row is written without a vector and reindex can backfill it
// later) and logs the failure — so silent degradation can't recur. This is the
// only embed entry point the write path (Remember / entity upsert / EditCard)
// should use.
func (s *Store) embedForWrite(ctx context.Context, text string) []float64 {
	if s.embedder == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, writeEmbedTimeout)
	defer cancel()
	vec, err := s.embedder.Embed(ctx, text)
	if err != nil {
		rilllog.Logger().Warn("write-path embed failed (proceeding without vector; reindex can backfill)",
			"error", err)
		return nil
	}
	return vec
}
