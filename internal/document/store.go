package document

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jasondostal/rill/internal/db"
	"github.com/jasondostal/rill/internal/memory"
)

// maxDocBytes is the per-document content ceiling. It sits safely under the
// SurrealDB Go driver's CBOR decoder limit (10,000,000 bytes per string), which
// otherwise fails the read-back AFTER a partial write. ~9MB is millions of
// characters — far beyond any realistic prose document. Override via
// RILL_MAX_DOC_BYTES if you raise the driver's decoder limit too.
func maxDocBytes() int {
	if v := os.Getenv("RILL_MAX_DOC_BYTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 9_000_000
}

// Store is the document CRUD entry point. It uses the WebSocket SurrealDB
// handle (the same one auth/REST use), NOT the memory package's HTTP /sql
// client. That matters: documents carry large bodies, and the HTTP /sql route
// has a request-size limit. The WebSocket driver sends content as a BOUND
// PARAMETER (CONTENT $data / SET content = $content), so a multi-megabyte doc
// never gets inlined into a SQL string. Documents need no embedder — they never
// enter the vector pipeline.
type Store struct {
	db *db.DB
}

// New builds a Store from a SurrealDB handle.
func New(database *db.DB) *Store { return &Store{db: database} }

// Ping verifies the DB is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.Ping(ctx) }

// Put creates a new document (ID empty) or updates an existing one (ID set).
// Updating a non-existent id is a not-found error — create has no id.
func (s *Store) Put(ctx context.Context, in PutInput) (*Document, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	if max := maxDocBytes(); len(in.Content) > max {
		return nil, errs("document content is %d bytes, over the %d-byte limit (RILL_MAX_DOC_BYTES) — split it or raise the limit", len(in.Content), max)
	}

	// Resolve every entity association to a validated record id up front, so a
	// bad reference fails before any write.
	entityIDs := make([]string, 0, len(in.Entities))
	for _, e := range in.Entities {
		rid, err := s.resolveEntity(ctx, e)
		if err != nil {
			return nil, err
		}
		entityIDs = append(entityIDs, rid)
	}

	if strings.TrimSpace(in.ID) != "" {
		docID, err := canonicalDocID(in.ID)
		if err != nil {
			return nil, err
		}
		existing, err := s.Get(ctx, docID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, notFound("document %s not found", in.ID)
		}
		if err := s.update(ctx, docID, in, entityIDs, existing); err != nil {
			return nil, fmt.Errorf("update document: %w", err)
		}
		return s.Get(ctx, docID)
	}

	docID, err := s.create(ctx, in, entityIDs)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, docID)
}

// create inserts a new document (auto-generated id) with content as a bound
// param, then associates entities.
func (s *Store) create(ctx context.Context, in PutInput, entityIDs []string) (string, error) {
	docType := strings.TrimSpace(in.DocType)
	if docType == "" {
		docType = DefaultDocType
	}
	data := map[string]any{
		"title":    in.Title,
		"content":  in.Content,
		"doc_type": docType,
	}
	if in.Project != "" {
		data["project"] = in.Project
	}
	if in.Source != "" {
		data["source"] = in.Source
	}
	if in.Author != "" {
		data["author"] = in.Author
	}
	// Preserve original timestamps on import/backfill. updated_at defaults to
	// created_at when only the latter is given, so backfilled rows aren't newer
	// than they were authored.
	if in.CreatedAt != nil {
		data["created_at"] = in.CreatedAt.UTC()
		if in.UpdatedAt != nil {
			data["updated_at"] = in.UpdatedAt.UTC()
		} else {
			data["updated_at"] = in.CreatedAt.UTC()
		}
	}
	rec, err := s.db.Create(ctx, "document", data)
	if err != nil {
		return "", fmt.Errorf("create document: %w", err)
	}
	docID := db.RecordID(rec)
	if docID == "" {
		return "", fmt.Errorf("create document: no id returned")
	}
	if len(entityIDs) > 0 {
		if err := s.relate(ctx, docID, entityIDs); err != nil {
			return "", err
		}
	}
	return docID, nil
}

// update merges the input over the existing record (empty field = keep
// existing, so a content-only edit can't wipe metadata) and reconciles entity
// associations (replaced only when a non-empty list is provided). Content rides
// as a bound param.
func (s *Store) update(ctx context.Context, docID string, in PutInput, entityIDs []string, existing *Document) error {
	content := in.Content
	if content == "" {
		content = existing.Content
	}
	docType := strings.TrimSpace(in.DocType)
	if docType == "" {
		docType = existing.DocType
	}
	project := in.Project
	if project == "" {
		project = existing.Project
	}
	source := in.Source
	if source == "" {
		source = existing.Source
	}
	author := in.Author
	if author == "" {
		author = existing.Author
	}
	vars := map[string]any{
		"title": in.Title, "content": content, "dt": docType,
		"project": project, "source": source, "author": author,
	}

	var b strings.Builder
	b.WriteString("BEGIN TRANSACTION;\n")
	fmt.Fprintf(&b, "UPDATE %s SET title=$title, content=$content, doc_type=$dt, project=$project, source=$source, author=$author, updated_at=time::now();\n", docID)
	if len(entityIDs) > 0 {
		fmt.Fprintf(&b, "DELETE doc_about WHERE in = %s;\n", docID)
		for _, rid := range entityIDs {
			fmt.Fprintf(&b, "RELATE %s->doc_about->%s SET created_at = time::now();\n", docID, rid)
		}
	}
	b.WriteString("COMMIT TRANSACTION;")
	_, err := s.db.Query(ctx, b.String(), vars)
	return err
}

