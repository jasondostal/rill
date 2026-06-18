package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	rilllog "github.com/jasondostal/rill/internal/log"
)

// RecallQuery is the input to Recall().
type RecallQuery struct {
	Query   string `json:"query"`              // natural language; embedded for vector search
	Kind    Kind   `json:"kind,omitempty"`     // optional filter
	Project string `json:"project,omitempty"`  // optional scope
	Author  string `json:"author,omitempty"`   // optional filter
	K       int    `json:"k,omitempty"`        // top-K results; default 5
}

// MemoryHit is a recalled memory with similarity / relevance data.
type MemoryHit struct {
	ID       string  `json:"id"`
	Summary  string  `json:"summary"`
	Details  string  `json:"details,omitempty"`
	Kind     string  `json:"kind"`
	Project  string  `json:"project,omitempty"`
	Author   string  `json:"author"`
	Tags     []string `json:"tags,omitempty"`
	Valence  string  `json:"valence,omitempty"`
	// Distance is the cosine distance from the query vector (lower = more
	// similar). A POINTER so an FTS-only hit reports no distance (nil) rather
	// than a fabricated 0, which would read as "perfectly similar".
	Distance *float64 `json:"distance,omitempty"`
	// Score is the fused Reciprocal-Rank-Fusion score (higher = better). Set by
	// fuseRRF; lets callers see hybrid ranking, not just raw vector distance.
	Score     float64 `json:"score,omitempty"`
	CreatedAt string  `json:"created_at"`
}

// EntityHit is a related entity surfaced alongside memory hits.
type EntityHit struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Card         string `json:"card,omitempty"`
	MentionCount int    `json:"mention_count"`
	Promoted     bool   `json:"promoted"`
}

// RecallResult is the return value of Recall().
type RecallResult struct {
	Memories []MemoryHit `json:"memories"`
	Entities []EntityHit `json:"entities"` // entities mentioned by the recalled memories
}

// fusionPool is how many candidates to pull from EACH retrieval path before
// fusing. Deeper than the final k so a strong hit ranked just outside k in one
// signal can still be rescued by agreement in the other.
const fusionPool = 25

// rrfK is the Reciprocal Rank Fusion constant. 60 is the value from the
// original RRF paper (Cormack et al.) and the de-facto standard — it damps the
// contribution of low ranks without letting rank-0 dominate everything.
const rrfK = 60.0

// Recall does TRUE hybrid retrieval: it runs BOTH vector search (on the
// `summary` embedding) and full-text search, then fuses the two ranked lists
// with Reciprocal Rank Fusion. Both signals always run and compete on equal
// footing — a memory that's a strong keyword match but a weak vector match (or
// vice-versa) still surfaces, and agreement between the two is rewarded. Falls
// back gracefully to whichever path is available (FTS-only when no embedder).
// Finally surfaces the entities mentioned by the fused hits.
func (s *Store) Recall(ctx context.Context, q RecallQuery) (*RecallResult, error) {
	k := q.K
	if k <= 0 {
		k = 5
	}
	if q.Query == "" {
		return &RecallResult{}, nil
	}

	pool := k
	if pool < fusionPool {
		pool = fusionPool
	}

	// Vector path (best-effort: an embed/vector failure degrades to FTS-only,
	// loudly — never silently, which is how prod went embedder-less unnoticed).
	var vectorHits []MemoryHit
	if s.embedder != nil {
		if emb, err := s.embedder.Embed(ctx, q.Query); err != nil {
			rilllog.Logger().Warn("recall: query embed failed, degrading to FTS-only", "error", err)
		} else if vh, err := s.recallByVector(ctx, emb, pool, q); err != nil {
			rilllog.Logger().Warn("recall: vector search failed, degrading to FTS-only", "error", err)
		} else {
			vectorHits = vh
		}
	}

	// FTS path (always runs now — it's a co-equal signal, not just a gap-filler).
	ftsHits, err := s.recallByFTS(ctx, q.Query, pool, q)
	if err != nil {
		rilllog.Logger().Warn("recall: FTS search failed", "error", err)
	}

	hits := fuseRRF(vectorHits, ftsHits, k)
	result := &RecallResult{Memories: hits}

	// Pull entities mentioned by the hits.
	if len(hits) > 0 {
		ents, err := s.entitiesMentionedBy(ctx, hits)
		if err == nil {
			result.Entities = ents
		}
	}

	return result, nil
}

