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
//	--config string           Path to config file (default: ~/.nanobot-go/config.json)
//
// Onboard flags:
//
//	--config string           Path to config file (default: ~/.nanobot-go/config.json)
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

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

// setupSignalHandler cancels ctx on SIGINT/SIGTERM.
func setupSignalHandler(cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
}
