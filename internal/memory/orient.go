package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
// cache when stale=false; regenerates when stale or force_regen.
func (s *Store) Orient(ctx context.Context, q OrientQuery) (*OrientResult, error) {
	scope := "global"
	if q.Project != "" {
		scope = "project:" + q.Project
	}

	if !q.ForceRegen {
		cached, err := s.fetchOrientCache(ctx, scope)
		if err != nil {
			return nil, err
		}
		if cached != nil && !cached.Stale && cached.Rendered != "" {
			return &OrientResult{
				Scope:      scope,
				Rendered:   cached.Rendered,
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
	return &OrientResult{
		Scope:      scope,
		Rendered:   rendered,
		FromCache:  false,
		RenderedAt: ts,
	}, nil
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
// Render
// ============================================================

func (s *Store) renderOrient(ctx context.Context, project string) (string, error) {
	var b strings.Builder
	scopeLabel := "global"
	if project != "" {
		scopeLabel = "project:" + project
	}

	b.WriteString(fmt.Sprintf("# orient — %s\n", scopeLabel))
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

	// Active preferences (active prefers edges)
	prefs, _ := s.fetchActivePreferences(ctx)
	if len(prefs) > 0 {
		b.WriteString("## Active preferences\n")
		for _, p := range prefs {
			b.WriteString(fmt.Sprintf("- **%s** %s _(%s)_\n", p.Subject, p.Object, p.Valence))
		}
		b.WriteString("\n")
	}

	// Active relationships (works_at, works_on, uses, depends_on — active edges)
	rels, _ := s.fetchActiveRelationships(ctx)
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

	return b.String(), nil
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
		b.WriteString(e.DerivedCard)
		if !strings.HasSuffix(e.DerivedCard, "\n") {
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

func (s *Store) fetchActivePreferences(ctx context.Context) ([]prefRow, error) {
	stmt := `SELECT in.name AS subject, out.name AS object, valence, valid_from FROM prefers WHERE valid_until IS NONE ORDER BY valid_from DESC LIMIT 30;`
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

func (s *Store) fetchActiveRelationships(ctx context.Context) ([]relRow, error) {
	var out []relRow
	// works_at
	r, _ := s.db.SQL(ctx, `SELECT in.name AS subject, out.name AS object, role_title AS role, valid_from FROM works_at WHERE valid_until IS NONE ORDER BY valid_from DESC LIMIT 10;`, true)
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
	r, _ = s.db.SQL(ctx, fmt.Sprintf(`SELECT in.name AS subject, out.name AS object, valid_from FROM works_on WHERE valid_until IS NONE AND out.last_edited_at > %s ORDER BY valid_from DESC LIMIT 10;`, EscapeDatetime(orientRecencyCutoff())), true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "works_on"
		}
		out = append(out, rows...)
	}
	// uses
	r, _ = s.db.SQL(ctx, `SELECT in.name AS subject, out.name AS object, valid_from FROM uses WHERE valid_until IS NONE ORDER BY valid_from DESC LIMIT 10;`, true)
	if len(r) > 0 && len(r[0].Result) > 0 {
		var rows []relRow
		_ = json.Unmarshal(r[0].Result, &rows)
		for i := range rows {
			rows[i].Predicate = "uses"
		}
		out = append(out, rows...)
	}
	// depends_on
	r, _ = s.db.SQL(ctx, `SELECT in.name AS subject, out.name AS object, valid_from FROM depends_on WHERE valid_until IS NONE ORDER BY valid_from DESC LIMIT 10;`, true)
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
