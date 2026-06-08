package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Remember writes a memory + caller-declared entities + edges
// in a single logical transaction. Consolidation happens on write:
//
//   - Same (subject, predicate, object) tuple → bump weight, append source,
//     no new edge row.
//   - Exclusive predicate, same subject, different object → close prior
//     active edge (valid_until = now, superseded_by = new), create new.
//   - Preference valence flip on same (person, preference) → close prior,
//     create new.
//   - Entity with same (name, type) → bump mention_count, refresh last_seen,
//     merge aliases. No new entity row.
//
// The caller cannot write to entity cards. After memory + mentions + edges
// land, the system recomputes derived_card for every touched entity (those
// mentioned plus both endpoints of every edge). Then orient_cache scopes
// are marked stale.
func (s *Store) Remember(ctx context.Context, p RememberPayload) (*RememberResult, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	// Soft summary budget: trim an over-long summary to a clean boundary and
	// spill the remainder into details rather than rejecting the write. Done
	// before the embed so we embed the focused claim, not the overflow.
	var spillNote string
	if origLen := utf8.RuneCountInString(p.Summary); origLen > summaryTarget {
		var spilled bool
		p.Summary, p.Details, spilled = normalizeSummary(p.Summary, p.Details)
		if spilled {
			spillNote = fmt.Sprintf("summary was %d chars (soft budget ~%d) — auto-trimmed to a sentence boundary and the remainder moved into details; nothing was lost. Tip: lead with one atomic claim and put elaboration in details.", origLen, summaryTarget)
		}
	}

	// Bi-temporal split:
	//   txTime    = transaction time (when we wrote it). Drives memory_id,
	//               created_at, updated_at, entity.last_edited_at. NEVER
	//               overridable — the system's clock is the source of truth
	//               for when a row was created.
	//   eventTime = event time (when the thing happened in the world).
	//               Drives memory.valid_from, edge.valid_from, entity.first_seen,
	//               entity.last_seen. Overridable via p.ValidFrom for
	//               backfill / porting historical memories from another system.
	txTime := now()
	eventTime := txTime
	if p.ValidFrom != nil {
		eventTime = p.ValidFrom.UTC()
	}

	// 1) Embed the summary (best-effort + time-bounded, OUTSIDE the transaction —
	// an embedding failure or a slow upstream must never kill or stall the write).
	embedding := s.embedForWrite(ctx, p.Summary)

	// 2) Plan every write. The reads (entity/edge consolidation lookups) run
	// here; the resulting statements are gathered in order and executed as ONE
	// transaction (step 3) so a failure anywhere rolls the whole thing back — no
	// partial memory/entity/edge writes. Derived-card + orient-cache updates stay
	// OUTSIDE the transaction (steps 4–5): they're non-fatal and re-runnable.
	memID := newMemoryID(txTime)
	memStmt, err := buildMemoryInsert(memID, p, txTime, eventTime, embedding)
	if err != nil {
		return nil, err
	}

	results := &RememberResult{MemoryID: memID}
	if spillNote != "" {
		results.Notes = append(results.Notes, spillNote)
	}
	writes := []string{memStmt} // ordered; memory first so RELATEs can reference it

	entityIDByKey := make(map[entKey]string)
	touched := map[string]bool{} // record id → true

	for _, e := range p.Entities {
		key := entKey{normalizeName(e.Name), e.Type}
		if _, seen := entityIDByKey[key]; seen {
			// Same entity declared twice in one call — plan it once. (Plan-time
			// reads can't see this call's own uncommitted CREATE.)
			continue
		}

		ref, hint, stmt, err := s.planUpsertEntity(ctx, e, p.Author, txTime, eventTime)
		if err != nil {
			return nil, fmt.Errorf("upsert entity %s:%s: %w", e.Type, e.Name, err)
		}
		writes = append(writes, stmt)
		results.Entities = append(results.Entities, *ref)
		entityIDByKey[key] = ref.ID
		touched[ref.ID] = true

		// Mentions edge: memory -> entity.
		writes = append(writes, mentionStmt(memID, ref.ID))

		// Optional inline version declaration — bi-temporal, superseding.
		if v := strings.TrimSpace(e.Version); v != "" {
			verStmts, err := s.planSetVersion(ctx, ref.ID, v, eventTime)
			if err != nil {
				return nil, fmt.Errorf("set version for %s: %w", ref.ID, err)
			}
			writes = append(writes, verStmts...)
		}

		if hint != nil {
			results.ConsolidationHints = append(results.ConsolidationHints, *hint)
		}
	}

	// Edges. Validate has guaranteed both endpoints are declared — no implicit
	// upserts. Dedupe by consolidation tuple so a doubly-declared edge in one
	// call doesn't create two active rows for the same tuple.
	seenEdge := map[string]bool{}
	for _, edge := range p.Edges {
		subjID, ok := entityIDByKey[entKey{normalizeName(edge.Subject), edge.SubjectType}]
		if !ok {
			// Defense in depth — Validate should have caught this.
			return nil, fmt.Errorf("edge subject %s:%q not declared (validate slipped?)", edge.SubjectType, edge.Subject)
		}
		objID, ok := entityIDByKey[entKey{normalizeName(edge.Object), edge.ObjectType}]
		if !ok {
			return nil, fmt.Errorf("edge object %s:%q not declared (validate slipped?)", edge.ObjectType, edge.Object)
		}

		table, _ := EdgeTableFor(edge.Predicate)
		dkey := table + "|" + subjID + "|" + objID
		if edge.Predicate == "prefers" {
			dkey += "|" + string(edge.Valence)
		}
		if seenEdge[dkey] {
			continue
		}
		seenEdge[dkey] = true

		ref, stmts, err := s.planEdge(ctx, edge, subjID, objID, memID, eventTime)
		if err != nil {
			return nil, fmt.Errorf("plan edge %s -[%s]-> %s: %w", subjID, edge.Predicate, objID, err)
		}
		writes = append(writes, stmts...)
		if ref != nil {
			results.Edges = append(results.Edges, *ref)
		}
		// Both endpoints are touched even if the edge consolidated to an existing row.
		touched[subjID] = true
		touched[objID] = true
	}

	// 3) Execute all writes as ONE transaction. A per-statement error cancels the
	// whole transaction (SurrealDB 3.x); SQL surfaces the real, classified cause.
	batch := "BEGIN TRANSACTION;\n" + strings.Join(writes, "\n") + "\nCOMMIT TRANSACTION;"
	if _, err := s.db.SQL(ctx, batch, true); err != nil {
		return nil, fmt.Errorf("remember write: %w", err)
	}

	// 4) Recompute derived_card for every touched entity. Sync — must complete
	// before we return, so the response reflects the post-write state.
	for recID := range touched {
		if err := s.recomputeDerivedCard(ctx, recID); err != nil {
			// Non-fatal — the memory + edges already landed. Surface the entity
			// in the result so callers know which card might be stale.
			results.RecomputeWarnings = append(results.RecomputeWarnings,
				fmt.Sprintf("%s: %s", recID, err.Error()))
			continue
		}
	}

	// 5) Mark orient caches stale.
	scopes := []string{"global"}
	if p.Project != "" {
		scopes = append(scopes, "project:"+p.Project)
	}
	for _, scope := range scopes {
		if err := s.markOrientStale(ctx, scope); err != nil {
			// Non-fatal; cache miss is recoverable.
			results.OrientScopesStale = append(results.OrientScopesStale, scope+"(error)")
			continue
		}
		results.OrientScopesStale = append(results.OrientScopesStale, scope)
	}

	return results, nil
}

