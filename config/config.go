// Package config provides configuration loading and management for nanobot.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Config is the root configuration for nanobot.
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
	Model            string  `json:"model"`
	MaxTokens        int     `json:"maxTokens"`
	Temperature      float64 `json:"temperature"`
	MaxToolIterations int    `json:"maxToolIterations"`
	MemoryWindow     int     `json:"memoryWindow"`
}

// ProvidersConfig holds configuration for LLM providers.
type ProvidersConfig struct {
	Custom        ProviderConfig `json:"custom"`
	Anthropic     ProviderConfig `json:"anthropic"`
	OpenAI        ProviderConfig `json:"openai"`
	OpenRouter    ProviderConfig `json:"openrouter"`
	DeepSeek      ProviderConfig `json:"deepseek"`
	Groq          ProviderConfig `json:"groq"`
	Zhipu         ProviderConfig `json:"zhipu"`
	DashScope     ProviderConfig `json:"dashscope"`
	VLLM          ProviderConfig `json:"vllm"`
	Gemini        ProviderConfig `json:"gemini"`
	Moonshot      ProviderConfig `json:"moonshot"`
	MiniMax       ProviderConfig `json:"minimax"`
	AiHubMix      ProviderConfig `json:"aihubmix"`
	SiliconFlow   ProviderConfig `json:"siliconflow"`
	VolcEngine    ProviderConfig `json:"volcengine"`
	OpenAICodex   ProviderConfig `json:"openaiCodex"`
	GitHubCopilot ProviderConfig `json:"githubCopilot"`
}

// ProviderConfig holds LLM provider configuration.
type ProviderConfig struct {
	APIKey       string            `json:"apiKey"`
	APIBase      string            `json:"apiBase"`
	ExtraHeaders map[string]string `json:"extraHeaders"`
}

// ChannelsConfig holds configuration for chat channels.
type ChannelsConfig struct {
	SendProgress   bool            `json:"sendProgress"`
	SendToolHints  bool            `json:"sendToolHints"`
	WhatsApp       WhatsAppConfig  `json:"whatsapp"`
	Telegram       TelegramConfig  `json:"telegram"`
	Feishu         FeishuConfig    `json:"feishu"`
	DingTalk       DingTalkConfig  `json:"dingtalk"`
	Discord        DiscordConfig   `json:"discord"`
	Email          EmailConfig     `json:"email"`
	Mochat         MochatConfig    `json:"mochat"`
	Slack          SlackConfig     `json:"slack"`
	QQ             QQConfig        `json:"qq"`
}

// WhatsAppConfig holds WhatsApp channel configuration.
type WhatsAppConfig struct {
	Enabled     bool     `json:"enabled"`
	BridgeURL   string   `json:"bridgeUrl"`
	BridgeToken string   `json:"bridgeToken"`
	AllowFrom   []string `json:"allowFrom"`
}

