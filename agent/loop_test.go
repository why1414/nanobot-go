package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

// --- mock provider ---

// chatFunc is a function that handles a single Chat call.
type chatFunc func(ctx context.Context, messages []provider.Message, opts provider.ChatOptions) (*provider.LLMResponse, error)

type mockProvider struct {
	defaultModel string
	calls        []chatFunc
	callIndex    int
}

func (m *mockProvider) DefaultModel() string { return m.defaultModel }

func (m *mockProvider) Chat(ctx context.Context, messages []provider.Message, opts provider.ChatOptions) (*provider.LLMResponse, error) {
	if m.callIndex >= len(m.calls) {
		return nil, fmt.Errorf("mockProvider: unexpected call #%d", m.callIndex+1)
	}
	fn := m.calls[m.callIndex]
	m.callIndex++
	return fn(ctx, messages, opts)
}

// strPtr is a helper to get a *string.
func strPtr(s string) *string { return &s }

// --- mock tool ---

type mockTool struct {
	name   string
	result string
	called atomic.Int32
}

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return "mock tool " + t.name }
func (t *mockTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *mockTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	t.called.Add(1)
	return t.result, nil
}

// --- helpers ---

func newTestLoop(p provider.LLMProvider, tools ...*mockTool) (*AgentLoop, *bus.MessageBus) {
	b := bus.NewMessageBus(16)
	reg := tool.NewToolRegistry()
	for _, t := range tools {
		reg.Register(t)
	}
	loop := NewAgentLoop(b, p, reg, AgentOptions{
		MaxIter: 5,
	}, nil, nil, "/tmp/test-workspace")
	return loop, b
}

func sendMsg(ctx context.Context, b *bus.MessageBus, content string) {
	_ = b.PublishInbound(ctx, &bus.InboundMessage{
		Channel:  "test",
		SenderID: "user1",
		ChatID:   "chat1",
		Content:  content,
	})
}

// --- tests ---

// TestAgentLoop_basicChat verifies a simple single-turn chat with no tool calls.
func TestAgentLoop_basicChat(t *testing.T) {
	p := &mockProvider{
		defaultModel: "gpt-test",
		calls: []chatFunc{
			func(_ context.Context, _ []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
				return &provider.LLMResponse{
					Content:      strPtr("Hello, world!"),
					FinishReason: "stop",
				}, nil
			},
		},
	}

	loop, b := newTestLoop(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sendMsg(ctx, b, "Hi")

	msg, err := b.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("consume inbound: %v", err)
	}
	resp, err := loop.ProcessMessage(ctx, msg)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if resp.Channel != "test" || resp.ChatID != "chat1" {
		t.Errorf("unexpected routing: channel=%s chatID=%s", resp.Channel, resp.ChatID)
	}
}

// TestAgentLoop_toolCall verifies the ReAct loop: one tool call then final answer.
func TestAgentLoop_toolCall(t *testing.T) {
	mt := &mockTool{name: "my_tool", result: "tool result data"}

	p := &mockProvider{
		defaultModel: "gpt-test",
		calls: []chatFunc{
			// First LLM call → requests a tool call.
			func(_ context.Context, _ []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
				return &provider.LLMResponse{
					ToolCalls: []provider.ToolCallRequest{
						{ID: "tc1", Name: "my_tool", Arguments: map[string]any{"key": "value"}},
					},
					FinishReason: "tool_calls",
				}, nil
			},
			// Second LLM call → final answer after seeing tool result.
			func(_ context.Context, msgs []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
				// Verify the tool result is present in the messages.
				found := false
				for _, m := range msgs {
					if m.Role == "tool" {
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("expected tool result message in context")
				}
				return &provider.LLMResponse{
					Content:      strPtr("Done using the tool!"),
					FinishReason: "stop",
				}, nil
			},
		},
	}

	loop, b := newTestLoop(p, mt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sendMsg(ctx, b, "Use the tool please")

	msg, err := b.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("consume inbound: %v", err)
	}
	resp, err := loop.ProcessMessage(ctx, msg)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if resp.Content != "Done using the tool!" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if mt.called.Load() != 1 {
		t.Errorf("expected tool called once, got %d", mt.called.Load())
	}
}

// TestAgentLoop_multiTurn verifies that history is correctly passed across turns.
func TestAgentLoop_multiTurn(t *testing.T) {
	callCount := 0
	p := &mockProvider{
		defaultModel: "gpt-test",
		calls: []chatFunc{
			func(_ context.Context, msgs []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
				callCount++
				return &provider.LLMResponse{Content: strPtr("First reply"), FinishReason: "stop"}, nil
			},
			func(_ context.Context, msgs []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
				callCount++
				// Should contain system + prior assistant + prior user + new user messages.
				var roles []string
				for _, m := range msgs {
					roles = append(roles, m.Role)
				}
				rolesStr := strings.Join(roles, ",")
				// Expect system, user (turn1), assistant (turn1), user (turn2)
				if !strings.Contains(rolesStr, "assistant") {
					return nil, fmt.Errorf("expected assistant message in history, got roles: %s", rolesStr)
				}
				return &provider.LLMResponse{Content: strPtr("Second reply"), FinishReason: "stop"}, nil
			},
		},
	}

	loop, b := newTestLoop(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First turn.
	sendMsg(ctx, b, "Hello")
	msg1, _ := b.ConsumeInbound(ctx)
	resp1, err := loop.ProcessMessage(ctx, msg1)
	if err != nil || resp1.Content != "First reply" {
		t.Fatalf("turn 1 failed: err=%v resp=%v", err, resp1)
	}

	// Second turn — same session key (channel:chatID = test:chat1).
	sendMsg(ctx, b, "How are you?")
	msg2, _ := b.ConsumeInbound(ctx)
	resp2, err := loop.ProcessMessage(ctx, msg2)
	if err != nil || resp2.Content != "Second reply" {
		t.Fatalf("turn 2 failed: err=%v resp=%v", err, resp2)
	}

	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}
}

// TestAgentLoop_maxIter verifies that the loop returns a friendly message when
// the maximum iteration count is exceeded (LLM keeps requesting tools).
func TestAgentLoop_maxIter(t *testing.T) {
	mt := &mockTool{name: "loop_tool", result: "keep going"}

	// Always return a tool call — this should trigger max-iter protection.
	alwaysToolCall := func(_ context.Context, _ []provider.Message, _ provider.ChatOptions) (*provider.LLMResponse, error) {
		return &provider.LLMResponse{
			ToolCalls: []provider.ToolCallRequest{
				{ID: "tc-loop", Name: "loop_tool", Arguments: map[string]any{}},
			},
			FinishReason: "tool_calls",
		}, nil
	}

	// We need MaxIter call slots — create enough.
	calls := make([]chatFunc, 10)
	for i := range calls {
		calls[i] = alwaysToolCall
	}
	p := &mockProvider{defaultModel: "gpt-test", calls: calls}

	loop, b := newTestLoop(p, mt)
	// Override MaxIter to something small.
	loop.opts.MaxIter = 3

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sendMsg(ctx, b, "Do something loopy")
	msg, _ := b.ConsumeInbound(ctx)
	resp, err := loop.ProcessMessage(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Content, "maximum number") {
		t.Errorf("expected max-iter message, got: %q", resp.Content)
	}
	// Tool should have been called MaxIter times.
	if mt.called.Load() != int32(loop.opts.MaxIter) {
		t.Errorf("expected tool called %d times, got %d", loop.opts.MaxIter, mt.called.Load())
	}
}
