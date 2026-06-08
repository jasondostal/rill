package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	rilllog "github.com/jasondostal/rill/internal/log"
)

// ListMemoriesQuery is the input to ListMemories().
type ListMemoriesQuery struct {
	Kind    Kind      `json:"kind,omitempty"`    // optional filter
	Project string    `json:"project,omitempty"` // optional filter
	Author  string    `json:"author,omitempty"`  // optional filter
	Limit   int       `json:"limit,omitempty"`   // default 50
	Before  time.Time `json:"before,omitempty"`  // cursor — return memories created strictly before this
}

// MemoryRow is the projection used in the time-ordered browse list.
type MemoryRow struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"`
	Details   string    `json:"details,omitempty"`
	Kind      string    `json:"kind"`
	Tags      []string  `json:"tags,omitempty"`
	Author    string    `json:"author"`
	Project   string    `json:"project,omitempty"`
	Valence   string    `json:"valence,omitempty"`
	Pinned    bool      `json:"pinned"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryDetail is the full payload for the memory detail page.
type MemoryDetail struct {
	MemoryRow
	MentionedEntities []EntityRef `json:"mentioned_entities"`
}

// ListMemories returns memories matching the query, ordered by created_at
// DESC (newest first). Inactive (soft-deleted) memories are excluded by default.
func (s *Store) ListMemories(ctx context.Context, q ListMemoriesQuery) ([]MemoryRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}

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
	if !q.Before.IsZero() {
		filters = append(filters, fmt.Sprintf("created_at < %s", EscapeDatetime(q.Before)))
	}

	stmt := fmt.Sprintf(`SELECT
		id, summary, details, kind, tags, author, project, valence,
		pinned, is_active, created_at
		FROM memory
		WHERE %s
		ORDER BY created_at DESC
		LIMIT %d;`,
		strings.Join(filters, " AND "), limit)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []MemoryRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// EditMemoryPatch is a partial update of a memory's mutable fields.
// Only non-nil fields are applied. Author is required for audit.
type EditMemoryPatch struct {
	Summary *string  `json:"summary,omitempty"`
	Details *string  `json:"details,omitempty"`
	Tags    []string `json:"tags,omitempty"`    // nil = leave; empty slice = clear
	Valence *string  `json:"valence,omitempty"` // optional; "" clears
	Project *string  `json:"project,omitempty"` // optional; "" clears
	Pinned  *bool    `json:"pinned,omitempty"`  // ★ toggle; nil = leave
	Author  string   `json:"author"`
}

// EditMemory patches mutable fields on an existing memory. If Summary changes,
// the embedding is recomputed (so vector recall stays accurate). The memory's
// updated_at is bumped. Affected entities' derived_cards are re-rendered
// (because the projected content may have changed). Orient cache is invalidated.
//
// Immutable (deliberately): id, kind, author, created_at, valid_from. Use
// Forget + Remember if you need to change those.
func (s *Store) EditMemory(ctx context.Context, memID string, patch EditMemoryPatch) (*MemoryDetail, error) {
	if !strings.HasPrefix(memID, "memory:") {
		return nil, errs("memory id must start with 'memory:' (got %q)", memID)
	}
	memID = normalizeMemoryID(memID)
	if err := safeRecordID(memID); err != nil {
		return nil, err
	}
	if patch.Author == "" {
		return nil, errs("author is required for an EditMemory call")
	}

	before, err := s.GetMemory(ctx, memID)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, errs("memory %s not found", memID)
	}

	// Soft summary budget (same contract as Remember): if an edited summary runs
	// over, trim it to a clean boundary and spill the remainder into details
	// rather than storing a bloated, poorly-embedding summary. The spill target
	// is the explicitly-patched details if present, else the memory's existing
	// details. summaryPatch/detailsPatch carry the effective values.
	summaryPatch, detailsPatch := patch.Summary, patch.Details
	if summaryPatch != nil && utf8.RuneCountInString(*summaryPatch) > summaryTarget {
		base := before.Details
		if detailsPatch != nil {
			base = *detailsPatch
		}
		head, merged, _ := normalizeSummary(*summaryPatch, base)
		summaryPatch, detailsPatch = &head, &merged
		rilllog.Logger().Info("edit: summary over soft budget, trimmed + spilled into details", "memory", memID)
	}

	var sets []string
	if summaryPatch != nil {
		sets = append(sets, fmt.Sprintf("summary = %s", EscapeStr(*summaryPatch)))
	}
	if detailsPatch != nil {
		if *detailsPatch == "" {
			sets = append(sets, "details = NONE")
		} else {
			sets = append(sets, fmt.Sprintf("details = %s", EscapeStr(*detailsPatch)))
		}
	}
	if patch.Tags != nil {
		tagsJSON, _ := json.Marshal(patch.Tags)
		sets = append(sets, fmt.Sprintf("tags = %s", string(tagsJSON)))
	}
	if patch.Valence != nil {
		if *patch.Valence == "" {
			sets = append(sets, "valence = NONE")
		} else {
			sets = append(sets, fmt.Sprintf("valence = %s", EscapeStr(*patch.Valence)))
		}
	}
	if patch.Project != nil {
		if *patch.Project == "" {
			sets = append(sets, "project = NONE")
		} else {
			sets = append(sets, fmt.Sprintf("project = %s", EscapeStr(*patch.Project)))
		}
	}
	if patch.Pinned != nil {
		sets = append(sets, fmt.Sprintf("pinned = %t", *patch.Pinned))
	}

	// A pin toggle touches neither the projected content nor orient, so a
	// pinned-only edit can skip the derived-card recompute + orient invalidation.
	contentChanged := summaryPatch != nil || detailsPatch != nil ||
		patch.Tags != nil || patch.Valence != nil || patch.Project != nil

	if summaryPatch != nil {
		if emb := s.embedForWrite(ctx, *summaryPatch); emb != nil {
			b, _ := json.Marshal(emb)
			sets = append(sets, fmt.Sprintf("embedding = %s", string(b)))
		}
	}

	if len(sets) == 0 {
		return before, nil
	}

	sets = append(sets, fmt.Sprintf("updated_at = %s", EscapeDatetime(now())))
	stmt := fmt.Sprintf(`UPDATE %s SET %s;`, memID, strings.Join(sets, ", "))
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}

	if contentChanged {
		for _, e := range before.MentionedEntities {
			_ = s.recomputeDerivedCard(ctx, e.ID)
		}

		_ = s.markOrientStale(ctx, "global")
		if before.Project != "" {
			_ = s.markOrientStale(ctx, "project:"+before.Project)
		}
		if patch.Project != nil && *patch.Project != "" {
			_ = s.markOrientStale(ctx, "project:"+*patch.Project)
		}
	}

	return s.GetMemory(ctx, memID)
}

// normalizeMemoryID ensures the id suffix is backtick-wrapped so SurrealQL
// can parse it. Memory ids look like `memory:`20260523T033406.061014636Z``
// where the suffix starts with a digit and contains dots — SurrealDB
// requires backticks around such identifiers. URLs strip the backticks for
// cleanliness; this is where we put them back.
func normalizeMemoryID(memID string) string {
	const prefix = "memory:"
	if !strings.HasPrefix(memID, prefix) {
		return memID
	}
	suffix := memID[len(prefix):]
	if suffix == "" {
		return memID
	}
	// Already wrapped — leave alone.
	if strings.HasPrefix(suffix, "`") && strings.HasSuffix(suffix, "`") {
		return memID
	}
	// Bare suffix that starts with a letter and is all letters/digits/underscores
	// is a valid identifier and needs no wrapping. Anything else gets wrapped.
	needsWrap := false
	if !isIdentStart(suffix[0]) {
		needsWrap = true
	} else {
		for i := 1; i < len(suffix); i++ {
			if !isIdentChar(suffix[i]) {
				needsWrap = true
				break
			}
		}
	}
	if !needsWrap {
		return memID
	}
	return prefix + "`" + suffix + "`"
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentChar(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// GetMemory returns the full memory row plus the entities mentioned by it.
func (s *Store) GetMemory(ctx context.Context, memID string) (*MemoryDetail, error) {
	if !strings.HasPrefix(memID, "memory:") {
		return nil, errs("memory id must start with 'memory:' (got %q)", memID)
	}
	memID = normalizeMemoryID(memID)
	if err := safeRecordID(memID); err != nil {
		return nil, err
	}

	// Memory row.
	stmt := fmt.Sprintf(`SELECT
		id, summary, details, kind, tags, author, project, valence,
		pinned, is_active, created_at
		FROM %s;`, memID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []MemoryRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	detail := &MemoryDetail{MemoryRow: rows[0]}

	// Mentioned entities via the mentions edge.
	entStmt := fmt.Sprintf(`SELECT
		out.id AS id,
		out.name AS name,
		meta::tb(out) AS type
		FROM mentions WHERE in = %s;`, memID)
	entRes, err := s.db.SQL(ctx, entStmt, true)
	if err != nil {
		return nil, err
	}
	if len(entRes) > 0 && len(entRes[0].Result) > 0 {
		var ents []EntityRef
		if err := json.Unmarshal(entRes[0].Result, &ents); err != nil {
			return nil, err
		}
		detail.MentionedEntities = ents
	}

	return detail, nil
}
