package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/settings"
)

// orientRecencyDays is the window (in days) within which a project counts as
// "active" for orient. Projects whose entity last_edited_at is older than this
// drop out of the orient project view — their edges stay fully valid in the
// graph (valid_until is untouched); only visibility changes. Recency drives
// what surfaces; validity is preserved. Configurable via RILL_ORIENT_RECENCY_DAYS.
func orientRecencyDays() int {
	return settings.Get().OrientRecencyDays()
}

// orientRecencyCutoff is the timestamp before which a project is considered
// dormant for orient rendering.
func orientRecencyCutoff() time.Time {
	return now().AddDate(0, 0, -orientRecencyDays())
}

// orientMemChars caps the length (in characters) of each recent-memory line in
// orient. The full summary is always one recall()/get_memory away, so orient
// shows a headline to stay lean for small-context models. Rules and entity cards
// are deliberately NOT truncated — a rule's detail IS the rule. Configurable via
// RILL_ORIENT_MEM_CHARS; <= 0 disables truncation (full summaries).
func orientMemChars() int {
	return settings.Get().OrientMemChars()
}

// orientCardChars caps the prose-bearing card lines (Identity/Facts/Decisions)
// when an entity card renders into orient. The full card is always one
// get_entity away, so orient shows headlines to stay lean for small-context
// models. Configurable via RILL_ORIENT_CARD_CHARS; <= 0 disables truncation.
func orientCardChars() int {
	return settings.Get().OrientCardChars()
}

// isRecencyGatedPredicate reports whether an edge predicate represents
// "something worked on / operated" — these are recency-filtered in orient so
// dormant projects don't flood the view. Other predicates (works_at, uses,
// depends_on, family, etc.) always render.
func isRecencyGatedPredicate(pred string) bool {
	return pred == "works_on" || pred == "operates"
}

// OrientQuery is the input to Orient().
type OrientQuery struct {
	Project    string `json:"project,omitempty"` // optional scope; empty = global
	ForceRegen bool   `json:"force_regen,omitempty"`
}

// OrientResult holds the rendered orient blob plus metadata.
type OrientResult struct {
	Scope      string    `json:"scope"`
	Rendered   string    `json:"rendered"`
	FromCache  bool      `json:"from_cache"`
	RenderedAt time.Time `json:"rendered_at"`
}

// Orient returns the rendered orient blob for the given scope. Serves from
// cache when stale=false; regenerates when stale or force_regen. The
// per-caller "## Since last orient" delta is computed fresh on every call
// (never cached — see spliceDelta) and spliced into the body regardless of
// whether the rest of the render came from cache.
func (s *Store) Orient(ctx context.Context, q OrientQuery) (*OrientResult, error) {
	scope := "global"
	if q.Project != "" {
		scope = "project:" + q.Project
	}

	// Resolve caller and render the delta BEFORE we touch (overwrite) their
	// last_oriented_at watermark below — the delta describes everything
	// since the *prior* value. The new watermark must be the instant the
	// delta was computed, NOT the time the render finishes: anything written
	// while this call renders is after the delta snapshot, and stamping a
	// later now() would exclude those writes from every future delta.
	snapshot := now()
	caller := callerKeyFromContext(ctx)
	deltaSection := s.renderOrientDelta(ctx, caller, q.Project)

	if !q.ForceRegen {
		cached, err := s.fetchOrientCache(ctx, scope)
		if err != nil {
			return nil, err
		}
		if cached != nil && !cached.Stale && cached.Rendered != "" {
			s.touchOrientCaller(ctx, caller, snapshot)
			return &OrientResult{
				Scope:      scope,
				Rendered:   spliceDelta(cached.Rendered, deltaSection),
				FromCache:  true,
				RenderedAt: cached.RenderedAt,
			}, nil
		}
	}

	rendered, err := s.renderOrient(ctx, q.Project)
	if err != nil {
		return nil, err
	}
	ts := now()
	if err := s.storeOrientCache(ctx, scope, rendered, ts); err != nil {
		// non-fatal — we still return the rendered text
	}
	s.touchOrientCaller(ctx, caller, snapshot)
	return &OrientResult{
		Scope:      scope,
		Rendered:   spliceDelta(rendered, deltaSection),
		FromCache:  false,
		RenderedAt: ts,
	}, nil
}

// callerKeyFromContext derives the per-caller delta key from the request's
// auth identity: "<type>/<name>", falling back to "anonymous" when no
// identity was set on the context (unauthenticated/internal callers).
func callerKeyFromContext(ctx context.Context) string {
	id := auth.IdentityFromContext(ctx)
	if id.Type == "" && id.Name == "" {
		return "anonymous"
	}
	return id.Type + "/" + id.Name
}

// spliceDelta inserts the per-caller delta section right after orient's
// fixed header block ("# orient — <scope>\n_generated <ts>_\n\n") and before
// the first rendered section. The delta is per-caller and time-dependent, so
// it must never be part of the cached orient_cache blob (see renderOrient) —
// this is the only place it enters the response, computed fresh every call.
func spliceDelta(body, deltaSection string) string {
	if deltaSection == "" {
		return body
	}
	idx := strings.Index(body, "\n\n")
	if idx < 0 {
		return deltaSection + body
	}
	insertAt := idx + 2
	return body[:insertAt] + deltaSection + body[insertAt:]
}

// ============================================================
// Cache fetch / store
// ============================================================

type orientCacheRow struct {
	Scope      string    `json:"scope"`
	Rendered   string    `json:"rendered"`
	Stale      bool      `json:"stale"`
	RenderedAt time.Time `json:"rendered_at"`
}

func (s *Store) fetchOrientCache(ctx context.Context, scope string) (*orientCacheRow, error) {
	stmt := fmt.Sprintf(`SELECT scope, rendered, stale, rendered_at FROM orient_cache WHERE scope = %s LIMIT 1;`,
		EscapeStr(scope))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []orientCacheRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *Store) storeOrientCache(ctx context.Context, scope, rendered string, ts time.Time) error {
	stmt := fmt.Sprintf(`UPSERT orient_cache:`+"`%s`"+` SET
		scope = %s,
		rendered = %s,
		stale = false,
		rendered_at = %s;`,
		safeID(scope), EscapeStr(scope), EscapeStr(rendered), EscapeDatetime(ts))
	_, err := s.db.SQL(ctx, stmt, true)
	return err
}

// ============================================================
// Per-caller delta ("## Since last orient")
// ============================================================

