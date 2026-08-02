// Package memoryv3 implements the intentional-memory model.
//
// See docs/exec-plans/active/intentional-memory/plan.md for the design.
//
// The core idea: memories are saved intentionally by an author (a human
// handle or an agent like claude). The caller declares entities and edges as
// part of the payload —
// no auto-extraction happens in the hot path. Cards (per-entity markdown
// blobs) are the canonical hand-curated view; orient renders them with
// bi-temporal filters and caches the output.
package memory

import (
	"errors"
	"strings"
	"time"
)

// Kind is the high-level type of a stored memory. Closed set.
type Kind string

const (
	KindDecision   Kind = "decision"
	KindPreference Kind = "preference"
	KindInsight    Kind = "insight"
	KindProcedure  Kind = "procedure"
	KindFact       Kind = "fact"
	KindIdentity   Kind = "identity"
	KindRule       Kind = "rule"
	KindIdea       Kind = "idea" // "look into / todo" — added for the macOS sidecar capture flow
)

// ValidKinds is the closed set of allowed Kind values.
var ValidKinds = []Kind{
	KindDecision, KindPreference, KindInsight,
	KindProcedure, KindFact, KindIdentity, KindRule, KindIdea,
}

// EntityType is the type of an entity record. Closed set; each value
// maps to a SurrealDB table of the same name.
type EntityType string

const (
	EntityPerson       EntityType = "person"
	EntityProject      EntityType = "project"
	EntityTool         EntityType = "tool"
	EntityOrganization EntityType = "organization"
	EntityPlace        EntityType = "place"
	EntityPreference   EntityType = "preference"
	EntityConcept      EntityType = "concept"
)

// ValidEntityTypes is the closed set of allowed EntityType values.
var ValidEntityTypes = []EntityType{
	EntityPerson, EntityProject, EntityTool, EntityOrganization,
	EntityPlace, EntityPreference, EntityConcept,
}

// Valence is the polarity of a preference. Closed set.
type Valence string

const (
	ValencePositive Valence = "positive"
	ValenceNegative Valence = "negative"
	ValenceNeutral  Valence = "neutral"
)

// ValidValences is the closed set of allowed Valence values.
var ValidValences = []Valence{
	ValencePositive, ValenceNegative, ValenceNeutral,
}

// ExclusivePredicates: predicates where only ONE active edge can exist
// from a given subject at a time. A new edge of an exclusive predicate
// with the same subject closes any prior active edge by setting
// valid_until = now and superseded_by = new_edge_id.
//
// Non-exclusive predicates (uses, depends_on, mentioned_with, etc.)
// are additive: multiple coexist.
var ExclusivePredicates = map[string]bool{
	"works_at":      true,
	"version_is":    true,
	"lives_at":      true,
	"role_at":       true,
	"married_to":    true,
	"employer_of":   true,
	"owns":          true,
	"current_focus": true,
	"status_is":     true,
}

// IsExclusive reports whether the given predicate is in the exclusive set.
func IsExclusive(predicate string) bool {
	return ExclusivePredicates[predicate]
}

// KnownPredicateTable maps caller-declared predicates to the SurrealDB
// relation table they live on. Predicates not listed fall back to the
// generic `assertion` table with the predicate stored in a field.
var KnownPredicateTable = map[string]string{
	"works_on":   "works_on",
	"uses":       "uses",
	"prefers":    "prefers",
	"works_at":   "works_at",
	"depends_on": "depends_on",
	"part_of":    "part_of",
}

// EdgeTableFor returns the table name for an edge with the given predicate.
// Returns ("assertion", true) if the predicate doesn't have a dedicated table.
func EdgeTableFor(predicate string) (table string, generic bool) {
	if t, ok := KnownPredicateTable[predicate]; ok {
		return t, false
	}
	return "assertion", true
}

