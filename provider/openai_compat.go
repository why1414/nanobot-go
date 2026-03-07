// Package provider — OpenAI-compatible HTTP provider.
//
// OpenAICompatProvider implements LLMProvider by speaking directly to any
// standard OpenAI-format /chat/completions endpoint.
// It handles both standard single-choice responses and non-standard multi-choice
// responses (e.g., GitHub Copilot Proxy splitting content and tool_calls across
// multiple choices).
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultMaxTokens   = 4096
	defaultTemperature = 0.7
	defaultTimeout     = 120 * time.Second
)

// OpenAICompatProvider calls any OpenAI-compatible /chat/completions endpoint.
type OpenAICompatProvider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	extraHeaders map[string]string
	client       *http.Client
}

// NewOpenAICompatProvider creates a provider that calls baseURL with the given
// API key and uses defaultModel when ChatOptions.Model is empty.
//
// baseURL should be the API root without a trailing slash, e.g.
// "https://api.openai.com/v1".
func NewOpenAICompatProvider(apiKey, baseURL, defaultModel string) *OpenAICompatProvider {
	return &OpenAICompatProvider{
		apiKey:       apiKey,
		baseURL:      strings.TrimRight(baseURL, "/"),
		defaultModel: defaultModel,
		extraHeaders: nil,
		client:       &http.Client{Timeout: defaultTimeout},
	}
}

// WithExtraHeaders returns a copy of the provider with additional HTTP headers
// sent on every request (e.g. "HTTP-Referer", "X-Title" for OpenRouter).
func (p *OpenAICompatProvider) WithExtraHeaders(headers map[string]string) *OpenAICompatProvider {
	cp := *p
	cp.extraHeaders = headers
	return &cp
}

// DefaultModel implements LLMProvider.
// Returns the model name without provider prefix.
func (p *OpenAICompatProvider) DefaultModel() string {
	return parseModelName(p.defaultModel)
}

// Chat implements LLMProvider.
func (p *OpenAICompatProvider) Chat(ctx context.Context, messages []Message, opts ChatOptions) (*LLMResponse, error) {
	model := opts.Model
	if model == "" {
		model = p.defaultModel
	}

	// Strip provider prefix if present (e.g., "custom/gpt-4" -> "gpt-4")
	model = parseModelName(model)

	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	temperature := opts.Temperature
	if temperature == 0 {
		temperature = defaultTemperature
	}

	reqBody := buildRequest(model, messages, opts.Tools, maxTokens, temperature)

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("provider: marshal request: %w", err)
	}

	body, err := p.doRequest(ctx, data)
	if err != nil {
		return nil, err
	}

	// Log raw response to help diagnose tool_calls parsing issues.
	slog.Debug("raw LLM response", "body", truncate(string(body), 2048))

	return parseResponse(body)
}

// doRequest sends a marshalled JSON body to /chat/completions and returns the
// raw response bytes. It is shared by CopilotProvider.
func (p *OpenAICompatProvider) doRequest(ctx context.Context, data []byte) ([]byte, error) {
	url := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("provider: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for k, v := range p.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("provider: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider: http %d: %s", resp.StatusCode, truncate(string(body), 512))
	}

	return body, nil
}

// ─── request builders ────────────────────────────────────────────────────────

// openaiRequest is the JSON body sent to /chat/completions.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	Tools       []openaiTool    `json:"tools,omitempty"`
	ToolChoice  string          `json:"tool_choice,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature"`
}

type openaiMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCalls  []openaiTCall   `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type openaiTCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function openaiTCallFunc `json:"function"`
}

type openaiTCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiTool struct {
	Type     string           `json:"type"`
	Function openaiToolFunc   `json:"function"`
}

type openaiToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

func buildRequest(model string, messages []Message, tools []Tool, maxTokens int, temperature float64) openaiRequest {
	msgs := make([]openaiMessage, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, convertMessage(m))
	}

	var oaiTools []openaiTool
	for _, t := range tools {
		oaiTools = append(oaiTools, openaiTool{
			Type: "function",
			Function: openaiToolFunc{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}

	req := openaiRequest{
		Model:       model,
		Messages:    msgs,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
	if len(oaiTools) > 0 {
		req.Tools = oaiTools
		req.ToolChoice = "auto"
	}
	return req
}

func convertMessage(m Message) openaiMessage {
	msg := openaiMessage{
		Role:       m.Role,
		ToolCallID: m.ToolCallID,
		Name:       m.Name,
	}

	// Encode content.
	switch v := m.Content.(type) {
	case string:
		if v != "" {
			raw, _ := json.Marshal(v)
			msg.Content = raw
		}
	case []ContentBlock:
		type rawBlock struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		blocks := make([]rawBlock, 0, len(v))
		for _, b := range v {
			if b.Text != "" {
				blocks = append(blocks, rawBlock{Type: b.Type, Text: b.Text})
			}
		}
		if len(blocks) > 0 {
			raw, _ := json.Marshal(blocks)
			msg.Content = raw
		}
	case nil:
		// assistant messages with only tool_calls may have nil content.
	}

	// Encode tool calls on assistant messages.
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, openaiTCall{
			ID:   tc.ID,
			Type: "function",
			Function: openaiTCallFunc{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}

	return msg
}

// ─── response parsing ─────────────────────────────────────────────────────────

// openaiResponse mirrors the OpenAI chat completion response shape.
type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage"`
}

