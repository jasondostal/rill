package memory

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeSummary_ShortIsUnchanged(t *testing.T) {
	s, d, spilled := normalizeSummary("A short atomic claim.", "some details")
	if spilled {
		t.Fatal("short summary should not spill")
	}
	if s != "A short atomic claim." || d != "some details" {
		t.Fatalf("short summary/details must pass through unchanged, got %q / %q", s, d)
	}
}

func TestNormalizeSummary_SpillsIntoEmptyDetails(t *testing.T) {
	long := strings.Repeat("This is one sentence of the claim. ", 30) // ~1050 chars, sentence boundaries
	s, d, spilled := normalizeSummary(long, "")
	if !spilled {
		t.Fatal("over-budget summary should spill")
	}
	if n := utf8.RuneCountInString(s); n > summaryTarget {
		t.Fatalf("trimmed summary must be <= %d runes, got %d", summaryTarget, n)
	}
	if d == "" {
		t.Fatal("spilled remainder must land in details")
	}
	// Nothing lost: the original (whitespace-normalized) is the head followed by
	// the tail.
	if !strings.HasPrefix(strings.TrimSpace(long), s) {
		t.Errorf("trimmed summary should be a prefix of the original")
	}
	if !strings.HasSuffix(strings.TrimSpace(long), strings.TrimSpace(d)) {
		t.Errorf("spilled details should be the suffix of the original")
	}
}

func TestNormalizeSummary_PrependsToExistingDetails(t *testing.T) {
	long := strings.Repeat("Sentence here. ", 60) // well over budget
	s, d, spilled := normalizeSummary(long, "ORIGINAL DETAILS")
	if !spilled {
		t.Fatal("should spill")
	}
	if !strings.HasSuffix(d, "ORIGINAL DETAILS") {
		t.Errorf("existing details must be preserved at the end, got %q", d)
	}
	if !strings.Contains(d, "\n\n") {
		t.Errorf("spilled text should be separated from existing details by a blank line")
	}
	if utf8.RuneCountInString(s) > summaryTarget {
		t.Errorf("summary still over budget")
	}
}

func TestSplitAtBoundary_PrefersSentence(t *testing.T) {
	s := strings.Repeat("Word ", 200) + "END. " + strings.Repeat("more ", 200)
	// Force a small budget so the sentence terminator is the obvious cut.
	head, tail := splitAtBoundary(s, utf8.RuneCountInString(strings.Repeat("Word ", 200)+"END."))
	if !strings.HasSuffix(head, ".") {
		t.Errorf("head should end at the sentence terminator, got tail-end %q", head[len(head)-5:])
	}
	if tail == "" {
		t.Error("tail should be non-empty")
	}
}

func TestSplitAtBoundary_WordWhenNoSentence(t *testing.T) {
	s := strings.Repeat("alpha bravo ", 100) // no sentence punctuation, has spaces
	head, tail := splitAtBoundary(s, 50)
	if utf8.RuneCountInString(head) > 50 {
		t.Fatalf("head must be <= 50 runes, got %d", utf8.RuneCountInString(head))
	}
	if strings.HasSuffix(head, "alph") || strings.HasSuffix(head, "brav") {
		t.Errorf("head must not cut mid-word, got %q", head)
	}
	if tail == "" {
		t.Error("tail should be non-empty")
	}
}

func TestSplitAtBoundary_HardCutNoSpaces(t *testing.T) {
	s := strings.Repeat("x", 700) // no boundaries at all
	head, tail := splitAtBoundary(s, 600)
	if utf8.RuneCountInString(head) != 600 {
		t.Fatalf("hard cut should keep exactly 600 runes, got %d", utf8.RuneCountInString(head))
	}
	if utf8.RuneCountInString(tail) != 100 {
		t.Fatalf("tail should hold the remaining 100 runes, got %d", utf8.RuneCountInString(tail))
	}
}

func TestSplitAtBoundary_MultibyteRuneCount(t *testing.T) {
	s := strings.Repeat("é", 700) // 700 runes, 1400 bytes
	head, tail := splitAtBoundary(s, 600)
	if utf8.RuneCountInString(head) != 600 {
		t.Fatalf("rune (not byte) counting: head should be 600 runes, got %d", utf8.RuneCountInString(head))
	}
	if utf8.RuneCountInString(tail) != 100 {
		t.Fatalf("tail should be 100 runes, got %d", utf8.RuneCountInString(tail))
	}
}
