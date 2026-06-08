package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/jasondostal/rill/internal/db"
)

// TestRequestID_FormatAndUniqueness asserts the request-ID generator returns
// short hex strings and produces distinct values across calls.
func TestRequestID_FormatAndUniqueness(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-f]+$`)

	id1 := requestID()
	id2 := requestID()

	if id1 == "" || id2 == "" {
		t.Fatalf("requestID returned empty string: %q %q", id1, id2)
	}
	// 6 bytes hex encoded → 12 chars (or "unknown" on RNG failure).
	if id1 != "unknown" && len(id1) != 12 {
		t.Errorf("requestID length = %d, want 12 (or 'unknown'), got %q", len(id1), id1)
	}
	if id1 != "unknown" && !hexRe.MatchString(id1) {
		t.Errorf("requestID = %q, not lower-case hex", id1)
	}
	if id1 == id2 {
		t.Errorf("requestID returned duplicate %q twice — RNG looks dead", id1)
	}
}

// decodeError pulls the canonical errorResponse out of a httptest recorder.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var body errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v (raw=%q)", err, rec.Body.String())
	}
	return body
}

func TestWriteBadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	writeBadRequest(rec, "field x missing")

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", body.Code)
	}
	if body.Error != "field x missing" {
		t.Errorf("error = %q, want 'field x missing'", body.Error)
	}
}

func TestWriteBadRequest_DefaultsEmptyMsg(t *testing.T) {
	rec := httptest.NewRecorder()
	writeBadRequest(rec, "")

	body := decodeError(t, rec)
	if body.Error != "bad request" {
		t.Errorf("empty msg should default; got %q", body.Error)
	}
}

func TestWriteValidationError_IncludesDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	writeValidationError(rec, "validation failed", map[string]any{"field": "username", "reason": "too short"})

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "validation_error" {
		t.Errorf("code = %q, want validation_error", body.Code)
	}
	if body.Details["field"] != "username" {
		t.Errorf("details.field missing or wrong: %v", body.Details)
	}
}

func TestWriteUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUnauthorized(rec, "")

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "unauthorized" {
		t.Errorf("code = %q, want unauthorized", body.Code)
	}
	if body.Error != "unauthenticated" {
		t.Errorf("empty msg should default to 'unauthenticated'; got %q", body.Error)
	}
}

func TestWriteForbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	writeForbidden(rec, "")

	if rec.Code != 403 {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", body.Code)
	}
}

func TestWriteForbiddenScope_IncludesRequiredScopeDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeForbiddenScope(rec, "admin")

	if rec.Code != 403 {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "insufficient_scope" {
		t.Errorf("code = %q, want insufficient_scope", body.Code)
	}
	if body.Details["required_scope"] != "admin" {
		t.Errorf("details.required_scope = %v, want 'admin'", body.Details["required_scope"])
	}
}

func TestWriteNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNotFound(rec, "memory not found")

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Code)
	}
	if body.Error != "memory not found" {
		t.Errorf("error = %q, want 'memory not found'", body.Error)
	}
}

func TestWriteMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	writeMethodNotAllowed(rec)

	if rec.Code != 405 {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "method_not_allowed" {
		t.Errorf("code = %q, want method_not_allowed", body.Code)
	}
}

func TestWriteConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	writeConflict(rec, "user already exists")

	if rec.Code != 409 {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "conflict" {
		t.Errorf("code = %q, want conflict", body.Code)
	}
}

func TestWriteRateLimited(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRateLimited(rec)

	if rec.Code != 429 {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "rate_limited" {
		t.Errorf("code = %q, want rate_limited", body.Code)
	}
}

// TestWriteInternalError_SanitizesAndGeneratesRequestID is the critical one:
// the body must NOT leak the underlying err to the client, and a request_id
// must be present so server-side log lines can be correlated.
func TestWriteInternalError_SanitizesAndGeneratesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, "handler_x", errors.New("secret: db password leak"), "")

	if rec.Code != 500 {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", body.Code)
	}
	if body.Error != "internal error" {
		t.Errorf("body should be sanitized; got error=%q", body.Error)
	}
	// Critical: underlying err string MUST NOT appear in the response body.
	if rec.Body.String() != "" && contains(rec.Body.String(), "secret") {
		t.Errorf("body leaked underlying err: %q", rec.Body.String())
	}
	if body.RequestID == "" {
		t.Error("RequestID was empty; need it to correlate server logs")
	}
}

func TestWriteInternalError_HonorsCallerRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	writeInternalError(rec, "handler_x", errors.New("boom"), "rid-from-handler")
	body := decodeError(t, rec)
	if body.RequestID != "rid-from-handler" {
		t.Errorf("RequestID = %q, want rid-from-handler", body.RequestID)
	}
}

// TestClassifyDBError_Sentinels — this is the regression guard for the
// entity_merge 500→400 fix. If anyone reverts the sentinel-wrapping in
// db.RequireTable or the errors.Is dispatch in classifyDBError, this fails.
func TestClassifyDBError_Sentinels(t *testing.T) {
	t.Run("nil err returns false and writes nothing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if classifyDBError(rec, nil) {
			t.Error("classifyDBError(nil) should return false")
		}
		if rec.Code != 200 {
			t.Errorf("nil err shouldn't write; got status %d", rec.Code)
		}
	})

	t.Run("ErrInvalidRecordID maps to 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		err := fmt.Errorf("%w: blank string", db.ErrInvalidRecordID)
		if !classifyDBError(rec, err) {
			t.Fatal("classifyDBError should return true for ErrInvalidRecordID")
		}
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		body := decodeError(t, rec)
		if body.Code != "bad_request" {
			t.Errorf("code = %q, want bad_request", body.Code)
		}
	})

	t.Run("ErrWrongTable maps to 400", func(t *testing.T) {
		rec := httptest.NewRecorder()
		err := fmt.Errorf("%w: expected entity, got auth_token", db.ErrWrongTable)
		if !classifyDBError(rec, err) {
			t.Fatal("classifyDBError should return true for ErrWrongTable")
		}
		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("unrelated err returns false", func(t *testing.T) {
		rec := httptest.NewRecorder()
		err := errors.New("something else")
		if classifyDBError(rec, err) {
			t.Error("classifyDBError should return false for unrelated errors")
		}
		if rec.Code != 200 {
			t.Errorf("unrelated err shouldn't write; got status %d", rec.Code)
		}
	})
}

// contains is strings.Contains without the import (tiny helper for the body-leak check).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
