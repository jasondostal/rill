package memory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Error taxonomy ------------------------------------------------------------
//
// Store methods classify failures into a small set of sentinels so the REST,
// MCP, and CLI layers can map them to the right status/code WITHOUT parsing
// message strings. Every sentinel here is "user-facing": its message is safe
// to return to the caller (it names what the caller did wrong, not internals).
// Anything that does NOT wrap one of these is treated as internal and sanitized.
//
// ErrInvalidPayload lives in types.go (kept there to avoid churn) and is part
// of this same set — Classify() treats it as KindInvalid.
var (
	// ErrConstraint — the write violated a database constraint the caller can
	// fix by changing input: a relation endpoint of the wrong entity type, an
	// ASSERT failure, etc. Distinct from ErrInvalidPayload only in that the
	// database — not our Go validation — rejected it.
	ErrConstraint = errors.New("constraint violation")
	// ErrNotFound — a referenced record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict — the write conflicts with existing state (duplicate / already exists).
	ErrConflict = errors.New("conflict")
)

// errs constructs an error wrapping ErrInvalidPayload.
func errs(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidPayload}, args...)...)
}

// ErrorKind is the coarse category the API layers map to a status / JSON-RPC code.
type ErrorKind int

const (
	// KindInternal — unclassified. Sanitize the message, log it, return 500 / -32603.
	KindInternal ErrorKind = iota
	// KindInvalid — caller input failed validation. 400 / user-facing.
	KindInvalid
	// KindConstraint — the DB rejected the write for a caller-fixable reason. 422 / user-facing.
	KindConstraint
	// KindNotFound — referenced record missing. 404 / user-facing.
	KindNotFound
	// KindConflict — conflicts with existing state. 409 / user-facing.
	KindConflict
)

// Classify maps an error to its kind by walking the sentinel chain (errors.Is,
// so wrapped errors still classify). REST and MCP share this so a given Store
// error surfaces as the same category on every transport.
func Classify(err error) ErrorKind {
	switch {
	case err == nil:
		return KindInternal
	case errors.Is(err, ErrInvalidPayload):
		return KindInvalid
	case errors.Is(err, ErrConstraint):
		return KindConstraint
	case errors.Is(err, ErrNotFound):
		return KindNotFound
	case errors.Is(err, ErrConflict):
		return KindConflict
	default:
		return KindInternal
	}
}

// IsUserFacing reports whether the error's message is safe to surface verbatim.
func IsUserFacing(err error) bool { return Classify(err) != KindInternal }

// --- SurrealDB statement-error classification ------------------------------

// coerceRe matches SurrealDB's field-coercion error, e.g.
//   Couldn't coerce value for field `in` of `uses:abc`: Expected `record<person>` but found `tool:slack`
var coerceRe = regexp.MustCompile("coerce value for field `([^`]+)` of `[^`]+`: Expected `([^`]+)` but found `([^`]+)`")

// classifyStmtErr turns a raw SurrealDB statement-error string into a typed,
// cleaned error. Recognized shapes become user-facing sentinels with a tidy
// message; anything unrecognized stays internal (we never guess a user-facing
// class we're unsure about — that would risk leaking internals).
func classifyStmtErr(raw string) error {
	switch {
	case coerceRe.MatchString(raw):
		m := coerceRe.FindStringSubmatch(raw)
		field, expected, found := m[1], m[2], m[3]
		if field == "in" || field == "out" {
			endpoint := "subject"
			if field == "out" {
				endpoint = "object"
			}
			return fmt.Errorf("%w: edge %s %q is not allowed for this relation (expected %s)",
				ErrConstraint, endpoint, found, expected)
		}
		return fmt.Errorf("%w: field %q got %q, expected %s", ErrConstraint, field, found, expected)
	case strings.Contains(raw, "ASSERT") || strings.Contains(raw, "assertion"):
		return fmt.Errorf("%w: a value failed a validation rule (%s)", ErrConstraint, raw)
	case strings.Contains(raw, "already exists") || strings.Contains(raw, "already contains"):
		return fmt.Errorf("%w: %s", ErrConflict, raw)
	default:
		// Unrecognized — keep the raw text for the server log; stays internal.
		return fmt.Errorf("surreal: %s", raw)
	}
}
