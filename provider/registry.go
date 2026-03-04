// Package provider — registry of well-known LLM providers.
//
// Adding a new provider: append a ProviderSpec to providers and it will
// automatically participate in FindByModel / FindGateway lookups.
package provider

import "strings"

// ProviderSpec holds metadata for one LLM provider.
type ProviderSpec struct {
	// Name is the config-key name, e.g. "dashscope".
	Name string
	// Keywords are lowercase model-name substrings that identify this provider.
	Keywords []string
	// EnvKey is the primary environment variable for the API key.
	EnvKey string
	// DisplayName is a human-readable label shown in status output.
	DisplayName string

	// LiteLLMPrefix is prepended to model names for LiteLLM routing (unused in Go
	// direct HTTP path, kept for documentation and future use).
	LiteLLMPrefix string
	// SkipPrefixes: skip adding LiteLLMPrefix when model already starts with one of these.
	SkipPrefixes []string

	// IsGateway marks providers that can route any model (e.g. OpenRouter).
	IsGateway bool
	// IsLocal marks locally-deployed providers (e.g. vLLM, Ollama).
	IsLocal bool

	// DetectByKeyPrefix: auto-detect when API key starts with this string.
	DetectByKeyPrefix string
	// DetectByBaseKeyword: auto-detect when api_base URL contains this string.
	DetectByBaseKeyword string
	// DefaultAPIBase is the canonical base URL for this provider.
	DefaultAPIBase string

	// StripModelPrefix: remove "provider/" before re-prefixing (e.g. AiHubMix).
	StripModelPrefix bool

	// IsOAuth indicates OAuth-based auth (no API key, e.g. OpenAI Codex).
	IsOAuth bool
	// IsDirect bypasses any routing layer (direct HTTP calls).
	IsDirect bool
	// SupportsPromptCaching enables cache_control injection (Anthropic, OpenRouter).
	SupportsPromptCaching bool
}

// Label returns the display name, falling back to title-cased Name.
func (s *ProviderSpec) Label() string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	return strings.Title(s.Name) //nolint:staticcheck // simple label, not user-visible locale text
}

