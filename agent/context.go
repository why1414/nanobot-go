package agent

import (
	"encoding/json"
	"fmt"

	"github.com/libo/nanobot-go/provider"
)

// BuildMessages constructs the slice of provider.Message to send to the LLM.
// It prepends a system prompt (if non-empty), appends history messages, then
// appends the current user message.
func BuildMessages(systemPrompt string, history []SessionMessage, current string) []provider.Message {
	var msgs []provider.Message

	if systemPrompt != "" {
		msgs = append(msgs, provider.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for _, h := range history {
		m := provider.Message{
			Role:       h.Role,
			Content:    h.Content,
			ToolCallID: h.ToolCallID,
			Name:       h.Name,
		}
		if len(h.ToolCalls) > 0 {
			m.ToolCalls = h.ToolCalls
		}
		msgs = append(msgs, m)
	}

	msgs = append(msgs, provider.Message{
		Role:    "user",
		Content: current,
	})
	return msgs
}

// AddAssistantMessage appends an assistant message (with optional tool calls) to messages.
// toolCalls should be []provider.ToolCall produced from an LLMResponse.
func AddAssistantMessage(messages []provider.Message, content string, toolCalls []provider.ToolCall) []provider.Message {
	m := provider.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
	}
	return append(messages, m)
}

// AddToolResult appends a tool-result message to messages.
func AddToolResult(messages []provider.Message, toolCallID, toolName, result string) []provider.Message {
	return append(messages, provider.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
		Name:       toolName,
	})
}

// toolCallsFromResponse converts provider.ToolCallRequest slice (from LLMResponse)
// into provider.ToolCall slice suitable for embedding in a provider.Message.
func toolCallsFromResponse(tcs []provider.ToolCallRequest) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		argsJSON, _ := json.Marshal(tc.Arguments)
		out = append(out, provider.ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: provider.ToolCallFunction{
				Name:      tc.Name,
				Arguments: string(argsJSON),
			},
		})
	}
	return out
}

// toolCallHint returns a concise hint string like `exec("ls -la")` for logging.
func toolCallHint(tcs []provider.ToolCallRequest) string {
	if len(tcs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tcs))
	for _, tc := range tcs {
		val := ""
		for _, v := range tc.Arguments {
			if s, ok := v.(string); ok {
				val = s
				break
			}
		}
		if len(val) > 40 {
			parts = append(parts, fmt.Sprintf("%s(%q…)", tc.Name, val[:40]))
		} else if val != "" {
			parts = append(parts, fmt.Sprintf("%s(%q)", tc.Name, val))
		} else {
			parts = append(parts, tc.Name)
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
