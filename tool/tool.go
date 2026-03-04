// Package tool provides the tool interface and registry for agent tools.
package tool

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Tool is the interface that all agent tools must implement.
type Tool interface {
	// Name returns the tool name used in LLM function calls.
	Name() string
	// Description returns a human-readable description of what the tool does.
	Description() string
	// Parameters returns the JSON Schema for the tool's parameters.
	Parameters() map[string]any
	// Execute runs the tool with the given parameters and returns a string result.
	Execute(ctx context.Context, params map[string]any) (string, error)
}

// ToolRegistry holds registered tools and provides lookup and execution.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry. Overwrites any existing tool with the same name.
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns the tool with the given name, or nil if not found.
func (r *ToolRegistry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

// GetDefinitions returns all registered tools in OpenAI function-call format.
func (r *ToolRegistry) GetDefinitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]map[string]any, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		})
	}
	return defs
}

// Execute runs the named tool with the given parameters.
// Returns an error string (not an error value) so it can be fed back to the LLM.
func (r *ToolRegistry) Execute(ctx context.Context, name string, params map[string]any) string {
	r.mu.RLock()
	t := r.tools[name]
	r.mu.RUnlock()

	if t == nil {
		return fmt.Sprintf("Error: unknown tool %q", name)
	}

	slog.Info("executing tool", "tool", name)
	result, err := t.Execute(ctx, params)
	if err != nil {
		slog.Warn("tool error", "tool", name, "error", err)
		return fmt.Sprintf("Error: %s", err.Error())
	}
	return result
}
