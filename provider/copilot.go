// Package provider — GitHub Copilot Proxy provider.
//
// CopilotProvider wraps OpenAICompatProvider but overrides response parsing
// to handle the non-standard behavior of the GitHub Copilot Proxy, which
// sometimes splits a single assistant turn across multiple choices:
//
//	choices[0]: { content: "...", tool_calls: null }
//	choices[1]: { content: null,  tool_calls: [...] }
//
// Standard OpenAI-compatible endpoints always return n=1 with everything in
// choices[0], so OpenAICompatProvider is the right default for those.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// CopilotProvider calls the GitHub Copilot Proxy /chat/completions endpoint.
// It merges all choices in the response into a single logical LLMResponse so
// that split content+tool_calls are handled correctly.
type CopilotProvider struct {
	*OpenAICompatProvider
}

// NewCopilotProvider creates a CopilotProvider backed by the given Copilot
// proxy endpoint.  apiKey is the OAuth/PAT token; baseURL is the proxy base
// URL (e.g. "https://api.githubcopilot.com").
func NewCopilotProvider(apiKey, baseURL, defaultModel string) *CopilotProvider {
	return &CopilotProvider{
		OpenAICompatProvider: NewOpenAICompatProvider(apiKey, baseURL, defaultModel),
	}
}

// Chat implements LLMProvider, delegating the HTTP call to the embedded
// provider and parsing the response with multi-choice merging.
func (p *CopilotProvider) Chat(ctx context.Context, messages []Message, opts ChatOptions) (*LLMResponse, error) {
	model := opts.Model
	if model == "" {
		model = p.defaultModel
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	temperature := opts.Temperature
	if temperature == 0 {
		temperature = defaultTemperature
	}

	data, err := json.Marshal(buildRequest(model, messages, opts.Tools, maxTokens, temperature))
	if err != nil {
		return nil, fmt.Errorf("provider: marshal request: %w", err)
	}

	body, err := p.doRequest(ctx, data)
	if err != nil {
		return nil, err
	}
	slog.Debug("raw LLM response", "body", truncate(string(body), 2048))
	return parseCopilotResponse(body)
}

// parseCopilotResponse merges all choices into a single LLMResponse.
// The Copilot Proxy may send content in choices[0] and tool_calls in
// choices[1]; merging ensures both are captured.
func parseCopilotResponse(body []byte) (*LLMResponse, error) {
	var raw openaiResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("provider: decode response: %w", err)
	}
	if len(raw.Choices) == 0 {
		return nil, fmt.Errorf("provider: response has no choices")
	}

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

	if len(raw.Choices) > 1 {
		slog.Info("copilot: merged multiple choices", "count", len(raw.Choices), "tool_calls", len(toolCalls))
	}

	usage := map[string]int{}
	if raw.Usage != nil {
		usage["prompt_tokens"] = raw.Usage.PromptTokens
		usage["completion_tokens"] = raw.Usage.CompletionTokens
		usage["total_tokens"] = raw.Usage.TotalTokens
	}

	if finishReason == "tool_calls" && len(toolCalls) == 0 {
		slog.Warn("copilot: finish_reason=tool_calls but no tool_calls parsed; check raw response",
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
