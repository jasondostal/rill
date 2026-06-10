package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jasondostal/rill/internal/mcp"
	"github.com/jasondostal/rill/internal/memory"
)

// V3 MCP tools — surface the intentional-memory model directly to agents.
// They share a Store but each is a separate tool so the schema is precise.

// ============================================================
// remember
// ============================================================

type RememberTool struct {
	store *memory.Store
}

func NewRememberTool(s *memory.Store) *RememberTool { return &RememberTool{store: s} }

func (t *RememberTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "remember",
		Description:    "Store an intentional memory in rill. rill is a GRAPH of durable claims, not a worklog — its value is that memories enrich entity cards. So most memories should TOUCH THE GRAPH: declare the entities the claim is about and the edge it asserts. A fact/insight/decision/procedure with NO entities and NO edges is a smell — it's almost always a log / status / changelog / 'what I did this session' entry, which belongs in git, not rill; reconsider or rewrite it as an atomic claim about something. Conversely, CREATE new entities freely when something genuinely new appears (a tool, project, concept, person) — don't ossify the graph or cram a new thing into an existing node. (rule / identity / preference memories may legitimately stand alone.) The agent declares entities + edges directly — no auto-extraction. Summary = ONE atomic fact-bearing claim — aim for ~3-4 sentences (~90 words / 600 chars), not a paragraph of updates. Going over is FINE: the overflow auto-spills into `details` (you'll get a note back), never an error — so don't pad and don't sweat the exact length. Details are unbounded. Auto-consolidates: same (subject, predicate, object) bumps the existing edge; exclusive predicates (works_at, version_is, etc.) supersede prior; valence flips on `prefers` supersede prior. Specify author=claude (or your real model id) when calling autonomously.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"summary": map[string]any{"type": "string", "description": "ONE atomic fact-bearing claim — aim for ~3-4 sentences (~600 chars). Embedded for vector recall, so write the claim, not the narration. Overflow auto-spills into details (no error), so don't pad or truncate to hit a length."},
				"details": map[string]any{"type": "string", "description": "Optional narrative context. FTS-indexed but not embedded. Write durable claims: if you must record in-flight state (an open TODO, a 'NOT yet X', a remaining-steps plan), date-stamp it ('as of YYYY-MM-DD') and expect a later edit_memory to close it out when it resolves — undated open-items rot into false claims."},
				"kind":    map[string]any{"type": "string", "description": "decision | preference | insight | procedure | fact | identity | rule | idea"},
				"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"author":  map[string]any{"type": "string", "description": "<human-handle> | claude | <named-agent>"},
				"project": map[string]any{"type": "string", "description": "Optional scope (e.g. 'rill', 'homelab')."},
				"valence": map[string]any{"type": "string", "description": "positive | negative | neutral (only for kind=preference)"},
				"entities": map[string]any{
					"type":        "array",
					"description": "The entities this claim is about — declare ALL of them; this is how the memory enriches the graph. Each: {name, type, aliases?, summary?, force_new?}. Type ∈ person|project|tool|organization|place|preference|concept. Reuse existing entities, and CREATE new ones freely for genuinely new things. DEDUP: exact name AND known aliases fold into the existing node automatically (so check the graph and prefer an existing entity's canonical name). A name that looks like an alternate FORM of an existing same-type entity (e.g. 'Acme CU' when 'Acme Communities Credit Union' exists) is REJECTED with the candidate named — reuse that name, or merge_entity, or set force_new:true on this one entity if it's genuinely distinct. An exact name match under a DIFFERENT type is also rejected (the same thing recorded under two types is the most common real dupe) — reuse the existing node's type, or force_new:true for a genuine homonym. Specializations (e.g. 'Rill Sidecar' alongside 'rill') are allowed through.",
					"items":       map[string]any{"type": "object"},
				},
				"edges": map[string]any{
					"type":        "array",
					"description": "The relationships this claim asserts or updates — prefer declaring at least one when the claim is relational (X uses Y, X depends_on Y, X part_of Y, person works_at org). Each: {subject, subject_type, predicate, object, object_type, valence?, role_title?, weight?}. Both endpoints must also appear in entities[].",
					"items":       map[string]any{"type": "object"},
				},
			},
			"required": []string{"summary", "kind", "author"},
		},
	}
}

