//	Command nanobot-go is the CLI entry point for NanoBot-Go.
//
// Subcommands:
//
//	nanobot-go agent [flags]        # interactive chat (default when no subcommand)
//	nanobot-go gateway [flags]      # start the gateway (Feishu + CLI)
//	nanobot-go onboard [flags]      # initialize config and workspace
//
// Agent flags:
//
//	-m, --message string      Single message to send (non-interactive)
//	-s, --session string      Session ID (default: cli:direct)
//	--no-markdown             Disable Markdown rendering of responses
//	--logs                    Show runtime logs during chat
//	--model string            LLM model (overrides config)
//	--api-key string          API key (or set env: ANTHROPIC_API_KEY, etc.)
//	--api-base string         API base URL (overrides config)
//	--workspace string        Workspace directory (overrides config)
//	--max-iter int            Max agent iterations per message (overrides config)
//	--temp float              Sampling temperature (overrides config)
//	--max-tokens int          Max response tokens (overrides config)
//	--config string           Path to config file (default: ~/.nanobot-go/config.json)
//
// Onboard flags:
//
//	--config string           Path to config file (default: ~/.nanobot-go/config.json)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/libo/nanobot-go/agent"
	"github.com/libo/nanobot-go/bus"
	"github.com/libo/nanobot-go/channel"
	"github.com/libo/nanobot-go/config"
	"github.com/libo/nanobot-go/cron"
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
	case "onboard":
		runOnboard(os.Args[2:])
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
	config    string
	// Override flags (empty means use config value)
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
	}

	fs.StringVar(&f.message, "m", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.message, "message", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.sessionID, "s", f.sessionID, "Session ID")
	fs.StringVar(&f.sessionID, "session", f.sessionID, "Session ID")
	noMarkdown := fs.Bool("no-markdown", false, "Disable Markdown rendering")
	fs.BoolVar(&f.logs, "logs", false, "Show runtime logs during chat")
	fs.StringVar(&f.config, "config", "", "Path to config file")
	fs.StringVar(&f.model, "model", "", "LLM model name (overrides config)")
	fs.StringVar(&f.apiKey, "api-key", "", "API key (overrides config)")
	fs.StringVar(&f.apiBase, "api-base", "", "API base URL (overrides config)")
	fs.StringVar(&f.workspace, "workspace", "", "Workspace directory (overrides config)")
	fs.IntVar(&f.maxIter, "max-iter", 0, "Max agent iterations per message (overrides config)")
	fs.Float64Var(&f.temp, "temp", 0, "Sampling temperature (overrides config)")
	fs.IntVar(&f.maxTokens, "max-tokens", 0, "Max response tokens (overrides config)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go agent [flags]\n\nFlags:\n")
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
	fmt.Println("nanobot-go")
	fmt.Println(content)
	fmt.Println()
}

