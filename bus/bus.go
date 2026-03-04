// Package bus provides a message bus for decoupled channel-agent communication.
package bus

import (
	"context"
	"fmt"
	"time"
)

// InboundMessage is a message received from a chat channel.
type InboundMessage struct {
	Channel   string
	SenderID  string
	ChatID    string
	Content   string
	Media     []string
	Metadata  map[string]any
	Timestamp time.Time
}

// SessionKey returns a unique key for session identification.
func (m *InboundMessage) SessionKey() string {
	return m.Channel + ":" + m.ChatID
}

// OutboundMessage is a message to send to a chat channel.
type OutboundMessage struct {
	Channel  string
	ChatID   string
	Content  string
	ReplyTo  string
	Metadata map[string]any
}

// MessageBus decouples chat channels from the agent core using Go channels.
// Channels push messages to Inbound; the agent processes them and pushes
// responses to Outbound.
type MessageBus struct {
	Inbound  chan *InboundMessage
	Outbound chan *OutboundMessage
}

// NewMessageBus creates a MessageBus with buffered channels of the given size.
func NewMessageBus(bufSize int) *MessageBus {
	return &MessageBus{
		Inbound:  make(chan *InboundMessage, bufSize),
		Outbound: make(chan *OutboundMessage, bufSize),
	}
}

// PublishInbound publishes an inbound message from a channel to the agent.
// Blocks until the message is accepted or ctx is cancelled.
func (b *MessageBus) PublishInbound(ctx context.Context, msg *InboundMessage) error {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	select {
	case b.Inbound <- msg:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish inbound: %w", ctx.Err())
	}
}

// ConsumeInbound returns the next inbound message.
// Blocks until a message is available or ctx is cancelled.
func (b *MessageBus) ConsumeInbound(ctx context.Context) (*InboundMessage, error) {
	select {
	case msg := <-b.Inbound:
		return msg, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("consume inbound: %w", ctx.Err())
	}
}

// PublishOutbound publishes an outbound response from the agent to channels.
// Blocks until the message is accepted or ctx is cancelled.
func (b *MessageBus) PublishOutbound(ctx context.Context, msg *OutboundMessage) error {
	select {
	case b.Outbound <- msg:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish outbound: %w", ctx.Err())
	}
}

// ConsumeOutbound returns the next outbound message.
// Blocks until a message is available or ctx is cancelled.
func (b *MessageBus) ConsumeOutbound(ctx context.Context) (*OutboundMessage, error) {
	select {
	case msg := <-b.Outbound:
		return msg, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("consume outbound: %w", ctx.Err())
	}
}
