package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

// AgentOptions holds configuration for the agent loop.
type AgentOptions struct {
	// SystemPrompt is prepended as a system message on every turn.
	SystemPrompt string
	// Model is the LLM model name to use (uses provider default if empty).
	Model string
	// MaxIter is the maximum number of LLM + tool-call iterations per message (default 40).
	MaxIter int
	// Temperature controls sampling randomness (default 0.1).
	Temperature float64
	// MaxTokens caps the response length (default 65536).
	MaxTokens int
	// MemoryWindow is the max number of history messages passed to the LLM (default 100).
	MemoryWindow int
}

func (o *AgentOptions) withDefaults() AgentOptions {
	out := *o
	if out.MaxIter <= 0 {
		out.MaxIter = 40
	}
	if out.Temperature == 0 {
		out.Temperature = 0.1
	}
	if out.MaxTokens <= 0 {
		out.MaxTokens = 65536
	}
	if out.MemoryWindow <= 0 {
		out.MemoryWindow = 100
	}
	return out
}

// AgentLoop is the core processing engine.
//
// It:
//  1. Reads inbound messages from the bus.
//  2. Builds context with session history.
//  3. Calls the LLM in a ReAct loop (Reason → Act → Observe).
//  4. Executes tool calls and feeds results back to the LLM.
//  5. Publishes the final response to the outbound bus.
type AgentLoop struct {
	bus      *bus.MessageBus
	provider provider.LLMProvider
	tools    *tool.ToolRegistry
	sessions *SessionManager
	opts     AgentOptions
}

// NewAgentLoop creates an AgentLoop.
func NewAgentLoop(b *bus.MessageBus, p provider.LLMProvider, tools *tool.ToolRegistry, opts AgentOptions) *AgentLoop {
	return &AgentLoop{
		bus:      b,
		provider: p,
		tools:    tools,
		sessions: NewSessionManager(),
		opts:     opts.withDefaults(),
	}
}

// Run starts the main event loop. It processes inbound messages until ctx is cancelled.
func (l *AgentLoop) Run(ctx context.Context) error {
	slog.Info("agent loop started")
	for {
		msg, err := l.bus.ConsumeInbound(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				slog.Info("agent loop stopping", "reason", err)
				return nil
			}
			return fmt.Errorf("consume inbound: %w", err)
		}

		resp, err := l.ProcessMessage(ctx, msg)
		if err != nil {
			slog.Error("error processing message", "error", err)
			_ = l.bus.PublishOutbound(ctx, &bus.OutboundMessage{
				Channel:  msg.Channel,
				ChatID:   msg.ChatID,
				Content:  fmt.Sprintf("Sorry, I encountered an error: %s", err.Error()),
				Metadata: msg.Metadata,
			})
			continue
		}
		if resp != nil {
			if pubErr := l.bus.PublishOutbound(ctx, resp); pubErr != nil {
				slog.Warn("failed to publish outbound", "error", pubErr)
			}
		}
	}
}

// ProcessMessage processes a single inbound message and returns the outbound response.
func (l *AgentLoop) ProcessMessage(ctx context.Context, msg *bus.InboundMessage) (*bus.OutboundMessage, error) {
	preview := msg.Content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	slog.Info("processing message", "channel", msg.Channel, "sender", msg.SenderID, "content", preview)

	sessionKey := msg.SessionKey()
	history := l.sessions.GetHistory(sessionKey, l.opts.MemoryWindow)

	messages := BuildMessages(l.opts.systemPrompt(), history, msg.Content)

	finalContent, newMessages, err := l.runReActLoop(ctx, messages)
	if err != nil {
		return nil, err
	}

	// Persist from the current user message onward (index len(messages)-1 in the
	// pre-loop slice, which is the user message we just appended in BuildMessages).
	l.saveTurn(sessionKey, messages[:len(messages)-1], newMessages)

	resp := &bus.OutboundMessage{
		Channel:  msg.Channel,
		ChatID:   msg.ChatID,
		Content:  finalContent,
		Metadata: msg.Metadata,
	}
	return resp, nil
}

