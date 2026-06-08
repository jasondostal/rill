package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// planEdge applies the consolidation rules (via reads) and returns the ordered
// write statements plus the resulting EdgeRef — WITHOUT executing anything. New
// edges are given a Go-generated id so the statements can run inside a single
// transaction and still reference the new edge (for the superseded_by
// back-pointer) without parsing RETURN results.
//
// A duplicate-tuple call returns the existing edge's ref + a single weight-bump
// statement. The statements must be executed in the order returned.
func (s *Store) planEdge(ctx context.Context, edge EdgeDecl, subjID, objID, sourceMemID string, ts time.Time) (*EdgeRef, []string, error) {
	tableName, generic := EdgeTableFor(edge.Predicate)
	weight := edge.Weight
	if weight == 0 {
		weight = 1.0
	}

	// 1) Exact-tuple duplicate — bump weight and return (no new edge).
	// For `prefers`, the "tuple" includes valence so a valence flip falls
	// through to the supersession path instead of being treated as a dup.
	dupID, err := s.findActiveEdge(ctx, tableName, generic, edge.Predicate, subjID, objID, string(edge.Valence))
	if err != nil {
		return nil, nil, err
	}
	if dupID != "" {
		// Only re-stamp `source` when this write has a backing memory. The
		// graph-only paths (AddEdge, MergeEntity re-point) pass an empty
		// sourceMemID; interpolating it bare would emit `source = ;` and fail
		// to parse, so omit the assignment and just bump the weight.
		setClause := "weight += 0.1"
		if sourceMemID != "" {
			setClause += ", source = " + sourceMemID
		}
		stmt := fmt.Sprintf(`UPDATE %s SET %s;`, dupID, setClause)
		return &EdgeRef{ID: dupID, Predicate: edge.Predicate, Subject: subjID, Object: objID}, []string{stmt}, nil
	}

	var stmts []string
	var supersededID string

	// 2) For exclusive predicates: close prior active edges with the same
	//    subject (different object).
	if IsExclusive(edge.Predicate) {
		prior, err := s.findExclusiveActive(ctx, tableName, generic, edge.Predicate, subjID, objID)
		if err != nil {
			return nil, nil, err
		}
		if prior != "" {
			stmts = append(stmts, fmt.Sprintf(`UPDATE %s SET valid_until = %s;`, prior, EscapeDatetime(ts)))
			supersededID = prior
		}
	}

	// 3) For prefers with a valence flip on the same (person, preference): close prior.
	if edge.Predicate == "prefers" && edge.Valence != "" {
		prior, err := s.findValenceFlipPrior(ctx, subjID, objID, string(edge.Valence))
		if err != nil {
			return nil, nil, err
		}
		if prior != "" {
			stmts = append(stmts, fmt.Sprintf(`UPDATE %s SET valid_until = %s;`, prior, EscapeDatetime(ts)))
			supersededID = prior
		}
	}

	// 4) Create the new edge with a Go-generated id.
	newEdgeID := tableName + ":" + newEdgeKey()
	stmts = append(stmts, buildEdgeCreate(newEdgeID, generic, edge, subjID, objID, sourceMemID, ts, weight))

	// 5) Point the superseded prior at the new edge (now that we know its id).
	if supersededID != "" {
		stmts = append(stmts, fmt.Sprintf(`UPDATE %s SET superseded_by = %s;`, supersededID, newEdgeID))
	}

	return &EdgeRef{
		ID:         newEdgeID,
		Predicate:  edge.Predicate,
		Subject:    subjID,
		Object:     objID,
		Superseded: supersededID,
	}, stmts, nil
}

// writeEdge plans + immediately executes the edge writes. Used by the graph-only
// paths (AddEdge, MergeEntity) that mutate outside a Remember transaction. The
// Remember pipeline instead collects planEdge's statements into one transaction.
func (s *Store) writeEdge(ctx context.Context, edge EdgeDecl, subjID, objID, sourceMemID string, ts time.Time) (*EdgeRef, error) {
	ref, stmts, err := s.planEdge(ctx, edge, subjID, objID, sourceMemID, ts)
	if err != nil {
		return nil, err
	}
	for _, stmt := range stmts {
		if _, err := s.db.SQL(ctx, stmt, true); err != nil {
			return nil, err
		}
	}
	return ref, nil
}

// newEdgeKey returns a random record-id segment for a new edge. Hex is always a
// valid id; the leading letter keeps an all-numeric id from being parsed as a
// number.
func newEdgeKey() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("e%d", time.Now().UnixNano())
	}
	return "e" + hex.EncodeToString(b)
}