// fuseRRF combines the vector and FTS result lists with Reciprocal Rank Fusion:
// each hit accrues 1/(rrfK + rank) from every list it appears in (rank is
// 0-based position within that list), and hits are returned sorted by the
// summed score, truncated to k. A hit found by BOTH signals therefore outranks
// one found by a single signal at a comparable rank. Distance provenance is
// preserved: a hit that came from the vector list keeps its real cosine
// distance; an FTS-only hit keeps a nil distance (never a fabricated 0).
func fuseRRF(vector, fts []MemoryHit, k int) []MemoryHit {
	type acc struct {
		hit   MemoryHit
		score float64
	}
	merged := make(map[string]*acc)
	order := make([]string, 0, len(vector)+len(fts))

	add := func(h MemoryHit, rank int) {
		a, ok := merged[h.ID]
		if !ok {
			a = &acc{hit: h}
			merged[h.ID] = a
			order = append(order, h.ID)
		} else if a.hit.Distance == nil && h.Distance != nil {
			// A later list (FTS) carries no distance; keep the vector distance
			// from whichever list supplied it.
			a.hit.Distance = h.Distance
		}
		a.score += 1.0 / (rrfK + float64(rank))
	}

	for i, h := range vector {
		add(h, i)
	}
	for i, h := range fts {
		add(h, i)
	}

	out := make([]MemoryHit, 0, len(order))
	for _, id := range order {
		a := merged[id]
		a.hit.Score = a.score
		out = append(out, a.hit)
	}
	// Stable sort by fused score descending; ties keep insertion order
	// (vector-first), which is a sensible tiebreak.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })

	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
}

func (s *Store) recallByVector(ctx context.Context, emb []float64, k int, q RecallQuery) ([]MemoryHit, error) {
	embJSON, _ := json.Marshal(emb)
	filters := []string{"is_active = true"}
	if q.Kind != "" {
		filters = append(filters, fmt.Sprintf("kind = %s", EscapeStr(string(q.Kind))))
	}
	if q.Project != "" {
		filters = append(filters, fmt.Sprintf("project = %s", EscapeStr(q.Project)))
	}
	if q.Author != "" {
		filters = append(filters, fmt.Sprintf("author = %s", EscapeStr(q.Author)))
	}
	whereClause := strings.Join(filters, " AND ")
	stmt := fmt.Sprintf(`SELECT
		id, summary, details, kind, project, author, tags, valence, created_at,
		vector::distance::knn() AS distance
		FROM memory
		WHERE embedding <|%d,100|> %s AND %s
		ORDER BY distance ASC;`,
		k, string(embJSON), whereClause)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []MemoryHit
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) recallByFTS(ctx context.Context, query string, k int, q RecallQuery) ([]MemoryHit, error) {
	filters := []string{"is_active = true"}
	if q.Kind != "" {
		filters = append(filters, fmt.Sprintf("kind = %s", EscapeStr(string(q.Kind))))
	}
	if q.Project != "" {
		filters = append(filters, fmt.Sprintf("project = %s", EscapeStr(q.Project)))
	}
	if q.Author != "" {
		filters = append(filters, fmt.Sprintf("author = %s", EscapeStr(q.Author)))
	}
	whereClause := strings.Join(filters, " AND ")
	// Scored full-text search: the @1@/@2@ reference operators bind the BM25
	// indexes so search::score() yields a real relevance score, and we ORDER BY
	// it. The bare `@@` operator returns matches in storage order (unranked),
	// which RRF would then fuse as if it were a relevance ranking — garbage in.
	stmt := fmt.Sprintf(`SELECT
		id, summary, details, kind, project, author, tags, valence, created_at,
		(search::score(1) + search::score(2)) AS fts_score
		FROM memory
		WHERE (summary @1@ %s OR details @2@ %s) AND %s
		ORDER BY fts_score DESC
		LIMIT %d;`,
		EscapeStr(query), EscapeStr(query), whereClause, k)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []MemoryHit
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) entitiesMentionedBy(ctx context.Context, hits []MemoryHit) ([]EntityHit, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	// `in` on the mentions edge is a memory record link, so the IN list must be
	// record ids (memory:`...`), NOT JSON strings — a record-link vs string
	// comparison never matches, which is why this used to return nothing. Mirror
	// the GetMemory path: backtick-normalize each id and emit them unquoted.
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		rid := normalizeMemoryID(h.ID)
		if safeRecordID(rid) == nil {
			ids = append(ids, rid)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	stmt := fmt.Sprintf(`SELECT
		out.id AS id,
		out.name AS name,
		meta::tb(out) AS type,
		out.card AS card,
		out.mention_count AS mention_count,
		out.promoted AS promoted
		FROM mentions WHERE in IN [%s];`,
		strings.Join(ids, ", "))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []EntityHit
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	// Dedupe by id
	seen := map[string]bool{}
	out := rows[:0]
	for _, r := range rows {
		if r.ID == "" || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out, nil
}
