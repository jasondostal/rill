package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jasondostal/rill/internal/db"
	rilllog "github.com/jasondostal/rill/internal/log"
)

// requestID generates a short random hex string for tracing errors.
func requestID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// errorResponse is the canonical REST error envelope shared across packages.
// Kept identical to internal/server/errorResponse so clients see one shape.
type errorResponse struct {
	Error     string         `json:"error"`
	Code      string         `json:"code"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, msg, reqID string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error:     msg,
		Code:      code,
		RequestID: reqID,
		Details:   details,
	})
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "bad request"
	}
	writeError(w, http.StatusBadRequest, "bad_request", msg, "", nil)
}

func writeValidationError(w http.ResponseWriter, msg string, details map[string]any) {
	if msg == "" {
		msg = "validation failed"
	}
	writeError(w, http.StatusBadRequest, "validation_error", msg, "", details)
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "unauthenticated"
	}
	writeError(w, http.StatusUnauthorized, "unauthorized", msg, "", nil)
}

func writeForbidden(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "forbidden"
	}
	writeError(w, http.StatusForbidden, "forbidden", msg, "", nil)
}

func writeForbiddenScope(w http.ResponseWriter, requiredScope string) {
	writeError(w, http.StatusForbidden, "insufficient_scope",
		requiredScope+" scope required", "",
		map[string]any{"required_scope": requiredScope})
}

func writeNotFound(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "not found"
	}
	writeError(w, http.StatusNotFound, "not_found", msg, "", nil)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "", nil)
}

func writeConflict(w http.ResponseWriter, msg string) {
	if msg == "" {
		msg = "conflict"
	}
	writeError(w, http.StatusConflict, "conflict", msg, "", nil)
}

func writeRateLimited(w http.ResponseWriter) {
	writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded", "", nil)
}

// writeInternalError logs the underlying error and writes a sanitized 500.
// Mirrors internal/server.writeInternalError — auth needs its own copy to
// avoid an import cycle (server imports auth, so auth can't import server).
func writeInternalError(w http.ResponseWriter, where string, err error, reqID string) {
	if reqID == "" {
		reqID = requestID()
	}
	rilllog.Logger().Error(where+" error", "req_id", reqID, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error", reqID, nil)
}

// classifyDBError handles db.ErrInvalidRecordID / db.ErrWrongTable sentinels.
// Returns true if a response was written.
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
