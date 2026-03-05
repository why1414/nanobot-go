// Command nanobot is the CLI entry point for NanoBot.
//
// Subcommands:
//
//	nanobot agent [flags]        # interactive chat (default when no subcommand)
//	nanobot gateway [flags]      # start the gateway (Feishu + CLI)
//
// Agent flags:
//
//	-m, --message string      Single message to send (non-interactive)
//	-s, --session string      Session ID (default: cli:direct)
//	--no-markdown             Disable Markdown rendering of responses
//	--logs                    Show runtime logs during chat
//	--model string            LLM model (default: claude-haiku-4.5)
//	--api-key string          API key (or set env: ANTHROPIC_API_KEY, etc.)
//	--api-base string         API base URL
//	--workspace string        Workspace directory (default: cwd)
//	--max-iter int            Max agent iterations per message (default 40)
//	--temp float              Sampling temperature (default 0.1)
//	--max-tokens int          Max response tokens (default 65536)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/channel"
	"github.com/libo/nanobot-go/provider"
	"github.com/libo/nanobot-go/tool"
)

var sessionCounter atomic.Int64

func nextSessionID() int64 { return sessionCounter.Add(1) }

func main() {
	if len(os.Args) < 2 {
		runAgent(os.Args[1:])
		return
	}
	switch os.Args[1] {
	case "agent":
		runAgent(os.Args[2:])
	case "gateway":
		runGateway(os.Args[2:])
	default:
		// Treat unknown first arg as flags for agent (backward compat).
		runAgent(os.Args[1:])
	}
}

// agentFlags holds parsed flags for the agent subcommand.
type agentFlags struct {
	message   string
	sessionID string
	markdown  bool
	logs      bool
	model     string
	apiKey    string
	apiBase   string
	workspace string
	maxIter   int
	temp      float64
	maxTokens int
}

func parseAgentFlags(args []string) agentFlags {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)

	f := agentFlags{
		sessionID: "cli:direct",
		markdown:  true,
		model:     "claude-haiku-4.5",
		apiBase:   "http://localhost:4141/v1",
		maxIter:   40,
		temp:      0.1,
		maxTokens: 120000,
	}

	fs.StringVar(&f.message, "m", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.message, "message", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.sessionID, "s", f.sessionID, "Session ID")
	fs.StringVar(&f.sessionID, "session", f.sessionID, "Session ID")
	noMarkdown := fs.Bool("no-markdown", false, "Disable Markdown rendering")
	fs.BoolVar(&f.logs, "logs", false, "Show runtime logs during chat")
	fs.StringVar(&f.model, "model", f.model, "LLM model name")
	fs.StringVar(&f.apiKey, "api-key", "", "API key")
	fs.StringVar(&f.apiBase, "api-base", f.apiBase, "API base URL")
	fs.StringVar(&f.workspace, "workspace", "", "Workspace directory (default: cwd)")
	fs.IntVar(&f.maxIter, "max-iter", f.maxIter, "Max agent iterations per message")
	fs.Float64Var(&f.temp, "temp", f.temp, "Sampling temperature")
	fs.IntVar(&f.maxTokens, "max-tokens", f.maxTokens, "Max response tokens")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot agent [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	if *noMarkdown {
		f.markdown = false
	}
	return f
}

// printAgentResponse prints the agent's reply in the same style as the Python version.
func printAgentResponse(content string) {
	fmt.Println()
	fmt.Println("nanobot")
	fmt.Println(content)
	fmt.Println()
}