func (t *RememberTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var p memory.RememberPayload
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("%w: invalid remember params: %s", mcp.ErrUserFacing, err)
	}
	res, err := t.store.Remember(ctx, p)
	return res, asUserFacing(err)
}

// asUserFacing surfaces payload/validation errors (memory.ErrInvalidPayload) to
// the MCP caller with their message intact (wrapping mcp.ErrUserFacing). Other
// errors pass through untouched, to be sanitized to a generic message by the
// handler. Apply to any tool Call whose store method validates user input.
func asUserFacing(err error) error {
	if err != nil && errors.Is(err, memory.ErrInvalidPayload) {
		return fmt.Errorf("%w: %s", mcp.ErrUserFacing, err)
	}
	return err
}

// ============================================================
// recall
// ============================================================

type RecallTool struct {
	store *memory.Store
}

func NewRecallTool(s *memory.Store) *RecallTool { return &RecallTool{store: s} }

func (t *RecallTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "recall",
		Description:    "Hybrid recall over rill: vector search on memory summaries + FTS fallback on details. Returns matching memories and the entities they mention. Filterable by kind / project / author.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":   map[string]any{"type": "string"},
				"kind":    map[string]any{"type": "string"},
				"project": map[string]any{"type": "string"},
				"author":  map[string]any{"type": "string"},
				"k":       map[string]any{"type": "integer", "description": "Top-K (default 5)"},
			},
			"required": []string{"query"},
		},
	}
}

func (t *RecallTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var q memory.RecallQuery
	if err := json.Unmarshal(params, &q); err != nil {
		return nil, err
	}
	return t.store.Recall(ctx, q)
}

// ============================================================
// orient
// ============================================================

type OrientTool struct {
	store *memory.Store
}

func NewOrientTool(s *memory.Store) *OrientTool { return &OrientTool{store: s} }

func (t *OrientTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "orient",
		Description:    "Return the cached orient markdown blob for a scope. The blob assembles rules + identity cards + active project cards + active topics + active tools + active preferences (with valence) + active relationships (bi-temporally filtered) + recent intentional memories. Set force_regen=true to rebuild even if cache is fresh.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":     map[string]any{"type": "string", "description": "Optional scope (defaults to global)"},
				"force_regen": map[string]any{"type": "boolean"},
			},
		},
	}
}

func (t *OrientTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var q memory.OrientQuery
	if len(params) > 0 {
		if err := json.Unmarshal(params, &q); err != nil {
			return nil, err
		}
	}
	return t.store.Orient(ctx, q)
}

// ============================================================
// edit_notes
// ============================================================

type EditNotesTool struct {
	store *memory.Store
}

func NewEditNotesTool(s *memory.Store) *EditNotesTool { return &EditNotesTool{store: s} }

func (t *EditNotesTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "edit_notes",
		Description:    "Edit an entity's hand_notes — the free-form human-curated section that renders on the entity card alongside (not inside) the system-rendered derived_card. Use it for standing context about the entity that no single memory carries.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity": map[string]any{"type": "string", "description": "Full record id ('tool:pi') or just the name (then `type` is required)"},
				"type":   map[string]any{"type": "string", "description": "Entity type when entity is bare name"},
				"text":   map[string]any{"type": "string"},
				"mode":   map[string]any{"type": "string", "description": "append (default) | replace"},
				"author": map[string]any{"type": "string"},
			},
			"required": []string{"entity", "text", "author"},
		},
	}
}

