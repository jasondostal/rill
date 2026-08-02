package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jasondostal/rill/internal/document"
	"github.com/jasondostal/rill/internal/util"
)

// addDocCommands wires the `rill doc*` subcommands. Like the memory commands,
// they speak REST to the rill server (RILL_HOST + RILL_TOKEN) — no direct DB.
//
// doc-export-all + doc-import are the durable backup / migration path: dump
// docs to a directory of .md files (frontmatter + body), edit/move them, and
// import a directory back. Same path covers importing docs from any external
// source — dump them to .md, then `rill doc-import`.
func addDocCommands(root *cobra.Command) {
	root.AddCommand(docsListCmd())
	root.AddCommand(docShowCmd())
	root.AddCommand(docPutCmd())
	root.AddCommand(docDeleteCmd())
	root.AddCommand(docExportAllCmd())
	root.AddCommand(docImportCmd())
}

// bareDocID strips the "document:" prefix for path interpolation. The server
// re-adds it; record-id chars are URL-safe (auto-generated, no backticks).
func bareDocID(id string) string {
	return strings.TrimPrefix(id, "document:")
}

func docsListCmd() *cobra.Command {
	var project, docType string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "List documents (metadata only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			path := pathWithQuery("/api/docs", map[string]string{"project": project, "doc_type": docType})
			var res struct {
				Documents []document.DocRow `json:"documents"`
			}
			if err := newRESTClient().get(ctx, path, &res); err != nil {
				return err
			}
			if len(res.Documents) == 0 {
				fmt.Println("(no documents)")
				return nil
			}
			for _, d := range res.Documents {
				proj := d.Project
				if proj == "" {
					proj = "-"
				}
				fmt.Printf("%s\t%-10s\t%s\t%s\n", d.ID, d.DocType, proj, d.Title)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	cmd.Flags().StringVar(&docType, "doc-type", "", "filter by doc_type")
	return cmd
}

func docShowCmd() *cobra.Command {
	var flagJSON bool
	cmd := &cobra.Command{
		Use:   "doc <id>",
		Short: "Fetch one document: metadata header, then the full markdown content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var doc document.Document
			if err := newRESTClient().get(ctx, "/api/docs/"+url.PathEscape(bareDocID(args[0])), &doc); err != nil {
				return err
			}
			if flagJSON {
				return writeJSON(cmd.OutOrStdout(), doc)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\n", doc.Title)
			fmt.Fprintf(out, "  id:        %s\n", doc.ID)
			fmt.Fprintf(out, "  doc_type:  %s\n", doc.DocType)
			fmt.Fprintf(out, "  project:   %s\n", orDash(doc.Project))
			fmt.Fprintf(out, "  source:    %s\n", orDash(doc.Source))
			fmt.Fprintf(out, "  author:    %s\n", orDash(doc.Author))
			fmt.Fprintf(out, "  created:   %s\n", doc.CreatedAt.UTC().Format(time.RFC3339))
			fmt.Fprintf(out, "  updated:   %s\n", doc.UpdatedAt.UTC().Format(time.RFC3339))
			fmt.Fprintf(out, "\n%s\n", doc.Content)
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output JSON instead of human-readable")
	return cmd
}

func docPutCmd() *cobra.Command {
	var id, title, content, file, docType, project, source, author string
	cmd := &cobra.Command{
		Use:   "doc-put",
		Short: "Create or update a single document (--id to update; content from --file or stdin)",
		Long: `Create a document, or update one when --id is given. Mirrors the doc_put
MCP tool: creates via POST /api/docs, updates via PATCH /api/docs/{id}.

Content resolves in order: --content, then --file <path>, then piped stdin.
--file is the recommended path — building document content via shell command
substitution inside quotes (e.g. "$(cat notes.md)") has previously produced
corrupted documents, so prefer --file or piping stdin.

Title is required on both create AND update — the server validates it either
way (a PATCH with no title is rejected same as a POST would be).

Examples:
  rill doc-put --title "Runbook" --file runbook.md --doc-type writeup --author alice
  echo "# Notes" | rill doc-put --title Notes --author alice
  rill doc-put --id document:20260101T000000Z --title "Runbook (v2)" --file runbook.md --author alice`,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := resolveDocContent(content, file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				return fmt.Errorf("--title is required (the server validates it on update too, not just create)")
			}
			in := document.PutInput{
				ID:      id,
				Title:   title,
				Content: body,
				DocType: docType,
				Project: project,
				Source:  source,
				Author:  author,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var out document.Document
			if id != "" {
				err = newRESTClient().patch(ctx, "/api/docs/"+url.PathEscape(bareDocID(id)), in, &out)
			} else {
				err = newRESTClient().post(ctx, "/api/docs", in, &out)
			}
			if err != nil {
				return err
			}
			fmt.Printf("%s\t%s\n", out.ID, out.Title)
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "Document id to update (omit to create)")
	cmd.Flags().StringVar(&title, "title", "", "Document title (required)")
	cmd.Flags().StringVar(&content, "content", "", "Inline content (else --file or stdin)")
	cmd.Flags().StringVar(&file, "file", "", "Read content from this file")
	cmd.Flags().StringVar(&docType, "doc-type", "writeup", "doc_type (e.g. writeup, primer, review, reference)")
	cmd.Flags().StringVar(&project, "project", "", "project")
	cmd.Flags().StringVar(&source, "source", "", "source label")
	cmd.Flags().StringVar(&author, "author", os.Getenv("USER"), "author")
	return cmd
}

func docDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doc-delete <id>",
		Short: "Soft-delete a document (recoverable via the UI restore)",
		Long: `Soft-deletes a document (is_active = false). Recoverable via the UI restore.
Requires a token with admin scope — unlike most mutations, which only need write.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := newRESTClient().del(ctx, "/api/docs/"+url.PathEscape(bareDocID(args[0])), nil); err != nil {
				return err
			}
			fmt.Printf("deleted %s\n", args[0])
			return nil
		},
	}
}

// resolveDocContent picks document body content from, in order: --content,
// --file, then piped stdin. Returns "" when none is provided (a metadata-only
// update is valid). stdin is only consumed when piped — never block on a TTY.
func resolveDocContent(inline, file string, stdin io.Reader) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if file != "" {
		// #nosec G304 -- file is an explicit CLI arg from a trusted local user.
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read --file %s: %w", file, err)
		}
		return string(raw), nil
	}
	if f, ok := stdin.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
			raw, err := io.ReadAll(stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			return string(raw), nil
		}
	}
	return "", nil
}

func docExportAllCmd() *cobra.Command {
	var toDir, project, docType string
	cmd := &cobra.Command{
		Use:   "doc-export-all",
		Short: "Write every matching document to a directory as <slug>__<id>.md",
		RunE: func(cmd *cobra.Command, args []string) error {
			if toDir == "" {
				return fmt.Errorf("--to <dir> is required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := os.MkdirAll(toDir, 0o750); err != nil {
				return fmt.Errorf("mkdir %s: %w", toDir, err)
			}
			c := newRESTClient()
			path := pathWithQuery("/api/docs", map[string]string{"project": project, "doc_type": docType})
			var res struct {
				Documents []document.DocRow `json:"documents"`
			}
			if err := c.get(ctx, path, &res); err != nil {
				return err
			}
			written := 0
			for _, d := range res.Documents {
				raw, err := c.getRaw(ctx, "/api/docs/"+url.PathEscape(bareDocID(d.ID))+"/export.md")
				if err != nil {
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", d.ID, err)
					continue
				}
				fname := fmt.Sprintf("%s__%s.md", util.SanitizeFilename(d.Title), bareDocID(d.ID))
				if err := os.WriteFile(filepath.Join(toDir, fname), raw, 0o600); err != nil {
					return fmt.Errorf("write %s: %w", fname, err)
				}
				written++
			}
			fmt.Printf("Exported %d document(s) to %s\n", written, toDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&toDir, "to", "", "target directory (required)")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	cmd.Flags().StringVar(&docType, "doc-type", "", "filter by doc_type")
	return cmd
}

func docImportCmd() *cobra.Command {
	var project, docType string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "doc-import <dir>",
		Short: "Import *.md files (frontmatter + body) as new documents",
		Long: `Import every *.md file in <dir> as a new document.

Frontmatter (title, doc_type, project, source) is honored; flags supply
defaults when a field is absent. Files without frontmatter use the first H1
(or the filename) as the title. Entity associations are NOT auto-applied on
import (entities may not exist in the target) — link them afterward in the UI
or via doc_put. Same path covers importing docs from any external source:
dump them to .md, then run this.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			entries, err := os.ReadDir(dir)
			if err != nil {
				return fmt.Errorf("read dir: %w", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			c := newRESTClient()
			created, skipped := 0, 0
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
					continue
				}
				// #nosec G304 -- dir is the CLI arg; e.Name() is a basename from ReadDir.
				raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
					skipped++
					continue
				}
				fm, body := document.ParseMarkdown(string(raw))
				title := fm.Title
				if title == "" {
					title = firstH1(body)
				}
				if title == "" {
					title = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
				}
				p := fm.Project
				if p == "" {
					p = project
				}
				dt := fm.DocType
				if dt == "" {
					dt = docType
				}
				src := fm.Source
				if src == "" {
					src = "import"
				}
				note := ""
				if len(fm.Entities) > 0 {
					note = fmt.Sprintf("  (frontmatter entities not auto-linked: %s)", strings.Join(fm.Entities, ", "))
				}
				if dryRun {
					fmt.Printf("would create: %q  type=%s project=%s%s\n", title, orDash(dt), orDash(p), note)
					created++
					continue
				}
				in := document.PutInput{Title: title, Content: body, DocType: dt, Project: p, Source: src}
				var out document.Document
				if err := c.post(ctx, "/api/docs", in, &out); err != nil {
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
					skipped++
					continue
				}
				fmt.Printf("created %s  %q%s\n", out.ID, title, note)
				created++
			}
			verb := "Imported"
			if dryRun {
				verb = "Would import"
			}
			fmt.Printf("%s %d document(s), skipped %d\n", verb, created, skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "default project when frontmatter omits it")
	cmd.Flags().StringVar(&docType, "doc-type", "writeup", "default doc_type when frontmatter omits it")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "parse and report without writing")
	return cmd
}

func firstH1(body string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(t[2:])
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