func runAgent(args []string) {
	f := parseAgentFlags(args)

	if !f.logs {
		slog.SetDefault(slog.New(slog.NewTextHandler(ioDiscard{}, nil)))
	}

	workspaceDir := f.workspace
	if workspaceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		workspaceDir = cwd
	}

	p, err := provider.NewProvider(provider.ProviderConfig{
		APIKey:  f.apiKey,
		APIBase: f.apiBase,
		Model:   f.model,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, 0))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	mb := bus.NewMessageBus(32)

	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:       f.model,
		MaxIter:     f.maxIter,
		Temperature: f.temp,
		MaxTokens:   f.maxTokens,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupSignalHandler(cancel)

	// Parse session ID into channel + chat_id.
	cliChannelName, cliChatID := "cli", "direct"
	if strings.Contains(f.sessionID, ":") {
		parts := strings.SplitN(f.sessionID, ":", 2)
		cliChannelName, cliChatID = parts[0], parts[1]
	} else {
		cliChatID = f.sessionID
	}

	if f.message != "" {
		runAgentSingleMessage(ctx, cancel, mb, agentLoop, cliChannelName, cliChatID, f.message)
		return
	}

	runAgentInteractive(ctx, cancel, mb, agentLoop, cliChannelName, cliChatID)
}

// runAgentSingleMessage sends one message and prints the reply, then exits.
func runAgentSingleMessage(
	ctx context.Context,
	cancel context.CancelFunc,
	mb *bus.MessageBus,
	agentLoop *agent.AgentLoop,
	channelName, chatID, message string,
) {
	cliCh := channel.NewCLIChannel(mb)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		if err := agentLoop.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	if err := cliCh.HandleMessage(ctx, channelName, "user", channelName+":"+chatID, message, nil); err != nil {
		slog.Error("failed to publish message", "error", err)
		cancel()
		<-agentDone
		return
	}

	// Single consumer — read the reply directly from the bus.
	reply, err := mb.ConsumeOutbound(ctx)
	if err == nil && reply != nil && reply.Content != "" {
		printAgentResponse(reply.Content)
	}

	cancel()
	<-agentDone
}

// runAgentInteractive runs the interactive REPL, matching Python's behaviour.
func runAgentInteractive(
	ctx context.Context,
	cancel context.CancelFunc,
	mb *bus.MessageBus,
	agentLoop *agent.AgentLoop,
	channelName, chatID string,
) {
	// cliCh is only used to publish inbound messages; we read outbound directly
	// from the bus to avoid a two-consumer race on mb.Outbound.
	cliCh := channel.NewCLIChannel(mb)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		if err := agentLoop.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	fmt.Println("nanobot Interactive mode (type exit or Ctrl+C to quit)")
	fmt.Println()

	exitCmds := map[string]bool{
		"exit": true, "quit": true,
		"/exit": true, "/quit": true,
		":q": true,
	}

	scanner := bufio.NewScanner(os.Stdin)
	currentChatID := channelName + ":" + chatID

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nGoodbye!")
			<-agentDone
			return
		default:
		}

		fmt.Print("You: ")
		if !scanner.Scan() {
			fmt.Println("\nGoodbye!")
			cancel()
			<-agentDone
			return
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if exitCmds[strings.ToLower(input)] {
			fmt.Println("\nGoodbye!")
			cancel()
			<-agentDone
			return
		}
		if strings.ToLower(input) == "/new" {
			currentChatID = fmt.Sprintf("%s:%s:%d", channelName, chatID, nextSessionID())
			fmt.Println("[new conversation started]")
			fmt.Println()
			continue
		}

		if err := cliCh.HandleMessage(ctx, channelName, "user", currentChatID, input, nil); err != nil {
			fmt.Println("\nGoodbye!")
			cancel()
			<-agentDone
			return
		}

		// Wait for the reply directly from the bus — single consumer, no race.
		reply, err := mb.ConsumeOutbound(ctx)
		if err != nil || reply == nil {
			fmt.Println("\nGoodbye!")
			cancel()
			<-agentDone
			return
		}
		printAgentResponse(reply.Content)
	}
}

// setupSignalHandler cancels ctx on SIGINT/SIGTERM.
func setupSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
}

// ioDiscard is an io.Writer that discards all writes (used to silence slog).
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