func (t *EditNotesTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Entity string            `json:"entity"`
		Type   memory.EntityType `json:"type"`
		Text   string            `json:"text"`
		Mode   memory.NotesMode  `json:"mode"`
		Author string            `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	if args.Mode == "" {
		args.Mode = memory.NotesAppend
	}
	if err := t.store.EditHandNotes(ctx, args.Entity, args.Type, args.Text, args.Mode, args.Author); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// ============================================================
// add_edge / close_edge
// ============================================================

type AddEdgeTool struct{ store *memory.Store }

func NewAddEdgeTool(s *memory.Store) *AddEdgeTool { return &AddEdgeTool{store: s} }

func (t *AddEdgeTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "add_edge",
		Description:    "Add a single relationship edge between two existing entities (no memory written). Both endpoints must already exist — declare them via remember() first if needed. Recomputes both entities' derived cards.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"subject":      map[string]any{"type": "string"},
				"subject_type": map[string]any{"type": "string"},
				"predicate":    map[string]any{"type": "string"},
				"object":       map[string]any{"type": "string"},
				"object_type":  map[string]any{"type": "string"},
				"valence":      map[string]any{"type": "string"},
				"role_title":   map[string]any{"type": "string"},
				"weight":       map[string]any{"type": "number"},
				"author":       map[string]any{"type": "string"},
			},
			"required": []string{"subject", "subject_type", "predicate", "object", "object_type", "author"},
		},
	}
}

func (t *AddEdgeTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		memory.EdgeDecl
		Author string `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	return t.store.AddEdge(ctx, args.EdgeDecl, args.Author)
}

type CloseEdgeTool struct{ store *memory.Store }

func NewCloseEdgeTool(s *memory.Store) *CloseEdgeTool { return &CloseEdgeTool{store: s} }

func (t *CloseEdgeTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "close_edge",
		Description:    "Soft-close an active edge (sets valid_until = now). Provenance is preserved; the edge stays in the DB as inactive history. Use when a relationship has ENDED (left a job, stopped using a tool) — bi-temporal closure, not deletion. Recomputes the affected derived cards.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"edge_id": map[string]any{"type": "string", "description": "Full edge record id"},
				"author":  map[string]any{"type": "string"},
			},
			"required": []string{"edge_id", "author"},
		},
	}
}

func (t *CloseEdgeTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		EdgeID string `json:"edge_id"`
		Author string `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	if err := t.store.CloseEdge(ctx, args.EdgeID, args.Author); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// ============================================================
// promote / demote
// ============================================================

type PromoteTool struct {
	store *memory.Store
}

func NewPromoteTool(s *memory.Store) *PromoteTool { return &PromoteTool{store: s} }

func (t *PromoteTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "promote",
		Description:    "Mark an entity as promoted — it now appears in the default orient render. Agents may promote autonomously; promotion is reversible from CLI/UI.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity": map[string]any{"type": "string"},
				"type":   map[string]any{"type": "string"},
				"author": map[string]any{"type": "string"},
			},
			"required": []string{"entity", "author"},
		},
	}
}

func (t *PromoteTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Entity string            `json:"entity"`
		Type   memory.EntityType `json:"type"`
		Author string            `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	if err := t.store.Promote(ctx, args.Entity, args.Type, args.Author); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

type DemoteTool struct {
	store *memory.Store
}

func NewDemoteTool(s *memory.Store) *DemoteTool { return &DemoteTool{store: s} }

func (t *DemoteTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "demote",
		Description:    "Flip an entity back to unpromoted (it stops appearing in default orient render). Use this to counter autonomous agent promotions.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity": map[string]any{"type": "string"},
				"type":   map[string]any{"type": "string"},
				"author": map[string]any{"type": "string"},
			},
			"required": []string{"entity", "author"},
		},
	}
}

func (t *DemoteTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Entity string            `json:"entity"`
		Type   memory.EntityType `json:"type"`
		Author string            `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	if err := t.store.Demote(ctx, args.Entity, args.Type, args.Author); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// ============================================================
// list_entities / get_entity (browse)
// ============================================================

type ListEntitiesTool struct{ store *memory.Store }

func NewListEntitiesTool(s *memory.Store) *ListEntitiesTool { return &ListEntitiesTool{store: s} }

func (t *ListEntitiesTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "list_entities",
		Description:    "Browse entities in the graph. Returns up to `limit` entities (default 100) matching the filters. Each row includes name, type, mention_count, promoted flag, and the system-rendered derived_card (truncated). Use this to discover what is in the graph before writing a remember() — the cheapest way to see the current state. Sort: mention_count (default), recent, or name.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":     map[string]any{"type": "string", "description": "Filter by type. One of: person|project|tool|organization|place|preference|concept. Omit for all 7 types."},
				"promoted": map[string]any{"type": "boolean", "description": "Filter by promoted flag. true = only promoted (appear in orient), false = only unpromoted."},
				"sort":     map[string]any{"type": "string", "description": "mention_count | recent | name"},
				"limit":    map[string]any{"type": "integer", "description": "Max entities to return (default 100)"},
			},
		},
	}
}

func (t *ListEntitiesTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Type     memory.EntityType `json:"type"`
		Promoted *bool             `json:"promoted"`
		Sort     string            `json:"sort"`
		Limit    int               `json:"limit"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}
	}
	rows, err := t.store.ListEntities(ctx, memory.ListEntitiesQuery{
		Type:     args.Type,
		Promoted: args.Promoted,
		Sort:     args.Sort,
		Limit:    args.Limit,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"entities": rows}, nil
}