// AllEdgeTables lists every relation table add_edge/remember can write an
// edge to: the six dedicated tables (KnownPredicateTable's values) plus the
// generic `assertion` fallback, which carries its predicate in a field
// rather than in the table name. This is the vocabulary a caller must walk
// to see every semantic edge regardless of predicate (merge, derived-card
// rendering, entity listing, stats, and the orient per-caller delta each
// iterate this same set).
var AllEdgeTables = []string{"works_on", "uses", "prefers", "works_at", "depends_on", "part_of", "assertion"}

// ============================================================
// Input payloads (caller-facing)
// ============================================================

// EntityDecl is a caller-declared entity in a remember() payload.
type EntityDecl struct {
	Name    string     `json:"name"`
	Type    EntityType `json:"type"`
	Aliases []string   `json:"aliases,omitempty"`
	Summary string     `json:"summary,omitempty"` // short auto-set blurb; not the card
	Version string     `json:"version,omitempty"` // optional: sets the entity's current version (bi-temporal, superseding)
	// ForceNew bypasses the lexical-variant soft-block (see dedup_lexical.go).
	// Set true ONLY when the caller has confirmed a name that looks like an
	// alternate form of an existing entity is in fact a genuinely distinct one.
	// It does NOT bypass exact-slug or exact-alias resolution — those always
	// fold into the existing entity.
	ForceNew bool `json:"force_new,omitempty"`
}

// EdgeDecl is a caller-declared relationship edge in a remember() payload.
type EdgeDecl struct {
	Subject     string     `json:"subject"`              // entity name
	SubjectType EntityType `json:"subject_type"`         // entity type
	Predicate   string     `json:"predicate"`            // works_on / uses / prefers / version_is / ...
	Object      string     `json:"object"`               // entity name
	ObjectType  EntityType `json:"object_type"`          // entity type
	Valence     Valence    `json:"valence,omitempty"`    // for prefers
	RoleTitle   string     `json:"role_title,omitempty"` // for works_at
	Weight      float64    `json:"weight,omitempty"`     // defaults to 1.0
}

// RememberPayload is the body of a remember() call.
//
// The caller declares every entity it intends to reference in entities[].
// Edges must reference only declared entities — no implicit creation.
// Cards are NOT writable here; the system maintains derived_card from the
// graph after every write. Human-edited hand_notes is updated via EditHandNotes.
type RememberPayload struct {
	Summary string   `json:"summary"`
	Details string   `json:"details,omitempty"`
	Kind    Kind     `json:"kind"`
	Tags    []string `json:"tags,omitempty"`
	Author  string   `json:"author"` // <human-handle> | claude | <named-agent>
	Project string   `json:"project,omitempty"`
	Valence Valence  `json:"valence,omitempty"` // only for kind=preference
	Open    bool     `json:"open,omitempty"`    // ★ mark this memory an open loop; opened_at = created_at

	Entities []EntityDecl `json:"entities,omitempty"`
	Edges    []EdgeDecl   `json:"edges,omitempty"`

	ValidFrom  *time.Time `json:"valid_from,omitempty"`  // nil = now
	ValidUntil *time.Time `json:"valid_until,omitempty"` // nil = forever
}

// ============================================================
// Output (write result)
// ============================================================

// ConsolidationHint surfaces a probable-duplicate scenario the caller may
// want to confirm. E.g. a new entity is vector-close to an existing one
// but the names don't match exactly.
type ConsolidationHint struct {
	Kind     string  `json:"kind"`     // "entity_vector_similar"
	Subject  string  `json:"subject"`  // record id of the new entity
	Existing string  `json:"existing"` // record id of the similar existing entity
	Distance float64 `json:"distance"` // cosine distance
	Note     string  `json:"note"`
}

// EntityRef returns the entity record id after upsert (e.g. "tool:pi").
type EntityRef struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    EntityType `json:"type"`
	Created bool       `json:"created"` // true if newly inserted, false if existing was updated
}

// EdgeRef is the result of an edge upsert.
type EdgeRef struct {
	ID         string `json:"id"`
	Predicate  string `json:"predicate"`
	Subject    string `json:"subject_id"`
	Object     string `json:"object_id"`
	Superseded string `json:"superseded_edge_id,omitempty"`
}

