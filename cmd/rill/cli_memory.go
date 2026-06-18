package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jasondostal/rill/internal/memory"
)

// addMemoryCommands wires `rill ...` subcommands. Every command speaks REST
// to the rill server using RILL_HOST + RILL_TOKEN. No direct SurrealDB access.
func addMemoryCommands(root *cobra.Command) {
	root.AddCommand(memRememberCmd())
	root.AddCommand(memOrientCmd())
	root.AddCommand(memRecallCmd())
	root.AddCommand(memPromoteCmd())
	root.AddCommand(memDemoteCmd())
	root.AddCommand(memMergeEntityCmd())
	root.AddCommand(memSetVersionCmd())
	root.AddCommand(memForgetCmd())
	root.AddCommand(memEditNotesCmd())
	root.AddCommand(memEditMemoryCmd())
	root.AddCommand(memAddEdgeCmd())
	root.AddCommand(memCloseEdgeCmd())
	root.AddCommand(memEntitiesCmd())
	root.AddCommand(memEntityCmd())
	root.AddCommand(memMemoriesCmd())
	root.AddCommand(memMemoryCmd())
	root.AddCommand(memPingCmd())
}

// parseEntityRef accepts either "person:alice" (full record id) or a
// bare name with --type. Returns (type, slug) for path interpolation. When a
// bare name is given, the server slugifies it; the CLI just URL-encodes.
func parseEntityRef(ref, flagType string) (typ, slug string, err error) {
	if i := strings.IndexByte(ref, ':'); i > 0 {
		return ref[:i], ref[i+1:], nil
	}
	if flagType == "" {
		return "", "", fmt.Errorf("entity ref %q has no table prefix and --type was not supplied", ref)
	}
	return flagType, ref, nil
}

// entityPath builds /api/entity/{type}/{slug} with URL-encoding.
func entityPath(typ, slug, sub string) string {
	p := "/api/entity/" + url.PathEscape(typ) + "/" + url.PathEscape(slug)
	if sub != "" {
		p += "/" + sub
	}
	return p
}

// ---- ping ----

func memPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Verify the rill server is reachable",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var out map[string]any
			if err := newRESTClient().get(ctx, "/api/ping", &out); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "rill OK")
			return nil
		},
	}
}

// ---- remember ----

