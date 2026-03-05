package channel

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libo/nanobot-go/bus"
)

const (
	cliChannelName = "cli"
	cliChatID      = "cli:local"
	cliSenderID    = "user"
)

// CLIChannel reads user input from stdin and prints agent replies to stdout.
// It implements the Channel interface for interactive terminal sessions.
type CLIChannel struct {
	BaseChannel
	// replies receives responses from the agent (via Send) so that Start can
	// display them after the thinking indicator.
	replies chan string
}

// NewCLIChannel creates a CLIChannel backed by the given MessageBus.
func NewCLIChannel(b *bus.MessageBus) *CLIChannel {
	return &CLIChannel{
		BaseChannel: NewBaseChannel(b, nil),
		replies:     make(chan string, 8),
	}
}

// Name returns "cli".
func (c *CLIChannel) Name() string { return cliChannelName }

// Send delivers an outbound message to the terminal. It is called by the
// dispatcher goroutine in main when the agent produces a reply.
func (c *CLIChannel) Send(_ context.Context, msg *bus.OutboundMessage) error {
	if msg.Content == "" {
		return nil
	}
	select {
	case c.replies <- msg.Content:
	default:
		// Fallback: print directly if buffer is full.
		fmt.Printf("\nAssistant: %s\n\n", msg.Content)
	}
	return nil
}

// Start runs the interactive stdin loop. It blocks until ctx is cancelled or
// the user types /exit / /quit.
//
// Supported slash commands:
//
//	/new   — clear session (new chat_id so agent history resets)
//	/exit  — quit
//	/quit  — quit
func (c *CLIChannel) Start(ctx context.Context) error {
	fmt.Println("NanoBot interactive mode")
	fmt.Println("Type /exit or /quit to quit, /new to start a new conversation.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	chatID := cliChatID

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nGoodbye!")
			return nil
		default:
		}

		fmt.Print("You: ")
		if !scanner.Scan() {
			fmt.Println("\nGoodbye!")
			return nil
		}
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" {
			continue
		}

		switch strings.ToLower(trimmed) {
		case "/exit", "/quit":
			fmt.Println("Goodbye!")
			return nil
		case "/new":
			chatID = fmt.Sprintf("cli:local:%d", nextSessionID())
			fmt.Println("[new conversation started]")
			fmt.Println()
			continue
		}

		// Publish to the agent via the bus.
		if err := c.HandleMessage(ctx, cliChannelName, cliSenderID, chatID, trimmed, nil); err != nil {
			fmt.Println("\nGoodbye!")
			return nil
		}

		// Wait for the agent's reply, showing a spinner in the meantime.
		reply := c.WaitForReply(ctx)
		if reply == "" {
			// ctx was cancelled while waiting.
			fmt.Println("\nGoodbye!")
			return nil
		}
		fmt.Printf("\nAssistant: %s\n\n", reply)
	}
}

// WaitForReply blocks until a reply is available in the replies channel or ctx
// is cancelled. While waiting it prints a simple spinner on stderr.
func (c *CLIChannel) WaitForReply(ctx context.Context) string {
	spinnerFrames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frameIdx := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	clearSpinner := func() {
		fmt.Fprint(os.Stderr, "\r                    \r")
	}

	for {
		select {
		case <-ctx.Done():
			clearSpinner()
			return ""
		case reply, ok := <-c.replies:
			clearSpinner()
			if !ok {
				return ""
			}
			return reply
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "\r%s thinking...", spinnerFrames[frameIdx])
			frameIdx = (frameIdx + 1) % len(spinnerFrames)
		}
	}
}

// sessionCounter is used to generate unique chat IDs for /new sessions.
var sessionCounter atomic.Int64

func nextSessionID() int64 {
	return sessionCounter.Add(1)
}
