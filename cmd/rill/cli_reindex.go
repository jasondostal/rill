package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jasondostal/rill/internal/memory"
)

// runReindex backfills embeddings across the corpus. Server-side maintenance op
// (direct Store access, like `serve`) — run inside the container where the
// SurrealDB + embedder env live:
//
//	docker exec rill rill reindex-embeddings          # only rows missing a vector
//	docker exec rill rill reindex-embeddings --all    # re-embed everything (model change)
func runReindex(args []string) {
	all := false
	for _, a := range args {
		if a == "--all" {
			all = true
		}
	}

	ctx := context.Background()
	store := memory.NewFromEnv()
	if err := store.Ping(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "reindex: db unreachable:", err)
		os.Exit(1)
	}

	mode := "missing-only"
	if all {
		mode = "ALL (re-embed everything)"
	}
	fmt.Printf("reindex-embeddings: %s\n", mode)

	results, err := store.ReindexEmbeddings(ctx, all)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reindex:", err)
		os.Exit(1)
	}

	var scanned, embedded, failed int
	for _, r := range results {
		fmt.Printf("  %-14s scanned=%-4d embedded=%-4d failed=%d\n", r.Table, r.Scanned, r.Embedded, r.Failed)
		scanned += r.Scanned
		embedded += r.Embedded
		failed += r.Failed
	}
	fmt.Printf("done: scanned=%d embedded=%d failed=%d\n", scanned, embedded, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
