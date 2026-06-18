package memory

import (
	"context"
	"time"

	"github.com/jasondostal/rill/internal/embedding"
	rilllog "github.com/jasondostal/rill/internal/log"
)

// Embedder is the subset of embedding.Client that the memory store depends on.
// Keeping it an interface (rather than the concrete *embedding.Client) lets
// tests inject a fake — to exercise the best-effort/timeout write path and the
// recall fusion logic without a live API key.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	Model() string
}

// Store is the top-level entry point for the memoryv3 package.
// It holds the SurrealDB client and an embedder. All public operations
// (Remember, Recall, EditCard, Promote, Demote, Forget, Orient) are
// methods on Store.
type Store struct {
	db       *Client
	embedder Embedder
}

// New builds a Store from a Config and an embedding client.
//
// If embedder is nil, memories will be stored without a vector; recall by
// semantic similarity will silently skip those rows. Useful for tests.
func New(db *Client, embedder Embedder) *Store {
	return &Store{db: db, embedder: embedder}
}

// NewFromEnv builds a Store using env-driven configs for both DB and embedder.
// Logs the embedder's status at boot so a missing key surfaces immediately
// rather than silently degrading recall to FTS-only (the bug that left prod
// embedder-less for weeks).
func NewFromEnv() *Store {
	emb := embedding.NewClientFromEnv()
	if key, src := embedding.LoadAPIKeyWithSource(); key == "" {
		rilllog.Logger().Warn("embedder disabled: no API key found — recall is FTS-only, no vectors written")
	} else {
		rilllog.Logger().Info("embedder enabled", "source", src, "model", emb.Model())
	}
	return New(NewClient(ConfigFromEnv()), emb)
}

// Ping verifies the DB is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// now returns the current time in UTC at nanosecond precision. Two
// Remember() calls in the same second must produce distinct memory ids,
// so we keep the full resolution.
func now() time.Time {
	return time.Now().UTC()
}
