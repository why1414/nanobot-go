// Command nanobot-go is the CLI entry point for NanoBot.
//
// Usage:
//
//	nanobot-go                   # start interactive TUI mode
//	nanobot-go -m "message"      # send a single message
//	nanobot-go gateway [flags]   # start the gateway (Feishu + CLI)
//
// Flags (agent / single-message mode):
//
//	-model string      LLM model (default: claude-haiku-4.5)
//	-api-key string    API key (or set env: OPENAI_API_KEY, ANTHROPIC_API_KEY, etc.)
//	-api-base string   API base URL (default: http://localhost:4141/v1)
//	-workspace string  Workspace directory (default: current directory)
//	-max-iter int      Max agent iterations per message (default 40)
//	-temp float        Sampling temperature (default 0.1)
//	-max-tokens int    Max response tokens (default 65536)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/channel"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

func main() {
	// Subcommand dispatch: check first arg for "gateway".
	if len(os.Args) > 1 && os.Args[1] == "gateway" {
		runGateway(os.Args[2:])
		return
	}

	// -------------------------------------------------------------------------
	// Flags (agent / TUI mode)
	// -------------------------------------------------------------------------
	model := flag.String("model", "claude-haiku-4.5", "LLM model name")
	apiKey := flag.String("api-key", "", "API key (falls back to env: ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.)")
	apiBase := flag.String("api-base", "http://localhost:4141/v1", "API base URL")
	workspace := flag.String("workspace", "", "Workspace directory (default: current directory)")
	maxIter := flag.Int("max-iter", 40, "Max agent iterations per message")
	temp := flag.Float64("temp", 0.1, "Sampling temperature")
	maxTokens := flag.Int("max-tokens", 65536, "Max response tokens")
	message := flag.String("m", "", "Single message to send (non-interactive)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go [flags]\n       nanobot-go gateway [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// -------------------------------------------------------------------------
	// Workspace
	// -------------------------------------------------------------------------
	workspaceDir := *workspace
	if workspaceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			slog.Error("failed to get working directory", "error", err)
			os.Exit(1)
		}
		workspaceDir = cwd
	}

	// -------------------------------------------------------------------------
	// LLM Provider
	// -------------------------------------------------------------------------
	p, err := provider.NewProvider(provider.ProviderConfig{
		APIKey:  *apiKey,
		APIBase: *apiBase,
		Model:   *model,
	})
	if err != nil {
		slog.Error("failed to create provider", "error", err)
		os.Exit(1)
	}

	// -------------------------------------------------------------------------
	// Tool registry
	// -------------------------------------------------------------------------
	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, 0))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	// -------------------------------------------------------------------------
	// Message bus
	// -------------------------------------------------------------------------
	mb := bus.NewMessageBus(32)

	// -------------------------------------------------------------------------
	// Agent loop
	// -------------------------------------------------------------------------
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:       *model,
		MaxIter:     *maxIter,
		Temperature: *temp,
		MaxTokens:   *maxTokens,
	})

	// -------------------------------------------------------------------------
	// Context with signal handling
	// -------------------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandler(cancel)

	// -------------------------------------------------------------------------
	// Single-message mode (-m flag)
	// -------------------------------------------------------------------------
	if *message != "" {
		runSingleMessage(ctx, cancel, mb, agentLoop, *message)
		return
	}

	// -------------------------------------------------------------------------
	// TUI mode (default: no args)
	// -------------------------------------------------------------------------
	tuiCh := channel.NewTUIChannel(mb, *model)

	var wg sync.WaitGroup

	// Agent loop goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := agentLoop.Run(ctx); err != nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// TUI channel goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := tuiCh.Start(ctx); err != nil {
			slog.Error("tui channel error", "error", err)
		}
		cancel()
	}()

	// Outbound dispatcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := mb.ConsumeOutbound(ctx)
			if err != nil {
				return
			}
			if sendErr := tuiCh.Send(ctx, msg); sendErr != nil {
				slog.Warn("failed to send outbound message", "error", sendErr)
			}
		}
	}()

	wg.Wait()
}

// runSingleMessage handles the -m flag: sends one message and prints the reply.
func runSingleMessage(
	ctx context.Context,
	cancel context.CancelFunc,
	mb *bus.MessageBus,
	agentLoop *agent.AgentLoop,
	message string,
) {
	cliCh := channel.NewCLIChannel(mb)

	var wg sync.WaitGroup

	// Agent loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := agentLoop.Run(ctx); err != nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// Outbound dispatcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := mb.ConsumeOutbound(ctx)
			if err != nil {
				return
			}
			if sendErr := cliCh.Send(ctx, msg); sendErr != nil {
				slog.Warn("failed to send outbound message", "error", sendErr)
			}
		}
	}()

	// Publish the single message then read one reply.
	if err := cliCh.HandleMessage(ctx, "cli", "user", "cli:local", message, nil); err != nil {
		slog.Error("failed to publish message", "error", err)
		cancel()
		wg.Wait()
		return
	}

	// Wait for one reply.
	replyMsg, err := mb.ConsumeOutbound(ctx)
	if err == nil && replyMsg != nil {
		fmt.Printf("\nAssistant: %s\n", replyMsg.Content)
	}

	cancel()
	wg.Wait()
}

// setupSignalHandler cancels ctx on SIGINT/SIGTERM.
func setupSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("received signal, shutting down")
		cancel()
	}()
}
