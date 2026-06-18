// Package mcp implements the Model Context Protocol (MCP) JSON-RPC 2.0 handler.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// JSONRPC version constant.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 success response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
}

// Error is a JSON-RPC 2.0 error response.
type Error struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Error   ErrData `json:"error"`
}

// ErrData holds error code and message.
type ErrData struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC error codes.
const (
	ErrParse    = -32700
	ErrInvalid  = -32600
	ErrMethod   = -32601
	ErrParams   = -32602
	ErrInternal = -32603
)

// ErrUserFacing marks an error whose message is safe to return to the MCP
// caller verbatim (validation / bad-input errors the caller must see to fix
// their request). Errors that do NOT wrap this are sanitized to a generic
// "internal error" so internal details (hostnames, DB strings) never leak.
var ErrUserFacing = errors.New("invalid request")

func newError(id any, code int, msg string) Error {
	return Error{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error:   ErrData{Code: code, Message: msg},
	}
}

func newResponse(id any, result any) Response {
	return Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

// ToolDefinition is the full schema for an MCP tool.
type ToolDefinition struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	InputSchema    map[string]any `json:"inputSchema"`
	RequiredScopes []string       `json:"-"` // token must have at least one of these
}

// DiscoverEntry is the short form returned by discover.
type DiscoverEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Tool is the interface all Rill tools implement.
type Tool interface {
	Definition() ToolDefinition
	Call(ctx context.Context, params json.RawMessage) (any, error)
}

// Registry holds all registered tools and provides discover/load.
type Registry struct {
	tools map[string]Tool
	order []string // preserve registration order
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *Registry) Register(t Tool) {
	name := t.Definition().Name
	if _, ok := r.tools[name]; !ok {
		r.order = append(r.order, name)
	}
	r.tools[name] = t
}

// Discover returns the list of all tools with short descriptions.
// Exposed as the discover tool.
func (r *Registry) Discover() []DiscoverEntry {
	entries := make([]DiscoverEntry, len(r.order))
	for i, name := range r.order {
		def := r.tools[name].Definition()
		entries[i] = DiscoverEntry{
			Name:        def.Name,
			Description: def.Description,
		}
	}
	return entries
}

// Load returns the full definition for a single tool.
func (r *Registry) Load(name string) (*ToolDefinition, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	def := t.Definition()
	return &def, nil
}

// GetRequiredScopes returns the required scopes for a tool, or nil if none.
func (r *Registry) GetRequiredScopes(name string) []string {
	t, ok := r.tools[name]
	if !ok {
		return nil
	}
	return t.Definition().RequiredScopes
}

// Call invokes a tool by name with the given params.
func (r *Registry) Call(ctx context.Context, name string, params json.RawMessage) (any, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return t.Call(ctx, params)
}
