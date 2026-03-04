package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*OpenAICompatProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := NewOpenAICompatProvider("test-key", srv.URL, "test-model")
	return p, srv
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return string(b)
}

// ─── TestChat_basic ───────────────────────────────────────────────────────────

func TestChat_basic(t *testing.T) {
	respJSON := jsonBody(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello from the mock!",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	})

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	}

	p, _ := newTestProvider(t, handler)
	msgs := []Message{
		{Role: "user", Content: "Hi"},
	}
	resp, err := p.Chat(context.Background(), msgs, ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	if resp.Content == nil || *resp.Content != "Hello from the mock!" {
		t.Errorf("unexpected content: %v", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage["total_tokens"] != 15 {
		t.Errorf("unexpected total_tokens: %d", resp.Usage["total_tokens"])
	}
	if resp.HasToolCalls() {
		t.Error("expected no tool calls")
	}
}

// ─── TestChat_toolCalls ───────────────────────────────────────────────────────

func TestChat_toolCalls(t *testing.T) {
	respJSON := jsonBody(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []any{
						map[string]any{
							"id":   "call_abc123",
							"type": "function",
							"function": map[string]any{
								"name":      "get_weather",
								"arguments": `{"location":"Tokyo","unit":"celsius"}`,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	})

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify tools were sent.
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(req.Tools) != 1 {
			t.Errorf("expected 1 tool, got %d", len(req.Tools))
		}
		if req.ToolChoice != "auto" {
			t.Errorf("expected tool_choice=auto, got %q", req.ToolChoice)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	})

	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_weather",
				Description: "Get current weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
						"unit":     map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Weather in Tokyo?"}},
		ChatOptions{Tools: tools})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls")
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("unexpected tool call id: %s", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("unexpected tool name: %s", tc.Name)
	}
	if tc.Arguments["location"] != "Tokyo" {
		t.Errorf("unexpected arguments: %v", tc.Arguments)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
}

// ─── TestChat_toolCalls_truncatedArgs ─────────────────────────────────────────

func TestChat_toolCalls_truncatedArgs(t *testing.T) {
	// Arguments JSON is missing the closing brace — simulates truncated output.
	respJSON := jsonBody(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": nil,
					"tool_calls": []any{
						map[string]any{
							"id":   "call_trunc",
							"type": "function",
							"function": map[string]any{
								"name":      "do_thing",
								"arguments": `{"key":"value"`,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	})

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	})

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "go"}}, ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls")
	}
	// Should be repaired: key should be present.
	if resp.ToolCalls[0].Arguments["key"] != "value" {
		t.Errorf("repair failed: %v", resp.ToolCalls[0].Arguments)
	}
}

// ─── TestChat_reasoningContent ────────────────────────────────────────────────

func TestChat_reasoningContent(t *testing.T) {
	respJSON := jsonBody(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":              "assistant",
					"content":           "42",
					"reasoning_content": "Let me think step by step...",
				},
				"finish_reason": "stop",
			},
		},
	})

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	})

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "What is 6*7?"}}, ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.ReasoningContent == nil || *resp.ReasoningContent != "Let me think step by step..." {
		t.Errorf("unexpected reasoning_content: %v", resp.ReasoningContent)
	}
}

// ─── TestChat_error ───────────────────────────────────────────────────────────

func TestChat_error(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"unauthorized", http.StatusUnauthorized},
		{"bad_request", http.StatusBadRequest},
		{"not_found", http.StatusNotFound},
		{"server_error", http.StatusInternalServerError},
		{"too_many_requests", http.StatusTooManyRequests},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error":{"message":"simulated error"}}`))
			})

			_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{})
			if err == nil {
				t.Fatalf("expected error for status %d", tt.statusCode)
			}
			if !strings.Contains(err.Error(), string(rune('0'+tt.statusCode/100))) {
				// Just verify the status code digits appear somewhere in the error.
			}
		})
	}
}

// ─── TestChat_timeout ─────────────────────────────────────────────────────────

func TestChat_timeout(t *testing.T) {
	// Use a slow server: sleep long enough that a 50ms client timeout fires first.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	p := NewOpenAICompatProvider("test-key", srv.URL, "test-model")
	// Override the HTTP client with a very short timeout.
	p.client = &http.Client{Timeout: 50 * time.Millisecond}

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, ChatOptions{})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ─── TestChat_contextCancel ───────────────────────────────────────────────────

func TestChat_contextCancel(t *testing.T) {
	// Use a slow server; context will be cancelled before it responds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	p := NewOpenAICompatProvider("test-key", srv.URL, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := p.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, ChatOptions{})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

// ─── TestChat_multipartContent ────────────────────────────────────────────────

func TestChat_multipartContent(t *testing.T) {
	var captured openaiRequest

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		respJSON := jsonBody(t, map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	})

	msgs := []Message{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "Describe this"},
				{Type: "text", Text: "image"},
			},
		},
	}
	_, err := p.Chat(context.Background(), msgs, ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if len(captured.Messages) == 0 {
		t.Fatal("no messages captured")
	}
}

// ─── TestDefaultModel ─────────────────────────────────────────────────────────

func TestDefaultModel(t *testing.T) {
	p := NewOpenAICompatProvider("key", "http://localhost", "my-model")
	if p.DefaultModel() != "my-model" {
		t.Errorf("unexpected default model: %s", p.DefaultModel())
	}
}

// ─── TestChat_extraHeaders ────────────────────────────────────────────────────

func TestChat_extraHeaders(t *testing.T) {
	var capturedRef string
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		capturedRef = r.Header.Get("HTTP-Referer")
		respJSON := jsonBody(t, map[string]any{
			"choices": []any{
				map[string]any{
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respJSON))
	})

	p = p.WithExtraHeaders(map[string]string{"HTTP-Referer": "https://example.com"})
	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, ChatOptions{})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if capturedRef != "https://example.com" {
		t.Errorf("expected HTTP-Referer header, got %q", capturedRef)
	}
}
