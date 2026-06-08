package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jasondostal/rill/internal/auth"
	rilllog "github.com/jasondostal/rill/internal/log"
	"github.com/jasondostal/rill/internal/memory"
)

// HandlerOpts controls MCP handler behavior.
type HandlerOpts struct {
	// CompactTools causes tools/list to return name + description only,
	// omitting input schemas. Clients are expected to call load to fetch
	// a tool's full schema before invoking it.
	CompactTools bool
	// NamesOnly causes tools/list to return only tool names. Forces the
	// client to call load for description AND schema. ~10 tokens per tool.
	NamesOnly bool
}

// Handler dispatches MCP JSON-RPC requests to the tool registry.
type Handler struct {
	registry *Registry
	opts     HandlerOpts
}

// NewHandler creates an MCP handler with the given registry and options.
func NewHandler(reg *Registry, opts HandlerOpts) *Handler {
	return &Handler{registry: reg, opts: opts}
}

// ServeHTTP handles JSON-RPC requests at POST /mcp.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, newError(nil, ErrInvalid, "POST required"))
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, newError(nil, ErrParse, "invalid JSON"))
		return
	}

	if req.JSONRPC != JSONRPCVersion {
		writeJSON(w, http.StatusBadRequest, newError(req.ID, ErrInvalid, "jsonrpc must be 2.0"))
		return
	}

	result, err := h.dispatch(r.Context(), req.Method, req.Params)
	if err != nil {
		rilllog.Logger().Error("mcp: tool dispatch error", "method", req.Method, "req_id", req.ID, "error", err)
		// User-facing errors (validation / bad input / DB constraint / not-found /
		// conflict) are safe to surface with their message so the caller can fix
		// the request. ErrUserFacing covers transport-level cases (param decode);
		// memory.IsUserFacing covers every Store sentinel, so all tools classify
		// consistently. Everything else is sanitized to avoid leaking internals.
		if errors.Is(err, ErrUserFacing) || memory.IsUserFacing(err) {
			writeJSON(w, http.StatusOK, newError(req.ID, ErrParams, err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, newError(req.ID, ErrInternal, "internal error"))
		return
	}

	writeJSON(w, http.StatusOK, newResponse(req.ID, result))
}

func (h *Handler) dispatch(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return h.handleInitialize(params)
	case "tools/list":
		return h.handleToolsList()
	case "tools/call":
		return h.handleToolsCall(ctx, params)
	default:
		return nil, nil // notifications — silently accept
	}
}

func (h *Handler) handleInitialize(_ json.RawMessage) (any, error) {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]string{
			"name":    "rill",
			"version": "0.0.1",
		},
		"capabilities": map[string]any{
			"tools": map[string]bool{},
		},
	}, nil
}

func (h *Handler) handleToolsList() (any, error) {
	tools := h.registry.Discover()
	defs := make([]map[string]any, len(tools))
	for i, t := range tools {
		entry := map[string]any{
			"name": t.Name,
		}
		if !h.opts.NamesOnly {
			entry["description"] = t.Description
		}
		if !h.opts.CompactTools && !h.opts.NamesOnly {
			if def, err := h.registry.Load(t.Name); err == nil {
				entry["inputSchema"] = def.InputSchema
			} else {
				entry["inputSchema"] = map[string]any{"type": "object"}
			}
		}
		defs[i] = entry
	}
	return map[string]any{"tools": defs}, nil
}

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (h *Handler) handleToolsCall(ctx context.Context, params json.RawMessage) (any, error) {
	var p toolsCallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// Scope enforcement (fail-closed): every callable tool must declare its
	// required scopes, and the identity must hold at least one of them. A
	// registered tool with no declared scopes is treated as a bug and denied —
	// never as a public endpoint — so a forgotten RequiredScopes line cannot
	// silently expose a tool to any authenticated caller.
	id := auth.IdentityFromContext(ctx)
	required := h.registry.GetRequiredScopes(p.Name)
	if len(required) == 0 || !hasAnyScope(id.Scopes, required) {
		return nil, fmt.Errorf("forbidden — your token lacks the required scope for this operation")
	}

	result, err := h.registry.Call(ctx, p.Name, p.Arguments)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": toJSON(result),
			},
		},
	}, nil
}

func hasAnyScope(userScopes, required []string) bool {
	for _, r := range required {
		for _, u := range userScopes {
			if u == r {
				return true
			}
		}
	}
	return false
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		rilllog.Logger().Error("mcp: failed to marshal result", "error", err)
		return "{}"
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		rilllog.Logger().Error("mcp: failed to encode response", "error", err)
	}
}