type orientCallerRow struct {
	Caller         string    `json:"caller"`
	LastOrientedAt time.Time `json:"last_oriented_at"`
}

// fetchOrientCallerLastOriented returns the caller's prior last_oriented_at
// and whether this is their first-ever orient call (no orient_caller row
// yet). It does NOT write — Orient() reads this before calling
// touchOrientCaller so the delta is computed against the *prior* watermark.
func (s *Store) fetchOrientCallerLastOriented(ctx context.Context, caller string) (time.Time, bool, error) {
	stmt := fmt.Sprintf(`SELECT caller, last_oriented_at FROM orient_caller WHERE caller = %s LIMIT 1;`, EscapeStr(caller))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return time.Time{}, false, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return time.Time{}, true, nil
	}
	var rows []orientCallerRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return time.Time{}, false, err
	}
	if len(rows) == 0 {
		return time.Time{}, true, nil
	}
	return rows[0].LastOrientedAt, false, nil
}

// touchOrientCaller upserts the caller's last_oriented_at to ts — the instant
// the delta snapshot was taken, so writes landing mid-render stay inside the
// next delta window. Best-effort and fire-and-forget like markOrientStale — a
// failure here should never break orient's response.
func (s *Store) touchOrientCaller(ctx context.Context, caller string, ts time.Time) {
	stmt := fmt.Sprintf(`UPSERT orient_caller:`+"`%s`"+` SET caller = %s, last_oriented_at = %s;`,
		safeID(caller), EscapeStr(caller), EscapeDatetime(ts))
	_, _ = s.db.SQL(ctx, stmt, true)
}

// renderOrientDelta resolves the caller's prior watermark and renders the
// "## Since last orient" section. Soft-fails to "" (section omitted) on any
// error — the delta is a nice-to-have, never worth failing orient over.
func (s *Store) renderOrientDelta(ctx context.Context, caller, project string) string {
	since, firstTime, err := s.fetchOrientCallerLastOriented(ctx, caller)
	if err != nil {
		return ""
	}
	if firstTime {
		return "## Since last orient\n\n_first orient for this caller_\n\n"
	}
	delta, err := s.fetchOrientDelta(ctx, project, since)
	if err != nil || delta == nil {
		return ""
	}
	return renderDeltaSection(delta)
}

// orientDelta is everything that's changed since a caller's prior orient.
type orientDelta struct {
	DaysAgo            int
	NewMemoryTotal     int
	NewMemoryByProject []CountItem
	NewMemories        []recentMem
	TouchedEntities    []deltaEntityRow
	EdgesOpened        []deltaEdgeRow
	EdgesClosed        []deltaEdgeRow
	NewRuleCount       int
}

type deltaEntityRow struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type deltaEdgeRow struct {
	Predicate string `json:"predicate"`
	Subject   string `json:"subject"`
	Object    string `json:"object"`
}

// orientDeltaEdgeLimit caps how many opened/closed edges are fetched per
// predicate table, keeping the delta query set cheap even on a very active
// graph — the full picture is always a get_entity/recall away.
const orientDeltaEdgeLimit = 20

// fetchOrientDelta gathers everything that changed since `since`: new
// memories (count-by-project + up to 15 individual lines), entities touched,
// edges opened/closed across every predicate table (AllEdgeTables — the same
// vocabulary add_edge/remember write to), and a count of new rules. Every
// sub-query soft-fails independently (errors are swallowed, matching the
// other orient fetchers) so one slow/broken table doesn't blank the whole
// delta.
func (s *Store) fetchOrientDelta(ctx context.Context, project string, since time.Time) (*orientDelta, error) {
	d := &orientDelta{}
	d.DaysAgo = int(now().Sub(since).Hours() / 24)
	if d.DaysAgo < 0 {
		d.DaysAgo = 0
	}

	// (a) new memories — count by project (idx_memory_created makes the
	// range scan cheap) plus up to 15 lines in the same shape as the
	// recent-memories feed. Rules are counted separately below (d).
	memFilters := []string{"is_active = true", "kind != 'rule'", fmt.Sprintf("created_at > %s", EscapeDatetime(since))}
	if project != "" {
		memFilters = append(memFilters, fmt.Sprintf("(project = %s OR project IS NONE)", EscapeStr(project)))
	}
	memWhere := strings.Join(memFilters, " AND ")
	if counts, err := s.groupCount(ctx, "memory", "project", memWhere); err == nil {
		for id, c := range counts {
			d.NewMemoryByProject = append(d.NewMemoryByProject, CountItem{ID: id, Count: c})
			d.NewMemoryTotal += c
		}
		sort.Slice(d.NewMemoryByProject, func(i, j int) bool {
			a, b := d.NewMemoryByProject[i], d.NewMemoryByProject[j]
			if (a.ID == "") != (b.ID == "") {
				return b.ID == "" // named projects sort before the unscoped bucket
			}
			return a.ID < b.ID
		})
	}
	memStmt := fmt.Sprintf(`SELECT id, summary, kind, author, project, created_at FROM memory WHERE %s ORDER BY created_at DESC LIMIT 15;`, memWhere)
	if res, err := s.db.SQL(ctx, memStmt, true); err == nil && len(res) > 0 && len(res[0].Result) > 0 {
		var rows []recentMem
		if json.Unmarshal(res[0].Result, &rows) == nil {
			d.NewMemories = rows
		}
	}

	// (b) entities touched — last_edited_at > since, across every entity
	// table (idx_<table>_last_edited makes each scan cheap), names + types
	// only. Entities have no project field (see orient.go's project-scoping
	// notes elsewhere in this file), so this is always global.
	for _, t := range ValidEntityTypes {
		// last_edited_at must be in the SELECT list — SurrealDB rejects an
		// ORDER BY field that isn't projected ("missing order idiom").
		// deltaEntityRow doesn't declare it, so it's decoded and dropped.
		stmt := fmt.Sprintf(`SELECT name, meta::tb(id) AS type, last_edited_at FROM %s WHERE merged_into IS NONE AND last_edited_at > %s ORDER BY last_edited_at DESC LIMIT 25;`,
			t, EscapeDatetime(since))
		res, err := s.db.SQL(ctx, stmt, true)
		if err != nil || len(res) == 0 || len(res[0].Result) == 0 {
			continue
		}
		var rows []deltaEntityRow
		if json.Unmarshal(res[0].Result, &rows) == nil {
			d.TouchedEntities = append(d.TouchedEntities, rows...)
		}
	}

	// (c) edges opened/closed — across AllEdgeTables, the same predicate
	// vocabulary add_edge/remember dispatch writes to (dedicated tables +
	// the generic `assertion` fallback, which carries predicate as a
	// field). idx_<table>_valid_from / idx_<table>_valid_until keep each
	// range scan cheap.
	for _, tbl := range AllEdgeTables {
		predicateExpr := EscapeStr(tbl)
		if tbl == "assertion" {
			predicateExpr = "predicate"
		}
		// valid_from/valid_until must be in the SELECT list for the same
		// reason as (b) above — deltaEdgeRow doesn't declare them, so
		// they're decoded and dropped.
		openStmt := fmt.Sprintf(`SELECT %s AS predicate, in.name AS subject, out.name AS object, valid_from FROM %s WHERE valid_from > %s ORDER BY valid_from DESC LIMIT %d;`,
			predicateExpr, tbl, EscapeDatetime(since), orientDeltaEdgeLimit)
		if res, err := s.db.SQL(ctx, openStmt, true); err == nil && len(res) > 0 && len(res[0].Result) > 0 {
			var rows []deltaEdgeRow
			if json.Unmarshal(res[0].Result, &rows) == nil {
				d.EdgesOpened = append(d.EdgesOpened, rows...)
			}
		}
		closeStmt := fmt.Sprintf(`SELECT %s AS predicate, in.name AS subject, out.name AS object, valid_until FROM %s WHERE valid_until > %s ORDER BY valid_until DESC LIMIT %d;`,
			predicateExpr, tbl, EscapeDatetime(since), orientDeltaEdgeLimit)
		if res, err := s.db.SQL(ctx, closeStmt, true); err == nil && len(res) > 0 && len(res[0].Result) > 0 {
			var rows []deltaEdgeRow
			if json.Unmarshal(res[0].Result, &rows) == nil {
				d.EdgesClosed = append(d.EdgesClosed, rows...)
			}
		}
	}

	// (d) new rules count.
	ruleFilters := []string{"is_active = true", "kind = 'rule'", fmt.Sprintf("created_at > %s", EscapeDatetime(since))}
	if project != "" {
		ruleFilters = append(ruleFilters, fmt.Sprintf("(project = %s OR project IS NONE)", EscapeStr(project)))
	}
	if counts, err := s.countTables(ctx, []string{"memory"}, strings.Join(ruleFilters, " AND ")); err == nil {
		d.NewRuleCount = counts["memory"]
	}

	return d, nil
}

