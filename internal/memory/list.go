package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ListEntitiesQuery is the input to ListEntities().
type ListEntitiesQuery struct {
	Type     EntityType `json:"type,omitempty"`     // empty = union across all 7 tables
	Promoted *bool      `json:"promoted,omitempty"` // nil = no filter
	Sort     string     `json:"sort,omitempty"`     // "mention_count" (default), "recent", "name"
	Limit    int        `json:"limit,omitempty"`    // default 100
}

// EntityRow is a single row in the entity browser.
type EntityRow struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         string     `json:"type"`
	Aliases      []string   `json:"aliases,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Version      string     `json:"version,omitempty"` // current version (from version_is); populated by GetEntity
	HandNotes    string     `json:"hand_notes,omitempty"`
	DerivedCard  string     `json:"derived_card,omitempty"`
	MentionCount int        `json:"mention_count"`
	Promoted     bool       `json:"promoted"`
	FirstSeen    time.Time  `json:"first_seen"`
	LastSeen     time.Time  `json:"last_seen"`
	LastEditedBy string     `json:"last_edited_by,omitempty"`
	LastEditedAt *time.Time `json:"last_edited_at,omitempty"`
}

// EntityEdge is one relationship edge on an entity, with direction normalised.
type EntityEdge struct {
	ID         string     `json:"id"`
	Predicate  string     `json:"predicate"`
	Direction  string     `json:"direction"` // "out" (this entity is subject) | "in" (this entity is object)
	OtherID    string     `json:"other_id"`
	OtherName  string     `json:"other_name"`
	OtherType  string     `json:"other_type"`
	Valence    string     `json:"valence,omitempty"`
	RoleTitle  string     `json:"role_title,omitempty"`
	Weight     float64    `json:"weight"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
	Active     bool       `json:"active"`
}