type GetEntityTool struct{ store *memory.Store }

func NewGetEntityTool(s *memory.Store) *GetEntityTool { return &GetEntityTool{store: s} }

func (t *GetEntityTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "get_entity",
		Description:    "Fetch one entity's full state: header fields, hand_notes (human-curated), derived_card, mentions, and edges. Use this to see what the graph already knows about an entity before writing a remember() about it.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity": map[string]any{"type": "string", "description": "Full record id ('person:alice') OR bare name (then `type` is required)"},
				"type":   map[string]any{"type": "string", "description": "Entity type when `entity` is a bare name"},
			},
			"required": []string{"entity"},
		},
	}
}

func (t *GetEntityTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Entity string            `json:"entity"`
		Type   memory.EntityType `json:"type"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	return t.store.GetEntity(ctx, args.Entity, args.Type)
}

// ============================================================
// list_memories / get_memory (browse)
// ============================================================

type ListMemoriesTool struct{ store *memory.Store }

func NewListMemoriesTool(s *memory.Store) *ListMemoriesTool { return &ListMemoriesTool{store: s} }

func (t *ListMemoriesTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "list_memories",
		Description:    "Time-ordered browse of memories (newest first). Filter by kind, project, or author. Returns the memory list — for full details + mentioned entities, follow up with get_memory. Use this to scan recent activity or to verify what was written; use recall for semantic search.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":    map[string]any{"type": "string", "description": "Filter by kind. One of: decision|preference|insight|procedure|fact|identity|rule|idea"},
				"project": map[string]any{"type": "string"},
				"author":  map[string]any{"type": "string", "description": "Filter by author (e.g. operator handle for humans, 'claude' for autonomous)"},
				"limit":   map[string]any{"type": "integer", "description": "Max memories (default 50)"},
				"before":  map[string]any{"type": "string", "description": "RFC3339 timestamp — return memories strictly before this. Use the previous page's last created_at for pagination."},
			},
		},
	}
}

func (t *ListMemoriesTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Kind    memory.Kind `json:"kind"`
		Project string      `json:"project"`
		Author  string      `json:"author"`
		Limit   int         `json:"limit"`
		Before  string      `json:"before"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}
	}
	q := memory.ListMemoriesQuery{
		Kind:    args.Kind,
		Project: args.Project,
		Author:  args.Author,
		Limit:   args.Limit,
	}
	if args.Before != "" {
		if t, err := time.Parse(time.RFC3339Nano, args.Before); err == nil {
			q.Before = t
		}
	}
	rows, err := t.store.ListMemories(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"memories": rows}, nil
}

type GetMemoryTool struct{ store *memory.Store }

func NewGetMemoryTool(s *memory.Store) *GetMemoryTool { return &GetMemoryTool{store: s} }

func (t *GetMemoryTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "get_memory",
		Description:    "Fetch one memory's full payload (summary, details, kind, tags, valence, author, project, created_at) plus the entities it mentioned. The memory_id is the full record id, e.g. memory:`20260522T203609.445974376Z`.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"memory_id": map[string]any{"type": "string"},
			},
			"required": []string{"memory_id"},
		},
	}
}

func (t *GetMemoryTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	return t.store.GetMemory(ctx, args.MemoryID)
}