// renderDeltaSection renders the "## Since last orient" section body from a
// computed orientDelta. Deterministic string-builder render, same idiom as
// the rest of this file.
func renderDeltaSection(d *orientDelta) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Since last orient (%dd ago)\n\n", d.DaysAgo))

	// Zero-count blocks are omitted; an entirely quiet window collapses to a
	// single line rather than a column of zeros.
	if d.NewMemoryTotal == 0 && len(d.TouchedEntities) == 0 &&
		len(d.EdgesOpened) == 0 && len(d.EdgesClosed) == 0 && d.NewRuleCount == 0 {
		b.WriteString("_nothing new — graph unchanged since your last orient_\n\n")
		return b.String()
	}

	if d.NewMemoryTotal > 0 {
		b.WriteString(fmt.Sprintf("**New memories:** %d", d.NewMemoryTotal))
		if len(d.NewMemoryByProject) > 0 {
			parts := make([]string, 0, len(d.NewMemoryByProject))
			for _, c := range d.NewMemoryByProject {
				label := c.ID
				if label == "" {
					label = "(none)"
				}
				parts = append(parts, fmt.Sprintf("%s: %d", label, c.Count))
			}
			b.WriteString(fmt.Sprintf(" (%s)", strings.Join(parts, ", ")))
		}
		b.WriteString("\n")
		memMax := orientMemChars()
		for _, m := range d.NewMemories {
			b.WriteString(fmt.Sprintf("- _%s_ [%s, by %s] %s\n",
				m.CreatedAt[:10], m.Kind, m.Author, truncateForOrient(m.Summary, memMax)))
		}
		b.WriteString("\n")
	}

	if len(d.TouchedEntities) > 0 {
		b.WriteString(fmt.Sprintf("**Entities touched:** %d\n", len(d.TouchedEntities)))
		parts := make([]string, 0, len(d.TouchedEntities))
		for _, e := range d.TouchedEntities {
			parts = append(parts, fmt.Sprintf("%s (%s)", e.Name, e.Type))
		}
		b.WriteString("- " + strings.Join(parts, ", ") + "\n\n")
	}

	if len(d.EdgesOpened) > 0 {
		b.WriteString(fmt.Sprintf("**Edges opened:** %d\n", len(d.EdgesOpened)))
		for _, e := range d.EdgesOpened {
			b.WriteString(fmt.Sprintf("- %s: **%s** → **%s**\n", e.Predicate, e.Subject, e.Object))
		}
		b.WriteString("\n")
	}

	if len(d.EdgesClosed) > 0 {
		b.WriteString(fmt.Sprintf("**Edges closed:** %d\n", len(d.EdgesClosed)))
		for _, e := range d.EdgesClosed {
			b.WriteString(fmt.Sprintf("- %s: **%s** → **%s**\n", e.Predicate, e.Subject, e.Object))
		}
		b.WriteString("\n")
	}

	if d.NewRuleCount > 0 {
		b.WriteString(fmt.Sprintf("**New rules:** %d\n\n", d.NewRuleCount))
	}

	return b.String()
}

// ============================================================
// Render
// ============================================================