func memRememberCmd() *cobra.Command {
	var fromFile string
	cmd := &cobra.Command{
		Use:   "remember",
		Short: "Store an intentional memory (stdin = JSON payload)",
		Long: `Reads a RememberPayload JSON from stdin (or --file) and POSTs it to
/api/remember. The server:
  1. Writes a memory row (summary capped at 500 chars; details unbounded).
  2. Upserts each declared entity (bumps mention_count if existing).
  3. Writes a mentions edge memory -> entity for each declared entity.
  4. Writes each declared edge with consolidation rules.
  5. Recomputes derived_card on every touched entity.
  6. Marks orient_cache stale for global + project scope.

STRICT: every edge endpoint must appear in entities[]. No auto-creation.

Enums:
  kind          decision | preference | insight | procedure | fact | identity | rule | idea
  entity type   person | project | tool | organization | place | preference | concept
  valence       positive | negative | neutral   (only when kind=preference / predicate=prefers)

Full example payload:
  {
    "summary": "Alice prefers vim over emacs.",
    "kind": "preference",
    "author": "alice",
    "project": "rill",
    "tags": ["editor", "preference"],
    "entities": [
      {"name": "Alice Smith", "type": "person"},
      {"name": "vim", "type": "tool"}
    ],
    "edges": [
      {"subject": "Alice Smith", "subject_type": "person",
       "predicate": "prefers",
       "object": "vim", "object_type": "tool",
       "valence": "positive"}
    ]
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var raw []byte
			var err error
			if fromFile != "" {
				// #nosec G304 -- fromFile is the --file CLI flag (operator-controlled).
				raw, err = os.ReadFile(fromFile)
			} else {
				raw, err = io.ReadAll(cmd.InOrStdin())
			}
			if err != nil {
				return err
			}
			var p memory.RememberPayload
			if err := json.Unmarshal(raw, &p); err != nil {
				return fmt.Errorf("parse payload: %w", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var res memory.RememberResult
			if err := newRESTClient().post(ctx, "/api/remember", p, &res); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().StringVar(&fromFile, "file", "", "Read payload from file instead of stdin")
	return cmd
}

// ---- orient ----

func memOrientCmd() *cobra.Command {
	var (
		flagProject string
		flagForce   bool
		flagJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "orient",
		Short: "Print the cached orient blob for a scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			path := pathWithQuery("/api/orient", map[string]string{
				"project": flagProject,
				"force":   ifTrue(flagForce, "1"),
			})
			var res memory.OrientResult
			if err := newRESTClient().get(ctx, path, &res); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			fmt.Fprint(cmd.OutOrStdout(), res.Rendered)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagProject, "project", "", "Scope to a project (default: global)")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Force cache regeneration")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON wrapper instead of just the blob")
	return cmd
}

// ---- recall ----

func memRecallCmd() *cobra.Command {
	var (
		flagKind    string
		flagProject string
		flagAuthor  string
		flagK       int
		flagJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "Hybrid recall: vector + FTS, with linked entities",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := memory.RecallQuery{
				Query:   strings.Join(args, " "),
				Kind:    memory.Kind(flagKind),
				Project: flagProject,
				Author:  flagAuthor,
				K:       flagK,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var res memory.RecallResult
			if err := newRESTClient().post(ctx, "/api/recall", body, &res); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			for _, h := range res.Memories {
				fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] %s\n", h.ID, h.Kind, h.Summary)
			}
			if len(res.Entities) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "")
				for _, e := range res.Entities {
					fmt.Fprintf(cmd.OutOrStdout(), "  ~%s [%s] (mentions=%d)\n", e.Name, e.Type, e.MentionCount)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagKind, "kind", "", "Filter by memory kind")
	cmd.Flags().StringVar(&flagProject, "project", "", "Filter by project")
	cmd.Flags().StringVar(&flagAuthor, "author", "", "Filter by author")
	cmd.Flags().IntVar(&flagK, "k", 5, "Number of results")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON")
	return cmd
}

// ---- edit_memory ----

func memEditMemoryCmd() *cobra.Command {
	var (
		flagSummary string
		flagDetails string
		flagTags    string
		flagValence string
		flagProject string
		flagAuthor  string
		flagJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "edit-memory <memory-id>",
		Short: "Patch mutable fields on an existing memory",
		Long: `Patches mutable fields on a memory (summary, details, tags, valence, project).
Re-embeds the summary if changed. Recomputes derived_card on every entity
the memory mentions. IMMUTABLE: id, kind, author, created_at — use forget +
remember to change those.

Only flags that you provide are applied. Empty string clears optional fields
(details, valence, project). Tags is replaced wholesale when --tags is provided.

Examples:
  rill edit-memory memory:'2026...' --summary "fixed typo" --author alice
  rill edit-memory memory:'2026...' --tags "design,reference,a11y" --author alice
  rill edit-memory memory:'2026...' --details "" --author alice     # clear details`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if cmd.Flags().Changed("summary") {
				body["summary"] = flagSummary
			}
			if cmd.Flags().Changed("details") {
				body["details"] = flagDetails
			}
			if cmd.Flags().Changed("tags") {
				if flagTags == "" {
					body["tags"] = []string{}
				} else {
					parts := strings.Split(flagTags, ",")
					for i := range parts {
						parts[i] = strings.TrimSpace(parts[i])
					}
					body["tags"] = parts
				}
			}
			if cmd.Flags().Changed("valence") {
				body["valence"] = flagValence
			}
			if cmd.Flags().Changed("project") {
				body["project"] = flagProject
			}
			if flagAuthor != "" {
				body["author"] = flagAuthor
			}
			id := strings.TrimPrefix(args[0], "memory:")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var detail memory.MemoryDetail
			if err := newRESTClient().patch(ctx, "/api/memory/"+url.PathEscape(id), body, &detail); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), detail)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s updated; last_edited_by=%s\n", detail.ID, flagAuthor)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagSummary, "summary", "", "New summary (≤500 chars)")
	cmd.Flags().StringVar(&flagDetails, "details", "", "New details (empty string clears)")
	cmd.Flags().StringVar(&flagTags, "tags", "", "Comma-separated tags (replaces existing)")
	cmd.Flags().StringVar(&flagValence, "valence", "", "positive|negative|neutral (empty clears)")
	cmd.Flags().StringVar(&flagProject, "project", "", "Project scope (empty clears)")
	cmd.Flags().StringVar(&flagAuthor, "author", os.Getenv("USER"), "Author of this edit (required)")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON")
	return cmd
}

// ---- promote / demote ----

func memPromoteCmd() *cobra.Command {
	var flagType string
	cmd := &cobra.Command{
		Use:   "promote <entity-ref>",
		Short: "Flag an entity as promoted (appears in orient)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(args[0], flagType)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return newRESTClient().post(ctx, entityPath(typ, slug, "promote"), map[string]any{}, nil)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Entity type if ref isn't a full id (e.g. tool)")
	return cmd
}

