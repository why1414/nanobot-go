package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/channel"
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
	port := fs.Int("port", 18790, "Gateway port")
	model := fs.String("model", "claude-haiku-4.5", "LLM model name")
	apiKey := fs.String("api-key", "", "API key (falls back to env)")
	apiBase := fs.String("api-base", "http://localhost:4141/v1", "API base URL")
	workspace := fs.String("workspace", "", "Workspace directory (default: current directory)")
	maxIter := fs.Int("max-iter", 40, "Max agent iterations per message")
	temp := fs.Float64("temp", 0.1, "Sampling temperature")
	maxTokens := fs.Int("max-tokens", 65536, "Max response tokens")

	feishuAppID := fs.String("feishu-app-id", "", "Feishu App ID (or FEISHU_APP_ID)")
	feishuAppSecret := fs.String("feishu-app-secret", "", "Feishu App Secret (or FEISHU_APP_SECRET)")
	feishuEncryptKey := fs.String("feishu-encrypt-key", "", "Feishu Encrypt Key (or FEISHU_ENCRYPT_KEY)")
	var feishuAllow stringSliceFlag
	fs.Var(&feishuAllow, "feishu-allow", "Allowed Feishu sender IDs (repeat flag for multiple)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go gateway [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Fall back to environment variables for Feishu credentials.
	if *feishuAppID == "" {
		*feishuAppID = os.Getenv("FEISHU_APP_ID")
	}
	if *feishuAppSecret == "" {
		*feishuAppSecret = os.Getenv("FEISHU_APP_SECRET")
	}
	if *feishuEncryptKey == "" {
		*feishuEncryptKey = os.Getenv("FEISHU_ENCRYPT_KEY")
	}

	// Workspace
	workspaceDir := *workspace
	if workspaceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			slog.Error("failed to get working directory", "error", err)
			os.Exit(1)
		}
		workspaceDir = cwd
	}

	// Provider
	p, err := provider.NewProvider(provider.ProviderConfig{
		APIKey:  *apiKey,
		APIBase: *apiBase,
		Model:   *model,
	})
	if err != nil {
		slog.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	// Tools
	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, 0))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	// Bus
	mb := bus.NewMessageBus(32)

	// Agent loop
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:       *model,
		MaxIter:     *maxIter,
		Temperature: *temp,
		MaxTokens:   *maxTokens,
	})

	// Channels
	cliCh := channel.NewCLIChannel(mb)
	channels := []channel.Channel{cliCh}

	var feishuCh *channel.FeishuChannel
	if *feishuAppID != "" && *feishuAppSecret != "" {
		feishuCh = channel.NewFeishuChannel(channel.FeishuConfig{
			AppID:      *feishuAppID,
			AppSecret:  *feishuAppSecret,
			EncryptKey: *feishuEncryptKey,
			AllowFrom:  feishuAllow,
		}, mb)
		channels = append(channels, feishuCh)
		slog.Info("feishu channel enabled", "app_id", *feishuAppID)
	} else {
		slog.Info("feishu channel disabled (no credentials)")
	}

	slog.Info("starting nanobot gateway", "port", *port)

	// Context + signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandler(cancel)

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