func (s *Store) renderOrient(ctx context.Context, project string) (string, error) {
	// Project-scoped orient assembles a focused subgraph instead of the global
	// render — see renderFocusOrient. The promoted-entity dumps (topics/tools/
	// orgs/places, and the all-projects roll-up) don't apply once a single
	// project is in view.
	if project != "" {
		return s.renderFocusOrient(ctx, project)
	}

	var b strings.Builder
	b.WriteString("# orient — global\n")
	b.WriteString(fmt.Sprintf("_generated %s_\n\n", now().Format(time.RFC3339)))

	// Rules
	rules, _ := s.fetchActiveRules(ctx, project)
	if len(rules) > 0 {
		b.WriteString("## Rules\n")
		for _, r := range rules {
			b.WriteString(fmt.Sprintf("- %s\n", oneLiner(r.Content)))
		}
		b.WriteString("\n")
	}

	// Identity — promoted persons, owner-first when RILL_OWNER_ENTITY is set
	persons, _ := s.fetchPromoted(ctx, "person")
	if len(persons) > 0 {
		b.WriteString("## Identity\n")
		sort.SliceStable(persons, func(i, j int) bool {
			return persons[i].selfishness() > persons[j].selfishness()
		})
		for _, p := range persons {
			renderEntitySection(&b, p)
		}
		b.WriteString("\n")
	}

	// Active projects
	projects, _ := s.fetchPromoted(ctx, "project")
	if len(projects) > 0 {
		b.WriteString("## Active projects\n")
		// If we have a project scope, that project floats to top
		sort.SliceStable(projects, func(i, j int) bool {
			if project != "" {
				if projects[i].Name == project {
					return true
				}
				if projects[j].Name == project {
					return false
				}
			}
			return projects[i].LastSeen.After(projects[j].LastSeen)
		})
		for _, p := range projects {
			renderEntitySection(&b, p)
		}
		b.WriteString("\n")
	}

	// Active topics (concepts)
	concepts, _ := s.fetchPromoted(ctx, "concept")
	if len(concepts) > 0 {
		b.WriteString("## Active topics\n")
		for _, c := range concepts {
			renderEntitySection(&b, c)
		}
		b.WriteString("\n")
	}

	// Active tools
	tools, _ := s.fetchPromoted(ctx, "tool")
	if len(tools) > 0 {
		b.WriteString("## Active tools\n")
		for _, t := range tools {
			renderEntitySection(&b, t)
		}
		b.WriteString("\n")
	}

	// Active organizations
	orgs, _ := s.fetchPromoted(ctx, "organization")
	if len(orgs) > 0 {
		b.WriteString("## Active organizations\n")
		for _, o := range orgs {
			renderEntitySection(&b, o)
		}
		b.WriteString("\n")
	}

	// Active places
	places, _ := s.fetchPromoted(ctx, "place")
	if len(places) > 0 {
		b.WriteString("## Active places\n")
		for _, p := range places {
			renderEntitySection(&b, p)
		}
		b.WriteString("\n")
	}

	// The owner's own prefers/relationship edges already render inside their
	// Identity card (Preferences + Active edges sections), so the global
	// preference/relationship rollups below exclude owner-subject edges to avoid
	// restating them. Non-owner edges (e.g. a spouse's job, a project's deps)
	// still surface — those are additive, not duplicates.
	owner := settings.Get().OwnerEntity()

	// Active preferences (active prefers edges)
	prefs, _ := s.fetchActivePreferences(ctx, owner)
	if len(prefs) > 0 {
		b.WriteString("## Active preferences\n")
		for _, p := range prefs {
			b.WriteString(fmt.Sprintf("- **%s** %s _(%s)_\n", p.Subject, p.Object, p.Valence))
		}
		b.WriteString("\n")
	}

	// Active relationships (works_at, works_on, uses, depends_on — active edges)
	rels, _ := s.fetchActiveRelationships(ctx, owner)
	if len(rels) > 0 {
		b.WriteString("## Active relationships\n")
		for _, r := range rels {
			extra := ""
			if r.Role != "" {
				extra = fmt.Sprintf(" (%s)", r.Role)
			}
			b.WriteString(fmt.Sprintf("- **%s** %s **%s**%s\n", r.Subject, r.Predicate, r.Object, extra))
		}
		b.WriteString("\n")
	}

	// Recent intentional memories (last 14 days)
	recent, _ := s.fetchRecentMemories(ctx, 14, project)
	if len(recent) > 0 {
		b.WriteString("## Recent intentional memories (last 14 days)\n")
		memMax := orientMemChars()
		for _, m := range recent {
			b.WriteString(fmt.Sprintf("- _%s_ [%s, by %s] %s\n",
				m.CreatedAt[:10], m.Kind, m.Author, truncateForOrient(m.Summary, memMax)))
		}
		b.WriteString("\n")
	}

	// Open loops — every is_active memory flagged open, regardless of age.
	loops, _ := s.fetchOpenLoops(ctx, project)
	if len(loops) > 0 {
		b.WriteString("## Open loops\n")
		memMax := orientMemChars()
		for _, l := range loops {
			prefix := ""
			if l.Project != "" {
				prefix = fmt.Sprintf("[%s] ", l.Project)
			}
			opened := l.OpenedAt
			if len(opened) < 10 {
				opened = l.CreatedAt // defensive fallback — opened_at should always be set alongside open=true
			}
			openedDate := opened
			if len(opened) >= 10 {
				openedDate = opened[:10]
			}
			b.WriteString(fmt.Sprintf("- %s%s _(opened %s)_\n",
				prefix, truncateForOrient(l.Summary, memMax), openedDate))
		}
		b.WriteString("\n")
	}

	// Map — the FINAL section, always. The index of everything reachable but
	// not rendered above (dormant projects, documents, entity counts). Global
	// orient only; Focus mode gets a one-line pointer back to this instead.
	b.WriteString(s.renderOrientMap(ctx))

	return b.String(), nil
}

// ============================================================
// Focus (project-scoped orient)
// ============================================================

// renderFocusOrient assembles the focused subgraph for orient(project=X):
// Rules (project-scoped + global) + owner Identity card + "## Focus: X" (the
// project entity's full card, its 1-hop edges, project-scoped docs/open
// loops/recent memories) + a pointer back to the global map. The per-caller
// delta is spliced in by Orient() afterward, same as the global render — this
// function only needs to keep the same "# orient — ...\n_generated ..._\n\n"
// header shape for spliceDelta's insertion point to land correctly.
//
// Promoted-entity dumps (Active topics/tools/organizations/places, and the
// all-projects roll-up) are deliberately NOT rendered here — Focus mode
// replaces them with the single project's full subgraph.
func (s *Store) renderFocusOrient(ctx context.Context, project string) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# orient — project:%s\n", project))
	b.WriteString(fmt.Sprintf("_generated %s_\n\n", now().Format(time.RFC3339)))

	// Rules — project-scoped + global, same fetcher as the global render.
	rules, _ := s.fetchActiveRules(ctx, project)
	if len(rules) > 0 {
		b.WriteString("## Rules\n")
		for _, r := range rules {
			b.WriteString(fmt.Sprintf("- %s\n", oneLiner(r.Content)))
		}
		b.WriteString("\n")
	}

	// Owner Identity card — just the owner (if configured), not every
	// promoted person; the point of Focus is to stay narrow.
	if owner := settings.Get().OwnerEntity(); owner != "" {
		if row, _ := s.fetchEntityByID(ctx, owner); row != nil {
			b.WriteString("## Identity\n")
			renderEntitySection(&b, *row)
		}
	}

	focus, _ := s.renderFocusCard(ctx, project)
	b.WriteString(focus)

	b.WriteString("_global map: orient() without project_\n")

	return b.String(), nil
}