// RememberResult is the return value of Remember().
type RememberResult struct {
	MemoryID           string              `json:"memory_id"`
	Entities           []EntityRef         `json:"entities"`
	Edges              []EdgeRef           `json:"edges"`
	ConsolidationHints []ConsolidationHint `json:"consolidation_hints,omitempty"`
	OrientScopesStale  []string            `json:"orient_scopes_stale"`          // scopes whose orient cache got marked stale
	RecomputeWarnings  []string            `json:"recompute_warnings,omitempty"` // entities where derived_card recompute failed
	Notes              []string            `json:"notes,omitempty"`              // soft, non-fatal FYIs to the caller (e.g. summary auto-spilled into details)
}

// ============================================================
// Internal validation
// ============================================================

// ErrInvalidPayload is returned when a remember() payload fails validation.
var ErrInvalidPayload = errors.New("invalid payload")

// Validate checks a RememberPayload against the schema rules.
//
// Strict: every edge endpoint (subject + object) must be present in
// entities[]. The pipeline does not implicitly create entities referenced
// only by edges. Callers must declare them explicitly — this is the
// "intentional memory" contract.
func (p *RememberPayload) Validate() error {
	if p.Summary == "" {
		return errs("summary is required")
	}
	// No hard length cap: an over-budget summary is not rejected. Remember()
	// soft-trims it to ~summaryTarget at a clean boundary and spills the
	// remainder into details (see normalizeSummary), so callers — especially
	// smaller models — never get bounced for a slightly-long claim.
	if !validKind(p.Kind) {
		return errs("kind %q is not in the allowed set: %v", p.Kind, ValidKinds)
	}
	if p.Author == "" {
		return errs("author is required (human-handle | claude | named-agent)")
	}
	if p.Kind == KindPreference && p.Valence != "" && !validValence(p.Valence) {
		return errs("valence %q is not in the allowed set: %v", p.Valence, ValidValences)
	}

	// Build the declared-entities lookup keyed by (name-lower, type).
	declared := map[entKey]bool{}
	for i, e := range p.Entities {
		if e.Name == "" {
			return errs("entities[%d].name is required", i)
		}
		if !validEntityType(e.Type) {
			return errs("entities[%d].type %q is not in the allowed set: %v", i, e.Type, ValidEntityTypes)
		}
		declared[entKey{normalizeName(e.Name), e.Type}] = true
	}

	for i, edge := range p.Edges {
		if edge.Subject == "" || edge.Object == "" || edge.Predicate == "" {
			return errs("edges[%d] missing subject/object/predicate", i)
		}
		if !validEntityType(edge.SubjectType) || !validEntityType(edge.ObjectType) {
			return errs("edges[%d] entity types invalid", i)
		}
		if edge.Valence != "" && !validValence(edge.Valence) {
			return errs("edges[%d].valence %q invalid", i, edge.Valence)
		}
		if !declared[entKey{normalizeName(edge.Subject), edge.SubjectType}] {
			return errs("edges[%d] subject %s:%q is not declared in entities[] — caller must declare every endpoint", i, edge.SubjectType, edge.Subject)
		}
		if !declared[entKey{normalizeName(edge.Object), edge.ObjectType}] {
			return errs("edges[%d] object %s:%q is not declared in entities[] — caller must declare every endpoint", i, edge.ObjectType, edge.Object)
		}
	}
	return nil
}

// normalizeName lowercases + trims for entKey matching. Mirrors
// remember.go's entityIDByKey keying so Validate's "is it declared?"
// check uses the same equivalence the pipeline does.
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validKind(k Kind) bool {
	for _, v := range ValidKinds {
		if v == k {
			return true
		}
	}
	return false
}

func validEntityType(t EntityType) bool {
	for _, v := range ValidEntityTypes {
		if v == t {
			return true
		}
	}
	return false
}

func validValence(v Valence) bool {
	for _, ok := range ValidValences {
		if v == ok {
			return true
		}
	}
	return false
}