func runAgent(args []string) {
	f := parseAgentFlags(args)

	if !f.logs {
		slog.SetDefault(slog.New(slog.NewTextHandler(ioDiscard{}, nil)))
	}

	// Load config
	cfg, err := config.LoadConfig(f.config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI overrides
	cfg.MergeFlags(f.model, f.apiKey, f.apiBase, f.workspace, f.maxIter, f.temp, f.maxTokens)

	// Determine workspace
	workspaceDir := cfg.WorkspacePath()

	// Determine model and provider
	model := cfg.GetModel()
	apiKey := cfg.GetAPIKey(model)
	apiBase := cfg.GetAPIBase(model)

	// Log current model configuration
	slog.Info("model configuration", "model", model)

	// Create provider
	p := provider.NewOpenAICompatProvider(apiKey, apiBase, model)

	// Initialize tools
	tools := tool.NewToolRegistry()
	tools.Register(tool.NewShellTool(workspaceDir, time.Duration(cfg.Tools.Exec.Timeout)*time.Second))
	tools.Register(tool.NewReadFileTool(workspaceDir))
	tools.Register(tool.NewWriteFileTool(workspaceDir))
	tools.Register(tool.NewEditFileTool(workspaceDir))
	tools.Register(tool.NewListDirTool(workspaceDir))

	// Initialize cron service (without callback for CLI mode)
	cronService := cron.NewCronService(filepath.Join(workspaceDir, "cron.json"), nil)
	tools.Register(tool.NewCronTool(cronService))

	// Initialize workspace (ensure directories and files exist)
	builtinSkillsDir := "" // Will use embedded skills if available
	if err := agent.EnsureWorkspace(workspaceDir, builtinSkillsDir); err != nil {
		slog.Warn("failed to initialize workspace", "error", err)
	}

	// Initialize skills loader
	skillsLoader := agent.NewSkillsLoader(workspaceDir, builtinSkillsDir)

	// Initialize memory store
	memoryStore := agent.NewMemoryStore(workspaceDir)

	// Create message bus
	mb := bus.NewMessageBus(32)

	// Create agent loop
	agentLoop := agent.NewAgentLoop(mb, p, tools, agent.AgentOptions{
		Model:        model,
		MaxIter:      cfg.Agents.Defaults.MaxToolIterations,
		Temperature:  cfg.Agents.Defaults.Temperature,
		MaxTokens:    cfg.Agents.Defaults.MaxTokens,
		MemoryWindow: cfg.Agents.Defaults.MemoryWindow,
	}, memoryStore, skillsLoader, workspaceDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupSignalHandler(cancel)

	// Start cron service
	if err := cronService.Start(ctx); err != nil {
		slog.Warn("failed to start cron service", "error", err)
	}
	defer cronService.Stop()

	// Parse session ID into channel + chat_id
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

	// Single consumer — read the reply directly from the bus
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
	// from the bus to avoid a two-consumer race on mb.Outbound
	cliCh := channel.NewCLIChannel(mb)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		if err := agentLoop.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	fmt.Println("nanobot-go Interactive mode (type exit or Ctrl+C to quit)")
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

		// Wait for the reply directly from the bus — single consumer, no race
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

// runOnboard initializes nanobot-go configuration and workspace.
func runOnboard(args []string) {
	fs := flag.NewFlagSet("onboard", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config file")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go onboard [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Initialize nanobot-go configuration and workspace.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	// Determine config path
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.GetConfigPath()
	}

	cfgDir := filepath.Dir(cfgPath)

	fmt.Println()
	fmt.Println("🤖 nanobot-go")
	fmt.Println()

	// Check if config already exists
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config already exists at %s\n", cfgPath)
		fmt.Println("  y = overwrite with defaults (existing values will be lost)")
		fmt.Println("  N = refresh config, keeping existing values and adding new fields")

		fmt.Print("Overwrite? [y/N]: ")
		var response string
		fmt.Scanln(&response)

		if response == "y" || response == "Y" {
			// Create new default config
			cfg := config.DefaultConfig()
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Config reset to defaults at %s\n", cfgPath)
		} else {
			// Load existing and re-save (to add new fields)
			cfg, err := config.LoadConfig(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
				os.Exit(1)
			}
			if err := config.SaveConfig(cfg, cfgPath); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("✓ Config refreshed at %s (existing values preserved)\n", cfgPath)
		}
	} else {
		// Create new config
		cfg := config.DefaultConfig()
		if err := os.MkdirAll(cfgDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
			os.Exit(1)
		}
		if err := config.SaveConfig(cfg, cfgPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Created config at %s\n", cfgPath)
	}

	// Load config to get workspace path
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()

	// Determine templates directory (relative to executable)
	execPath, _ := os.Executable()
	templatesDir := filepath.Join(filepath.Dir(execPath), "..", "templates")
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		// Try relative to current directory
		templatesDir = "templates"
		if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
			templatesDir = "" // No templates available
		}
	}

	// Initialize workspace using agent.InitWorkspace
	if err := agent.EnsureWorkspace(workspace, templatesDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing workspace: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Initialized workspace at %s\n", workspace)

	// List created files
	if templatesDir != "" {
		fmt.Println("  Created memory/")
		fmt.Println("  Created memory/MEMORY.md")
		fmt.Println("  Created memory/HISTORY.md")
		fmt.Println("  Created sessions/")
		fmt.Println("  Created skills/")
		fmt.Println("  Created AGENTS.md (from template)")
		fmt.Println("  Created SOUL.md (from template)")
		fmt.Println("  Created USER.md (from template)")
		fmt.Println("  Created TOOLS.md (from template)")
	} else {
		fmt.Println("  Created memory/")
		fmt.Println("  Created memory/MEMORY.md")
		fmt.Println("  Created memory/HISTORY.md")
		fmt.Println("  Created sessions/")
		fmt.Println("  Created skills/")
	}

	fmt.Println()
	fmt.Println("🤖 nanobot-go is ready!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Add your API key to ~/.nanobot-go/config.json")
	fmt.Println("     Get one at: https://openrouter.ai/keys")
	fmt.Println("  2. Chat: nanobot-go agent -m \"Hello!\"")
	fmt.Println()
	fmt.Println("Want Telegram/WhatsApp? See: https://github.com/HKUDS/nanobot#-chat-apps")
}

// ioDiscard is an io.Writer that discards all writes (used to silence slog).
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