func memDemoteCmd() *cobra.Command {
	var flagType string
	cmd := &cobra.Command{
		Use:   "demote <entity-ref>",
		Short: "Flip an entity back to unpromoted",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(args[0], flagType)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return newRESTClient().post(ctx, entityPath(typ, slug, "demote"), map[string]any{}, nil)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Entity type if ref isn't a full id")
	return cmd
}

// ---- merge_entity ----

func memMergeEntityCmd() *cobra.Command {
	var (
		flagType           string
		flagTarget         string
		flagAuthor         string
		flagAllowCrossType bool
	)
	cmd := &cobra.Command{
		Use:   "merge-entity <source-entity-ref>",
		Short: "Merge the source entity into --target (source is absorbed, target survives)",
		Long: `Fold the source entity — its edges, mentions, and memories — into the
surviving --target entity, then remove the source. The target's derived_card
is recomputed.

By default source and target must be the SAME type. Pass --allow-cross-type
(with a full record-id target) only when they are genuinely the same thing
recorded under two types.

Example:
  rill merge-entity tool:kimi-k2 --target tool:kimi --author alice`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(args[0], flagType)
			if err != nil {
				return err
			}
			body := map[string]any{"target": flagTarget}
			if flagAllowCrossType {
				body["allow_cross_type"] = true
			}
			if flagAuthor != "" {
				body["author"] = flagAuthor
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var out any
			if err := newRESTClient().post(ctx, entityPath(typ, slug, "merge"), body, &out); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Source entity type if ref isn't a full id")
	cmd.Flags().StringVar(&flagTarget, "target", "", "Surviving entity: full record id or bare name (required)")
	cmd.Flags().BoolVar(&flagAllowCrossType, "allow-cross-type", false, "Permit merging across entity types (target must be a full record id)")
	cmd.Flags().StringVar(&flagAuthor, "author", os.Getenv("USER"), "Author of this edit")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

// ---- set_version ----

func memSetVersionCmd() *cobra.Command {
	var (
		flagType    string
		flagVersion string
		flagAuthor  string
	)
	cmd := &cobra.Command{
		Use:   "set-version <entity-ref>",
		Short: "Set an entity's current version label (bi-temporal; closes the prior)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(args[0], flagType)
			if err != nil {
				return err
			}
			body := map[string]any{"version": flagVersion}
			if flagAuthor != "" {
				body["author"] = flagAuthor
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var out any
			if err := newRESTClient().post(ctx, entityPath(typ, slug, "version"), body, &out); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Entity type if ref isn't a full id")
	cmd.Flags().StringVar(&flagVersion, "version", "", "Version label, e.g. '2.3.0' (required)")
	cmd.Flags().StringVar(&flagAuthor, "author", os.Getenv("USER"), "Author of this edit")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

// ---- forget ----

func memForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <memory-id>",
		Short: "Soft-delete a memory (sets is_active = false)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimPrefix(args[0], "memory:")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return newRESTClient().del(ctx, "/api/memory/"+url.PathEscape(id), nil)
		},
	}
}

// ---- edit_notes ----

func memEditNotesCmd() *cobra.Command {
	var (
		flagType      string
		flagAuthor    string
		flagMode      string
		flagFromStdin bool
		flagText      string
	)
	cmd := &cobra.Command{
		Use:   "edit-notes <entity-ref>",
		Short: "Edit an entity's hand_notes (human-curated markdown). NOT the system-rendered derived_card.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(args[0], flagType)
			if err != nil {
				return err
			}
			text := flagText
			if flagFromStdin {
				raw, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				text = string(raw)
			}
			mode := flagMode
			if mode == "" {
				mode = "append"
			}
			body := map[string]any{"text": text, "mode": mode}
			if flagAuthor != "" {
				body["author"] = flagAuthor
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return newRESTClient().post(ctx, entityPath(typ, slug, "hand_notes"), body, nil)
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Entity type if ref isn't a full id")
	cmd.Flags().StringVar(&flagAuthor, "author", os.Getenv("USER"), "Author of this edit")
	cmd.Flags().StringVar(&flagMode, "mode", "append", "append | replace")
	cmd.Flags().BoolVar(&flagFromStdin, "stdin", false, "Read text from stdin")
	cmd.Flags().StringVar(&flagText, "text", "", "Text to apply (or use --stdin)")
	return cmd
}

// ---- add_edge ----

func memAddEdgeCmd() *cobra.Command {
	var (
		flagSubject, flagSubjectType string
		flagObject, flagObjectType   string
		flagPredicate                string
		flagValence, flagRole        string
		flagAuthor                   string
	)
	cmd := &cobra.Command{
		Use:   "add-edge",
		Short: "Add a relationship edge between two existing entities (no memory written)",
		Long: `Writes a single edge between two entities that ALREADY exist.
Recomputes derived_card on both endpoints. No memory row is written —
use 'remember' if you also want a narrative.

Both endpoints must already exist (declare via 'remember' first).
Otherwise: error "entity X:Y does not exist — declare it via remember() before linking".

Predicates:
  Dedicated (typed RELATION tables):
    works_on   (person -> project)
    uses       (person -> tool)
    prefers    (person -> preference)  + --valence
    works_at   (person -> organization)  + --role
    depends_on (any -> any)
    part_of    (any -> any)
  Generic (free-form, lands in 'assertion' table):
    is_a, lives_in, manages, married_to, owns, ... any string

Examples:
  rill add-edge --subject "Alice Smith" --subject-type person \
                   --predicate uses \
                   --object SurrealDB --object-type tool \
                   --author alice`,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{
				"subject":      flagSubject,
				"subject_type": flagSubjectType,
				"predicate":    flagPredicate,
				"object":       flagObject,
				"object_type":  flagObjectType,
			}
			if flagValence != "" {
				body["valence"] = flagValence
			}
			if flagRole != "" {
				body["role_title"] = flagRole
			}
			if flagAuthor != "" {
				body["author"] = flagAuthor
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var ref memory.EdgeRef
			if err := newRESTClient().post(ctx, "/api/edge", body, &ref); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), ref)
		},
	}
	cmd.Flags().StringVar(&flagSubject, "subject", "", "Subject entity name")
	cmd.Flags().StringVar(&flagSubjectType, "subject-type", "", "Subject entity type")
	cmd.Flags().StringVar(&flagPredicate, "predicate", "", "Edge predicate (e.g. works_on, is_a, prefers)")
	cmd.Flags().StringVar(&flagObject, "object", "", "Object entity name")
	cmd.Flags().StringVar(&flagObjectType, "object-type", "", "Object entity type")
	cmd.Flags().StringVar(&flagValence, "valence", "", "Valence (only for prefers): positive|negative|neutral")
	cmd.Flags().StringVar(&flagRole, "role", "", "Role title (only for works_at)")
	cmd.Flags().StringVar(&flagAuthor, "author", os.Getenv("USER"), "Author of this edit")
	_ = cmd.MarkFlagRequired("subject")
	_ = cmd.MarkFlagRequired("subject-type")
	_ = cmd.MarkFlagRequired("predicate")
	_ = cmd.MarkFlagRequired("object")
	_ = cmd.MarkFlagRequired("object-type")
	return cmd
}

// ---- close_edge ----

func memCloseEdgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close-edge <edge-id>",
		Short: "Soft-close an edge (sets valid_until=now; preserves provenance)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return newRESTClient().post(ctx, "/api/edge/"+url.PathEscape(args[0])+"/close", map[string]any{}, nil)
		},
	}
	return cmd
}

// ---- entities (browse) ----

func memEntitiesCmd() *cobra.Command {
	var (
		flagType     string
		flagPromoted string
		flagSort     string
		flagLimit    int
		flagJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "entities",
		Short: "Browse entities (list with filters)",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]string{
				"type":  flagType,
				"sort":  flagSort,
				"limit": strconv.Itoa(flagLimit),
			}
			switch flagPromoted {
			case "true", "false":
				params["promoted"] = flagPromoted
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var resp struct {
				Entities []memory.EntityRow `json:"entities"`
			}
			if err := newRESTClient().get(ctx, pathWithQuery("/api/entities", params), &resp); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), resp)
			}
			for _, r := range resp.Entities {
				star := " "
				if r.Promoted {
					star = "★"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-13s %s (×%d)\n", star, r.Type+":", r.Name, r.MentionCount)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Filter by entity type")
	cmd.Flags().StringVar(&flagPromoted, "promoted", "", "true | false (omit for all)")
	cmd.Flags().StringVar(&flagSort, "sort", "mention_count", "mention_count | recent | name")
	cmd.Flags().IntVar(&flagLimit, "limit", 100, "Max entities to return")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of human-readable")
	return cmd
}

