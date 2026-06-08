package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ReindexResult reports the outcome of backfilling one table.
type ReindexResult struct {
	Table    string `json:"table"`
	Scanned  int    `json:"scanned"`
	Embedded int    `json:"embedded"`
	Failed   int    `json:"failed"`
}

type reindexRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// ReindexEmbeddings backfills vector embeddings across the corpus: memory
// summaries and entity name(+summary) rows. With all=false it only touches rows
// whose embedding IS NONE (cheap, re-runnable, the normal "we just turned the
// embedder on" case). With all=true it re-embeds every row — use after changing
// the embedding model, since vectors from different models aren't comparable.
//
// The entity text mirrors planUpsertEntity ("name" or "name. summary") so a
// backfilled vector is identical to what a fresh write would produce. Embedding
// is batched; a batch failure is counted, not fatal — the rest still proceed.
func (s *Store) ReindexEmbeddings(ctx context.Context, all bool) ([]ReindexResult, error) {
	if s.embedder == nil {
		return nil, fmt.Errorf("reindex: no embedder configured")
	}
	// Probe once so a missing/invalid key fails loudly here instead of silently
	// marking every row failed (the embedder client is non-nil even with no key).
	if _, err := s.embedder.Embed(ctx, "rill embedder probe"); err != nil {
		return nil, fmt.Errorf("reindex: embedder not usable (%w) — check EMBEDDING_API_KEY and model", err)
	}

	var out []ReindexResult

	memRes, err := s.reindexTable(ctx, "memory", false, all, "is_active = true")
	if err != nil {
		return out, fmt.Errorf("reindex memory: %w", err)
	}
	out = append(out, memRes)

	for _, t := range ValidEntityTypes {
		r, err := s.reindexTable(ctx, string(t), true, all, "merged_into IS NONE")
		if err != nil {
			return out, fmt.Errorf("reindex %s: %w", t, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// reindexTable backfills one table. isEntity selects the text shape (entity =
// name[. summary]; otherwise = summary). baseFilter scopes the active rows.
func (s *Store) reindexTable(ctx context.Context, table string, isEntity, all bool, baseFilter string) (ReindexResult, error) {
	res := ReindexResult{Table: table}

	filters := []string{baseFilter}
	if !all {
		filters = append(filters, "embedding IS NONE")
	}
	stmt := fmt.Sprintf(`SELECT id, name, summary FROM %s WHERE %s;`, table, strings.Join(filters, " AND "))
	rows, err := s.fetchReindexRows(ctx, stmt)
	if err != nil {
		return res, err
	}
	res.Scanned = len(rows)

	const batch = 64
	for i := 0; i < len(rows); i += batch {
		end := i + batch
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		texts := make([]string, len(chunk))
		for j, r := range chunk {
			texts[j] = reindexText(isEntity, r)
		}
		vecs, err := s.embedder.EmbedBatch(ctx, texts)
		if err != nil || len(vecs) != len(chunk) {
			res.Failed += len(chunk)
			continue
		}
		for j, r := range chunk {
			b, err := json.Marshal(vecs[j])
			if err != nil {
				res.Failed++
				continue
			}
			// r.ID is the record id exactly as SurrealDB returned it (already
			// backtick-quoted where needed), so it interpolates safely.
			if _, err := s.db.SQL(ctx, fmt.Sprintf(`UPDATE %s SET embedding = %s;`, r.ID, string(b)), true); err != nil {
				res.Failed++
				continue
			}
			res.Embedded++
		}
	}
	return res, nil
}

// reindexText builds the text to embed for a row, matching planUpsertEntity's
// entity text and buildMemoryInsert's summary embedding.
func reindexText(isEntity bool, r reindexRow) string {
	if !isEntity {
		return r.Summary
	}
	if strings.TrimSpace(r.Summary) != "" {
		return r.Name + ". " + r.Summary
	}
	return r.Name
}

func (s *Store) fetchReindexRows(ctx context.Context, stmt string) ([]reindexRow, error) {
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []reindexRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
