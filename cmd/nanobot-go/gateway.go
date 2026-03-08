package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/why1414/nanobot-go/channel"
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

	// Initialize app
	application, err := Initialize(*configPath)
	if err != nil {
		slog.Error("failed to initialize app", "error", err)
		os.Exit(1)
	}

	// Gateway port
	gatewayPort := application.Config.Gateway.Port

	// Initialize channels
	cliCh := channel.NewCLIChannel(application.MessageBus)
	channels := []channel.Channel{cliCh}

	var feishuCh *channel.FeishuChannel
	if application.Config.Channels.Feishu.AppID != "" && application.Config.Channels.Feishu.AppSecret != "" {
		feishuCh = channel.NewFeishuChannel(channel.FeishuConfig{
			AppID:      application.Config.Channels.Feishu.AppID,
			AppSecret:  application.Config.Channels.Feishu.AppSecret,
			EncryptKey: application.Config.Channels.Feishu.EncryptKey,
			AllowFrom:  application.Config.Channels.Feishu.AllowFrom,
		}, application.MessageBus)
		channels = append(channels, feishuCh)
		slog.Info("feishu channel enabled", "app_id", application.Config.Channels.Feishu.AppID)
	} else {
		slog.Info("feishu channel disabled (no credentials)")
	}

	slog.Info("starting nanobot gateway", "port", gatewayPort)

	// Context + signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setupSignalHandler(cancel)

	// Start cron service
	if err := application.StartCron(ctx); err != nil {
		slog.Warn("failed to start cron service", "error", err)
	}
	defer application.StopCron()

	var wg sync.WaitGroup

	// Agent loop goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := application.AgentLoop.Run(ctx); err != nil {
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
			msg, err := application.MessageBus.ConsumeOutbound(ctx)
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
