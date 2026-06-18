package tools

import (
	"context"
	"encoding/json"

	"github.com/jasondostal/rill/internal/mcp"
)

// DiscoverTool implements the discover meta-tool.
// It returns the list of all registered tools with short descriptions.
type DiscoverTool struct {
	registry *mcp.Registry
}

func NewDiscoverTool(reg *mcp.Registry) *DiscoverTool {
	return &DiscoverTool{registry: reg}
}

func (t *DiscoverTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "discover",
		Description: "List available tools (name + short description). Standard MCP tools/list may return compact entries with no schema; use load(tool: <name>) to fetch the full input schema before calling a tool. Use this for any tool you haven't called recently.",
		// Metadata-only: any authenticated token (any real scope) may discover.
		RequiredScopes: []string{"read", "write", "admin"},
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *DiscoverTool) Call(_ context.Context, _ json.RawMessage) (any, error) {
	return map[string]any{
		"tools": t.registry.Discover(),
	}, nil
}

// LoadTool implements the load meta-tool.
// It returns the full ToolDefinition for a specific tool.
type LoadTool struct {
	registry *mcp.Registry
}

func NewLoadTool(reg *mcp.Registry) *LoadTool {
	return &LoadTool{registry: reg}
}

func (t *LoadTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "load",
		Description: "Load the full definition and input schema for a specific Rill tool. Use discover first to find tool names, then call this to get the complete schema before invoking the tool.",
		// Metadata-only: any authenticated token (any real scope) may load schemas.
		RequiredScopes: []string{"read", "write", "admin"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tool": map[string]any{
					"type":        "string",
					"description": "Name of the tool to load (e.g., 'memory_store')",
				},
			},
			"required": []string{"tool"},
		},
	}
}

type loadParams struct {
	Tool string `json:"tool"`
}

func (t *LoadTool) Call(_ context.Context, params json.RawMessage) (any, error) {
	var p loadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	def, err := t.registry.Load(p.Tool)
	if err != nil {
		return nil, err
	}
	return def, nil
}