// findActiveEdge returns the record id of an existing edge with the same
// (subject, predicate, object) tuple that is still active (valid_until IS NONE).
// For `prefers`, valence is part of the tuple (different valence != same edge).
// Empty string if none.
func (s *Store) findActiveEdge(ctx context.Context, table string, generic bool, predicate, subjID, objID, valence string) (string, error) {
	cond := fmt.Sprintf(`in = %s AND out = %s AND valid_until IS NONE`, subjID, objID)
	if generic {
		cond += fmt.Sprintf(` AND predicate = %s`, EscapeStr(predicate))
	}
	if predicate == "prefers" && valence != "" {
		cond += fmt.Sprintf(` AND valence = %s`, EscapeStr(valence))
	}
	stmt := fmt.Sprintf(`SELECT id FROM %s WHERE %s LIMIT 1;`, table, cond)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return "", err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return "", nil
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ID, nil
}

// findExclusiveActive returns the record id of a prior active edge with the
// same subject + same predicate but a different object. Used for the
// exclusive-predicate supersession step.
func (s *Store) findExclusiveActive(ctx context.Context, table string, generic bool, predicate, subjID, newObjID string) (string, error) {
	cond := fmt.Sprintf(`in = %s AND out != %s AND valid_until IS NONE`, subjID, newObjID)
	if generic {
		cond += fmt.Sprintf(` AND predicate = %s`, EscapeStr(predicate))
	}
	stmt := fmt.Sprintf(`SELECT id FROM %s WHERE %s LIMIT 1;`, table, cond)
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return "", err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return "", nil
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ID, nil
}

// findValenceFlipPrior returns the id of an active `prefers` edge from the
// same person to the same preference but with a DIFFERENT valence. Empty
// string if none. Used for valence-flip supersession.
func (s *Store) findValenceFlipPrior(ctx context.Context, subjID, objID, newValence string) (string, error) {
	stmt := fmt.Sprintf(`SELECT id FROM prefers
		WHERE in = %s AND out = %s
		AND valence != %s
		AND valid_until IS NONE
		LIMIT 1;`,
		subjID, objID, EscapeStr(newValence))
	res, err := s.db.SQL(ctx, stmt, true)
	if err != nil {
		return "", err
	}
	if len(res) == 0 || len(res[0].Result) == 0 {
		return "", nil
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res[0].Result, &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return rows[0].ID, nil
}

// buildEdgeCreate generates a RELATE statement that creates an edge with an
// explicit, caller-supplied id (so callers know the id without RETURN parsing,
// which lets the statement run inside a transaction).
func buildEdgeCreate(edgeID string, generic bool, edge EdgeDecl, subjID, objID, sourceMemID string, ts time.Time, weight float64) string {
	predicateClause := ""
	if generic {
		predicateClause = fmt.Sprintf("predicate = %s, ", EscapeStr(edge.Predicate))
	}
	valenceClause := ""
	if edge.Predicate == "prefers" && edge.Valence != "" {
		valenceClause = fmt.Sprintf("valence = %s, ", EscapeStr(string(edge.Valence)))
	}
	roleClause := ""
	if edge.Predicate == "works_at" && edge.RoleTitle != "" {
		roleClause = fmt.Sprintf("role_title = %s, ", EscapeStr(edge.RoleTitle))
	}
	// AddEdge passes "" — it has no source memory. Don't emit `source = ,`.
	sourceExpr := "NONE"
	if sourceMemID != "" {
		sourceExpr = sourceMemID
	}
	return fmt.Sprintf(`RELATE %s -> %s -> %s SET
		%s%s%sweight = %.3f,
		valid_from = %s,
		source = %s,
		created_at = %s;`,
		subjID, edgeID, objID,
		predicateClause, valenceClause, roleClause,
		weight,
		EscapeDatetime(ts),
		sourceExpr,
		EscapeDatetime(ts),
	)
}

// firstEdgeID extracts the id of the first edge from a RELATE ... RETURN AFTER result set.
func firstEdgeID(results []surrealResult) (string, error) {
	if len(results) == 0 {
		return "", nil
	}
	last := results[len(results)-1]
	if len(last.Result) == 0 {
		return "", nil
	}
	// Result may be either a single object or an array.
	trimmed := strings.TrimSpace(string(last.Result))
	if strings.HasPrefix(trimmed, "[") {
		var rows []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(last.Result, &rows); err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "", nil
		}
		return rows[0].ID, nil
	}
	var single struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(last.Result, &single); err != nil {
		return "", err
	}
	return single.ID, nil
}
