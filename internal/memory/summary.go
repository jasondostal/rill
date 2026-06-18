package memory

import (
	"strings"
	"unicode/utf8"
)

// summaryTarget is the SOFT length budget (in runes) for a memory summary — the
// embedded, atomic claim. It is deliberately NOT a hard gate: a summary over
// budget is trimmed to a clean boundary and the remainder spills into details,
// so a write never fails just for running a little long. Keeping the embedded
// summary focused (~3–4 sentences) makes vector recall sharper; the full text
// is never lost because it moves into the FTS-indexed details. ~600 runes.
const summaryTarget = 600

// normalizeSummary enforces the soft summary budget by SPILLING, not rejecting.
// If the summary fits, it and the details pass through unchanged. If it's over
// budget it's split at the nearest sentence (then word, then hard) boundary at
// or before the budget: the head stays as the summary, and the tail is
// prepended to details (separated by a blank line from any existing details).
// Returns the adjusted summary, adjusted details, and whether a spill happened.
func normalizeSummary(summary, details string) (string, string, bool) {
	if utf8.RuneCountInString(summary) <= summaryTarget {
		return summary, details, false
	}
	head, tail := splitAtBoundary(summary, summaryTarget)
	if tail == "" {
		return head, details, head != summary
	}
	if strings.TrimSpace(details) == "" {
		return head, tail, true
	}
	return head, tail + "\n\n" + details, true
}

// splitAtBoundary splits s into a head of at most max runes and the remaining
// tail. It prefers the last sentence terminator at or before the budget, then
// the last word boundary, then a hard cut. It never searches earlier than half
// the budget, so a punctuation-free or space-free run still yields a usefully
// long head instead of collapsing to a fragment.
func splitAtBoundary(s string, max int) (head, tail string) {
	r := []rune(s)
	if len(r) <= max {
		return s, ""
	}
	floor := max / 2
	cut := -1

	// Prefer a sentence terminator (keep it with the head).
	for i := max - 1; i >= floor; i-- {
		if r[i] == '.' || r[i] == '!' || r[i] == '?' {
			cut = i + 1
			break
		}
	}
	// Else fall back to a word boundary.
	if cut < 0 {
		for i := max - 1; i >= floor; i-- {
			if r[i] == ' ' {
				cut = i
				break
			}
		}
	}
	// Else hard-cut at the budget.
	if cut < 0 {
		cut = max
	}

	head = strings.TrimSpace(string(r[:cut]))
	tail = strings.TrimSpace(string(r[cut:]))
	return head, tail
}
