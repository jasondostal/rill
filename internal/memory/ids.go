package memory

import (
	"regexp"
	"strings"
)

var (
	reIDTable = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	reIDBare  = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
)

// safeRecordID validates that id is a well-formed SurrealDB record id
// (`table:idpart`) that is safe to interpolate into a query string. The store
// builds many statements with fmt.Sprintf("… %s …", recID); since s.db.SQL runs
// multi-statement queries, an unvalidated user-supplied id/ref is a SurrealQL
// injection vector (e.g. `uses:x; DELETE memory; --`). This guard is the
// chokepoint for every record id that originates from a caller.
//
// Accepts exactly two id shapes:
//   - a bare identifier: [A-Za-z0-9_]+ (covers timestamps like 20260529T… and
//     hash ids), or
//   - a backtick-quoted literal with NO embedded backtick: `…` (covers ids with
//     dots/spaces, e.g. memory:`2026…Z` or person:`Alice Jones`) — the missing
//     embedded backtick is what makes it impossible to break out of the quoting.
//
// Everything else — spaces, ';', '--', quotes, an embedded backtick — is
// rejected. table must be a lowercase identifier.
func safeRecordID(id string) error {
	i := strings.IndexByte(id, ':')
	if i <= 0 {
		return errs("invalid record id %q (expected table:id)", id)
	}
	table, rest := id[:i], id[i+1:]
	if !reIDTable.MatchString(table) {
		return errs("invalid record id %q (bad table)", id)
	}
	if rest == "" {
		return errs("invalid record id %q (empty id)", id)
	}
	if reIDBare.MatchString(rest) {
		return nil
	}
	if len(rest) >= 2 && rest[0] == '`' && rest[len(rest)-1] == '`' &&
		!strings.Contains(rest[1:len(rest)-1], "`") {
		return nil
	}
	return errs("invalid record id %q (unsafe id form)", id)
}
