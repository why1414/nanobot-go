package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// GetConfigPath returns the default configuration file path.
func GetConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".nanobot-go/config.json"
	}
	return filepath.Join(home, ".nanobot-go", "config.json")
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

// applyEnvOverrides is kept for future extensions (currently empty).
func applyEnvOverrides(cfg *Config) {
	// No environment variable overrides for this demo
	// All configuration comes from config.json
}

// applyProviderEnvOverrides is kept for future extensions (currently empty).
func applyProviderEnvOverrides(cfg *Config) {
	// No environment variable overrides for this demo
	// All provider configuration comes from config.json
}

// GetAPIKey returns the API key for the configured provider.
// The model parameter should be in format "provider/model-name".
// Returns empty string if provider not found.
func (c *Config) GetAPIKey(model string) string {
	providerName := extractProviderName(model)
	if providerName == "" {
		// Fallback to "custom" provider if no prefix
		providerName = "custom"
	}

	if provider, ok := c.Providers[providerName]; ok {
		return provider.APIKey
	}
	return ""
}

// GetAPIBase returns the API base URL for the configured provider.
// The model parameter should be in format "provider/model-name".
// Returns default OpenAI endpoint if provider not found.
func (c *Config) GetAPIBase(model string) string {
	providerName := extractProviderName(model)
	if providerName == "" {
		// Fallback to "custom" provider if no prefix
		providerName = "custom"
	}

	if provider, ok := c.Providers[providerName]; ok {
		if provider.APIBase != "" {
			return provider.APIBase
		}
	}

	// Default to OpenAI's endpoint
	return "https://api.openai.com/v1"
}

// extractProviderName extracts the provider name from a "provider/model" format.
// Returns empty string if no provider prefix is found.
func extractProviderName(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[:idx]
	}
	return ""
}

// GetModel returns the configured model.
// Model format: provider/model-name (e.g., "copilot/gpt-5-mini", "openai/gpt-4")
// Returns empty string if not configured (user must set it in config.json)
func (c *Config) GetModel() string {
	return c.Agents.Defaults.Model
}

// MergeFlags merges CLI flag values into the config.
// Non-empty flag values override config file values.
func (c *Config) MergeFlags(model, apiKey, apiBase, workspace string, maxIter int, temp float64, maxTokens int) {
	if model != "" {
		c.Agents.Defaults.Model = model
	}
	if apiKey != "" || apiBase != "" {
		// Store in custom provider config
		if c.Providers == nil {
			c.Providers = make(map[string]ProviderConfig)
		}
		custom := c.Providers["custom"]
		if apiKey != "" {
			custom.APIKey = apiKey
		}
		if apiBase != "" {
			custom.APIBase = apiBase
		}
		c.Providers["custom"] = custom
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