// ============================================================
// edit_memory (in-place patch)
// ============================================================

type EditMemoryTool struct{ store *memory.Store }

func NewEditMemoryTool(s *memory.Store) *EditMemoryTool { return &EditMemoryTool{store: s} }

func (t *EditMemoryTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "edit_memory",
		Description:    "Patch mutable fields on an existing memory (summary, details, tags, valence, project, pinned). Re-embeds the summary if changed. Recomputes derived_card on every entity the memory mentions. IMMUTABLE: id, kind, author, created_at (use forget + remember to change those). Author required for audit. CURATION DUTY: when you READ a memory and can see a claim has gone stale (a 'NOT yet' that since happened, a TODO that's done, a plan that was executed), edit it closed right then — mark resolved items resolved, keep the durable lesson, and leave the memory better than you found it.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"memory_id": map[string]any{"type": "string", "description": "Full record id"},
				"summary":   map[string]any{"type": "string", "description": "New summary — ONE atomic claim (~3-4 sentences / ~600 chars; overflow auto-spills into details). Re-embeds."},
				"details":   map[string]any{"type": "string", "description": "New details. Empty string clears."},
				"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Replaces existing tags."},
				"valence":   map[string]any{"type": "string", "description": "positive|negative|neutral; empty clears."},
				"project":   map[string]any{"type": "string", "description": "New project scope; empty clears."},
				"pinned":    map[string]any{"type": "boolean", "description": "★ pin/unpin this memory."},
				"author":    map[string]any{"type": "string"},
			},
			"required": []string{"memory_id", "author"},
		},
	}
}

func (t *EditMemoryTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		MemoryID string   `json:"memory_id"`
		Summary  *string  `json:"summary,omitempty"`
		Details  *string  `json:"details,omitempty"`
		Tags     []string `json:"tags,omitempty"`
		Valence  *string  `json:"valence,omitempty"`
		Project  *string  `json:"project,omitempty"`
		Pinned   *bool    `json:"pinned,omitempty"`
		Author   string   `json:"author"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	return t.store.EditMemory(ctx, args.MemoryID, memory.EditMemoryPatch{
		Summary: args.Summary,
		Details: args.Details,
		Tags:    args.Tags,
		Valence: args.Valence,
		Project: args.Project,
		Pinned:  args.Pinned,
		Author:  args.Author,
	})
}

// ============================================================
// forget
// ============================================================

type ForgetTool struct {
	store *memory.Store
}

func NewForgetTool(s *memory.Store) *ForgetTool { return &ForgetTool{store: s} }

func (t *ForgetTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "forget",
		Description: "Soft-delete a memory (sets is_active = false). Provenance and any edges it sourced stay intact; recall and orient filter it out.",
		// Reversible graph-curation op (soft delete) — write scope, like remember/edit.
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"memory_id": map[string]any{"type": "string", "description": "Full record id like memory:`20260521T...`"},
			},
			"required": []string{"memory_id"},
		},
	}
}

func (t *ForgetTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	if err := t.store.Forget(ctx, args.MemoryID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok"}, nil
}

// ============================================================
// merge_entity
// ============================================================

type MergeEntityTool struct {
	store *memory.Store
}

func NewMergeEntityTool(s *memory.Store) *MergeEntityTool { return &MergeEntityTool{store: s} }

func (t *MergeEntityTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "merge_entity",
		Description: "Merge one duplicate entity into another. Re-points every edge + mention from `source` onto `target` (reusing dedup + exclusive-predicate consolidation), folds source's name/aliases/hand_notes into target, sums mention_count, and soft-retires source via merged_into (archived, reversible — not deleted). Use when the graph has a duplicate or a version-suffixed variant that should be a single node (e.g. 'Kimi K2.6' into 'Kimi'). Same-type by default; when the SAME thing was recorded under two different types (e.g. tool:fsevents vs concept:fsevents — the most common real dupe), set allow_cross_type:true and pass BOTH refs as full record ids. Reversible graph-curation op.",
		// Reversible (source archived via merged_into, not deleted) — write scope.
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Entity to retire — full record id (e.g. 'tool:kimi_k2_6') or bare name (then set `type`)."},
				"target": map[string]any{"type": "string", "description": "Surviving entity — full record id (e.g. 'tool:kimi') or bare name."},
				"type":   map[string]any{"type": "string", "description": "Entity type (person|project|tool|organization|place|preference|concept), required when source/target are bare names."},
				"author": map[string]any{"type": "string", "description": "<human-handle> | claude | <named-agent>"},
				"allow_cross_type": map[string]any{"type": "boolean", "description": "Permit merging across entity types — only when source and target are genuinely the SAME thing recorded under two types. Both refs must be full record ids. Default false."},
			},
			"required": []string{"source", "target", "author"},
		},
	}
}

func (t *MergeEntityTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		Source         string `json:"source"`
		Target         string `json:"target"`
		Type           string `json:"type"`
		Author         string `json:"author"`
		AllowCrossType bool   `json:"allow_cross_type"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, fmt.Errorf("%w: invalid merge_entity params: %s", mcp.ErrUserFacing, err)
	}
	if a.Author == "" {
		a.Author = "claude"
	}
	res, err := t.store.MergeEntity(ctx, a.Source, a.Target, memory.EntityType(a.Type), a.Author, a.AllowCrossType)
	return res, asUserFacing(err)
}