// ============================================================
// Internal helpers
// ============================================================

var (
	// safeIDRe matches characters allowed in a SurrealDB record id segment.
	safeIDRe   = regexp.MustCompile(`[^a-zA-Z0-9_]+`)
	multiUnder = regexp.MustCompile(`_+`)
)

// safeID slugifies a string for use as the right-hand side of a record id.
func safeID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = safeIDRe.ReplaceAllString(s, "_")
	s = multiUnder.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "x"
	}
	return s
}

// newMemoryID builds a deterministic-ish memory record id from the timestamp.
func newMemoryID(t time.Time) string {
	return fmt.Sprintf("memory:`%s`", t.UTC().Format("20060102T150405.000000000Z"))
}

// entityRecID returns a record id like `tool:pi` for a given (type, name).
func entityRecID(t EntityType, name string) string {
	return fmt.Sprintf("%s:%s", t, safeID(name))
}

// buildMemoryInsert returns a SurrealQL CREATE statement for a memory row.
// txTime drives created_at/updated_at (when we wrote the row).
// eventTime drives valid_from (when the thing the memory describes was true).
func buildMemoryInsert(id string, p RememberPayload, txTime, eventTime time.Time, embedding []float64) (string, error) {
	embJSON := "NONE"
	if embedding != nil {
		b, err := json.Marshal(embedding)
		if err != nil {
			return "", err
		}
		embJSON = string(b)
	}
	tagsJSON, err := json.Marshal(orEmpty(p.Tags))
	if err != nil {
		return "", err
	}
	detailsExpr := "NONE"
	if p.Details != "" {
		detailsExpr = EscapeStr(p.Details)
	}
	projectExpr := "NONE"
	if p.Project != "" {
		projectExpr = EscapeStr(p.Project)
	}
	valenceExpr := "NONE"
	if p.Valence != "" {
		valenceExpr = EscapeStr(string(p.Valence))
	}
	validUntilExpr := "NONE"
	if p.ValidUntil != nil {
		validUntilExpr = EscapeDatetime(*p.ValidUntil)
	}

	return fmt.Sprintf(`CREATE %s SET
		summary    = %s,
		details    = %s,
		kind       = %s,
		tags       = %s,
		author     = %s,
		project    = %s,
		valence    = %s,
		valid_from = %s,
		valid_until = %s,
		is_active  = true,
		embedding  = %s,
		created_at = %s,
		updated_at = %s;`,
		id,
		EscapeStr(p.Summary),
		detailsExpr,
		EscapeStr(string(p.Kind)),
		string(tagsJSON),
		EscapeStr(p.Author),
		projectExpr,
		valenceExpr,
		EscapeDatetime(eventTime), // valid_from = event time
		validUntilExpr,
		embJSON,
		EscapeDatetime(txTime), // created_at = transaction time
		EscapeDatetime(txTime), // updated_at = transaction time
	), nil
}

