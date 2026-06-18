package document

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jasondostal/rill/internal/memory"
)

func TestPutInputValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      PutInput
		wantErr bool
	}{
		{"ok minimal", PutInput{Title: "Hello"}, false},
		{"missing title", PutInput{Content: "body"}, true},
		{"blank title", PutInput{Title: "   "}, true},
		{"ok bare entity with type", PutInput{Title: "T", Entities: []EntityAssoc{{Name: "rill", Type: memory.EntityProject}}}, false},
		{"bare entity missing type", PutInput{Title: "T", Entities: []EntityAssoc{{Name: "rill"}}}, true},
		{"bad entity type", PutInput{Title: "T", Entities: []EntityAssoc{{Name: "rill", Type: memory.EntityType("widget")}}}, true},
		{"record-id entity needs no type", PutInput{Title: "T", Entities: []EntityAssoc{{Name: "project:rill"}}}, false},
		{"empty entity name", PutInput{Title: "T", Entities: []EntityAssoc{{Name: "", Type: memory.EntityProject}}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
			// Validation failures must classify as user-facing (ErrInvalidPayload).
			if err != nil && !memory.IsUserFacing(err) {
				t.Errorf("validation error not user-facing: %v", err)
			}
		})
	}
}

func TestCanonicalDocID(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"abc123", "document:abc123", false},                // bare auto-id suffix → prefixed
		{"document:abc123", "document:abc123", false},       // already full
		{"document:8f3a-2b1c", "document:8f3a-2b1c", false}, // hyphenated id ok
		{"", "", true},                         // empty
		{"foo; DROP TABLE document", "", true}, // injection attempt
		{"memory:abc", "", true},               // wrong table
	}
	for _, tc := range tests {
		got, err := canonicalDocID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("canonicalDocID(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if err == nil && got != tc.want {
			t.Errorf("canonicalDocID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateEntityRecordID(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"project:rill", false},
		{"tool:pi", false},
		{"person:ada_lovelace", false},
		{"widget:foo", true},     // unknown type
		{"rill", true},           // no table
		{"tool:foo; DROP", true}, // injection
	}
	for _, tc := range tests {
		err := validateEntityRecordID(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateEntityRecordID(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}

func TestRenderMarkdown(t *testing.T) {
	d := &Document{
		ID:        "document:`20260526T120000.000000000Z`",
		Title:     "My Primer",
		Content:   "# Heading\n\nBody text.",
		DocType:   "primer",
		Project:   "rill",
		Source:    "import",
		CreatedAt: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		Entities:  []EntityRef{{ID: "project:rill", Name: "rill", Type: "project"}},
	}
	out := RenderMarkdown(d)
	for _, want := range []string{
		"---\n",
		`title: "My Primer"`,
		`doc_type: "primer"`,
		`project: "rill"`,
		`source: "import"`,
		`entities: ["project:rill"]`,
		"# Heading",
		"Body text.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderMarkdown output missing %q\n---\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("RenderMarkdown output must end with newline")
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	d := &Document{
		ID:        "document:abc123",
		Title:     "Round Trip",
		Content:   "# Heading\n\nBody with a list:\n\n- one\n- two\n",
		DocType:   "reference",
		Project:   "rill",
		Source:    "import",
		CreatedAt: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		Entities:  []EntityRef{{ID: "project:rill"}, {ID: "tool:pi"}},
	}
	fm, body := ParseMarkdown(RenderMarkdown(d))
	if fm.RillID != d.ID || fm.Title != d.Title || fm.DocType != d.DocType ||
		fm.Project != d.Project || fm.Source != d.Source {
		t.Errorf("frontmatter mismatch: %+v", fm)
	}
	if len(fm.Entities) != 2 || fm.Entities[0] != "project:rill" || fm.Entities[1] != "tool:pi" {
		t.Errorf("entities round-trip mismatch: %v", fm.Entities)
	}
	if strings.TrimRight(body, "\n") != strings.TrimRight(d.Content, "\n") {
		t.Errorf("body round-trip mismatch:\n got: %q\nwant: %q", body, d.Content)
	}
}

func TestPutInputDecodesTimestamps(t *testing.T) {
	// External-export shape: RFC3339 with offset must unmarshal into *time.Time.
	raw := `{"title":"T","content":"x","created_at":"2026-04-30T23:48:17.505495+00:00"}`
	var p PutInput
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.CreatedAt == nil {
		t.Fatal("created_at did not decode")
	}
	if got := p.CreatedAt.UTC().Format(time.RFC3339); got != "2026-04-30T23:48:17Z" {
		t.Errorf("created_at = %s", got)
	}
	if p.UpdatedAt != nil {
		t.Error("updated_at should be nil when absent")
	}
}

func TestParseMarkdownNoFrontmatter(t *testing.T) {
	in := "# Just markdown\n\nNo frontmatter here.\n"
	fm, body := ParseMarkdown(in)
	if fm.Title != "" || fm.RillID != "" {
		t.Errorf("expected zero frontmatter, got %+v", fm)
	}
	if body != in {
		t.Errorf("body should equal input when no frontmatter:\n got: %q\nwant: %q", body, in)
	}
}