// relate associates a document with entities in one transaction. All ids are
// pre-validated record references.
func (s *Store) relate(ctx context.Context, docID string, entityIDs []string) error {
	var b strings.Builder
	b.WriteString("BEGIN TRANSACTION;\n")
	for _, rid := range entityIDs {
		fmt.Fprintf(&b, "RELATE %s->doc_about->%s SET created_at = time::now();\n", docID, rid)
	}
	b.WriteString("COMMIT TRANSACTION;")
	if _, err := s.db.Query(ctx, b.String(), nil); err != nil {
		return fmt.Errorf("associate entities: %w", err)
	}
	return nil
}

// Get returns the full document plus associated entities, or (nil, nil) when
// the id doesn't exist or is soft-deleted.
func (s *Store) Get(ctx context.Context, id string) (*Document, error) {
	docID, err := canonicalDocID(id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryRecord(ctx,
		"SELECT title, content, doc_type, project, source, author, is_active, created_at, updated_at FROM %s WHERE is_active = true",
		docID, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	m := rows[0]
	doc := &Document{
		ID:        docID,
		Title:     db.StringField(m, "title"),
		Content:   db.StringField(m, "content"),
		DocType:   db.StringField(m, "doc_type"),
		Project:   db.StringField(m, "project"),
		Source:    db.StringField(m, "source"),
		Author:    db.StringField(m, "author"),
		IsActive:  boolField(m, "is_active"),
		CreatedAt: db.TimeField(m, "created_at"),
		UpdatedAt: db.TimeField(m, "updated_at"),
		Entities:  []EntityRef{},
	}

	erows, err := s.db.QueryRecord(ctx,
		"SELECT out.id AS id, out.name AS name, meta::tb(out) AS type FROM doc_about WHERE in = %s",
		docID, nil)
	if err != nil {
		return nil, err
	}
	for _, em := range erows {
		doc.Entities = append(doc.Entities, EntityRef{
			ID:   db.RecordID(em),
			Name: db.StringField(em, "name"),
			Type: db.StringField(em, "type"),
		})
	}
	return doc, nil
}

// List returns documents matching the query, newest first, content omitted.
func (s *Store) List(ctx context.Context, q ListQuery) ([]DocRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	where := []string{"is_active = true"}
	vars := map[string]any{}
	if q.Project != "" {
		where = append(where, "project = $project")
		vars["project"] = q.Project
	}
	if q.DocType != "" {
		where = append(where, "doc_type = $dt")
		vars["dt"] = q.DocType
	}
	if !q.Before.IsZero() {
		where = append(where, "created_at < $before")
		vars["before"] = q.Before
	}
	if q.Entity != "" {
		if err := validateEntityRecordID(q.Entity); err != nil {
			return nil, err
		}
		// Entity id is validated (allowlisted table + safe chars), safe to inline.
		where = append(where, fmt.Sprintf("id IN (SELECT VALUE in FROM doc_about WHERE out = %s)", q.Entity))
	}
	query := fmt.Sprintf("SELECT id, title, doc_type, project, source, created_at, updated_at FROM document WHERE %s ORDER BY created_at DESC LIMIT %d",
		strings.Join(where, " AND "), limit)
	rows, err := s.db.Query(ctx, query, vars)
	if err != nil {
		return nil, err
	}
	out := make([]DocRow, 0, len(rows))
	for _, m := range rows {
		out = append(out, docRowFrom(m))
	}
	return out, nil
}

// Delete soft-deletes a document (is_active = false). doc_about edges stay
// intact; List/Get filter the doc out.
func (s *Store) Delete(ctx context.Context, id string) error {
	docID, err := canonicalDocID(id)
	if err != nil {
		return err
	}
	existing, err := s.Get(ctx, docID)
	if err != nil {
		return err
	}
	if existing == nil {
		return notFound("document %s not found", id)
	}
	_, err = s.db.QueryRecord(ctx, "UPDATE %s SET is_active = false, updated_at = time::now()", docID, nil)
	return err
}

// Restore reverses a soft-delete (is_active = true), making the document visible
// to List/Get again. doc_about edges were never removed, so associations come
// back intact. Returns notFound if no row exists for the id at all (vs. Delete/
// Get, which can't see an inactive row). Idempotent — restoring an already-active
// doc is a no-op that returns the current doc.
func (s *Store) Restore(ctx context.Context, id string) (*Document, error) {
	docID, err := canonicalDocID(id)
	if err != nil {
		return nil, err
	}
	// Existence check that bypasses the is_active = true filter Get applies.
	rows, err := s.db.QueryRecord(ctx, "SELECT is_active FROM %s", docID, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, notFound("document %s not found", id)
	}
	if _, err := s.db.QueryRecord(ctx, "UPDATE %s SET is_active = true", docID, nil); err != nil {
		return nil, err
	}
	return s.Get(ctx, docID)
}

// Associate links a document to an existing entity. Idempotent — a repeat call
// doesn't create a duplicate edge. Returns the refreshed document.
func (s *Store) Associate(ctx context.Context, id string, e EntityAssoc) (*Document, error) {
	docID, err := canonicalDocID(id)
	if err != nil {
		return nil, err
	}
	existing, err := s.Get(ctx, docID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, notFound("document %s not found", id)
	}
	rid, err := s.resolveEntity(ctx, e)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("BEGIN TRANSACTION;\nDELETE doc_about WHERE in = %s AND out = %s;\nRELATE %s->doc_about->%s SET created_at = time::now();\nCOMMIT TRANSACTION;",
		docID, rid, docID, rid)
	if _, err := s.db.Query(ctx, q, nil); err != nil {
		return nil, fmt.Errorf("associate: %w", err)
	}
	return s.Get(ctx, docID)
}

// Unassociate removes a document↔entity link. No-op if the link doesn't exist.
func (s *Store) Unassociate(ctx context.Context, id, entityID string) (*Document, error) {
	docID, err := canonicalDocID(id)
	if err != nil {
		return nil, err
	}
	if err := validateEntityRecordID(entityID); err != nil {
		return nil, err
	}
	existing, err := s.Get(ctx, docID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, notFound("document %s not found", id)
	}
	if _, err := s.db.Query(ctx, fmt.Sprintf("DELETE doc_about WHERE in = %s AND out = %s;", docID, entityID), nil); err != nil {
		return nil, fmt.Errorf("unassociate: %w", err)
	}
	return s.Get(ctx, docID)
}

// resolveEntity turns an EntityAssoc into a validated, existing record id.
func (s *Store) resolveEntity(ctx context.Context, e EntityAssoc) (string, error) {
	name := strings.TrimSpace(e.Name)
	// Full record id form "type:slug".
	if strings.Contains(name, ":") {
		if err := validateEntityRecordID(name); err != nil {
			return "", err
		}
		rows, err := s.db.QueryRecord(ctx, "SELECT id FROM %s", name, nil)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", notFound("entity %q does not exist", name)
		}
		return name, nil
	}
	// Bare name + type: resolve within the typed table by name or alias. Table
	// is validated by PutInput.Validate (closed set), so it's safe to inline.
	table := string(e.Type)
	rows, err := s.db.Query(ctx,
		fmt.Sprintf("SELECT id FROM %s WHERE string::lowercase(name) = string::lowercase($n) OR $n IN aliases LIMIT 1", table),
		map[string]any{"n": name})
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", notFound("entity %s:%q does not exist — create it via remember() first", table, name)
	}
	rid := db.RecordID(rows[0])
	if rid == "" {
		return "", notFound("entity %s:%q does not exist", table, name)
	}
	return rid, nil
}