// orEmpty returns the input slice or an empty []string if nil. Avoids `null`
// in JSON encoding.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// planUpsertEntity decides whether to create a new entity or bump an existing
// one (same type+name) and returns the single write statement — WITHOUT
// executing it. The reads (existence check, embedding, vector-dedup lookup)
// happen here. Returns an EntityRef and optionally a ConsolidationHint (when a
// different existing entity of the same type is vector-similar).
//
// txTime drives last_edited_at (write-time). eventTime drives first_seen +
// last_seen (when we encountered the entity in the world — overridable for
// backfill so historical entities get historical first_seen).
func (s *Store) planUpsertEntity(ctx context.Context, e EntityDecl, author string, txTime, eventTime time.Time) (*EntityRef, *ConsolidationHint, string, error) {
	rid := entityRecID(e.Type, e.Name)

	// 1) Does the exact (table, slug) already exist? → bump + merge aliases.
	existing, err := s.fetchEntity(ctx, rid)
	if err != nil {
		return nil, nil, "", err
	}
	if existing != nil {
		ref, stmt := planBumpEntity(existing, e, author, txTime, eventTime)
		return ref, nil, stmt, nil
	}

	// 2) No exact slug. Pull same-type peers once — used for both alias-exact
	// resolution and lexical variant detection. A read error here degrades to
	// "no peers": dedup is best-effort and must never block a legitimate write.
	peers, _ := s.fetchTypePeers(ctx, e.Type)

	// 2a) Alias-exact resolution: if the new name slugifies to an existing
	// entity's alias, that entity IS this thing — fold in, don't mint a twin.
	// This is identity, not a fuzzy guess, so force_new does not bypass it.
	newSlug := safeID(e.Name)
	for i := range peers {
		for _, a := range peers[i].Aliases {
			if safeID(a) != newSlug {
				continue
			}
			full, ferr := s.fetchEntity(ctx, peers[i].ID)
			if ferr != nil || full == nil {
				break // fall through to normal creation rather than fail the write
			}
			ref, stmt := planBumpEntity(full, e, author, txTime, eventTime)
			return ref, nil, stmt, nil
		}
	}

	// 2b) Lexical variant soft-block: a name that looks like an alternate form
	// of an existing same-type entity (shared distinctive token, not a
	// specialization sibling) is rejected so the caller reuses/merges instead
	// of duplicating. Overridable per-entity via force_new.
	if !e.ForceNew {
		if cand := lexicalDupCandidate(entityTokens(e.Name, e.Aliases), peers, lexDFMax); cand != nil {
			return nil, nil, "", errs(
				"new %s %q looks like an alternate form of existing %s (%q). "+
					"If it's the SAME thing, declare it using the existing name (or call merge_entity); "+
					"if it's genuinely DIFFERENT, set force_new:true on this entity to override.",
				e.Type, e.Name, cand.ID, cand.Name)
		}
	}

	// 3) New entity. Embed its name (+ summary if provided) for vector dedup.
	// Best-effort + time-bounded: a missing vector only weakens the dedup hint.
	text := e.Name
	if e.Summary != "" {
		text = e.Name + ". " + e.Summary
	}
	emb := s.embedForWrite(ctx, text)

	// 4) Look for vector-similar existing entities (same type) → consolidation hint.
	var hint *ConsolidationHint
	if emb != nil {
		near, err := s.nearestEntity(ctx, e.Type, emb, 0.20)
		if err == nil && near != nil && !strings.EqualFold(near.Name, e.Name) {
			hint = &ConsolidationHint{
				Kind:     "entity_vector_similar",
				Subject:  rid,
				Existing: near.ID,
				Distance: near.Distance,
				Note:     fmt.Sprintf("new %s %q is vector-close (cosine=%.3f) to existing %s %q — possible same entity", e.Type, e.Name, near.Distance, e.Type, near.Name),
			}
		}
	}

	aliasesJSON, _ := json.Marshal(orEmpty(append([]string{e.Name}, e.Aliases...)))
	summaryExpr := "NONE"
	if e.Summary != "" {
		summaryExpr = EscapeStr(e.Summary)
	}
	embExpr := "NONE"
	if emb != nil {
		b, _ := json.Marshal(emb)
		embExpr = string(b)
	}

	createStmt := fmt.Sprintf(`CREATE %s SET
		name = %s,
		aliases = %s,
		summary = %s,
		hand_notes = NONE,
		derived_card = NONE,
		promoted = false,
		first_seen = %s,
		last_seen = %s,
		mention_count = 1,
		last_edited_by = %s,
		last_edited_at = %s,
		embedding = %s;`,
		rid,
		EscapeStr(e.Name),
		string(aliasesJSON),
		summaryExpr,
		EscapeDatetime(eventTime), // first_seen = event time
		EscapeDatetime(eventTime), // last_seen = event time
		EscapeStr(author),
		EscapeDatetime(txTime), // last_edited_at = transaction time
		embExpr,
	)
	return &EntityRef{ID: rid, Name: e.Name, Type: e.Type, Created: true}, hint, createStmt, nil
}

