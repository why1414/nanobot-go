// Package config provides configuration loading and management for nanobot-go.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config is the root configuration for nanobot-go.
type Config struct {
	Agents   AgentsConfig    `json:"agents"`
	Channels ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Gateway  GatewayConfig   `json:"gateway"`
	Tools    ToolsConfig     `json:"tools"`
}

// AgentsConfig holds agent configuration.
type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
}

// AgentDefaults holds default agent settings.
type AgentDefaults struct {
	Workspace        string  `json:"workspace"`
	// Model format: provider/model-name (e.g., "copilot/gpt-5-mini", "openai/gpt-4")
	Model            string  `json:"model"`
	MaxTokens        int     `json:"maxTokens"`
	Temperature      float64 `json:"temperature"`
	MaxToolIterations int    `json:"maxToolIterations"`
	MemoryWindow     int     `json:"memoryWindow"`
}

// ProvidersConfig holds configuration for LLM providers.
// Provider names are keys (e.g., "custom", "openrouter", "openai").
type ProvidersConfig map[string]ProviderConfig

// ProviderConfig holds LLM provider configuration.
type ProviderConfig struct {
	APIKey       string            `json:"apiKey"`
	APIBase      string            `json:"apiBase"`
	ExtraHeaders map[string]string `json:"extraHeaders"`
}

// ChannelsConfig holds configuration for chat channels.
type ChannelsConfig struct {
	SendProgress   bool          `json:"sendProgress"`
	SendToolHints  bool          `json:"sendToolHints"`
	Feishu         FeishuConfig  `json:"feishu"`
}

// FeishuConfig holds Feishu/Lark channel configuration.
type FeishuConfig struct {
	Enabled            bool     `json:"enabled"`
	AppID              string   `json:"appId"`
	AppSecret          string   `json:"appSecret"`
	EncryptKey         string   `json:"encryptKey"`
	VerificationToken  string   `json:"verificationToken"`
	AllowFrom          []string `json:"allowFrom"`
}

// HeartbeatConfig holds heartbeat service configuration.
type HeartbeatConfig struct {
	Enabled   bool `json:"enabled"`
	IntervalS int  `json:"intervalS"`
}

// GatewayConfig holds gateway/server configuration.
type GatewayConfig struct {
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
}

// WebSearchConfig holds web search tool configuration.
type WebSearchConfig struct {
	APIKey     string `json:"apiKey"`
	MaxResults int    `json:"maxResults"`
}

// WebToolsConfig holds web tools configuration.
type WebToolsConfig struct {
	Search WebSearchConfig `json:"search"`
}

// ExecToolConfig holds shell exec tool configuration.
type ExecToolConfig struct {
	Timeout int `json:"timeout"`
}

// MCPServerConfig holds MCP server connection configuration.
type MCPServerConfig struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	ToolTimeout int               `json:"toolTimeout"`
}

// ToolsConfig holds tools configuration.
type ToolsConfig struct {
	Exec               ExecToolConfig            `json:"exec"`
	RestrictToWorkspace bool                      `json:"restrictToWorkspace"`
	MCPServers         map[string]MCPServerConfig `json:"mcpServers"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         "~/.nanobot-go/workspace",
				Model:             "",
				MaxTokens:         8192,
				Temperature:       0.1,
				MaxToolIterations: 40,
				MemoryWindow:      100,
			},
		},
		Channels: ChannelsConfig{
			SendProgress:  true,
			SendToolHints: false,
		},
		Providers: ProvidersConfig{
			"custom": {
				APIBase: "http://localhost:4141/v1",
			},
		},
		Gateway: GatewayConfig{
			Host: "localhost",
			Port: 18790,
			Heartbeat: HeartbeatConfig{
				Enabled:   true,
				IntervalS: 30 * 60,
			},
		},
		Tools: ToolsConfig{
			Exec: ExecToolConfig{
				Timeout: 60,
			},
		},
	}
}

// WorkspacePath returns the expanded workspace path.
func (c *Config) WorkspacePath() string {
	return expandHome(c.Agents.Defaults.Workspace)
}

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
