package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// runGateway implements the "gateway" subcommand.
func runGateway(args []string) {
	fs := flag.NewFlagSet("gateway", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file (default: ~/.nanobot-go/config.json)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go gateway [flags]\n\nFlags:\n")
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

	// Gateway port
	gatewayPort := cfg.Gateway.Port

	// Determine workspace
	workspaceDir := cfg.WorkspacePath()

	// Determine model and provider
	modelName := cfg.GetModel()
	apiKeyValue := cfg.GetAPIKey(modelName)
	apiBaseValue := cfg.GetAPIBase(modelName)

	// Log current model configuration
	slog.Info("model configuration", "model", modelName, "apiBase", apiBaseValue)

	// Create provider
	p := provider.NewOpenAICompatProvider(apiKeyValue, apiBaseValue, modelName)

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

	// Initialize workspace (ensure directories and files exist)
	if err := agent.EnsureWorkspace(workspaceDir, ""); err != nil {
		slog.Warn("failed to initialize workspace", "error", err)
	}

	// Initialize skills loader
	skillsLoader := agent.NewSkillsLoader(workspaceDir, "")

	// Initialize memory store
	memoryStore := agent.NewMemoryStore(workspaceDir)

	// Create message bus
	mb := bus.NewMessageBus(32)

	// Create agent loop
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:        modelName,
		MaxIter:      cfg.Agents.Defaults.MaxToolIterations,
		Temperature:  cfg.Agents.Defaults.Temperature,
		MaxTokens:    cfg.Agents.Defaults.MaxTokens,
		MemoryWindow: cfg.Agents.Defaults.MemoryWindow,
	}, memoryStore, skillsLoader, workspaceDir)

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
