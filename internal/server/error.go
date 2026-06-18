package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/jasondostal/rill/internal/memory"
)

// requestID generates a short random hex string for tracing errors.
func requestID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// errorResponse is the canonical REST error envelope.
// All HTTP error responses share this shape so clients can rely on a single
// parser. `code` is a stable, machine-readable identifier; `error` is a
// human-readable message. `request_id` and `details` are optional.
type errorResponse struct {
	Error     string         `json:"error"`
	Code      string         `json:"code"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// writeError is the low-level writer. Callers should prefer the typed helpers
// below (writeBadRequest, writeInternalError, etc.) which fix the code and
// status pairing.
func writeError(w http.ResponseWriter, status int, code, msg string, reqID string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error:     msg,
		Code:      code,
		RequestID: reqID,
		Details:   details,
	})
}

// writeBadRequest writes a 400 with code "bad_request".
func writeBadRequest(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "bad request"
	}
	writeError(w, http.StatusBadRequest, "bad_request", msg, "", nil)
}

// writeValidationError writes a 400 with code "validation_error" and an
// optional details object naming the offending field/value. Use sparingly —
// only when you have a specific field to name.
func writeValidationError(w http.ResponseWriter, msg string, details map[string]any) {
	if msg == "" {
		msg = "validation failed"
	}
	writeError(w, http.StatusBadRequest, "validation_error", msg, "", details)
}

// writeUnauthorized writes a 401 with code "unauthorized".
func writeUnauthorized(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "unauthenticated"
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", msg, "", nil)
}

// writeForbidden writes a 403 with code "forbidden".
func writeForbidden(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "forbidden"
	}
	writeError(w, http.StatusForbidden, "forbidden", msg, "", nil)
}

// writeForbiddenScope writes a 403 specific to a missing bearer scope.
// `details` includes `required_scope` for clients to surface to operators.
func writeForbiddenScope(w http.ResponseWriter, requiredScope string) {
	writeError(w, http.StatusForbidden, "insufficient_scope",
		requiredScope+" scope required", "",
		map[string]any{"required_scope": requiredScope})
}

// writeNotFound writes a 404 with code "not_found".
func writeNotFound(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "not found"
	}
	writeError(w, http.StatusNotFound, "not_found", msg, "", nil)
}

// writeMethodNotAllowed writes a 405 with code "method_not_allowed".
func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
}

// writeConflict writes a 409 with code "conflict".
func writeConflict(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "conflict"
	}
	writeError(w, http.StatusConflict, "conflict", msg, "", nil)
}

// writeRateLimited writes a 429 with code "rate_limited".
func writeRateLimited(w http.ResponseWriter) {
	writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded", "", nil)
}

// writeInternalError logs the underlying error server-side with the request ID
// and writes a sanitized 500 to the client. `where` is a short label
// (e.g. "recall", "entity_merge") used in the log line for grep-ability.
func writeInternalError(w http.ResponseWriter, where string, err error, reqID string) {
	if reqID == "" {
		reqID = requestID()
	}
	rilllog.Logger().Error(where+" error", "req_id", reqID, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error", reqID, nil)
}

// classifyDBError inspects err for db.ErrInvalidRecordID / db.ErrWrongTable
// sentinels (raised when a caller passes a malformed record ID or one pointing
// at the wrong table, e.g. `auth_token:foo` to an entity endpoint).
// Returns true if it wrote a 400 response so callers can short-circuit
// instead of falling through to writeInternalError. Returns false otherwise —
// caller should keep going.
func classifyDBError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, db.ErrInvalidRecordID):
		writeBadRequest(w, "invalid record id")
		return true
	case errors.Is(err, db.ErrWrongTable):
		writeBadRequest(w, "record id refers to wrong table")
		return true
	}
	return false
}

// writeStoreError is the single entry point REST handlers use for errors
// returned by the memory.Store. It classifies the error and writes the matching
// response: malformed/wrong-table record IDs → 400; the user-facing service
// categories (validation, constraint, not-found, conflict) return their real
// message + a stable code; anything else is logged and sanitized via
// writeInternalError (so internal details never leak). Behavior for genuinely
// internal errors is identical to calling writeInternalError directly.
func writeStoreError(w http.ResponseWriter, where string, err error, reqID string) {
	if classifyDBError(w, err) {
		return
	}
	switch memory.Classify(err) {
	case memory.KindInvalid:
		writeError(w, http.StatusBadRequest, "validation_error", err.Error(), reqID, nil)
	case memory.KindConstraint:
		writeError(w, http.StatusUnprocessableEntity, "constraint_violation", err.Error(), reqID, nil)
	case memory.KindNotFound:
		writeError(w, http.StatusNotFound, "not_found", err.Error(), reqID, nil)
	case memory.KindConflict:
		writeError(w, http.StatusConflict, "conflict", err.Error(), reqID, nil)
	default:
		writeInternalError(w, where, err, reqID)
	}
}