type openaiChoice struct {
	Message      openaiChoiceMsg `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type openaiChoiceMsg struct {
	Role             string          `json:"role"`
	Content          *string         `json:"content"`
	ToolCalls        []openaiTCall   `json:"tool_calls"`
	ReasoningContent *string         `json:"reasoning_content"` // DeepSeek-R1, Kimi
	Reasoning        *string         `json:"reasoning"`         // some providers use this key
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func parseResponse(body []byte) (*LLMResponse, error) {
	var raw openaiResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider: decode response: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("provider: response has no choices")
	}

	// Standard OpenAI-compatible APIs return n=1 choices.
	// Non-standard proxies (e.g., Copilot Proxy) may split content and tool_calls
	// across multiple choices, so we merge all choices to handle both cases.
	if len(raw.Choices) == 1 {
		// Fast path: standard single-choice response
		return parseSingleChoice(&raw)
	}

	// Slow path: merge multiple choices (compatibility with non-standard proxies)
	return parseMultipleChoices(&raw)
}

// parseSingleChoice handles the standard single-choice response.
func parseSingleChoice(raw *openaiResponse) (*LLMResponse, error) {
	choice := raw.Choices[0]
	msg := choice.Message

	finishReason := choice.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	// reasoning_content may come under either key depending on the provider.
	var reasoning *string
	if msg.ReasoningContent != nil {
		reasoning = msg.ReasoningContent
	} else if msg.Reasoning != nil {
		reasoning = msg.Reasoning
	}

	// Parse tool calls, repairing truncated JSON arguments where possible.
	toolCalls := make([]ToolCallRequest, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		args, err := parseArguments(tc.Function.Arguments)
		if err != nil {
			// Fall back to empty map rather than failing the whole request.
			args = map[string]any{}
		}
		toolCalls = append(toolCalls, ToolCallRequest{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	usage := map[string]int{}
	if raw.Usage != nil {
		usage["prompt_tokens"] = raw.Usage.PromptTokens
		usage["completion_tokens"] = raw.Usage.CompletionTokens
		usage["total_tokens"] = raw.Usage.TotalTokens
	}

	// Warn when the model signals tool_calls via finish_reason but provided no
	// parseable tool call objects — this usually means a serialization issue on
	// the provider side and would cause the agent loop to hang without a warning.
	if finishReason == "tool_calls" && len(toolCalls) == 0 {
		slog.Warn("finish_reason=tool_calls but no tool_calls parsed; check raw response", "raw_tool_calls", len(msg.ToolCalls))
	}

	return &LLMResponse{
		Content:          msg.Content,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		Usage:            usage,
		ReasoningContent: reasoning,
	}, nil
}

// parseMultipleChoices merges all choices into a single LLMResponse.
// This handles non-standard proxies that split content and tool_calls across
// multiple choices (e.g., Copilot Proxy sending content in choices[0] and
// tool_calls in choices[1]).
func parseMultipleChoices(raw *openaiResponse) (*LLMResponse, error) {
	var (
		mergedContent  *string
		mergedRawCalls []openaiTCall
		finishReason   string
		reasoning      *string
	)

	for _, choice := range raw.Choices {
		msg := choice.Message

		// Accumulate content — pick the first non-nil content.
		if mergedContent == nil && msg.Content != nil {
			mergedContent = msg.Content
		}

		// Accumulate tool calls from all choices.
		mergedRawCalls = append(mergedRawCalls, msg.ToolCalls...)

		// Prefer "tool_calls" finish_reason over "stop" since it indicates
		// tools must be executed.
		if choice.FinishReason == "tool_calls" || finishReason == "" {
			finishReason = choice.FinishReason
		}

		if reasoning == nil {
			if msg.ReasoningContent != nil {
				reasoning = msg.ReasoningContent
			} else if msg.Reasoning != nil {
				reasoning = msg.Reasoning
			}
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	toolCalls := make([]ToolCallRequest, 0, len(mergedRawCalls))
	for _, tc := range mergedRawCalls {
		args, err := parseArguments(tc.Function.Arguments)
		if err != nil {
			args = map[string]any{}
		}
		toolCalls = append(toolCalls, ToolCallRequest{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	slog.Info("provider: merged multiple choices", "count", len(raw.Choices), "tool_calls", len(toolCalls))

	usage := map[string]int{}
	if raw.Usage != nil {
		usage["prompt_tokens"] = raw.Usage.PromptTokens
		usage["completion_tokens"] = raw.Usage.CompletionTokens
		usage["total_tokens"] = raw.Usage.TotalTokens
	}

	if finishReason == "tool_calls" && len(toolCalls) == 0 {
		slog.Warn("provider: finish_reason=tool_calls but no tool_calls parsed; check raw response",
			"raw_tool_calls", len(mergedRawCalls))
	}

	return &LLMResponse{
		Content:          mergedContent,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		Usage:            usage,
		ReasoningContent: reasoning,
	}, nil
}

// parseArguments decodes a JSON string into map[string]any.
// It attempts to repair common truncation issues (missing closing brackets).
func parseArguments(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return map[string]any{}, nil
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err == nil {
		return result, nil
	}

	// Attempt simple repair: append missing closing braces.
	repaired := raw
	open := strings.Count(raw, "{") - strings.Count(raw, "}")
	for i := 0; i < open && i < 5; i++ {
		repaired += "}"
	}
	if err := json.Unmarshal([]byte(repaired), &result); err == nil {
		return result, nil
	}

	return nil, fmt.Errorf("provider: parse tool arguments: invalid JSON: %s", truncate(raw, 64))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseModelName extracts the model name from a provider/model format.
// If the model contains a "/", it returns the part after the "/".
// Otherwise, it returns the model as-is.
// Examples:
//   - "custom/gpt-5-mini" -> "gpt-5-mini"
//   - "openai/gpt-4" -> "gpt-4"
//   - "gpt-4" -> "gpt-4"
func parseModelName(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}
