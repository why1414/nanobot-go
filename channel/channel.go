// Package channel defines the Channel interface and BaseChannel helper for
// integrating chat platforms with the nanobot-go message bus.
package channel

import (
	"context"
	"log/slog"

	"github.com/libo/nanobot-go/bus"
)

// Channel is the interface that every chat-platform integration must implement.
type Channel interface {
	// Name returns a short identifier for this channel (e.g. "cli", "telegram").
	Name() string
	// Start begins listening for messages. It blocks until ctx is cancelled.
	Start(ctx context.Context) error
	// Send delivers an outbound message to the platform.
	Send(ctx context.Context, msg *bus.OutboundMessage) error
}

// BaseChannel provides common allowlist checking and message publishing logic
// that concrete channel implementations can embed.
type BaseChannel struct {
	msgBus    *bus.MessageBus
	allowList []string // empty means allow everyone
}

// NewBaseChannel creates a BaseChannel. allowList may be nil or empty to allow
// all senders.
func NewBaseChannel(b *bus.MessageBus, allowList []string) BaseChannel {
	return BaseChannel{
		msgBus:    b,
		allowList: allowList,
	}
}

// IsAllowed reports whether senderID is permitted to use the bot.
// Returns true when allowList is empty (open access) or when senderID is
// present in the list.
func (b *BaseChannel) IsAllowed(senderID string) bool {
	if len(b.allowList) == 0 {
		return true
	}
	for _, allowed := range b.allowList {
		if senderID == allowed {
			return true
		}
	}
	return false
}

// HandleMessage checks permissions and, if allowed, publishes an InboundMessage
// to the bus.
func (b *BaseChannel) HandleMessage(
	ctx context.Context,
	channelName, senderID, chatID, content string,
	metadata map[string]any,
) error {
	if !b.IsAllowed(senderID) {
		slog.Warn("access denied",
			"channel", channelName,
			"sender", senderID,
		)
		return nil
	}

	msg := &bus.InboundMessage{
		Channel:  channelName,
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Metadata: metadata,
	}
	return b.msgBus.PublishInbound(ctx, msg)
}
