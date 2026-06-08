package document

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"
)

// RenderMarkdown serializes a Document to a complete .md file: YAML frontmatter
// (so metadata round-trips through export/import) followed by the body. The
// frontmatter is intentionally simple scalars + one inline list — vim, Obsidian,
// and Hugo all parse it, and ImportMarkdown reads it back.
func RenderMarkdown(d *Document) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "rill_id: %q\n", d.ID)
	fmt.Fprintf(&b, "title: %q\n", d.Title)
	fmt.Fprintf(&b, "doc_type: %q\n", d.DocType)
	if d.Project != "" {
		fmt.Fprintf(&b, "project: %q\n", d.Project)
	}
	if d.Source != "" {
		fmt.Fprintf(&b, "source: %q\n", d.Source)
	}
	if len(d.Entities) > 0 {
		b.WriteString("entities: [")
		for i, e := range d.Entities {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", e.ID)
		}
		b.WriteString("]\n")
	}
	fmt.Fprintf(&b, "created_at: %q\n", d.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "updated_at: %q\n", d.UpdatedAt.UTC().Format(time.RFC3339))
	b.WriteString("---\n\n")
	b.WriteString(d.Content)
	if !strings.HasSuffix(d.Content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// Frontmatter is the parsed metadata block from an exported .md file.
type Frontmatter struct {
	RillID   string
	Title    string
	DocType  string
	Project  string
	Source   string
	Entities []string // entity record ids
}

// ParseMarkdown splits a .md file into frontmatter + body. With no leading
// "---" fence it returns a zero Frontmatter and the whole input as the body, so
// plain markdown (no frontmatter) imports cleanly. Round-trips RenderMarkdown.
func ParseMarkdown(input string) (Frontmatter, string) {
	var fm Frontmatter
	s := bufio.NewScanner(strings.NewReader(input))
	s.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // tolerate very long lines

	if !s.Scan() {
		return fm, input
	}
	if strings.TrimSpace(s.Text()) != "---" {
		return fm, input // no frontmatter
	}

	var body strings.Builder
	inFM := true
	skipBlank := false
	for s.Scan() {
		line := s.Text()
		if inFM {
			if strings.TrimSpace(line) == "---" {
				inFM = false
				skipBlank = true // RenderMarkdown writes one blank line after the fence
				continue
			}
			applyFrontmatterLine(&fm, line)
			continue
		}
		if skipBlank {
			skipBlank = false
			if strings.TrimSpace(line) == "" {
				continue
			}
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	return fm, body.String()
}

func applyFrontmatterLine(fm *Frontmatter, line string) {
	key, val, ok := strings.Cut(line, ":")
	if !ok {
		return
	}
	key = strings.TrimSpace(key)
	val = strings.TrimSpace(val)
	switch key {
	case "rill_id":
		fm.RillID = unquote(val)
	case "title":
		fm.Title = unquote(val)
	case "doc_type":
		fm.DocType = unquote(val)
	case "project":
		fm.Project = unquote(val)
	case "source":
		fm.Source = unquote(val)
	case "entities":
		fm.Entities = parseInlineList(val)
	}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = s[1 : len(s)-1]
	}
	return s
}

func parseInlineList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := unquote(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ExportMarkdown fetches a document and returns its .md representation plus the
// loaded Document (for filename derivation by the caller).
func (s *Store) ExportMarkdown(ctx context.Context, id string) (string, *Document, error) {
	doc, err := s.Get(ctx, id)
	if err != nil {
		return "", nil, err
	}
	if doc == nil {
		return "", nil, notFound("document %s not found", id)
	}
	return RenderMarkdown(doc), doc, nil
}
