package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/channel"
	"github.com/libo/nanobot-go/config"
	"github.com/libo/nanobot-go/cron"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

// stringSliceFlag allows a flag to be specified multiple times.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string  { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error { *s = append(*s, v); return nil }

// runGateway implements the "gateway" subcommand.
func runGateway(args []string) {
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file (default: ~/.nanobot/config.json)")
	port := fs.Int("port", 0, "Gateway port (overrides config)")
	model := fs.String("model", "", "LLM model name (overrides config)")
	apiKey := fs.String("api-key", "", "API key (overrides config)")
	apiBase := fs.String("api-base", "", "API base URL (overrides config)")
	workspace := fs.String("workspace", "", "Workspace directory (overrides config)")
	maxIter := fs.Int("max-iter", 0, "Max agent iterations per message (overrides config)")
	temp := fs.Float64("temp", 0, "Sampling temperature (overrides config)")
	maxTokens := fs.Int("max-tokens", 0, "Max response tokens (overrides config)")

	// Feishu channel flags (override config)
	feishuAppID := fs.String("feishu-app-id", "", "Feishu App ID (overrides config)")
	feishuAppSecret := fs.String("feishu-app-secret", "", "Feishu App Secret (overrides config)")
	feishuEncryptKey := fs.String("feishu-encrypt-key", "", "Feishu Encrypt Key (overrides config)")
	var feishuAllow stringSliceFlag
	fs.Var(&feishuAllow, "feishu-allow", "Allowed Feishu sender IDs (repeat flag for multiple)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot gateway [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Load config
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Apply CLI overrides
	cfg.MergeFlags(*model, *apiKey, *apiBase, *workspace, *maxIter, *temp, *maxTokens)

	// Apply Feishu overrides
	if *feishuAppID != "" {
		cfg.Channels.Feishu.AppID = *feishuAppID
	}
	if *feishuAppSecret != "" {
		cfg.Channels.Feishu.AppSecret = *feishuAppSecret
	}
	if *feishuEncryptKey != "" {
		cfg.Channels.Feishu.EncryptKey = *feishuEncryptKey
	}
	if len(feishuAllow) > 0 {
		cfg.Channels.Feishu.AllowFrom = feishuAllow
	}

	// Gateway port
	gatewayPort := cfg.Gateway.Port
	if *port > 0 {
		gatewayPort = *port
	}

	// Determine workspace
	workspaceDir := cfg.WorkspacePath()

	// Determine model and provider
	modelName := cfg.GetModel()
	apiKeyValue := cfg.GetAPIKey(modelName)
	apiBaseValue := cfg.GetAPIBase(modelName)

	// Create provider
	p, err := provider.NewProvider(provider.ProviderConfig{
		APIKey:  apiKeyValue,
		APIBase: apiBaseValue,
		Model:   modelName,
	})
	if err != nil {
		slog.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	// Initialize tools
	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, time.Duration(cfg.Tools.Exec.Timeout)*time.Second))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	// Initialize cron service
	cronService := cron.NewCronService(filepath.Join(workspaceDir, "cron.json"), nil)
	tools.Register(tool.NewCronTool(cronService))

	// Initialize skills loader
	skillsLoader := agent.NewSkillsLoader(workspaceDir, "")

	// Initialize memory store
	memoryStore := agent.NewMemoryStore(workspaceDir)

	// Build system prompt
	systemPrompt := agent.BuildSystemPrompt(workspaceDir, skillsLoader, memoryStore)

	// Create message bus
	mb := bus.NewMessageBus(32)

	// Create agent loop
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		SystemPrompt: systemPrompt,
		Model:        modelName,
		MaxIter:      cfg.Agents.Defaults.MaxToolIterations,
		Temperature:  cfg.Agents.Defaults.Temperature,
		MaxTokens:    cfg.Agents.Defaults.MaxTokens,
		MemoryWindow: cfg.Agents.Defaults.MemoryWindow,
	}, memoryStore)

	// Initialize channels
	cliCh := channel.NewCLIChannel(mb)
	channels := []channel.Channel{cliCh}

	var feishuCh *channel.FeishuChannel
	if cfg.Channels.Feishu.AppID != "" && cfg.Channels.Feishu.AppSecret != "" {
		feishuCh = channel.NewFeishuChannel(channel.FeishuConfig{
			AppID:      cfg.Channels.Feishu.AppID,
			AppSecret:  cfg.Channels.Feishu.AppSecret,
			EncryptKey: cfg.Channels.Feishu.EncryptKey,
			AllowFrom:  cfg.Channels.Feishu.AllowFrom,
		}, mb)
		channels = append(channels, feishuCh)
		slog.Info("feishu channel enabled", "app_id", cfg.Channels.Feishu.AppID)
	} else {
		slog.Info("feishu channel disabled (no credentials)")
	}

	slog.Info("starting nanobot gateway", "port", gatewayPort)

	// Context + signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandler(cancel)

	// Start cron service
	if err := cronService.Start(ctx); err != nil {
		slog.Warn("failed to start cron service", "error", err)
	}
	defer cronService.Stop()

	var wg sync.WaitGroup

	// Agent loop goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := agentLoop.Run(ctx); err != nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// Start all channel goroutines
	for _, ch := range channels {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Start(ctx); err != nil {
				slog.Error("channel error", "channel", ch.Name(), "error", err)
			}
			// If CLI exits, shut everything down.
			if ch.Name() == "cli" {
				cancel()
			}
		}()
	}

	// Outbound dispatcher — routes agent replies to the correct channel.
	channelMap := make(map[string]channel.Channel, len(channels))
	for _, ch := range channels {
		channelMap[ch.Name()] = ch
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := mb.ConsumeOutbound(ctx)
			if err != nil {
				return
			}
			target, ok := channelMap[msg.Channel]
			if !ok {
				// Default to CLI.
				target = cliCh
			}
			if sendErr := target.Send(ctx, msg); sendErr != nil {
				slog.Warn("failed to send outbound message", "error", sendErr)
			}
		}
	}()

	wg.Wait()
}
