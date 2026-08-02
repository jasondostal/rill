package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jasondostal/rill/internal/auth"
	"github.com/jasondostal/rill/internal/mcp"
)

func TestInitialize(t *testing.T) {
	h := setupHandler()
	rec := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	var resp mcp.Response
	mustDecode(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
}

// TestPing guards the MCP keepalive reply. The response must carry a present,
// empty result object — a bare {"jsonrpc":"2.0","id":N} (result dropped by
// omitempty) is an invalid JSON-RPC response that strict clients reject, which
// showed up as Hermes "gateway heartbeat failed" reconnect loops.
func TestPing(t *testing.T) {
	h := setupHandler()
	rec := call(t, h, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)

	if body := rec.Body.String(); !strings.Contains(body, `"result"`) {
		t.Fatalf("ping response missing result field: %s", body)
	}

	var resp mcp.Response
	mustDecode(t, rec, &resp)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected empty result object, got %T", resp.Result)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestToolsList(t *testing.T) {
	h := setupHandler()
	rec := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	var resp mcp.Response
	mustDecode(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result map")
	}
	toolList, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}
	if len(toolList) != 1 {
		t.Errorf("expected 1 tool (test_write), got %d", len(toolList))
	}
}

func TestToolsListFullSchemas(t *testing.T) {
	h := setupHandler()
	rec := call(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	var resp mcp.Response
	mustDecode(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result map")
	}
	toolList, ok := result["tools"].([]any)
	if !ok || len(toolList) == 0 {
		t.Fatal("expected non-empty tools array")
	}
	for i, entry := range toolList {
		tool, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("tool[%d] is not a map", i)
		}
		if _, hasDesc := tool["description"]; !hasDesc {
			t.Errorf("tool[%d] missing description — tools/list must always return full definitions", i)
		}
		if _, hasSchema := tool["inputSchema"]; !hasSchema {
			t.Errorf("tool[%d] missing inputSchema — tools/list must always return full definitions", i)
		}
	}
}

func setupHandler() *mcp.Handler {
	return setupHandlerOpts(mcp.HandlerOpts{})
}

func setupHandlerOpts(opts mcp.HandlerOpts) *mcp.Handler {
	reg := mcp.NewRegistry()
	// Register a test tool that requires write scope for scope enforcement tests.
	reg.Register(&testWriteTool{})
	return mcp.NewHandler(reg, opts)
}

// testWriteTool is a tool that requires write scope for testing scope enforcement.
type testWriteTool struct{}

func (t *testWriteTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "test_write",
		Description:    "Test tool requiring write scope",
		RequiredScopes: []string{"write"},
		InputSchema:    map[string]any{"type": "object"},
	}
}

func (t *testWriteTool) Call(ctx context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"ok": true}, nil
}