// runReActLoop executes the Reason-Act-Observe loop.
// Returns the final assistant content and the full (extended) message slice.
func (l *AgentLoop) runReActLoop(ctx context.Context, messages []provider.Message) (string, []provider.Message, error) {
	toolDefs := buildProviderTools(l.tools.GetDefinitions())
	opts := provider.ChatOptions{
		Model:       l.opts.modelName(l.provider),
		Temperature: l.opts.Temperature,
		MaxTokens:   l.opts.MaxTokens,
		Tools:       toolDefs,
	}

	for iter := 0; iter < l.opts.MaxIter; iter++ {
		resp, err := l.provider.Chat(ctx, messages, opts)
		if err != nil {
			return "", messages, fmt.Errorf("LLM chat: %w", err)
		}

		// Use ShouldCallTools() instead of HasToolCalls() so that a response
		// with finish_reason="tool_calls" but no parsed ToolCalls objects still
		// enters the tool-call branch (and emits a warning) rather than being
		// silently treated as a final answer.
		if resp.ShouldCallTools() {
			if !resp.HasToolCalls() {
				// finish_reason="tool_calls" but ToolCalls list is empty — the
				// provider warning was already logged in parseResponse.  There
				// is nothing to execute; break out to avoid an infinite loop.
				slog.Warn("ShouldCallTools() true but no tool calls available; treating as final answer")
				finalContent := ""
				if resp.Content != nil {
					finalContent = *resp.Content
				}
				messages = AddAssistantMessage(messages, finalContent, nil)
				return finalContent, messages, nil
			}
			slog.Info("tool calls requested", "tools", toolCallHint(resp.ToolCalls), "iter", iter+1)

			// Append assistant message with embedded tool calls.
			assistantToolCalls := toolCallsFromResponse(resp.ToolCalls)
			content := ""
			if resp.Content != nil {
				content = *resp.Content
			}
			messages = AddAssistantMessage(messages, content, assistantToolCalls)

			// Execute each tool and append result messages.
			for _, tc := range resp.ToolCalls {
				result := l.tools.Execute(ctx, tc.Name, tc.Arguments)
				messages = AddToolResult(messages, tc.ID, tc.Name, result)
			}
		} else {
			// No tool calls — final answer.
			finalContent := ""
			if resp.Content != nil {
				finalContent = *resp.Content
			}
			preview := finalContent
			if len(preview) > 120 {
				preview = preview[:120] + "..."
			}
			slog.Info("final response", "content", preview)
			// Append the final assistant message so it is persisted in the session.
			messages = AddAssistantMessage(messages, finalContent, nil)
			return finalContent, messages, nil
		}
	}

	// Exceeded max iterations.
	slog.Warn("max iterations reached", "max_iter", l.opts.MaxIter)
	msg := fmt.Sprintf(
		"I reached the maximum number of tool call iterations (%d) without completing the task. "+
			"You can try breaking the task into smaller steps.",
		l.opts.MaxIter,
	)
	return msg, messages, nil
}

// saveTurn persists the new messages produced during this turn to the session.
// initialLen is the number of messages that existed before the ReAct loop ran.
func (l *AgentLoop) saveTurn(sessionKey string, before, after []provider.Message) {
	const maxToolResultChars = 500
	newProviderMsgs := after[len(before):]
	sessionMsgs := make([]SessionMessage, 0, len(newProviderMsgs))
	for _, m := range newProviderMsgs {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		// Truncate large tool results.
		if m.Role == "tool" && len(content) > maxToolResultChars {
			content = content[:maxToolResultChars] + "\n... (truncated)"
		}
		sessionMsgs = append(sessionMsgs, SessionMessage{
			Role:       m.Role,
			Content:    content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
			Timestamp:  time.Now(),
		})
	}
	l.sessions.AppendMessages(sessionKey, sessionMsgs)
}

// buildProviderTools converts raw tool definitions (map[string]any) to provider.Tool slice.
func buildProviderTools(defs []map[string]any) []provider.Tool {
	tools := make([]provider.Tool, 0, len(defs))
	for _, d := range defs {
		fn, _ := d["function"].(map[string]any)
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params := fn["parameters"]
		tools = append(tools, provider.Tool{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		})
	}
	return tools
}

func (o *AgentOptions) modelName(p provider.LLMProvider) string {
	if o.Model != "" {
		return o.Model
	}
	return p.DefaultModel()
}

func (o *AgentOptions) systemPrompt() string {
	return o.SystemPrompt
}
