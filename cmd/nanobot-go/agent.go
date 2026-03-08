package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/why1414/nanobot-go/channel"
)

var sessionCounter atomic.Int64

func nextSessionID() int64 { return sessionCounter.Add(1) }

// agentFlags holds parsed flags for the agent subcommand.
type agentFlags struct {
	message string
	config  string
}

func parseAgentFlags(args []string) agentFlags {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)

	f := agentFlags{}

	fs.StringVar(&f.message, "m", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.message, "message", "", "Single message to send (non-interactive)")
	fs.StringVar(&f.config, "config", "", "Path to config file")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: nanobot-go agent [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	return f
}

// printAgentResponse prints the agent's reply in the same style as the Python version.
func printAgentResponse(content string) {
	fmt.Println()
	fmt.Println("nanobot-go")
	fmt.Println(content)
	fmt.Println()
}

// runAgent implements the "agent" subcommand.
func runAgent(args []string) {
	f := parseAgentFlags(args)

	// Initialize app
	application, err := Initialize(f.config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing app: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	setupSignalHandler(cancel)

	// Start cron service
	if err := application.StartCron(ctx); err != nil {
		slog.Warn("failed to start cron service", "error", err)
	}
	defer application.StopCron()

	// Default session ID
	cliChannelName, cliChatID := "cli", "direct"

	if f.message != "" {
		runAgentSingleMessage(ctx, cancel, application, cliChannelName, cliChatID, f.message)
		return
	}

	runAgentInteractive(ctx, cancel, application, cliChannelName, cliChatID)
}

// runAgentSingleMessage sends one message and prints the reply, then exits.
func runAgentSingleMessage(
	ctx context.Context,
	cancel context.CancelFunc,
	application *App,
	channelName, chatID, message string,
) {
	cliCh := channel.NewCLIChannel(application.MessageBus)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		if err := application.AgentLoop.Run(ctx); err != nil && ctx.Err() == nil {
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
	reply, err := application.MessageBus.ConsumeOutbound(ctx)
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
	application *App,
	channelName, chatID string,
) {
	// cliCh is only used to publish inbound messages; we read outbound directly
	// from the bus to avoid a two-consumer race on mb.Outbound
	cliCh := channel.NewCLIChannel(application.MessageBus)

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		if err := application.AgentLoop.Run(ctx); err != nil && ctx.Err() == nil {
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
		reply, err := application.MessageBus.ConsumeOutbound(ctx)
		if err != nil || reply == nil {
			fmt.Println("\nGoodbye!")
			cancel()
			<-agentDone
			return
		}
		printAgentResponse(reply.Content)
	}
}
