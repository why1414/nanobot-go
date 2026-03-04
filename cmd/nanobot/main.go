// Command nanobot-go is the CLI entry point for NanoBot.
//
// Usage:
//
//	nanobot-go [flags]
//
// Flags:
//
//	-model string      LLM model (default: claude-sonnet-4.6)
//	-api-key string    API key (or set env: OPENAI_API_KEY, ANTHROPIC_API_KEY, etc.)
//	-api-base string   API base URL (default: http://localhost:4141/v1)
//	-workspace string  Workspace directory (default: current directory)
//	-max-iter int      Max agent iterations per message (default 40)
//	-temp float        Sampling temperature (default 0.1)
//	-max-tokens int    Max response tokens (default 4096)
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
	// -------------------------------------------------------------------------
	// Flags
	// -------------------------------------------------------------------------
	model := flag.String("model", "claude-sonnet-4.6", "LLM model name")
	apiKey := flag.String("api-key", "", "API key (falls back to env: ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.)")
	apiBase := flag.String("api-base", "http://localhost:4141/v1", "API base URL")
	workspace := flag.String("workspace", "", "Workspace directory (default: current directory)")
	maxIter := flag.Int("max-iter", 40, "Max agent iterations per message")
	temp := flag.Float64("temp", 0.1, "Sampling temperature")
	maxTokens := flag.Int("max-tokens", 4096, "Max response tokens")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go [flags]\n\nFlags:\n")
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
	// CLI channel
	// -------------------------------------------------------------------------
	cliCh := channel.NewCLIChannel(mb)

	// -------------------------------------------------------------------------
	// Context with signal handling
	// -------------------------------------------------------------------------
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("received signal, shutting down")
		cancel()
	}()

	// -------------------------------------------------------------------------
	// Launch goroutines
	// -------------------------------------------------------------------------
	var wg sync.WaitGroup

	// Goroutine 1: agent loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := agentLoop.Run(ctx); err != nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// Goroutine 2: CLI channel stdin reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := cliCh.Start(ctx); err != nil {
			slog.Error("cli channel error", "error", err)
		}
		// When the CLI exits (user typed /quit or EOF), cancel context to stop
		// the other goroutines.
		cancel()
	}()

	// Goroutine 3: outbound dispatcher — routes agent replies to the CLI channel
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			msg, err := mb.ConsumeOutbound(ctx)
			if err != nil {
				// ctx cancelled — normal shutdown
				return
			}
			if msg.Channel == cliCh.Name() || msg.Channel == "" {
				if sendErr := cliCh.Send(ctx, msg); sendErr != nil {
					slog.Warn("failed to send outbound message", "error", sendErr)
				}
			}
		}
	}()

	wg.Wait()
}