// ---- entity (detail) ----

func memEntityCmd() *cobra.Command {
	var (
		flagType string
		flagJSON bool
	)
	cmd := &cobra.Command{
		Use:   "entity <ref>",
		Short: "Show one entity's full state (header, hand_notes, derived_card, edges, mentions)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, slug, err := parseEntityRef(strings.Join(args, " "), flagType)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var detail memory.EntityDetail
			if err := newRESTClient().get(ctx, entityPath(typ, slug, ""), &detail); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), detail)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s  (%s)\n", detail.Name, detail.Type)
			fmt.Fprintf(out, "  id:        %s\n", detail.ID)
			fmt.Fprintf(out, "  mentions:  %d   promoted: %v\n", detail.MentionCount, detail.Promoted)
			if detail.HandNotes != "" {
				fmt.Fprintf(out, "\n--- HAND NOTES ---\n%s\n", detail.HandNotes)
			}
			if detail.DerivedCard != "" {
				fmt.Fprintf(out, "\n--- DERIVED CARD (system-rendered) ---\n%s\n", detail.DerivedCard)
			}
			if len(detail.Edges) > 0 {
				fmt.Fprintln(out, "\n--- EDGES ---")
				for _, e := range detail.Edges {
					status := ""
					if !e.Active {
						status = " [CLOSED]"
					}
					arrow := "->"
					if e.Direction == "in" {
						arrow = "<-"
					}
					fmt.Fprintf(out, "  %-30s  %s %s %s (%s)%s\n", e.ID, e.Predicate, arrow, e.OtherName, e.OtherType, status)
				}
			}
			if len(detail.Mentions) > 0 {
				fmt.Fprintln(out, "\n--- MENTIONS ---")
				for _, m := range detail.Mentions {
					fmt.Fprintf(out, "  [%-10s] %s\n", m.Kind, m.Summary)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagType, "type", "", "Entity type when <ref> is a bare name")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of human-readable")
	return cmd
}

