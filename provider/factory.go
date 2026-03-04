// Package provider — factory for constructing LLMProvider instances.
package provider

import (
	"fmt"
	"os"
	"strings"
)

// ProviderConfig holds everything needed to instantiate an LLMProvider.
type ProviderConfig struct {
	// APIKey is the provider's authentication token.
	// When empty, the factory falls back to the relevant environment variable.
	APIKey string

	// APIBase overrides the default base URL.
	// When empty, the spec's DefaultAPIBase (or the provider's own default) is used.
	APIBase string

	// Model is the default model for this provider instance.
	Model string

	// ProviderName selects a specific registered provider by name (e.g. "openrouter").
	// When empty, the factory auto-detects from APIKey / APIBase / Model.
	ProviderName string

	// ExtraHeaders are sent verbatim on every HTTP request.
	ExtraHeaders map[string]string
}

// NewProvider constructs an LLMProvider from cfg.
//
// Resolution order:
//  1. If ProviderName == "custom" (or IsDirect spec), return a bare
//     OpenAICompatProvider with the supplied APIBase and Model.
//  2. Detect gateway (ProviderName → api_key prefix → api_base keyword).
//  3. Auto-detect standard provider from Model name.
//  4. Default to a plain OpenAI-compatible provider if nothing matches.
//
// The returned provider always calls the API directly via HTTP; no LiteLLM
// or other routing layer is involved.
func NewProvider(cfg ProviderConfig) (LLMProvider, error) {
	// --- Resolve API key (config beats env) ----------------------------------
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = resolveEnvKey(cfg)
	}

	// --- Resolve base URL ----------------------------------------------------
	baseURL := cfg.APIBase

	// --- Custom / direct provider --------------------------------------------
	if cfg.ProviderName == "custom" {
		if baseURL == "" {
			return nil, fmt.Errorf("provider: custom provider requires APIBase")
		}
		return buildProvider(apiKey, baseURL, cfg.Model, cfg.ExtraHeaders), nil
	}

	// --- Gateway detection ---------------------------------------------------
	gateway := FindGateway(cfg.ProviderName, apiKey, baseURL)
	if gateway != nil {
		if baseURL == "" {
			baseURL = gateway.DefaultAPIBase
		}
		if baseURL == "" {
			return nil, fmt.Errorf("provider: gateway %q requires APIBase", gateway.Name)
		}
		return buildProvider(apiKey, baseURL, cfg.Model, cfg.ExtraHeaders), nil
	}

	// --- Standard provider ---------------------------------------------------
	var spec *ProviderSpec
	if cfg.ProviderName != "" {
		spec = FindByName(cfg.ProviderName)
	}
	if spec == nil && cfg.Model != "" {
		spec = FindByModel(cfg.Model)
	}

	if spec != nil {
		if baseURL == "" {
			baseURL = spec.DefaultAPIBase
		}
		// For known providers without a custom baseURL, use the standard endpoint.
		// Most standard providers don't set DefaultAPIBase because the SDK handles it,
		// but for our direct HTTP path we need OpenAI's canonical URL as fallback.
		if baseURL == "" {
			baseURL = standardBaseURL(spec.Name)
		}
		if apiKey == "" {
			apiKey = os.Getenv(spec.EnvKey)
		}
	}

	if baseURL == "" {
		// Last-resort fallback: treat as plain OpenAI-compatible.
		baseURL = "https://api.openai.com/v1"
	}

	return buildProvider(apiKey, baseURL, cfg.Model, cfg.ExtraHeaders), nil
}

// buildProvider wires a configured OpenAICompatProvider.
func buildProvider(apiKey, baseURL, model string, extraHeaders map[string]string) *OpenAICompatProvider {
	p := NewOpenAICompatProvider(apiKey, baseURL, model)
	if len(extraHeaders) > 0 {
		p = p.WithExtraHeaders(extraHeaders)
	}
	return p
}

// resolveEnvKey looks up the API key env var implied by cfg.
func resolveEnvKey(cfg ProviderConfig) string {
	var spec *ProviderSpec
	if cfg.ProviderName != "" {
		spec = FindByName(cfg.ProviderName)
	}
	if spec == nil && cfg.Model != "" {
		spec = FindByModel(cfg.Model)
	}
	if spec == nil {
		// Try gateway detection without an explicit key (api_base keyword).
		spec = FindGateway(cfg.ProviderName, "", cfg.APIBase)
	}
	if spec != nil && spec.EnvKey != "" {
		return os.Getenv(spec.EnvKey)
	}
	return ""
}

// standardBaseURL returns the well-known REST endpoint for named providers
// that the Go direct-HTTP path needs but the Python side delegates to LiteLLM.
func standardBaseURL(name string) string {
	switch strings.ToLower(name) {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	default:
		return ""
	}
}