// planBumpEntity builds the UPDATE that folds a re-declaration into an existing
// entity: bumps mention_count, refreshes last_seen, merges aliases (including
// the declared name), fills summary only if the entity had none. Used by the
// exact-slug path and the alias-exact resolution path in planUpsertEntity.
func planBumpEntity(existing *existingEntity, e EntityDecl, author string, txTime, eventTime time.Time) (*EntityRef, string) {
	aliases := mergeAliases(existing.Aliases, append([]string{e.Name}, e.Aliases...))
	aliasesJSON, _ := json.Marshal(aliases)
	summaryClause := ""
	if e.Summary != "" && existing.Summary == "" {
		summaryClause = fmt.Sprintf("summary = %s, ", EscapeStr(e.Summary))
	}
	stmt := fmt.Sprintf(`UPDATE %s SET
		mention_count += 1,
		last_seen = %s,
		aliases = %s,
		%s
		last_edited_by = %s,
		last_edited_at = %s;`,
		existing.ID,
		EscapeDatetime(eventTime), // last_seen = event time
		string(aliasesJSON),
		summaryClause,
		EscapeStr(author),
		EscapeDatetime(txTime), // last_edited_at = transaction time
	)
	return &EntityRef{ID: existing.ID, Name: existing.Name, Type: e.Type, Created: false}, stmt
}

// upsertEntity plans + immediately executes the entity write. Used by
// resolveOrInsert and other non-transactional callers; the Remember pipeline
// uses planUpsertEntity directly so the write joins its transaction.
func (s *Store) upsertEntity(ctx context.Context, e EntityDecl, author string, txTime, eventTime time.Time) (*EntityRef, *ConsolidationHint, error) {
	ref, hint, stmt, err := s.planUpsertEntity(ctx, e, author, txTime, eventTime)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.db.SQL(ctx, stmt, true); err != nil {
		return nil, hint, err
	}
	return ref, hint, nil
}

// entKey is the dedupe key for an entity within a single Remember() call.
type entKey struct {
	name string
	typ  EntityType
}

// resolveOrInsert returns the record id for an entity, upserting it lightly
// if not already present in the per-call entityIDByKey cache.
func (s *Store) resolveOrInsert(ctx context.Context, name string, t EntityType, author string, ts time.Time, cache map[entKey]string) (string, error) {
	key := entKey{strings.ToLower(strings.TrimSpace(name)), t}
	if id, ok := cache[key]; ok {
		return id, nil
	}
	ref, _, err := s.upsertEntity(ctx, EntityDecl{Name: name, Type: t}, author, ts, ts)
	if err != nil {
		return "", err
	}
	cache[key] = ref.ID
	return ref.ID, nil
}

func mergeAliases(existing, additions []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, a := range existing {
		k := strings.ToLower(strings.TrimSpace(a))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	for _, a := range additions {
		k := strings.ToLower(strings.TrimSpace(a))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	return out
}