func call(t *testing.T, h *mcp.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Requests reaching the handler have already been authenticated by the auth
	// middleware. Model that with a full-scope identity so scope-gated tools
	// (now including the discover/load meta-tools) are callable. Tests that
	// exercise scope denial inject their own narrower identity directly.
	req = req.WithContext(auth.WithIdentity(req.Context(),
		auth.Identity{Type: "session", Name: "test", Scopes: []string{"read", "write", "admin"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustDecode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
}

func extractContent(t *testing.T, resp mcp.Response) string {
	t.Helper()
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result map")
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("expected content array")
	}
	textObj, ok := content[0].(map[string]any)
	if !ok {
		t.Fatal("expected text object")
	}
	return textObj["text"].(string)
}

// Test scope enforcement: nil scopes cannot call write tools.
func TestHandleToolsCall_NilScopesCannotCallWriteTool(t *testing.T) {
	h := setupHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_write","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	// Inject identity with nil scopes.
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Type: "bearer", Name: "test"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var errResp mcp.Error
	mustDecode(t, rec, &errResp)
	if errResp.Error.Code == 0 {
		t.Error("expected error for nil scopes calling write tool")
	}
}

// Test scope enforcement: empty scopes cannot call write tools.
func TestHandleToolsCall_EmptyScopesCannotCallWriteTool(t *testing.T) {
	h := setupHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_write","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Type: "bearer", Name: "test", Scopes: []string{}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var errResp mcp.Error
	mustDecode(t, rec, &errResp)
	if errResp.Error.Code == 0 {
		t.Error("expected error for empty scopes calling write tool")
	}
}

// Test scope enforcement: read scope cannot call write tools.
func TestHandleToolsCall_ReadScopesCannotCallWriteTool(t *testing.T) {
	h := setupHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_write","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Type: "bearer", Name: "test", Scopes: []string{"read"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var errResp mcp.Error
	mustDecode(t, rec, &errResp)
	if errResp.Error.Code == 0 {
		t.Error("expected error for read scope calling write tool")
	}
}

// Test scope enforcement: write scope can call write tools.
func TestHandleToolsCall_WriteScopesCanCallWriteTool(t *testing.T) {
	h := setupHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"test_write","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Type: "bearer", Name: "test", Scopes: []string{"write"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp mcp.Response
	mustDecode(t, rec, &resp)
	if resp.Result == nil {
		t.Error("expected success for write scope calling write tool")
	}
}

func TestMCPHandler_SanitizesInternalErrors(t *testing.T) {
	reg := mcp.NewRegistry()
	reg.Register(&errorTool{}) // tool that always returns an error with sensitive info
	h := mcp.NewHandler(reg, mcp.HandlerOpts{})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"error_tool","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	// Authenticate so the call passes scope enforcement and actually reaches the
	// tool — the point of this test is sanitizing the error the tool returns.
	req = req.WithContext(auth.WithIdentity(req.Context(),
		auth.Identity{Type: "session", Name: "test", Scopes: []string{"read", "write", "admin"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var errResp mcp.Error
	mustDecode(t, rec, &errResp)
	if errResp.Error.Code != mcp.ErrInternal {
		t.Errorf("expected code %d, got %d", mcp.ErrInternal, errResp.Error.Code)
	}
	if errResp.Error.Message != "internal error" {
		t.Errorf("expected sanitized message 'internal error', got %q", errResp.Error.Message)
	}
	if strings.Contains(errResp.Error.Message, "connection refused") {
		t.Error("error message leaked internal details")
	}
}

// errorTool always returns an error containing sensitive internal details.
type errorTool struct{}

func (t *errorTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:           "error_tool",
		Description:    "Tool that always errors",
		RequiredScopes: []string{"read", "write", "admin"},
		InputSchema:    map[string]any{"type": "object"},
	}
}

func (t *errorTool) Call(ctx context.Context, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("dial tcp 10.0.0.1:8000: connection refused — db cluster unreachable")
}

// undeclaredScopeTool deliberately declares NO RequiredScopes.
type undeclaredScopeTool struct{}

func (t *undeclaredScopeTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "undeclared_scope",
		Description: "Tool that forgot to declare RequiredScopes",
		InputSchema: map[string]any{"type": "object"},
	}
}

func (t *undeclaredScopeTool) Call(ctx context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{"ok": true}, nil
}

// Fail-closed: a tool that declares no scopes must be denied even for a
// full-scope identity. A forgotten RequiredScopes line cannot silently expose
// a tool to any authenticated caller (regression guard for the R6 hardening).
func TestHandleToolsCall_FailsClosedForUndeclaredScopes(t *testing.T) {
	reg := mcp.NewRegistry()
	reg.Register(&undeclaredScopeTool{})
	h := mcp.NewHandler(reg, mcp.HandlerOpts{})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"undeclared_scope","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithIdentity(req.Context(),
		auth.Identity{Type: "session", Name: "admin", Scopes: []string{"read", "write", "admin"}}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var errResp mcp.Error
	mustDecode(t, rec, &errResp)
	if errResp.Error.Code == 0 {
		t.Error("expected fail-closed denial for a tool with no declared scopes")
	}
}
