package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// BuildCommands wires every CLI subcommand under a single root.
// Memory subcommands speak REST to the rill server using a bearer token —
// no direct SurrealDB access. The CLI is portable: it runs from any machine
// that can reach RILL_HOST.
//
// The MCP server itself is started via `rill serve` and is handled in
// main.go BEFORE Cobra runs.
func BuildCommands() *cobra.Command {
	root := &cobra.Command{
		Use:   "rill",
		Short: "Intentional-memory store",
		Long: `Rill — a memory server with an intentional graph model.
The same binary runs the server (rill serve) and acts as a CLI client.

Environment:
  RILL_HOST    Server base URL (default: http://localhost:9090)
  RILL_TOKEN   Personal access token (create via /settings on the server UI)

Model:
  memory       — narrative provenance (one row per remember() call)
  entities     — typed nodes (person, project, tool, organization, place, preference, concept)
  edges        — typed relationships (works_on, uses, prefers, works_at, depends_on, part_of)
  assertion    — generic table for any predicate not above (is_a, lives_in, manages, etc.)
  hand_notes   — human-curated markdown per entity (writable via edit-notes)
  derived_card — system-rendered markdown per entity, auto-recomputed from edges + memories (view-only)

Enums:
  kinds         decision | preference | insight | procedure | fact | identity | rule | idea
  entity types  person | project | tool | organization | place | preference | concept
  valences      positive | negative | neutral   (only for kind=preference / predicate=prefers)
  exclusive predicates (auto-close prior): works_at, version_is, lives_at, role_at, married_to, employer_of, owns, current_focus, status_is

Common workflows:
  Browse:   rill entities                       # list all entities
            rill entity person:alice            # one entity's full state
            rill memories                       # recent memories, newest first
            rill memory <id>                    # full memory detail
            rill orient                         # rendered orient blob
  Write:    rill remember --file payload.json
            rill add-edge --subject Alice --subject-type person --predicate uses --object SurrealDB --object-type tool --author alice
            rill close-edge uses:abc123 --author alice
            rill edit-notes person:alice --text "Note." --author alice
            rill edit-memory <id> --summary "fixed typo" --author alice
  Curate:   rill promote person:alice --author alice
            rill demote person:alice --author alice
            rill forget memory:'2026...'
  Search:   rill recall "what does alice work on" --k 5`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// serve is handled in main.go before Cobra runs; we list it here for --help discoverability.
	root.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Start the rill server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("server mode is handled by the main binary — run 'rill serve' directly")
		},
	})

	addMemoryCommands(root)
	addDocCommands(root)
	addCompletionCommand(root)
	return root
}