// EntityMention is a memory that mentioned this entity.
type EntityMention struct {
	MemoryID  string    `json:"memory_id"`
	Summary   string    `json:"summary"`
	Kind      string    `json:"kind"`
	Author    string    `json:"author"`
	Project   string    `json:"project,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// EntityDetail is the full payload for the entity detail page.
type EntityDetail struct {
	EntityRow
	Mentions []EntityMention `json:"mentions"`
	Edges    []EntityEdge    `json:"edges"`
}

// ListEntities returns entities matching the query.
//
// When q.Type is empty, this runs one query per entity table and merges
// the results in memory. That's fine for browser UI sizes (limit ~100).
func (s *Store) ListEntities(ctx context.Context, q ListEntitiesQuery) ([]EntityRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	sortBy := orderClause(q.Sort)

	tables := []EntityType{q.Type}
	if q.Type == "" {
		tables = ValidEntityTypes
	}

	var out []EntityRow
	for _, t := range tables {
		rows, err := s.listOneTable(ctx, t, q.Promoted, sortBy, limit)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", t, err)
		}
		out = append(out, rows...)
	}

	if q.Type == "" {
		// Re-sort merged result and trim to limit.
		sortInPlace(out, q.Sort)
		if len(out) > limit {
			out = out[:limit]
		}
	}
	return out, nil
}

func (s *Store) listOneTable(ctx context.Context, t EntityType, promoted *bool, sortBy string, limit int) ([]EntityRow, error) {
	// merged_into IS NONE hides entities retired by a merge. (Safe even before
	// the field is defined: an undefined field reads as NONE, so all rows pass.)
	where := []string{"1 = 1", "merged_into IS NONE"}
	if promoted != nil {
		if *promoted {
			where = append(where, "promoted = true")
		} else {
			where = append(where, "promoted = false")
		}
	}
	stmt := fmt.Sprintf(`SELECT
		id, name, aliases, summary, hand_notes, derived_card,
		mention_count, promoted,
		first_seen, last_seen,
		last_edited_by, last_edited_at,
		meta::tb(id) AS type
		FROM %s WHERE %s ORDER BY %s LIMIT %d;`,
		t, strings.Join(where, " AND "), sortBy, limit)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []EntityRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// GetEntity returns the full detail for a single entity: its row, the memories
// that mentioned it, and all relationship edges (in + out directions merged).
func (s *Store) GetEntity(ctx context.Context, ref string, typ EntityType) (*EntityDetail, error) {
	recID, err := s.resolveEntityRef(ctx, ref, typ)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(recID, ":") {
		return nil, errs("invalid entity ref %q", ref)
	}

	row, err := s.fetchEntityRow(ctx, recID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}

	// Current version (bi-temporal version_is), if any.
	if v, _ := s.CurrentVersion(ctx, recID); v != "" {
		row.Version = v
	}

	mentions, err := s.fetchMentionsForEntity(ctx, recID)
	if err != nil {
		return nil, fmt.Errorf("mentions: %w", err)
	}

	edges, err := s.fetchEdgesForEntity(ctx, recID)
	if err != nil {
		return nil, fmt.Errorf("edges: %w", err)
	}

	return &EntityDetail{EntityRow: *row, Mentions: mentions, Edges: edges}, nil
}

func (s *Store) fetchEntityRow(ctx context.Context, recID string) (*EntityRow, error) {
	stmt := fmt.Sprintf(`SELECT
		id, name, aliases, summary, hand_notes, derived_card,
		mention_count, promoted,
		first_seen, last_seen,
		last_edited_by, last_edited_at,
		meta::tb(id) AS type
		FROM %s;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []EntityRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *Store) fetchMentionsForEntity(ctx context.Context, recID string) ([]EntityMention, error) {
	stmt := fmt.Sprintf(`SELECT
		in.id AS memory_id,
		in.summary AS summary,
		in.kind AS kind,
		in.author AS author,
		in.project AS project,
		in.created_at AS created_at
		FROM mentions WHERE out = %s AND in.is_active = true ORDER BY in.created_at DESC LIMIT 100;`,
		recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []EntityMention
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchEdgesForEntity collects all edges touching recID across the 6 dedicated
// relation tables + the generic assertion table. Direction is "out" when
// recID is the subject, "in" when it's the object.
func (s *Store) fetchEdgesForEntity(ctx context.Context, recID string) ([]EntityEdge, error) {
	tables := []string{"works_on", "uses", "prefers", "works_at", "depends_on", "part_of", "assertion"}
	var out []EntityEdge
	for _, tbl := range tables {
		// Outgoing
		outRows, err := s.fetchEdgeDirection(ctx, tbl, recID, "out")
		if err != nil {
			return nil, fmt.Errorf("%s out: %w", tbl, err)
		}
		out = append(out, outRows...)
		// Incoming
		inRows, err := s.fetchEdgeDirection(ctx, tbl, recID, "in")
		if err != nil {
			return nil, fmt.Errorf("%s in: %w", tbl, err)
		}
		out = append(out, inRows...)
	}
	return out, nil
}

func (s *Store) fetchEdgeDirection(ctx context.Context, table, recID, dir string) ([]EntityEdge, error) {
	// dir="out": recID is `in` (subject), other end is `out` (object).
	// dir="in":  recID is `out` (object),  other end is `in`  (subject).
	var filterCol, otherCol string
	if dir == "out" {
		filterCol = "in"
		otherCol = "out"
	} else {
		filterCol = "out"
		otherCol = "in"
	}
	// For the assertion table, predicate lives in a column. The 6 dedicated
	// tables have no predicate column; we synthesize from the table name.
	predExpr := fmt.Sprintf("%q", table)
	if table == "assertion" {
		predExpr = "predicate"
	}
	stmt := fmt.Sprintf(`SELECT
		id,
		%[3]s AS predicate,
		%[4]q AS direction,
		%[1]s.id AS other_id,
		%[1]s.name AS other_name,
		meta::tb(%[1]s) AS other_type,
		valence,
		role_title,
		weight,
		valid_from,
		valid_until
		FROM %[5]s WHERE %[2]s = %[6]s ORDER BY valid_from DESC LIMIT 50;`,
		otherCol, filterCol, predExpr, dir, table, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		// Some tables don't have role_title/valence/weight fields — that's a schema
		// reality, not an error. But the SELECT NONE-on-missing returns "" so we
		// shouldn't actually error here. If we do, bubble it up.
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []EntityEdge
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Active = rows[i].ValidUntil == nil
	}
	return rows, nil
}

// ============================================================
// helpers
// ============================================================

func orderClause(sort string) string {
	switch sort {
	case "recent":
		return "last_seen DESC"
	case "name":
		return "name ASC"
	case "mention_count", "":
		return "mention_count DESC, last_seen DESC"
	default:
		return "mention_count DESC, last_seen DESC"
	}
}

// sortInPlace mirrors orderClause for the in-memory merge across types.
func sortInPlace(rows []EntityRow, sort string) {
	switch sort {
	case "recent":
		// Insertion sort is fine at ~100 rows; avoiding a sort import for one call.
		for i := 1; i < len(rows); i++ {
			for j := i; j > 0 && rows[j].LastSeen.After(rows[j-1].LastSeen); j-- {
				rows[j], rows[j-1] = rows[j-1], rows[j]
			}
		}
	case "name":
		for i := 1; i < len(rows); i++ {
			for j := i; j > 0 && strings.ToLower(rows[j].Name) < strings.ToLower(rows[j-1].Name); j-- {
				rows[j], rows[j-1] = rows[j-1], rows[j]
			}
		}
	default:
		for i := 1; i < len(rows); i++ {
			for j := i; j > 0; j-- {
				a, b := rows[j], rows[j-1]
				if a.MentionCount > b.MentionCount ||
					(a.MentionCount == b.MentionCount && a.LastSeen.After(b.LastSeen)) {
					rows[j], rows[j-1] = b, a
					continue
				}
				break
			}
		}
	}
}
