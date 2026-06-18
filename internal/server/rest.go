package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/db"
	"github.com/jasondostal/rill/internal/document"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/jasondostal/rill/internal/memory"
	"github.com/jasondostal/rill/internal/settings"
	"github.com/jasondostal/rill/internal/util"
)

// restHandler owns the /api/* REST surface (excluding /api/auth/* and /api/mcp,
// which the server.go top-level handles).
type restHandler struct {
	authMgr  *auth.Manager
	db       *db.DB
	memStore *memory.Store
	docStore *document.Store
	mux      *http.ServeMux
}

func newRestHandler(authMgr *auth.Manager, database *db.DB, memStore *memory.Store, docStore *document.Store) http.Handler {
	h := &restHandler{
		authMgr:  authMgr,
		db:       database,
		memStore: memStore,
		docStore: docStore,
		mux:      http.NewServeMux(),
	}

	// Memory store surface.
	h.mux.HandleFunc("GET /api/ping", h.handlePing)
	h.mux.HandleFunc("POST /api/remember", h.handleRemember)
	h.mux.HandleFunc("GET /api/orient", h.handleOrient)
	h.mux.HandleFunc("POST /api/orient/regen", h.handleOrientRegen)
	h.mux.HandleFunc("GET /api/entities", h.handleListEntities)
	h.mux.HandleFunc("GET /api/entity/{type}/{slug}", h.handleGetEntity)
	h.mux.HandleFunc("POST /api/entity/{type}/{slug}/hand_notes", h.handleEditHandNotes)
	h.mux.HandleFunc("POST /api/entity/{type}/{slug}/promote", h.handlePromote)
	h.mux.HandleFunc("POST /api/entity/{type}/{slug}/demote", h.handleDemote)
	h.mux.HandleFunc("POST /api/entity/{type}/{slug}/merge", h.handleMergeEntity)
	h.mux.HandleFunc("POST /api/entity/{type}/{slug}/version", h.handleSetVersion)
	h.mux.HandleFunc("GET /api/stats", h.handleStats)
	h.mux.HandleFunc("GET /api/settings", h.handleGetSettings)
	h.mux.HandleFunc("PATCH /api/settings", h.handleUpdateSetting)
	h.mux.HandleFunc("GET /api/memories", h.handleListMemories)
	h.mux.HandleFunc("GET /api/memory/{id}", h.handleGetMemory)
	h.mux.HandleFunc("PATCH /api/memory/{id}", h.handleEditMemory)
	h.mux.HandleFunc("DELETE /api/memory/{id}", h.handleForget)
	h.mux.HandleFunc("POST /api/recall", h.handleRecall)
	h.mux.HandleFunc("POST /api/edge", h.handleAddEdge)
	h.mux.HandleFunc("POST /api/edge/{id}/close", h.handleCloseEdge)

	// Document surface (standalone markdown docs).
	if docStore != nil {
		h.mux.HandleFunc("GET /api/docs", h.handleListDocs)
		h.mux.HandleFunc("POST /api/docs", h.handlePutDoc)
		h.mux.HandleFunc("GET /api/docs/{id}", h.handleGetDoc)
		h.mux.HandleFunc("PATCH /api/docs/{id}", h.handlePutDoc)
		h.mux.HandleFunc("DELETE /api/docs/{id}", h.handleDeleteDoc)
		h.mux.HandleFunc("POST /api/docs/{id}/restore", h.handleRestoreDoc)
		h.mux.HandleFunc("GET /api/docs/{id}/export.md", h.handleExportDocMarkdown)
		h.mux.HandleFunc("POST /api/docs/{id}/entities", h.handleAssociateDoc)
		h.mux.HandleFunc("DELETE /api/docs/{id}/entities/{etype}/{eslug}", h.handleUnassociateDoc)
	}

	// Token CRUD (auth surface).
	h.mux.HandleFunc("/api/tokens", h.handleTokens)
	return h.mux
}

// requireScope enforces that the caller's identity holds `want`, writing a 403
// and returning false otherwise. Scope model: read = consult (queries);
// write = curate the graph, including reversible deletes (forget/merge);
// admin = operate the server itself (token CRUD, settings, hard doc delete).
// Mirrors the MCP per-tool scopes so a token means the same thing on every
// surface — closing the gap where the REST API authenticated but never checked
// scope, letting any token mutate (and mint an admin token).
func (h *restHandler) requireScope(w http.ResponseWriter, r *http.Request, want string) bool {
	if id := auth.IdentityFromContext(r.Context()); hasScope(id.Scopes, want) {
		return true
	}
	writeForbidden(w, "this operation requires the '"+want+"' scope")
	return false
}

