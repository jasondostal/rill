package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jasondostal/rill/internal/document"
	"github.com/jasondostal/rill/internal/mcp"
)

// Document MCP tools. Documents are large markdown blobs (primers, reviews,
// writeups, references). They are pull-only: an agent can fetch or write a doc
// on demand, but docs are NEVER auto-surfaced in orient. Content is unbounded
// (capped only by the server body limit, RILL_MAX_BODY_BYTES, default 10MB).

// ============================================================
// doc_put
// ============================================================

type DocPutTool struct{ store *document.Store }

func NewDocPutTool(s *document.Store) *DocPutTool { return &DocPutTool{store: s} }

func (t *DocPutTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "doc_put",
		Description:    "Create or update a markdown document (primer, review, writeup, reference). Content is unbounded — long docs are fine. Omit id to create; pass an existing id to update in place. Documents are pull-only: stored alongside the memory graph but NEVER auto-surfaced in orient. Associate with existing entities via entities[] ({name,type} or a record id). On update, entities[] REPLACES associations only when non-empty; an empty list leaves them untouched.",
		RequiredScopes: []string{"write"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Omit to create. Pass a document id to update."},
				"title":    map[string]any{"type": "string"},
				"content":  map[string]any{"type": "string", "description": "Markdown body. No length cap."},
				"doc_type": map[string]any{"type": "string", "description": "Free-form: primer | review | writeup | reference | ... (default 'writeup')."},
				"project":  map[string]any{"type": "string", "description": "Optional scope (e.g. 'rill'). Omit for global."},
				"source":   map[string]any{"type": "string", "description": "Optional origin (URL, 'import', ...)."},
				"entities": map[string]any{
					"type":        "array",
					"description": "Existing entities this doc is about. Each: {name, type} (type ∈ person|project|tool|organization|place|preference|concept) or a full record id like 'tool:pi'.",
					"items":       map[string]any{"type": "object"},
				},
				"author":     map[string]any{"type": "string", "description": "<human-handle> | claude | <named-agent>"},
				"created_at": map[string]any{"type": "string", "description": "RFC3339; CREATE only — preserves original authoring date on import/backfill. Omit for now()."},
				"updated_at": map[string]any{"type": "string", "description": "RFC3339; CREATE only. Defaults to created_at when omitted."},
			},
			"required": []string{"title"},
		},
	}
}

func (t *DocPutTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var in document.PutInput
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, fmt.Errorf("%w: invalid doc_put params: %s", mcp.ErrUserFacing, err)
	}
	if in.Author == "" {
		in.Author = "claude"
	}
	doc, err := t.store.Put(ctx, in)
	return doc, asUserFacing(err)
}

// ============================================================
// doc_get
// ============================================================

type DocGetTool struct{ store *document.Store }

func NewDocGetTool(s *document.Store) *DocGetTool { return &DocGetTool{store: s} }

func (t *DocGetTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "doc_get",
		Description:    "Fetch one document's full content + metadata + associated entities, by id. Returns the entire body (no truncation).",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Document record id (e.g. 'document:`20260526T...`')."},
			},
			"required": []string{"id"},
		},
	}
}

func (t *DocGetTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, fmt.Errorf("%w: invalid doc_get params: %s", mcp.ErrUserFacing, err)
	}
	doc, err := t.store.Get(ctx, a.ID)
	if err != nil {
		return nil, asUserFacing(err)
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: document %q not found", mcp.ErrUserFacing, a.ID)
	}
	return doc, nil
}

// ============================================================
// doc_list
// ============================================================

type DocListTool struct{ store *document.Store }

func NewDocListTool(s *document.Store) *DocListTool { return &DocListTool{store: s} }

func (t *DocListTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "doc_list",
		Description:    "List documents (metadata only — NO content, so listings stay cheap). Filter by project, doc_type, or entity (record id of an associated entity). Newest first. Use doc_get to fetch a body.",
		RequiredScopes: []string{"read"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":  map[string]any{"type": "string"},
				"doc_type": map[string]any{"type": "string"},
				"entity":   map[string]any{"type": "string", "description": "Entity record id (e.g. 'project:rill') — returns docs associated with it."},
				"limit":    map[string]any{"type": "integer", "description": "Max docs (default 100)."},
			},
		},
	}
}

func (t *DocListTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var q document.ListQuery
	if len(params) > 0 {
		if err := json.Unmarshal(params, &q); err != nil {
			return nil, fmt.Errorf("%w: invalid doc_list params: %s", mcp.ErrUserFacing, err)
		}
	}
	rows, err := t.store.List(ctx, q)
	if err != nil {
		return nil, asUserFacing(err)
	}
	if rows == nil {
		rows = []document.DocRow{}
	}
	return map[string]any{"documents": rows}, nil
}

// ============================================================
// doc_delete
// ============================================================

type DocDeleteTool struct{ store *document.Store }

func NewDocDeleteTool(s *document.Store) *DocDeleteTool { return &DocDeleteTool{store: s} }

func (t *DocDeleteTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "doc_delete",
		Description: "Soft-delete a document (is_active = false). It stops appearing in lists/reads; the row and its entity associations stay in the DB. Destructive — admin scope.",
		// Destructive mutation — gated to admin, like forget.
		RequiredScopes: []string{"admin"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Document record id."},
			},
			"required": []string{"id"},
		},
	}
}

func (t *DocDeleteTool) Call(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, fmt.Errorf("%w: invalid doc_delete params: %s", mcp.ErrUserFacing, err)
	}
	if err := t.store.Delete(ctx, a.ID); err != nil {
		return nil, asUserFacing(err)
	}
	return map[string]any{"status": "ok", "id": a.ID}, nil
}

// ============================================================
// Bulk registration
// ============================================================

// RegisterDocumentTools wires the document MCP tools into the registry.
func RegisterDocumentTools(registry *mcp.Registry, store *document.Store) {
	registry.Register(NewDocPutTool(store))
	registry.Register(NewDocGetTool(store))
	registry.Register(NewDocListTool(store))
	registry.Register(NewDocDeleteTool(store))
}