// renderFocusCard builds the "## Focus: X" section: the project entity's full
// card (hand_notes + derived_card, UNTRUNCATED — unlike the compact renders
// used elsewhere in orient, Focus mode is meant to be the deep dive), all its
// 1-hop edges in/out with neighbor name/type + a one-line neighbor summary,
// project-scoped document titles, project-scoped open loops, and
// project-scoped recent memories. Soft-fails to a stub section (never an
// error) if the project entity doesn't exist yet — a caller can still orient
// on a project name that's about to be created.
func (s *Store) renderFocusCard(ctx context.Context, project string) (string, error) {
	detail, err := s.GetEntity(ctx, project, EntityProject)
	if err != nil || detail == nil {
		return fmt.Sprintf("## Focus: %s\n\n_no project entity named %q found yet — declare it via remember() to build a card_\n\n",
			project, project), nil
	}

	summaries := s.fetchEntitySummaries(ctx, detail.Edges)
	docs, _ := s.fetchDocuments(ctx, project, 100)
	loops, _ := s.fetchOpenLoops(ctx, project)
	recent, _ := s.fetchRecentMemories(ctx, 14, project)

	return renderFocusBody(detail, summaries, docs, loops, recent, orientMemChars()), nil
}

// renderFocusBody is the pure template renderer for renderFocusCard — split
// out for testability, same idiom as renderDerivedCard.
func renderFocusBody(detail *EntityDetail, summaries map[string]string, docs []docTitleRow, loops []openLoopRow, recent []recentMem, memMax int) string {
	var b strings.Builder

	header := detail.Name
	if len(detail.Aliases) > 0 {
		var alts []string
		for _, a := range detail.Aliases {
			if !strings.EqualFold(a, detail.Name) {
				alts = append(alts, a)
			}
		}
		if len(alts) > 0 {
			header += " _(" + strings.Join(alts, ", ") + ")_"
		}
	}
	b.WriteString(fmt.Sprintf("## Focus: %s\n\n", header))

	wroteAny := false
	if detail.HandNotes != "" {
		b.WriteString(detail.HandNotes)
		if !strings.HasSuffix(detail.HandNotes, "\n") {
			b.WriteString("\n")
		}
		wroteAny = true
	}
	if detail.DerivedCard != "" {
		if wroteAny {
			b.WriteString("\n")
		}
		b.WriteString(detail.DerivedCard)
		if !strings.HasSuffix(detail.DerivedCard, "\n") {
			b.WriteString("\n")
		}
		wroteAny = true
	}
	if !wroteAny && detail.Summary != "" {
		b.WriteString(detail.Summary)
		b.WriteString("\n")
	}
	if detail.LastEditedBy != "" && detail.LastEditedAt != nil {
		b.WriteString(fmt.Sprintf("_last edited by %s at %s_\n", detail.LastEditedBy, detail.LastEditedAt.Format(time.RFC3339)))
	}
	b.WriteString("\n")

	if len(detail.Edges) > 0 {
		b.WriteString(fmt.Sprintf("**Edges (%d):**\n", len(detail.Edges)))
		for _, e := range detail.Edges {
			arrow := "→"
			if e.Direction == "in" {
				arrow = "←"
			}
			status := ""
			if !e.Active {
				status = " _(closed)_"
			}
			line := fmt.Sprintf("- %s %s **%s** _(%s)_%s", e.Predicate, arrow, e.OtherName, e.OtherType, status)
			if s := summaries[e.OtherID]; s != "" {
				line += ": " + truncateForOrient(s, memMax)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	if len(docs) > 0 {
		b.WriteString(fmt.Sprintf("**Documents (%d):**\n", len(docs)))
		for _, d := range docs {
			b.WriteString(fmt.Sprintf("- %s (%s)\n", d.Title, d.DocType))
		}
		b.WriteString("\n")
	}

	if len(loops) > 0 {
		b.WriteString(fmt.Sprintf("**Open loops (%d):**\n", len(loops)))
		for _, l := range loops {
			opened := l.OpenedAt
			if len(opened) < 10 {
				opened = l.CreatedAt // defensive fallback, same as the global Open loops section
			}
			openedDate := opened
			if len(opened) >= 10 {
				openedDate = opened[:10]
			}
			b.WriteString(fmt.Sprintf("- %s _(opened %s)_\n", truncateForOrient(l.Summary, memMax), openedDate))
		}
		b.WriteString("\n")
	}

	if len(recent) > 0 {
		b.WriteString(fmt.Sprintf("**Recent memories (last 14 days, %d):**\n", len(recent)))
		for _, m := range recent {
			b.WriteString(fmt.Sprintf("- _%s_ [%s, by %s] %s\n",
				m.CreatedAt[:10], m.Kind, m.Author, truncateForOrient(m.Summary, memMax)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// fetchEntityByID fetches a single entity's promotedEntity-shaped card by
// full record id (e.g. "person:alice") — used for Focus mode's single-entity
// Identity card. Deliberately lighter than GetEntity (no mentions/edges
// lookup), mirroring fetchPromoted's projection but filtered to one id.
func (s *Store) fetchEntityByID(ctx context.Context, recID string) (*promotedEntity, error) {
	if err := safeRecordID(recID); err != nil {
		return nil, err
	}
	stmt := fmt.Sprintf(`SELECT id, name, aliases, hand_notes, derived_card, summary, mention_count, last_seen, last_edited_by, last_edited_at FROM %s;`, recID)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []promotedEntity
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// fetchEntitySummaries batch-fetches the `summary` field for a set of edge
// neighbors, grouped by table (OtherType) so it's at most one query per
// entity table touched rather than one query per edge. Record ids on
// EntityEdge always come from the DB's own meta::tb()/.id projection (see
// fetchEdgeDirection), never from unescaped user input, so they're safe to
// inline the same way recID is inlined elsewhere in this package.
func (s *Store) fetchEntitySummaries(ctx context.Context, edges []EntityEdge) map[string]string {
	byTable := map[string][]string{}
	seen := map[string]bool{}
	for _, e := range edges {
		if e.OtherType == "" || e.OtherID == "" || seen[e.OtherID] {
			continue
		}
		seen[e.OtherID] = true
		byTable[e.OtherType] = append(byTable[e.OtherType], e.OtherID)
	}
	out := map[string]string{}
	for tbl, ids := range byTable {
		stmt := fmt.Sprintf(`SELECT id, summary FROM %s WHERE id IN [%s];`, tbl, strings.Join(ids, ", "))
		res, err := s.db.SQL(ctx, stmt, true)
		if err != nil || len(res) == 0 || len(res[0].Result) == 0 {
			continue
		}
		var rows []struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal(res[0].Result, &rows) == nil {
			for _, r := range rows {
				out[r.ID] = r.Summary
			}
		}
	}
	return out
}

// ============================================================
// Map (final section of global orient)
// ============================================================

// mapDormantProjectLimit is a generous safety cap on the dormant-projects
// listing — Jason explicitly asked for a GENEROUS map (err toward listing
// names), so this is only a backstop against a runaway table, not a design
// intent to hide projects.
const mapDormantProjectLimit = 500

// mapDocLimit caps the per-title document listing in the map; the total
// count is always shown, and an overflow note covers anything past the cap.
const mapDocLimit = 40

// mapTopEntityLimit caps the "most active" entity listing in the map.
const mapTopEntityLimit = 10

type mapProjectRow struct {
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	DerivedCard string `json:"derived_card"`
}

type docTitleRow struct {
	Title   string `json:"title"`
	DocType string `json:"doc_type"`
}

type mapEntityRow struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	MentionCount int    `json:"mention_count"`
}

// orientMapData is everything renderOrientMapBody needs, pre-fetched — split
// out so the pure render logic is unit-testable without a DB, same idiom as
// renderDerivedCard/renderFocusBody.
type orientMapData struct {
	DormantProjects []mapProjectRow
	DocTotal        int
	DocSample       []docTitleRow
	EntityCounts    map[string]int
	TopEntities     []mapEntityRow
}

// renderOrientMap fetches and renders the "## Map" section. Soft-fails like
// every other orient section — a failed sub-fetch just renders as empty/zero
// rather than blanking the whole section (or the whole orient call).
func (s *Store) renderOrientMap(ctx context.Context) string {
	data := orientMapData{EntityCounts: map[string]int{}}
	data.DormantProjects, _ = s.fetchDormantProjects(ctx)

	if counts, err := s.countTables(ctx, []string{"document"}, "is_active = true"); err == nil {
		data.DocTotal = counts["document"]
	}
	data.DocSample, _ = s.fetchDocuments(ctx, "", mapDocLimit)

	data.EntityCounts, data.TopEntities = s.fetchMapEntities(ctx)

	return renderOrientMapBody(data)
}

// renderOrientMapBody is the pure template renderer for the Map section.
func renderOrientMapBody(d orientMapData) string {
	var b strings.Builder
	b.WriteString("## Map\n\n")

	b.WriteString(fmt.Sprintf("**Other projects (%d):**\n", len(d.DormantProjects)))
	for _, p := range d.DormantProjects {
		hook := mapHook(p.Summary, p.DerivedCard)
		if hook != "" {
			b.WriteString(fmt.Sprintf("- %s — %s\n", p.Name, hook))
		} else {
			b.WriteString(fmt.Sprintf("- %s\n", p.Name))
		}
	}
	b.WriteString("\n")

	b.WriteString(fmt.Sprintf("**Documents:** %d\n", d.DocTotal))
	for _, doc := range d.DocSample {
		b.WriteString(fmt.Sprintf("- %s (%s)\n", doc.Title, doc.DocType))
	}
	if overflow := d.DocTotal - len(d.DocSample); overflow > 0 {
		b.WriteString(fmt.Sprintf("_...and %d more_\n", overflow))
	}
	b.WriteString("\n")

	b.WriteString("**Entities:** ")
	var typeParts []string
	for _, t := range ValidEntityTypes {
		typeParts = append(typeParts, fmt.Sprintf("%s: %d", t, d.EntityCounts[string(t)]))
	}
	b.WriteString(strings.Join(typeParts, ", "))
	b.WriteString("\n")
	if len(d.TopEntities) > 0 {
		var top []string
		for _, e := range d.TopEntities {
			top = append(top, fmt.Sprintf("%s (%s)", e.Name, e.Type))
		}
		b.WriteString("Top: " + strings.Join(top, ", ") + "\n")
	}
	b.WriteString("\n")

	b.WriteString("_pull: get_entity · doc_get · recall · orient(project=…)_\n")

	return b.String()
}

// fetchDormantProjects returns every non-merged project entity that isn't
// promoted — i.e. every project NOT already surfaced in the "Active
// projects" section (fetchPromoted selects promoted=true; this is its
// complement). No recency gate — dormant is exactly "not promoted" here.
func (s *Store) fetchDormantProjects(ctx context.Context) ([]mapProjectRow, error) {
	// promoted != true (not = false): rows predating the promoted field carry
	// NONE, and those are exactly the dormant ones. mention_count/last_seen
	// must be projected — SurrealDB rejects ORDER BY on non-projected fields.
	stmt := fmt.Sprintf(`SELECT name, summary, derived_card, mention_count, last_seen FROM project
		WHERE merged_into IS NONE AND promoted != true
		ORDER BY mention_count DESC, last_seen DESC LIMIT %d;`, mapDormantProjectLimit)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []mapProjectRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchDocuments returns document titles + doc_type, newest first, capped at
// limit. project == "" fetches across every project (global map use); a
// non-empty project scopes to it (Focus use).
func (s *Store) fetchDocuments(ctx context.Context, project string, limit int) ([]docTitleRow, error) {
	// is_active != false (not = true): docs predating the is_active field
	// carry NONE and are live. created_at must be projected — SurrealDB
	// rejects ORDER BY on non-projected fields.
	filters := []string{"is_active != false"}
	if project != "" {
		filters = append(filters, fmt.Sprintf("project = %s", EscapeStr(project)))
	}
	stmt := fmt.Sprintf(`SELECT title, doc_type, created_at FROM document WHERE %s ORDER BY created_at DESC LIMIT %d;`,
		strings.Join(filters, " AND "), limit)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []docTitleRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// fetchMapEntities returns entity counts by type (across all 7 tables) and
// the top mapTopEntityLimit entities globally by mention_count.
func (s *Store) fetchMapEntities(ctx context.Context) (map[string]int, []mapEntityRow) {
	entTables := make([]string, len(ValidEntityTypes))
	for i, t := range ValidEntityTypes {
		entTables[i] = string(t)
	}
	counts, err := s.countTables(ctx, entTables, "merged_into IS NONE")
	if err != nil {
		counts = map[string]int{}
	}

	var all []mapEntityRow
	for _, t := range ValidEntityTypes {
		stmt := fmt.Sprintf(`SELECT name, meta::tb(id) AS type, mention_count FROM %s
			WHERE merged_into IS NONE ORDER BY mention_count DESC LIMIT %d;`, t, mapTopEntityLimit)
		res, err := s.db.SQL(ctx, stmt, true)
		if err != nil || len(res) == 0 || len(res[0].Result) == 0 {
			continue
		}
		var rows []mapEntityRow
		if json.Unmarshal(res[0].Result, &rows) == nil {
			all = append(all, rows...)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].MentionCount > all[j].MentionCount })
	if len(all) > mapTopEntityLimit {
		all = all[:mapTopEntityLimit]
	}
	return counts, all
}

// mapHook derives the Map's "- name — hook" hook text: the first sentence of
// the entity's summary, or (if no summary) the first "## Facts" bullet from
// its derived_card, truncated to ~90 chars. Returns "" if neither is
// available — the caller renders just the bare name in that case.
func mapHook(summary, derivedCard string) string {
	if summary != "" {
		return truncateForOrient(firstSentence(summary), 90)
	}
	if line := firstFactsLine(derivedCard); line != "" {
		return truncateForOrient(line, 90)
	}
	return ""
}

// firstSentence returns the text up to (and including) the first sentence
// terminator, or the whole (de-newlined) string if there isn't one.
func firstSentence(s string) string {
	s = oneLiner(s)
	if i := strings.IndexAny(s, ".!?"); i >= 0 {
		return strings.TrimSpace(s[:i+1])
	}
	return s
}

// firstFactsLine returns the first "## Facts" bullet from a derived_card,
// with its trailing " _(author, day)_" attribution stripped (same peel as
// compactCardForOrient). Returns "" if the card has no Facts section.
func firstFactsLine(card string) string {
	section := ""
	for _, ln := range strings.Split(card, "\n") {
		if h, ok := strings.CutPrefix(ln, "## "); ok {
			section = strings.TrimSpace(h)
			continue
		}
		if section != "Facts" || !strings.HasPrefix(ln, "- ") {
			continue
		}
		body := strings.TrimPrefix(ln, "- ")
		if j := strings.LastIndex(body, " _("); j >= 0 && strings.HasSuffix(body, ")_") {
			body = body[:j]
		}
		return body
	}
	return ""
}

// renderEntitySection writes one card section for an entity.
func renderEntitySection(b *strings.Builder, e promotedEntity) {
	header := e.Name
	if e.Aliases != nil && len(e.Aliases) > 1 {
		// list aliases other than the canonical name
		var alts []string
		for _, a := range e.Aliases {
			if !strings.EqualFold(a, e.Name) {
				alts = append(alts, a)
			}
		}
		if len(alts) > 0 {
			header += " _(" + strings.Join(alts, ", ") + ")_"
		}
	}
	b.WriteString(fmt.Sprintf("### %s\n", header))
	// Render order: hand_notes first (curated voice), then derived_card (system view),
	// then summary as a final fallback if the entity has neither.
	wroteAny := false
	if e.HandNotes != "" {
		b.WriteString(e.HandNotes)
		if !strings.HasSuffix(e.HandNotes, "\n") {
			b.WriteString("\n")
		}
		wroteAny = true
	}
	if e.DerivedCard != "" {
		if wroteAny {
			b.WriteString("\n")
		}
		card := compactCardForOrient(e.DerivedCard, orientCardChars())
		b.WriteString(card)
		if !strings.HasSuffix(card, "\n") {
			b.WriteString("\n")
		}
		wroteAny = true
	}
	if !wroteAny && e.Summary != "" {
		b.WriteString(e.Summary)
		b.WriteString("\n")
	}
	if e.LastEditedBy != "" {
		b.WriteString(fmt.Sprintf("_last edited by %s at %s_\n", e.LastEditedBy, e.LastEditedAt.Format(time.RFC3339)))
	}
	b.WriteString("\n")
}

// ============================================================
// Internal fetch helpers
// ============================================================

type promotedEntity struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Aliases      []string  `json:"aliases"`
	HandNotes    string    `json:"hand_notes"`
	DerivedCard  string    `json:"derived_card"`
	Summary      string    `json:"summary"`
	MentionCount int       `json:"mention_count"`
	LastSeen     time.Time `json:"last_seen"`
	LastEditedBy string    `json:"last_edited_by"`
	LastEditedAt time.Time `json:"last_edited_at"`
}

// selfishness returns a sort-key hint that lands the owner's own identity card
// first in the Identity section. Configured via RILL_OWNER_ENTITY — set it to
// the owner's record id (e.g. "person:alice" or "person:`Alice Jones`").
// Match is by record id, which naturally folds in every alias on that entity,
// since aliases live on the same record. When unset, no entity is "owner" and
// promoted persons sort purely by mention_count / last_seen.
func (e promotedEntity) selfishness() int {
	if owner := settings.Get().OwnerEntity(); owner != "" && e.ID == owner {
		return 100
	}
	return 0
}

func (s *Store) fetchPromoted(ctx context.Context, table string) ([]promotedEntity, error) {
	stmt := fmt.Sprintf(`SELECT id, name, aliases, hand_notes, derived_card, summary, mention_count, last_seen, last_edited_by, last_edited_at FROM %s WHERE promoted = true ORDER BY mention_count DESC, last_seen DESC;`, table)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []promotedEntity
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type ruleRow struct {
	Content string `json:"content"`
	Project string `json:"project"`
}

// fetchActiveRules returns kind=rule memories for the rules section of orient.
// Rules are just memories with kind='rule'. Newest first.
func (s *Store) fetchActiveRules(ctx context.Context, project string) ([]ruleRow, error) {
	filters := []string{"is_active = true", "kind = 'rule'"}
	if project != "" {
		filters = append(filters, fmt.Sprintf("(project = %s OR project IS NONE)", EscapeStr(project)))
	} else {
		filters = append(filters, "project IS NONE")
	}
	stmt := fmt.Sprintf(`SELECT summary AS content, project, created_at FROM memory WHERE %s ORDER BY created_at DESC LIMIT 30;`,
		strings.Join(filters, " AND "))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []ruleRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type prefRow struct {
	Subject string `json:"subject"`
	Object  string `json:"object"`
	Valence string `json:"valence"`
}

func (s *Store) fetchActivePreferences(ctx context.Context, owner string) ([]prefRow, error) {
	where := "valid_until IS NONE"
	if owner != "" {
		where += " AND in != " + owner
	}
	stmt := fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, valence, valid_from FROM prefers WHERE %s ORDER BY valid_from DESC LIMIT 30;`, where)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []prefRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type relRow struct {
	Subject   string `json:"subject"`
	Object    string `json:"object"`
	Predicate string `json:"predicate"`
	Role      string `json:"role"`
}

func (s *Store) fetchActiveRelationships(ctx context.Context, owner string) ([]relRow, error) {
	// Exclude the owner as subject — their edges already render in the Identity
	// card's Active edges section. Non-owner subjects still surface (additive).
	ownerClause := ""
	if owner != "" {
		ownerClause = " AND in != " + owner
	}
	var out []relRow
	// works_at
	r, _ := s.db.SQL(ctx, fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, role_title AS role, valid_from FROM works_at WHERE valid_until IS NONE%s ORDER BY valid_from DESC LIMIT 10;`, ownerClause), true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "works_at"
		}
		out = append(out, rows...)
	}
	// works_on — recency-gated: only projects touched within the orient window.
	// Edges stay valid in the graph; this filters visibility only.
	r, _ = s.db.SQL(ctx, fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, valid_from FROM works_on WHERE valid_until IS NONE AND out.last_edited_at > %s%s ORDER BY valid_from DESC LIMIT 10;`, EscapeDatetime(orientRecencyCutoff()), ownerClause), true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "works_on"
		}
		out = append(out, rows...)
	}
	// uses
	r, _ = s.db.SQL(ctx, fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, valid_from FROM uses WHERE valid_until IS NONE%s ORDER BY valid_from DESC LIMIT 10;`, ownerClause), true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "uses"
		}
		out = append(out, rows...)
	}
	// depends_on
	r, _ = s.db.SQL(ctx, fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, valid_from FROM depends_on WHERE valid_until IS NONE%s ORDER BY valid_from DESC LIMIT 10;`, ownerClause), true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "depends_on"
		}
		out = append(out, rows...)
	}
	return out, nil
}

type recentMem struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Kind      string `json:"kind"`
	Author    string `json:"author"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) fetchRecentMemories(ctx context.Context, days int, project string) ([]recentMem, error) {
	cutoff := now().AddDate(0, 0, -days)
	// Exclude rules — they have their own top-level section in orient and
	// don't need to also appear in the chronological feed (per Deepseek feedback,
	// 2026-05-23 — promoted memories that already render in structured sections
	// add noise to the recent-memories feed).
	filters := []string{"is_active = true", "kind != 'rule'", fmt.Sprintf("created_at > %s", EscapeDatetime(cutoff))}
	if project != "" {
		filters = append(filters, fmt.Sprintf("(project = %s OR project IS NONE)", EscapeStr(project)))
	}
	stmt := fmt.Sprintf(`SELECT id, summary, kind, author, project, created_at FROM memory WHERE %s ORDER BY created_at DESC LIMIT 20;`,
		strings.Join(filters, " AND "))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []recentMem
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type openLoopRow struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	Project   string `json:"project"`
	OpenedAt  string `json:"opened_at"`
	CreatedAt string `json:"created_at"`
}

// fetchOpenLoops returns every is_active memory flagged open=true — unlike
// fetchRecentMemories, there is no age cutoff (an open loop should stay
// visible until it's closed, however old). Unmigrated rows read open as NONE,
// which the "open = true" filter naturally excludes (never matches). LIMIT is
// a generous safety cap, not a design intent to hide loops.
func (s *Store) fetchOpenLoops(ctx context.Context, project string) ([]openLoopRow, error) {
	filters := []string{"is_active = true", "open = true"}
	if project != "" {
		filters = append(filters, fmt.Sprintf("(project = %s OR project IS NONE)", EscapeStr(project)))
	}
	stmt := fmt.Sprintf(`SELECT id, summary, project, opened_at, created_at FROM memory WHERE %s ORDER BY opened_at ASC LIMIT 100;`,
		strings.Join(filters, " AND "))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return nil, nil
	}
	var rows []openLoopRow
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// compactCardForOrient truncates the prose-bearing lines of a pre-rendered
// derived_card to at most max characters each, so a heavy owner/project card
// doesn't dominate orient. Only the Identity, Facts, and Decisions sections are
// trimmed — Active edges and Preferences lines are already terse and pass
// through untouched. The trailing " _(author, day)_" attribution is preserved.
// The stored card is unchanged (get_entity still returns full text); this only
// affects the orient blob. max <= 0 returns the card verbatim.
func compactCardForOrient(card string, max int) string {
	if max <= 0 || card == "" {
		return card
	}
	trunc := map[string]bool{"Identity": true, "Facts": true, "Decisions": true}
	lines := strings.Split(card, "\n")
	section := ""
	for i, ln := range lines {
		if h, ok := strings.CutPrefix(ln, "## "); ok {
			section = strings.TrimSpace(h)
			continue
		}
		if !trunc[section] || !strings.HasPrefix(ln, "- ") {
			continue
		}
		body := strings.TrimPrefix(ln, "- ")
		// Peel off the trailing " _(author, day)_" so it survives truncation.
		summary, suffix := body, ""
		if j := strings.LastIndex(body, " _("); j >= 0 && strings.HasSuffix(body, ")_") {
			summary, suffix = body[:j], body[j:]
		}
		if len([]rune(summary)) <= max {
			continue
		}
		lines[i] = "- " + truncateForOrient(summary, max) + suffix
	}
	return strings.Join(lines, "\n")
}

func oneLiner(s string) string {
	s = strings.TrimSpace(s)
	// Collapse newlines to spaces for compact rendering
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

// truncateForOrient de-newlines s and trims it to at most max characters (runes,
// so multibyte chars never split). It prefers to cut at a sentence end, then a
// word boundary, before falling back to a hard cut; a trailing "…" marks a
// non-sentence cut. max <= 0 returns the full de-newlined string.
func truncateForOrient(s string, max int) string {
	s = oneLiner(s)
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	cut := string(r[:max])
	// Prefer the last sentence end, if it's at least halfway in (avoid a tiny stub).
	if i := strings.LastIndexAny(cut, ".!?"); i >= len(cut)/2 {
		return strings.TrimSpace(cut[:i+1])
	}
	// Else cut at the last word boundary.
	if i := strings.LastIndex(cut, " "); i >= len(cut)/2 {
		return strings.TrimSpace(cut[:i]) + "…"
	}
	return strings.TrimSpace(cut) + "…"
}