// hasScope reports whether the identity carries the given scope.
func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// authorFromContext returns the authenticated user's name (or "rill-ui").
func authorFromContext(r *http.Request) string {
	id := auth.IdentityFromContext(r.Context())
	if id.Name != "" {
		return id.Name
	}
	rilllog.Logger().Debug("write without identity", "path", r.URL.Path)
	return "rill-ui"
}

func validEntityType(t memory.EntityType) bool {
	for _, v := range memory.ValidEntityTypes {
		if v == t {
			return true
		}
	}
	return false
}

func (h *restHandler) handlePing(w http.ResponseWriter, r *http.Request) {
	if err := h.memStore.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "memory_unavailable", err.Error(), "", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *restHandler) handleRemember(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	var p memory.RememberPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if p.Author == "" {
		p.Author = authorFromContext(r)
	}
	res, err := h.memStore.Remember(r.Context(), p)
	if err != nil {
		if errors.Is(err, memory.ErrInvalidPayload) {
			writeBadRequest(w, err.Error())
			return
		}
		writeStoreError(w, "remember", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *restHandler) handleOrient(w http.ResponseWriter, r *http.Request) {
	q := memory.OrientQuery{Project: r.URL.Query().Get("project")}
	if r.URL.Query().Get("force") == "1" {
		q.ForceRegen = true
	}
	res, err := h.memStore.Orient(r.Context(), q)
	if err != nil {
		writeStoreError(w, "orient", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *restHandler) handleOrientRegen(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	res, err := h.memStore.Orient(r.Context(), memory.OrientQuery{
		Project:    r.URL.Query().Get("project"),
		ForceRegen: true,
	})
	if err != nil {
		writeStoreError(w, "orient_regen", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleStats serves the dashboard aggregates. Optional ?range=7d|30d|90d|all
// bounds the growth + activity time series (default 90d).
func (h *restHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	days := 90
	switch r.URL.Query().Get("range") {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d", "":
		days = 90
	case "all":
		days = 0
	}
	res, err := h.memStore.Stats(r.Context(), memory.StatsQuery{Days: days})
	if err != nil {
		writeStoreError(w, "stats", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleGetSettings returns every registered setting resolved against
// env/DB/default. Admin-scoped; secret values are never included (only a
// "configured" flag).
func (h *restHandler) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if id := auth.IdentityFromContext(r.Context()); !hasScope(id.Scopes, "admin") {
		writeForbidden(w, "viewing settings requires admin scope")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings.Get().List()})
}

// handleUpdateSetting persists a DB override for one editable, non-env-pinned
// setting and hot-applies it. Admin-scoped.
func (h *restHandler) handleUpdateSetting(w http.ResponseWriter, r *http.Request) {
	if id := auth.IdentityFromContext(r.Context()); !hasScope(id.Scopes, "admin") {
		writeForbidden(w, "changing settings requires admin scope")
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if err := settings.Get().Set(r.Context(), req.Key, req.Value, authorFromContext(r)); err != nil {
		switch {
		case errors.Is(err, settings.ErrUnknownKey), errors.Is(err, settings.ErrNotEditable),
			errors.Is(err, settings.ErrEnvLocked), errors.Is(err, settings.ErrInvalid):
			writeBadRequest(w, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "settings_write_failed", err.Error(), "", nil)
		}
		return
	}
	// Return the updated resolved setting so the UI can reflect source/value.
	for _, s := range settings.Get().List() {
		if s.Key == req.Key {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *restHandler) handleListEntities(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := memory.ListEntitiesQuery{
		Type: memory.EntityType(qs.Get("type")),
		Sort: qs.Get("sort"),
	}
	if v := qs.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	switch qs.Get("promoted") {
	case "true":
		t := true
		q.Promoted = &t
	case "false":
		f := false
		q.Promoted = &f
	}
	rows, err := h.memStore.ListEntities(r.Context(), q)
	if err != nil {
		writeStoreError(w, "list_entities", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": rows})
}

func (h *restHandler) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if slug == "" {
		writeBadRequest(w, "entity slug is required")
		return
	}
	detail, err := h.memStore.GetEntity(r.Context(), slug, typ)
	if err != nil {
		writeStoreError(w, "get_entity", err, "")
		return
	}
	if detail == nil {
		writeNotFound(w, "entity not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type editNotesReq struct {
	Text   string `json:"text"`
	Mode   string `json:"mode"`
	Author string `json:"author,omitempty"`
}

func (h *restHandler) handleEditHandNotes(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if slug == "" {
		writeBadRequest(w, "entity slug is required")
		return
	}
	var req editNotesReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	mode := memory.NotesMode(strings.ToLower(req.Mode))
	if mode == "" {
		mode = memory.NotesAppend
	}
	if mode != memory.NotesAppend && mode != memory.NotesReplace {
		writeValidationError(w, "mode must be 'append' or 'replace'", map[string]any{"field": "mode"})
		return
	}
	author := req.Author
	if author == "" {
		author = authorFromContext(r)
	}
	if err := h.memStore.EditHandNotes(r.Context(), slug, typ, req.Text, mode, author); err != nil {
		writeStoreError(w, "edit_hand_notes", err, "")
		return
	}
	detail, err := h.memStore.GetEntity(r.Context(), slug, typ)
	if err != nil {
		writeStoreError(w, "edit_hand_notes_refetch", err, "")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *restHandler) handlePromote(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if err := h.memStore.Promote(r.Context(), slug, typ, authorFromContext(r)); err != nil {
		writeStoreError(w, "promote", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *restHandler) handleDemote(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if err := h.memStore.Demote(r.Context(), slug, typ, authorFromContext(r)); err != nil {
		writeStoreError(w, "demote", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type mergeEntityReq struct {
	Target         string `json:"target"` // surviving entity: bare name (same type) or full record id
	Author         string `json:"author,omitempty"`
	AllowCrossType bool   `json:"allow_cross_type,omitempty"` // target must be a full record id
}

// handleMergeEntity folds the path entity (source) into the body's target.
// Write-scoped: it's a graph-curation op and reversible (the source is
// archived via merged_into, not hard-deleted).
func (h *restHandler) handleMergeEntity(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if slug == "" {
		writeBadRequest(w, "entity slug is required")
		return
	}
	var req mergeEntityReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Target) == "" {
		writeValidationError(w, "target is required", map[string]any{"field": "target"})
		return
	}
	author := req.Author
	if author == "" {
		author = authorFromContext(r)
	}
	res, err := h.memStore.MergeEntity(r.Context(), slug, req.Target, typ, author, req.AllowCrossType)
	if err != nil {
		if errors.Is(err, memory.ErrInvalidPayload) {
			writeBadRequest(w, err.Error())
			return
		}
		writeStoreError(w, "merge_entity", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type setVersionReq struct {
	Version string `json:"version"`
	Author  string `json:"author,omitempty"`
}

// handleSetVersion sets the path entity's current version (bi-temporal).
func (h *restHandler) handleSetVersion(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	typ := memory.EntityType(r.PathValue("type"))
	slug := r.PathValue("slug")
	if !validEntityType(typ) {
		writeBadRequest(w, "invalid entity type")
		return
	}
	if slug == "" {
		writeBadRequest(w, "entity slug is required")
		return
	}
	var req setVersionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	author := req.Author
	if author == "" {
		author = authorFromContext(r)
	}
	if err := h.memStore.SetVersion(r.Context(), slug, typ, req.Version, author); err != nil {
		if errors.Is(err, memory.ErrInvalidPayload) {
			writeBadRequest(w, err.Error())
			return
		}
		writeStoreError(w, "set_version", err, "")
		return
	}
	detail, err := h.memStore.GetEntity(r.Context(), slug, typ)
	if err != nil {
		writeStoreError(w, "set_version_refetch", err, "")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *restHandler) handleListMemories(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := memory.ListMemoriesQuery{
		Kind:    memory.Kind(qs.Get("kind")),
		Project: qs.Get("project"),
		Author:  qs.Get("author"),
	}
	if v := qs.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if v := qs.Get("before"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			q.Before = t
		}
	}
	rows, err := h.memStore.ListMemories(r.Context(), q)
	if err != nil {
		writeStoreError(w, "list_memories", err, "")
		return
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	var nextCursor string
	if len(rows) == limit {
		nextCursor = rows[len(rows)-1].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories":    rows,
		"next_cursor": nextCursor,
	})
}

func (h *restHandler) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "memory id required")
		return
	}
	if !strings.HasPrefix(id, "memory:") {
		id = "memory:" + id
	}
	detail, err := h.memStore.GetMemory(r.Context(), id)
	if err != nil {
		writeStoreError(w, "get_memory", err, "")
		return
	}
	if detail == nil {
		writeNotFound(w, "memory not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type editMemoryReq struct {
	Summary *string  `json:"summary,omitempty"`
	Details *string  `json:"details,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Valence *string  `json:"valence,omitempty"`
	Project *string  `json:"project,omitempty"`
	Pinned  *bool    `json:"pinned,omitempty"`
	Author  string   `json:"author,omitempty"`
}

func (h *restHandler) handleEditMemory(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "memory id required")
		return
	}
	if !strings.HasPrefix(id, "memory:") {
		id = "memory:" + id
	}
	var req editMemoryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	author := req.Author
	if author == "" {
		author = authorFromContext(r)
	}
	patch := memory.EditMemoryPatch{
		Summary: req.Summary,
		Details: req.Details,
		Tags:    req.Tags,
		Valence: req.Valence,
		Project: req.Project,
		Pinned:  req.Pinned,
		Author:  author,
	}
	detail, err := h.memStore.EditMemory(r.Context(), id, patch)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeNotFound(w, err.Error())
			return
		}
		writeStoreError(w, "edit_memory", err, "")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *restHandler) handleForget(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "memory:") {
		id = "memory:" + id
	}
	if err := h.memStore.Forget(r.Context(), id); err != nil {
		writeStoreError(w, "forget", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type recallReq struct {
	Query   string      `json:"query"`
	Kind    memory.Kind `json:"kind,omitempty"`
	Project string      `json:"project,omitempty"`
	Author  string      `json:"author,omitempty"`
	K       int         `json:"k,omitempty"`
}

func (h *restHandler) handleRecall(w http.ResponseWriter, r *http.Request) {
	var req recallReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if req.Query == "" {
		writeBadRequest(w, "query is required")
		return
	}
	res, err := h.memStore.Recall(r.Context(), memory.RecallQuery{
		Query: req.Query, Kind: req.Kind, Project: req.Project, Author: req.Author, K: req.K,
	})
	if err != nil {
		writeStoreError(w, "recall", err, "")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *restHandler) handleAddEdge(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	var body struct {
		memory.EdgeDecl
		Author string `json:"author,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	author := body.Author
	if author == "" {
		author = authorFromContext(r)
	}
	ref, err := h.memStore.AddEdge(r.Context(), body.EdgeDecl, author)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "invalid") {
			writeBadRequest(w, err.Error())
			return
		}
		writeStoreError(w, "add_edge", err, "")
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

func (h *restHandler) handleCloseEdge(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "edge id required")
		return
	}
	if err := h.memStore.CloseEdge(r.Context(), id, authorFromContext(r)); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "invalid") {
			writeBadRequest(w, err.Error())
			return
		}
		writeStoreError(w, "close_edge", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ============================================================
// Documents (standalone markdown docs)
// ============================================================

func (h *restHandler) handleListDocs(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := document.ListQuery{
		Project: qs.Get("project"),
		DocType: qs.Get("doc_type"),
		Entity:  qs.Get("entity"),
	}
	if v := qs.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Limit = n
		}
	}
	if v := qs.Get("before"); v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			q.Before = t
		}
	}
	rows, err := h.docStore.List(r.Context(), q)
	if err != nil {
		writeStoreError(w, "list_docs", err, "")
		return
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	var nextCursor string
	if len(rows) == limit {
		nextCursor = rows[len(rows)-1].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if rows == nil {
		rows = []document.DocRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": rows, "next_cursor": nextCursor})
}

// handlePutDoc serves both POST /api/docs (create, id optional in body) and
// PATCH /api/docs/{id} (update, id from path wins).
func (h *restHandler) handlePutDoc(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	var in document.PutInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if id := r.PathValue("id"); id != "" {
		in.ID = id
	}
	if in.Author == "" {
		in.Author = authorFromContext(r)
	}
	doc, err := h.docStore.Put(r.Context(), in)
	if err != nil {
		writeStoreError(w, "put_doc", err, "")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *restHandler) handleGetDoc(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "document id required")
		return
	}
	doc, err := h.docStore.Get(r.Context(), id)
	if err != nil {
		writeStoreError(w, "get_doc", err, "")
		return
	}
	if doc == nil {
		writeNotFound(w, "document not found")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *restHandler) handleDeleteDoc(w http.ResponseWriter, r *http.Request) {
	if id := auth.IdentityFromContext(r.Context()); !hasScope(id.Scopes, "admin") {
		writeForbidden(w, "deleting a document requires admin scope")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "document id required")
		return
	}
	if err := h.docStore.Delete(r.Context(), id); err != nil {
		writeStoreError(w, "delete_doc", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *restHandler) handleRestoreDoc(w http.ResponseWriter, r *http.Request) {
	if id := auth.IdentityFromContext(r.Context()); !hasScope(id.Scopes, "admin") {
		writeForbidden(w, "restoring a document requires admin scope")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "document id required")
		return
	}
	doc, err := h.docStore.Restore(r.Context(), id)
	if err != nil {
		writeStoreError(w, "restore_doc", err, "")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *restHandler) handleAssociateDoc(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "document id required")
		return
	}
	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	doc, err := h.docStore.Associate(r.Context(), id, document.EntityAssoc{
		Name: req.Name, Type: memory.EntityType(req.Type),
	})
	if err != nil {
		writeStoreError(w, "associate_doc", err, "")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (h *restHandler) handleUnassociateDoc(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "write") {
		return
	}
	id := r.PathValue("id")
	etype := r.PathValue("etype")
	eslug := r.PathValue("eslug")
	if id == "" || etype == "" || eslug == "" {
		writeBadRequest(w, "document id, entity type and slug required")
		return
	}
	doc, err := h.docStore.Unassociate(r.Context(), id, etype+":"+eslug)
	if err != nil {
		writeStoreError(w, "unassociate_doc", err, "")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleExportDocMarkdown serves a document as a downloadable .md file
// (frontmatter + body). Auth comes from the session cookie / bearer like every
// other /api route, so a plain <a download> link works.
func (h *restHandler) handleExportDocMarkdown(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, "document id required")
		return
	}
	md, doc, err := h.docStore.ExportMarkdown(r.Context(), id)
	if err != nil {
		writeStoreError(w, "export_doc_md", err, "")
		return
	}
	fname := util.SanitizeFilename(doc.Title) + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	// #nosec G705 -- served as text/markdown with Content-Disposition: attachment;
	// browsers download rather than render, so embedded HTML/JS in the markdown
	// is not an XSS vector here. (UI rendering goes through a separate sanitizer.)
	_, _ = w.Write([]byte(md))
}

// ============================================================
// Tokens (auth PAT CRUD)
// ============================================================

func (h *restHandler) handleTokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listTokens(w, r)
	case http.MethodPost:
		h.createToken(w, r)
	case http.MethodDelete:
		h.revokeToken(w, r)
	default:
		writeMethodNotAllowed(w)
	}
}

func (h *restHandler) listTokens(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "admin") {
		return
	}
	tokens, err := h.authMgr.ListTokens(r.Context())
	if err != nil {
		writeStoreError(w, "list_tokens", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (h *restHandler) createToken(w http.ResponseWriter, r *http.Request) {
	// Token management is an operator/security surface — admin only. Without
	// this, any authenticated token could mint itself an admin token.
	if !h.requireScope(w, r, "admin") {
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		TTL    string   `json:"ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid request body")
		return
	}
	if req.Name == "" {
		writeBadRequest(w, "name is required")
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read", "write"}
	}
	// Only known scopes — reject arbitrary strings so a token can't carry a
	// bogus scope that some future check might misinterpret.
	for _, s := range req.Scopes {
		if s != "read" && s != "write" && s != "admin" {
			writeValidationError(w, "invalid scope: "+s, map[string]any{"field": "scopes", "allowed": []string{"read", "write", "admin"}})
			return
		}
	}
	var ttl time.Duration
	if req.TTL != "" {
		var err error
		ttl, err = time.ParseDuration(req.TTL)
		if err != nil {
			writeValidationError(w, "ttl must be a Go duration (e.g. 24h, 7d)", map[string]any{"field": "ttl"})
			return
		}
	}
	tok, err := h.authMgr.CreateToken(r.Context(), req.Name, req.Scopes, ttl)
	if err != nil {
		writeStoreError(w, "create_token", err, "")
		return
	}
	writeJSON(w, http.StatusOK, tok)
}

func (h *restHandler) revokeToken(w http.ResponseWriter, r *http.Request) {
	if !h.requireScope(w, r, "admin") {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeBadRequest(w, "id query param required")
		return
	}
	if err := h.authMgr.RevokeToken(r.Context(), id); err != nil {
		writeStoreError(w, "revoke_token", err, "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