// canonicalDocID normalizes a document id (bare suffix or full "document:..."
// form) and validates its shape via db.RequireTable (rejects malformed/wrong-
// table ids, which the REST layer maps to 400).
func canonicalDocID(id string) (string, error) {
	full := id
	if !strings.HasPrefix(full, "document:") {
		full = "document:" + full
	}
	if err := db.RequireTable(full, "document"); err != nil {
		return "", err
	}
	return full, nil
}

// validateEntityRecordID confirms an id is a well-formed record id pointing at
// one of the entity tables.
func validateEntityRecordID(id string) error {
	table, _, ok := db.SplitRecordID(id)
	if !ok {
		return errs("invalid entity record id %q", id)
	}
	if !validEntityType(memory.EntityType(table)) {
		return errs("unknown entity type %q in %q", table, id)
	}
	return nil
}

func docRowFrom(m map[string]any) DocRow {
	return DocRow{
		ID:        db.RecordID(m),
		Title:     db.StringField(m, "title"),
		DocType:   db.StringField(m, "doc_type"),
		Project:   db.StringField(m, "project"),
		Source:    db.StringField(m, "source"),
		CreatedAt: db.TimeField(m, "created_at"),
		UpdatedAt: db.TimeField(m, "updated_at"),
	}
}

func boolField(m map[string]any, k string) bool {
	b, _ := m[k].(bool)
	return b
}
