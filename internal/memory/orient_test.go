package memory

import (
	"strings"
	"testing"
)

func TestCompactCardForOrient(t *testing.T) {
	longFact := strings.Repeat("alpha bravo charlie delta ", 20) // ~520 chars
	card := strings.Join([]string{
		"## Identity",
		"- " + longFact + " _(claude, 2026-06-18)_",
		"",
		"## Active edges",
		"- uses → **Claude Design** _(tool)_",
		"",
		"## Preferences",
		"- _(positive)_ **cave diving**",
		"",
		"## Facts",
		"- " + longFact + " _(claude, 2026-06-12)_",
		"- short fact stays whole _(claude, 2026-06-01)_",
		"",
		"## Decisions",
		"- " + longFact + " _(claude, 2026-06-15)_",
	}, "\n")

	got := compactCardForOrient(card, 200)

	// max <= 0 is a no-op.
	if compactCardForOrient(card, 0) != card {
		t.Fatalf("max=0 should return the card verbatim")
	}

	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 260 {
			t.Errorf("line not truncated (%d runes): %q", len([]rune(line)), line)
		}
	}

	// Attribution survives truncation on the prose sections.
	for _, suffix := range []string{"_(claude, 2026-06-18)_", "_(claude, 2026-06-12)_", "_(claude, 2026-06-15)_"} {
		if !strings.Contains(got, suffix) {
			t.Errorf("attribution %q dropped after truncation", suffix)
		}
	}

	// Terse sections pass through untouched.
	for _, keep := range []string{
		"- uses → **Claude Design** _(tool)_",
		"- _(positive)_ **cave diving**",
		"- short fact stays whole _(claude, 2026-06-01)_",
		"## Identity", "## Active edges", "## Preferences", "## Facts", "## Decisions",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("expected untouched line/header missing: %q", keep)
		}
	}
}