// TelegramConfig holds Telegram channel configuration.
type TelegramConfig struct {
	Enabled        bool     `json:"enabled"`
	Token          string   `json:"token"`
	AllowFrom      []string `json:"allowFrom"`
	Proxy          string   `json:"proxy"`
	ReplyToMessage bool     `json:"replyToMessage"`
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

// DingTalkConfig holds DingTalk channel configuration.
type DingTalkConfig struct {
	Enabled      bool     `json:"enabled"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"`
	AllowFrom    []string `json:"allowFrom"`
}

// DiscordConfig holds Discord channel configuration.
type DiscordConfig struct {
	Enabled     bool     `json:"enabled"`
	Token       string   `json:"token"`
	AllowFrom   []string `json:"allowFrom"`
	GatewayURL  string   `json:"gatewayUrl"`
	Intents     int      `json:"intents"`
}

// EmailConfig holds Email channel configuration.
type EmailConfig struct {
	Enabled            bool     `json:"enabled"`
	ConsentGranted     bool     `json:"consentGranted"`
	IMAPHost           string   `json:"imapHost"`
	IMAPPort           int      `json:"imapPort"`
	IMAPUsername       string   `json:"imapUsername"`
	IMAPPassword       string   `json:"imapPassword"`
	IMAPMailbox        string   `json:"imapMailbox"`
	IMAPUseSSL         bool     `json:"imapUseSSL"`
	SMTPHost           string   `json:"smtpHost"`
	SMTPPort           int      `json:"smtpPort"`
	SMTPUsername       string   `json:"smtpUsername"`
	SMTPPassword       string   `json:"smtpPassword"`
	SMTPUseTLS         bool     `json:"smtpUseTls"`
	SMTPUseSSL         bool     `json:"smtpUseSsl"`
	FromAddress        string   `json:"fromAddress"`
	AutoReplyEnabled   bool     `json:"autoReplyEnabled"`
	PollIntervalSecs   int      `json:"pollIntervalSecs"`
	MarkSeen           bool     `json:"markSeen"`
	MaxBodyChars       int      `json:"maxBodyChars"`
	SubjectPrefix      string   `json:"subjectPrefix"`
	AllowFrom          []string `json:"allowFrom"`
}

// MochatMentionConfig holds Mochat mention behavior configuration.
type MochatMentionConfig struct {
	RequireInGroups bool `json:"requireInGroups"`
}

// MochatGroupRule holds Mochat per-group mention requirement.
type MochatGroupRule struct {
	RequireMention bool `json:"requireMention"`
}

// MochatConfig holds Mochat channel configuration.
type MochatConfig struct {
	Enabled                    bool                         `json:"enabled"`
	BaseURL                    string                       `json:"baseUrl"`
	SocketURL                  string                       `json:"socketUrl"`
	SocketPath                 string                       `json:"socketPath"`
	SocketDisableMsgpack       bool                         `json:"socketDisableMsgpack"`
	SocketReconnectDelayMs     int                          `json:"socketReconnectDelayMs"`
	SocketMaxReconnectDelayMs  int                          `json:"socketMaxReconnectDelayMs"`
	SocketConnectTimeoutMs     int                          `json:"socketConnectTimeoutMs"`
	RefreshIntervalMs          int                          `json:"refreshIntervalMs"`
	WatchTimeoutMs             int                          `json:"watchTimeoutMs"`
	WatchLimit                 int                          `json:"watchLimit"`
	RetryDelayMs               int                          `json:"retryDelayMs"`
	MaxRetryAttempts           int                          `json:"maxRetryAttempts"`
	ClawToken                  string                       `json:"clawToken"`
	AgentUserID                string                       `json:"agentUserId"`
	Sessions                   []string                     `json:"sessions"`
	Panels                     []string                     `json:"panels"`
	AllowFrom                  []string                     `json:"allowFrom"`
	Mention                    MochatMentionConfig          `json:"mention"`
	Groups                     map[string]MochatGroupRule   `json:"groups"`
	ReplyDelayMode             string                       `json:"replyDelayMode"`
	ReplyDelayMs               int                          `json:"replyDelayMs"`
}

// SlackDMConfig holds Slack DM policy configuration.
type SlackDMConfig struct {
	Enabled   bool     `json:"enabled"`
	Policy    string   `json:"policy"`
	AllowFrom []string `json:"allowFrom"`
}

// SlackConfig holds Slack channel configuration.
type SlackConfig struct {
	Enabled        bool           `json:"enabled"`
	Mode           string         `json:"mode"`
	WebhookPath    string         `json:"webhookPath"`
	BotToken       string         `json:"botToken"`
	AppToken       string         `json:"appToken"`
	UserTokenReadOnly bool        `json:"userTokenReadOnly"`
	ReplyInThread  bool           `json:"replyInThread"`
	ReactEmoji     string         `json:"reactEmoji"`
	GroupPolicy    string         `json:"groupPolicy"`
	GroupAllowFrom []string       `json:"groupAllowFrom"`
	DM             SlackDMConfig  `json:"dm"`
}

// QQConfig holds QQ channel configuration.
type QQConfig struct {
	Enabled   bool     `json:"enabled"`
	AppID     string   `json:"appId"`
	Secret    string   `json:"secret"`
	AllowFrom []string `json:"allowFrom"`
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
	Web                WebToolsConfig            `json:"web"`
	Exec               ExecToolConfig            `json:"exec"`
	RestrictToWorkspace bool                      `json:"restrictToWorkspace"`
	MCPServers         map[string]MCPServerConfig `json:"mcpServers"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:         "~/.nanobot/workspace",
				Model:             "anthropic/claude-opus-4-5",
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
		Gateway: GatewayConfig{
			Host: "0.0.0.0",
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
