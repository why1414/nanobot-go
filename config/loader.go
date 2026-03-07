package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/libo/nanobot-go/provider"
)

// GetConfigPath returns the default configuration file path.
func GetConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nanobot/config.json"
	}
	return filepath.Join(home, ".nanobot", "config.json")
}

// LoadConfig loads configuration from a JSON file.
// If the file does not exist, returns the default configuration.
func LoadConfig(configPath string) (*Config, error) {
	path := configPath
	if path == "" {
		path = GetConfigPath()
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("config file not found, using defaults", "path", path)
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		slog.Warn("failed to parse config, using defaults", "error", err, "path", path)
		applyEnvOverrides(cfg)
		return cfg, nil
	}

	// Migrate old config formats
	migrateConfig(cfg)

	applyEnvOverrides(cfg)
	return cfg, nil
}

// SaveConfig saves configuration to a JSON file.
func SaveConfig(cfg *Config, configPath string) error {
	path := configPath
	if path == "" {
		path = GetConfigPath()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// migrateConfig handles migration from old config formats.
func migrateConfig(cfg *Config) {
	// Move tools.exec.restrictToWorkspace → tools.restrictToWorkspace
	// (already handled by struct field names matching)
}

// applyEnvOverrides applies environment variable overrides to the config.
// Variables use NANOBOT_ prefix and __ delimiter for nested keys.
// Example: NANOBOT_AGENTS__DEFAULTS__MODEL=claude-haiku-4.5
func applyEnvOverrides(cfg *Config) {
	// Provider API keys
	applyProviderEnvOverrides(cfg)

	// Agent defaults
	if v := os.Getenv("NANOBOT_AGENTS__DEFAULTS__MODEL"); v != "" {
		cfg.Agents.Defaults.Model = v
	}
	if v := os.Getenv("NANOBOT_AGENTS__DEFAULTS__WORKSPACE"); v != "" {
		cfg.Agents.Defaults.Workspace = v
	}

	// Gateway
	if v := os.Getenv("NANOBOT_GATEWAY__HOST"); v != "" {
		cfg.Gateway.Host = v
	}
	if v := os.Getenv("NANOBOT_GATEWAY__PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.Gateway.Port)
	}

	// Feishu channel
	if v := os.Getenv("NANOBOT_CHANNELS__FEISHU__APP_ID"); v != "" {
		cfg.Channels.Feishu.AppID = v
	}
	if v := os.Getenv("NANOBOT_CHANNELS__FEISHU__APP_SECRET"); v != "" {
		cfg.Channels.Feishu.AppSecret = v
	}
	if v := os.Getenv("NANOBOT_CHANNELS__FEISHU__ENCRYPT_KEY"); v != "" {
		cfg.Channels.Feishu.EncryptKey = v
	}

	// Telegram channel
	if v := os.Getenv("NANOBOT_CHANNELS__TELEGRAM__TOKEN"); v != "" {
		cfg.Channels.Telegram.Token = v
	}

	// Discord channel
	if v := os.Getenv("NANOBOT_CHANNELS__DISCORD__TOKEN"); v != "" {
		cfg.Channels.Discord.Token = v
	}

	// Slack channel
	if v := os.Getenv("NANOBOT_CHANNELS__SLACK__BOT_TOKEN"); v != "" {
		cfg.Channels.Slack.BotToken = v
	}
	if v := os.Getenv("NANOBOT_CHANNELS__SLACK__APP_TOKEN"); v != "" {
		cfg.Channels.Slack.AppToken = v
	}

	// QQ channel
	if v := os.Getenv("NANOBOT_CHANNELS__QQ__APP_ID"); v != "" {
		cfg.Channels.QQ.AppID = v
	}
	if v := os.Getenv("NANOBOT_CHANNELS__QQ__SECRET"); v != "" {
		cfg.Channels.QQ.Secret = v
	}

	// DingTalk channel
	if v := os.Getenv("NANOBOT_CHANNELS__DINGTALK__CLIENT_ID"); v != "" {
		cfg.Channels.DingTalk.ClientID = v
	}
	if v := os.Getenv("NANOBOT_CHANNELS__DINGTALK__CLIENT_SECRET"); v != "" {
		cfg.Channels.DingTalk.ClientSecret = v
	}
}

// applyProviderEnvOverrides applies environment variable overrides for provider API keys.
func applyProviderEnvOverrides(cfg *Config) {
	// Map provider names to their config field and env var
	providerEnvs := map[string]struct {
		configField *ProviderConfig
		envKey      string
	}{
		"anthropic":     {&cfg.Providers.Anthropic, "ANTHROPIC_API_KEY"},
		"openai":        {&cfg.Providers.OpenAI, "OPENAI_API_KEY"},
		"openrouter":    {&cfg.Providers.OpenRouter, "OPENROUTER_API_KEY"},
		"deepseek":      {&cfg.Providers.DeepSeek, "DEEPSEEK_API_KEY"},
		"groq":          {&cfg.Providers.Groq, "GROQ_API_KEY"},
		"zhipu":         {&cfg.Providers.Zhipu, "ZAI_API_KEY"},
		"dashscope":     {&cfg.Providers.DashScope, "DASHSCOPE_API_KEY"},
		"gemini":        {&cfg.Providers.Gemini, "GEMINI_API_KEY"},
		"moonshot":      {&cfg.Providers.Moonshot, "MOONSHOT_API_KEY"},
		"minimax":       {&cfg.Providers.MiniMax, "MINIMAX_API_KEY"},
		"aihubmix":      {&cfg.Providers.AiHubMix, "OPENAI_API_KEY"},
		"siliconflow":   {&cfg.Providers.SiliconFlow, "OPENAI_API_KEY"},
		"volcengine":    {&cfg.Providers.VolcEngine, "OPENAI_API_KEY"},
		"githubCopilot": {&cfg.Providers.GitHubCopilot, "GITHUB_TOKEN"},
	}

	for _, item := range providerEnvs {
		if item.configField.APIKey == "" {
			if v := os.Getenv(item.envKey); v != "" {
				item.configField.APIKey = v
			}
		}
	}
}

// MatchResult holds the result of provider matching.
type MatchResult struct {
	Config *ProviderConfig
	Name   string
	Spec   *provider.ProviderSpec
}

// GetProvider matches a provider by model name and returns its config and spec.
// The matching logic follows this order:
//  1. Explicit provider prefix (e.g., "deepseek/..." → deepseek provider)
//  2. Keyword match in model name (e.g., "claude-3-opus" → anthropic provider)
//  3. Fallback to first gateway with API key, then first standard provider with API key
func (c *Config) GetProvider(model string) *MatchResult {
	modelLower := strings.ToLower(model)
	modelNormalized := strings.ReplaceAll(modelLower, "-", "_")

	var prefix string
	var normalizedPrefix string
	if idx := strings.IndexByte(modelLower, '/'); idx >= 0 {
		prefix = modelLower[:idx]
		normalizedPrefix = strings.ReplaceAll(prefix, "-", "_")
	}

	// Helper to check keyword match
	kwMatches := func(kw string) bool {
		kw = strings.ToLower(kw)
		return strings.Contains(modelLower, kw) || strings.Contains(modelNormalized, strings.ReplaceAll(kw, "-", "_"))
	}

	// Provider config lookup by spec name
	getConfig := func(name string) *ProviderConfig {
		switch name {
		case "custom":
			return &c.Providers.Custom
		case "copilot":
			return &c.Providers.GitHubCopilot
		case "openrouter":
			return &c.Providers.OpenRouter
		case "aihubmix":
			return &c.Providers.AiHubMix
		case "siliconflow":
			return &c.Providers.SiliconFlow
		case "volcengine":
			return &c.Providers.VolcEngine
		case "anthropic":
			return &c.Providers.Anthropic
		case "openai":
			return &c.Providers.OpenAI
		case "openai_codex":
			return &c.Providers.OpenAICodex
		case "deepseek":
			return &c.Providers.DeepSeek
		case "gemini":
			return &c.Providers.Gemini
		case "zhipu":
			return &c.Providers.Zhipu
		case "dashscope":
			return &c.Providers.DashScope
		case "moonshot":
			return &c.Providers.Moonshot
		case "minimax":
			return &c.Providers.MiniMax
		case "vllm":
			return &c.Providers.VLLM
		case "groq":
			return &c.Providers.Groq
		default:
			return nil
		}
	}

	// 1. Explicit provider prefix wins
	for _, spec := range provider.AllProviders() {
		if prefix != "" && normalizedPrefix == strings.ReplaceAll(spec.Name, "-", "_") {
			cfg := getConfig(spec.Name)
			if cfg != nil && (spec.IsOAuth || cfg.APIKey != "") {
				return &MatchResult{Config: cfg, Name: spec.Name, Spec: spec}
			}
		}
	}

	// 2. Match by keyword (order follows provider registry)
	for _, spec := range provider.AllProviders() {
		cfg := getConfig(spec.Name)
		if cfg == nil {
			continue
		}
		for _, kw := range spec.Keywords {
			if kwMatches(kw) && (spec.IsOAuth || cfg.APIKey != "") {
				return &MatchResult{Config: cfg, Name: spec.Name, Spec: spec}
			}
		}
	}

	// 3. Fallback: gateways first, then others (OAuth providers NOT valid fallbacks)
	for _, spec := range provider.AllProviders() {
		if spec.IsOAuth {
			continue
		}
		cfg := getConfig(spec.Name)
		if cfg != nil && cfg.APIKey != "" {
			return &MatchResult{Config: cfg, Name: spec.Name, Spec: spec}
		}
	}

	return nil
}

// GetAPIKey returns the API key for the matched provider.
func (c *Config) GetAPIKey(model string) string {
	result := c.GetProvider(model)
	if result == nil || result.Config == nil {
		return ""
	}
	return result.Config.APIKey
}

// GetAPIBase returns the API base URL for the matched provider.
// Returns the configured API base or the provider's default if it's a gateway.
func (c *Config) GetAPIBase(model string) string {
	result := c.GetProvider(model)
	if result == nil || result.Config == nil {
		return ""
	}
	if result.Config.APIBase != "" {
		return result.Config.APIBase
	}
	if result.Spec != nil && result.Spec.IsGateway && result.Spec.DefaultAPIBase != "" {
		return result.Spec.DefaultAPIBase
	}
	return ""
}

// GetModel returns the configured model or a default.
func (c *Config) GetModel() string {
	if c.Agents.Defaults.Model != "" {
		return c.Agents.Defaults.Model
	}
	return "claude-haiku-4.5"
}

// MergeFlags merges CLI flag values into the config.
// Non-empty flag values override config file values.
func (c *Config) MergeFlags(model, apiKey, apiBase, workspace string, maxIter int, temp float64, maxTokens int) {
	if model != "" {
		c.Agents.Defaults.Model = model
	}
	if apiKey != "" {
		// Store in the matched provider's config
		result := c.GetProvider(model)
		if result != nil && result.Config != nil {
			result.Config.APIKey = apiKey
		} else {
			// No provider matched, store in custom
			c.Providers.Custom.APIKey = apiKey
		}
	}
	if apiBase != "" {
		result := c.GetProvider(model)
		if result != nil && result.Config != nil {
			result.Config.APIBase = apiBase
		} else {
			c.Providers.Custom.APIBase = apiBase
		}
	}
	if workspace != "" {
		c.Agents.Defaults.Workspace = workspace
	}
	if maxIter > 0 {
		c.Agents.Defaults.MaxToolIterations = maxIter
	}
	if temp > 0 {
		c.Agents.Defaults.Temperature = temp
	}
	if maxTokens > 0 {
		c.Agents.Defaults.MaxTokens = maxTokens
	}
}
