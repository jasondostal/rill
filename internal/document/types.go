// Package document implements standalone markdown document storage for rill.
//
// Documents are primers, reviews, writeups, references — human-facing prose
// that lives ALONGSIDE the memory graph but deliberately OUTSIDE it. A document
// is never embedded, never chunked, and never auto-surfaced in orient. It is
// fetched by id or by associated entity (doc_about). Documents are opaque
// blobs by design — kept out of the search pipeline so big prose can't bleed
// into vector-recall results.
//
// Storage backs onto the same SurrealDB as memory, reusing memory.Client.
package document

import (
	"fmt"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/memory"
)

// DefaultDocType is applied when a caller doesn't specify one.
const DefaultDocType = "writeup"

// EntityAssoc is a caller-declared association to an EXISTING entity. Name may
// be a bare entity name (resolved within Type's table by name or alias) or a
// full record id like "tool:pi" (Type then optional).
type EntityAssoc struct {
	Name string            `json:"name"`
	Type memory.EntityType `json:"type,omitempty"`
}

// PutInput is the create-or-update payload for a document.
//
//   - ID empty            → create with a fresh timestamp id.
//   - ID set, exists      → update in place (fields replaced, updated_at bumped).
//   - ID set, not present → create WITH that id (round-trip import after a
//     DB rebuild keeps the original id).
//
// Association semantics: on CREATE, the given entities (zero or more) become
// the doc's associations. On UPDATE, associations are REPLACED only when a
// non-empty entities list is provided; an empty/omitted list leaves existing
// associations untouched (so a content-only edit can't accidentally wipe them).
type PutInput struct {
	ID       string        `json:"id,omitempty"`
	Title    string        `json:"title"`
	Content  string        `json:"content"`
	DocType  string        `json:"doc_type,omitempty"`
	Project  string        `json:"project,omitempty"`
	Source   string        `json:"source,omitempty"`
	Entities []EntityAssoc `json:"entities,omitempty"`
	Author   string        `json:"author,omitempty"`

	// CreatedAt/UpdatedAt are honored only on CREATE — for import/backfill
	// (e.g. migrating docs from an external source) so the original authoring
	// date is preserved.
	// nil = use the DB clock (time::now()). Ignored on update (created_at is
	// immutable).
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// EntityRef is a thin reference to an associated entity (mirrors the shape
// memory uses for mentioned entities).
type EntityRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Document is the full record, including associated entities.
type Document struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Content   string      `json:"content"`
	DocType   string      `json:"doc_type"`
	Project   string      `json:"project,omitempty"`
	Source    string      `json:"source,omitempty"`
	Author    string      `json:"author,omitempty"`
	IsActive  bool        `json:"is_active"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Entities  []EntityRef `json:"entities"`
}

// DocRow is the light projection for list views — NO content, so listings stay
// cheap even when bodies are large.
type DocRow struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	DocType   string    `json:"doc_type"`
	Project   string    `json:"project,omitempty"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListQuery filters the document browse list.
type ListQuery struct {
	Project string    `json:"project,omitempty"`
	DocType string    `json:"doc_type,omitempty"`
	Entity  string    `json:"entity,omitempty"` // entity record id; returns docs associated with it
	Limit   int       `json:"limit,omitempty"`  // default 100
	Before  time.Time `json:"before,omitempty"` // cursor — created_at strictly before
}

// Validate checks a PutInput before any DB work.
func (p *PutInput) Validate() error {
	if strings.TrimSpace(p.Title) == "" {
		return errs("title is required")
	}
	for i, e := range p.Entities {
		if strings.TrimSpace(e.Name) == "" {
			return errs("entities[%d].name is required", i)
		}
		// A bare name (no table prefix) needs a valid type to resolve it.
		if !strings.Contains(e.Name, ":") && !validEntityType(e.Type) {
			return errs("entities[%d] (%q) needs a valid type to resolve: one of %v",
				i, e.Name, memory.ValidEntityTypes)
		}
	}
	return nil
}

func validEntityType(t memory.EntityType) bool {
	for _, v := range memory.ValidEntityTypes {
		if v == t {
			return true
		}
	}
	return false
}

// errs wraps memory.ErrInvalidPayload so the MCP/REST layers classify document
// validation failures exactly like memory ones (user-facing 400 / -32602).
func errs(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{memory.ErrInvalidPayload}, args...)...)
}

// notFound wraps memory.ErrNotFound for referenced-record-missing cases (404).
func notFound(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{memory.ErrNotFound}, args...)...)
}