// ============================================================
// set_version
// ============================================================

type SetVersionTool struct {
	store *memory.Store
}

func NewSetVersionTool(s *memory.Store) *SetVersionTool { return &SetVersionTool{store: s} }

func (t *SetVersionTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "set_version",
		Description:    "Set an entity's current version (bi-temporal, superseding). The version is a STRING attribute — e.g. 'K2.6', '3.6', '2.3.0' — not a graph node. Setting a new version closes the prior one but keeps full history (queryable as-of a date). Use for model versions, software/dependency versions, app releases, etc. You can also set a version inline when declaring an entity in remember() via the entity's `version` field.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity":  map[string]any{"type": "string", "description": "Full record id (e.g. 'tool:kimi') or bare name (then set `type`)."},
				"type":    map[string]any{"type": "string", "description": "Entity type when `entity` is a bare name."},
				"version": map[string]any{"type": "string", "description": "The version label, e.g. 'K2.6', '3.6', '2.3.0'."},
				"author":  map[string]any{"type": "string", "description": "<human-handle> | claude | <named-agent>"},
			},
			"required": []string{"entity", "version", "author"},
		},
	}
}

func (t *SetVersionTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		Entity  string `json:"entity"`
		Type    string `json:"type"`
		Version string `json:"version"`
		Author  string `json:"author"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, fmt.Errorf("%w: invalid set_version params: %s", mcp.ErrUserFacing, err)
	}
	if a.Author == "" {
		a.Author = "claude"
	}
	if err := t.store.SetVersion(ctx, a.Entity, memory.EntityType(a.Type), a.Version, a.Author); err != nil {
		return nil, asUserFacing(err)
	}
	return map[string]any{"status": "ok", "entity": a.Entity, "version": a.Version}, nil
}

// ============================================================
// Bulk registration
// ============================================================

// RegisterMemoryTools wires all memory MCP tools into the registry. Call this from main.go
// after the existing tool registrations. Safe no-op if store is nil — but the
// caller is responsible for not constructing a nil store in production.
func RegisterMemoryTools(registry *mcp.Registry, store *memory.Store) {
	registry.Register(NewRememberTool(store))
	registry.Register(NewRecallTool(store))
	registry.Register(NewOrientTool(store))
	registry.Register(NewEditNotesTool(store))
	registry.Register(NewAddEdgeTool(store))
	registry.Register(NewCloseEdgeTool(store))
	registry.Register(NewListEntitiesTool(store))
	registry.Register(NewGetEntityTool(store))
	registry.Register(NewListMemoriesTool(store))
	registry.Register(NewEditMemoryTool(store))
	registry.Register(NewGetMemoryTool(store))
	registry.Register(NewPromoteTool(store))
	registry.Register(NewDemoteTool(store))
	registry.Register(NewForgetTool(store))
	registry.Register(NewMergeEntityTool(store))
	registry.Register(NewSetVersionTool(store))
}