// ---- memories (browse) ----

func memMemoriesCmd() *cobra.Command {
	var (
		flagKind    string
		flagProject string
		flagAuthor  string
		flagLimit   int
		flagBefore  string
		flagJSON    bool
	)
	cmd := &cobra.Command{
		Use:   "memories",
		Short: "Browse memories, newest first (time-ordered)",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]string{
				"kind":    flagKind,
				"project": flagProject,
				"author":  flagAuthor,
				"limit":   strconv.Itoa(flagLimit),
				"before":  flagBefore,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var resp struct {
				Memories   []memory.MemoryRow `json:"memories"`
				NextCursor string             `json:"next_cursor,omitempty"`
			}
			if err := newRESTClient().get(ctx, pathWithQuery("/api/memories", params), &resp); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), resp)
			}
			for _, m := range resp.Memories {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%-10s] %-8s  %s\n",
					m.CreatedAt.UTC().Format("2006-01-02 15:04"), m.Kind, m.Author, m.Summary)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagKind, "kind", "", "Filter by memory kind")
	cmd.Flags().StringVar(&flagProject, "project", "", "Filter by project scope")
	cmd.Flags().StringVar(&flagAuthor, "author", "", "Filter by author (e.g. operator handle or 'claude')")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Max memories to return")
	cmd.Flags().StringVar(&flagBefore, "before", "", "Cursor: return memories strictly before this RFC3339 timestamp")
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of human-readable")
	return cmd
}

// ---- memory (detail) ----

func memMemoryCmd() *cobra.Command {
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "memory <id>",
		Short: "Show one memory's full payload + mentioned entities",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimPrefix(args[0], "memory:")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var detail memory.MemoryDetail
			if err := newRESTClient().get(ctx, "/api/memory/"+url.PathEscape(id), &detail); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), detail)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s [%s] by %s   %s\n", detail.ID, detail.Kind, detail.Author, detail.CreatedAt.UTC().Format(time.RFC3339))
			fmt.Fprintf(out, "  %s\n", detail.Summary)
			if detail.Details != "" {
				fmt.Fprintf(out, "\n--- DETAILS ---\n%s\n", detail.Details)
			}
			if len(detail.Tags) > 0 {
				fmt.Fprintf(out, "\ntags: %s\n", strings.Join(detail.Tags, ", "))
			}
			if len(detail.MentionedEntities) > 0 {
				fmt.Fprintln(out, "\n--- MENTIONED ENTITIES ---")
				for _, e := range detail.MentionedEntities {
					fmt.Fprintf(out, "  %s:%s\n", e.Type, e.Name)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of human-readable")
	return cmd
}

// ---- helpers ----

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func ifTrue(b bool, s string) string {
	if b {
		return s
	}
	return ""
}
