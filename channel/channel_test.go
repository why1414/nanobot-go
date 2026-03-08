package channel

import (
	"context"
	"testing"

	"github.com/why1414/nanobot-go/bus"
)

// TestBaseChannel_IsAllowed verifies allowlist logic.
func TestBaseChannel_IsAllowed(t *testing.T) {
	b := NewBaseChannel(nil, nil)

	// Empty allowlist — everyone is permitted.
	if !b.IsAllowed("alice") {
		t.Error("empty allowlist: expected alice to be allowed")
	}
	if !b.IsAllowed("bob") {
		t.Error("empty allowlist: expected bob to be allowed")
	}

	// Non-empty allowlist — only listed senders.
	b2 := NewBaseChannel(nil, []string{"alice", "carol"})
	if !b2.IsAllowed("alice") {
		t.Error("expected alice to be allowed")
	}
	if !b2.IsAllowed("carol") {
		t.Error("expected carol to be allowed")
	}
	if b2.IsAllowed("bob") {
		t.Error("expected bob to be denied")
	}
	if b2.IsAllowed("") {
		t.Error("expected empty string to be denied")
	}
}

// TestBaseChannel_HandleMessage_allowed verifies that an allowed sender's
// message is published to the bus.
func TestBaseChannel_HandleMessage_allowed(t *testing.T) {
	mb := bus.NewMessageBus(4)
	bc := NewBaseChannel(mb, nil) // no allowlist → all allowed

	ctx := context.Background()
	err := bc.HandleMessage(ctx, "test", "alice", "room1", "hello", nil)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}

	select {
	case msg := <-mb.Inbound:
		if msg.Content != "hello" {
			t.Errorf("expected content %q, got %q", "hello", msg.Content)
		}
		if msg.SenderID != "alice" {
			t.Errorf("expected senderID %q, got %q", "alice", msg.SenderID)
		}
		if msg.ChatID != "room1" {
			t.Errorf("expected chatID %q, got %q", "room1", msg.ChatID)
		}
		if msg.Channel != "test" {
			t.Errorf("expected channel %q, got %q", "test", msg.Channel)
		}
	default:
		t.Fatal("expected a message on the inbound bus, got none")
	}
}

// TestBaseChannel_HandleMessage_denied verifies that a denied sender's message
// is silently dropped (not published to the bus).
func TestBaseChannel_HandleMessage_denied(t *testing.T) {
	mb := bus.NewMessageBus(4)
	bc := NewBaseChannel(mb, []string{"alice"}) // only alice is allowed

	ctx := context.Background()
	err := bc.HandleMessage(ctx, "test", "mallory", "room1", "attack", nil)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}

	select {
	case msg := <-mb.Inbound:
		t.Fatalf("expected no message on bus, got: %+v", msg)
	default:
		// Correct: nothing was published.
	}
}

// TestCLIChannel_Name verifies the channel name.
func TestCLIChannel_Name(t *testing.T) {
	mb := bus.NewMessageBus(4)
	ch := NewCLIChannel(mb)
	if ch.Name() != "cli" {
		t.Errorf("expected name %q, got %q", "cli", ch.Name())
	}
}

// TestCLIChannel_Send verifies that Send enqueues the reply content.
func TestCLIChannel_Send(t *testing.T) {
	mb := bus.NewMessageBus(4)
	ch := NewCLIChannel(mb)

	ctx := context.Background()
	msg := &bus.OutboundMessage{
		Channel: "cli",
		ChatID:  "cli:local",
		Content: "hello from agent",
	}
	if err := ch.Send(ctx, msg); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	select {
	case got := <-ch.replies:
		if got != "hello from agent" {
			t.Errorf("expected %q, got %q", "hello from agent", got)
		}
	default:
		t.Fatal("expected reply in channel, got none")
	}
}

// TestCLIChannel_Send_empty verifies that empty content is a no-op.
func TestCLIChannel_Send_empty(t *testing.T) {
	mb := bus.NewMessageBus(4)
	ch := NewCLIChannel(mb)

	ctx := context.Background()
	if err := ch.Send(ctx, &bus.OutboundMessage{Content: ""}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	select {
	case got := <-ch.replies:
		t.Fatalf("expected no reply for empty content, got %q", got)
	default:
		// Correct.
	}
}
