// Package provider defines the core LLM provider interface and types.
package provider

import "context"

// ToolCallRequest represents a tool invocation requested by the LLM.
type ToolCallRequest struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// LLMResponse is the normalized response returned by any provider.
type LLMResponse struct {
	// Content is the text reply; nil when the model only emits tool calls.
	Content *string
	// ToolCalls are function-call requests from the model.
	ToolCalls []ToolCallRequest
	// FinishReason is "stop", "tool_calls", "length", or "error".
	FinishReason string
	// Usage contains token counts (prompt_tokens, completion_tokens, total_tokens).
	Usage map[string]int
	// ReasoningContent holds extended chain-of-thought text (DeepSeek-R1, Kimi, etc.).
	ReasoningContent *string
}

// HasToolCalls reports whether the response includes tool call requests.
func (r *LLMResponse) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ShouldCallTools reports whether the agent loop should enter the tool-call
// branch. This is true when:
//   - ToolCalls is non-empty (the model provided parsed tool calls), OR
//   - FinishReason is "tool_calls" (the model signalled tool use via the
//     finish reason, even if ToolCalls could not be parsed).
//
// Checking FinishReason alone prevents the common bug where the model returns
// content + tool_calls simultaneously and the loop treats it as a final answer.
func (r *LLMResponse) ShouldCallTools() bool {
	return len(r.ToolCalls) > 0 || r.FinishReason == "tool_calls"
}

// Message is an OpenAI-compatible chat message.
type Message struct {
	// Role is "system", "user", "assistant", or "tool".
	Role string
	// Content is either a plain string or []ContentBlock.
	Content any
	// ToolCalls are outgoing tool calls on an assistant message.
	ToolCalls []ToolCall
	// ToolCallID links a tool-result message back to its ToolCallRequest.
	ToolCallID string
	// Name is the function name on tool-result messages.
	Name string
}

// ContentBlock represents a typed content item inside a multipart message.
type ContentBlock struct {
	Type string
	Text string
}

// ToolCall is an assistant-side tool invocation embedded in a Message.
type ToolCall struct {
	ID       string
	Type     string // always "function"
	Function ToolCallFunction
}

// ToolCallFunction holds the name and raw JSON arguments of a tool call.
type ToolCallFunction struct {
	Name      string
	Arguments string // JSON-encoded
}

// Tool describes a callable function exposed to the LLM.
type Tool struct {
	Type     string       // always "function"
	Function ToolFunction
}

// ToolFunction is the OpenAI function-definition schema.
type ToolFunction struct {
	Name        string
	Description string
	Parameters  any // JSON Schema object
}

// ChatOptions controls a single Chat call.
type ChatOptions struct {
	// Model overrides the provider's default model.
	Model string
	// Tools is the list of functions the model may call.
	Tools []Tool
	// MaxTokens caps the response length (default 4096 when zero).
	MaxTokens int
	// Temperature controls randomness (default 0.7 when zero).
	Temperature float64
}

// LLMProvider is the interface all provider implementations must satisfy.
type LLMProvider interface {
	// Chat sends a completion request and returns the model's response.
	Chat(ctx context.Context, messages []Message, opts ChatOptions) (*LLMResponse, error)
	// DefaultModel returns the model name used when ChatOptions.Model is empty.
	DefaultModel() string
}