// providers is the ordered registry. Order = match priority.
// Gateways first; standard providers follow.
var providers = []*ProviderSpec{

	// Custom (direct OpenAI-compatible endpoint) ─────────────────────────────
	{
		Name:        "custom",
		Keywords:    nil,
		EnvKey:      "",
		DisplayName: "Custom",
		IsDirect:    true,
	},

	// Gateways ────────────────────────────────────────────────────────────────

	{
		Name:                  "openrouter",
		Keywords:              []string{"openrouter"},
		EnvKey:                "OPENROUTER_API_KEY",
		DisplayName:           "OpenRouter",
		LiteLLMPrefix:         "openrouter",
		IsGateway:             true,
		DetectByKeyPrefix:     "sk-or-",
		DetectByBaseKeyword:   "openrouter",
		DefaultAPIBase:        "https://openrouter.ai/api/v1",
		SupportsPromptCaching: true,
	},

	{
		Name:                "aihubmix",
		Keywords:            []string{"aihubmix"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "AiHubMix",
		LiteLLMPrefix:       "openai",
		IsGateway:           true,
		DetectByBaseKeyword: "aihubmix",
		DefaultAPIBase:      "https://aihubmix.com/v1",
		StripModelPrefix:    true,
	},

	{
		Name:                "siliconflow",
		Keywords:            []string{"siliconflow"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "SiliconFlow",
		LiteLLMPrefix:       "openai",
		IsGateway:           true,
		DetectByBaseKeyword: "siliconflow",
		DefaultAPIBase:      "https://api.siliconflow.cn/v1",
	},

	{
		Name:                "volcengine",
		Keywords:            []string{"volcengine", "volces", "ark"},
		EnvKey:              "OPENAI_API_KEY",
		DisplayName:         "VolcEngine",
		LiteLLMPrefix:       "volcengine",
		IsGateway:           true,
		DetectByBaseKeyword: "volces",
		DefaultAPIBase:      "https://ark.cn-beijing.volces.com/api/v3",
	},

	// Standard providers ──────────────────────────────────────────────────────

	{
		Name:                  "anthropic",
		Keywords:              []string{"anthropic", "claude"},
		EnvKey:                "ANTHROPIC_API_KEY",
		DisplayName:           "Anthropic",
		SupportsPromptCaching: true,
	},

	{
		Name:        "openai",
		Keywords:    []string{"openai", "gpt"},
		EnvKey:      "OPENAI_API_KEY",
		DisplayName: "OpenAI",
	},

	{
		Name:                "openai_codex",
		Keywords:            []string{"openai-codex", "codex"},
		EnvKey:              "",
		DisplayName:         "OpenAI Codex",
		DetectByBaseKeyword: "codex",
		DefaultAPIBase:      "https://chatgpt.com/backend-api",
		IsOAuth:             true,
	},

	{
		Name:        "deepseek",
		Keywords:    []string{"deepseek"},
		EnvKey:      "DEEPSEEK_API_KEY",
		DisplayName: "DeepSeek",
		LiteLLMPrefix: "deepseek",
		SkipPrefixes:  []string{"deepseek/"},
	},

	{
		Name:          "gemini",
		Keywords:      []string{"gemini"},
		EnvKey:        "GEMINI_API_KEY",
		DisplayName:   "Gemini",
		LiteLLMPrefix: "gemini",
		SkipPrefixes:  []string{"gemini/"},
	},

	{
		Name:          "zhipu",
		Keywords:      []string{"zhipu", "glm", "zai"},
		EnvKey:        "ZAI_API_KEY",
		DisplayName:   "Zhipu AI",
		LiteLLMPrefix: "zai",
		SkipPrefixes:  []string{"zhipu/", "zai/", "openrouter/", "hosted_vllm/"},
	},

	{
		Name:          "dashscope",
		Keywords:      []string{"qwen", "dashscope"},
		EnvKey:        "DASHSCOPE_API_KEY",
		DisplayName:   "DashScope",
		LiteLLMPrefix: "dashscope",
		SkipPrefixes:  []string{"dashscope/", "openrouter/"},
	},

	{
		Name:           "moonshot",
		Keywords:       []string{"moonshot", "kimi"},
		EnvKey:         "MOONSHOT_API_KEY",
		DisplayName:    "Moonshot",
		LiteLLMPrefix:  "moonshot",
		SkipPrefixes:   []string{"moonshot/", "openrouter/"},
		DefaultAPIBase: "https://api.moonshot.ai/v1",
	},

	{
		Name:           "minimax",
		Keywords:       []string{"minimax"},
		EnvKey:         "MINIMAX_API_KEY",
		DisplayName:    "MiniMax",
		LiteLLMPrefix:  "minimax",
		SkipPrefixes:   []string{"minimax/", "openrouter/"},
		DefaultAPIBase: "https://api.minimax.io/v1",
	},

	// Local deployment ────────────────────────────────────────────────────────

	{
		Name:          "vllm",
		Keywords:      []string{"vllm"},
		EnvKey:        "HOSTED_VLLM_API_KEY",
		DisplayName:   "vLLM/Local",
		LiteLLMPrefix: "hosted_vllm",
		IsLocal:       true,
	},

	// Auxiliary ───────────────────────────────────────────────────────────────

	{
		Name:          "groq",
		Keywords:      []string{"groq"},
		EnvKey:        "GROQ_API_KEY",
		DisplayName:   "Groq",
		LiteLLMPrefix: "groq",
		SkipPrefixes:  []string{"groq/"},
	},
}

// FindByModel matches a standard (non-gateway, non-local) provider by
// case-insensitive model-name keyword. Returns nil when no match is found.
func FindByModel(model string) *ProviderSpec {
	lower := strings.ToLower(model)
	normalized := strings.ReplaceAll(lower, "-", "_")

	var prefix string
	var normalizedPrefix string
	if idx := strings.IndexByte(lower, '/'); idx >= 0 {
		prefix = lower[:idx]
		normalizedPrefix = strings.ReplaceAll(prefix, "-", "_")
	}

	// Collect only standard (non-gateway, non-local) specs.
	var std []*ProviderSpec
	for _, s := range providers {
		if !s.IsGateway && !s.IsLocal {
			std = append(std, s)
		}
	}

	// First pass: explicit provider/ prefix takes priority.
	if prefix != "" {
		for _, s := range std {
			if normalizedPrefix == strings.ReplaceAll(s.Name, "-", "_") {
				return s
			}
		}
	}

	// Second pass: keyword anywhere in model name.
	for _, s := range std {
		for _, kw := range s.Keywords {
			kwNorm := strings.ReplaceAll(kw, "-", "_")
			if strings.Contains(lower, kw) || strings.Contains(normalized, kwNorm) {
				return s
			}
		}
	}
	return nil
}

// FindGateway detects a gateway or local provider in priority order:
//  1. providerName matches a gateway/local spec by name.
//  2. apiKey starts with DetectByKeyPrefix.
//  3. apiBase contains DetectByBaseKeyword.
func FindGateway(providerName, apiKey, apiBase string) *ProviderSpec {
	// 1. Direct name match.
	if providerName != "" {
		if s := FindByName(providerName); s != nil && (s.IsGateway || s.IsLocal) {
			return s
		}
	}

	// 2 & 3. Auto-detect.
	for _, s := range providers {
		if s.DetectByKeyPrefix != "" && strings.HasPrefix(apiKey, s.DetectByKeyPrefix) {
			return s
		}
		if s.DetectByBaseKeyword != "" && strings.Contains(apiBase, s.DetectByBaseKeyword) {
			return s
		}
	}
	return nil
}

// FindByName looks up a provider spec by its Name field.
func FindByName(name string) *ProviderSpec {
	for _, s := range providers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// AllProviders returns a read-only snapshot of every registered provider.
func AllProviders() []*ProviderSpec {
	cp := make([]*ProviderSpec, len(providers))
	copy(cp, providers)
	return cp
}
