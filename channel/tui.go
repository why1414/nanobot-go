// Package channel provides chat-platform integrations.
//
// tui.go implements a Bubble Tea TUI channel for interactive terminal chat.
package channel

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/libo/nanobot-go/bus"
)

const (
	tuiChannelName = "tui"
	tuiChatIDBase  = "tui:local"
)

// TUIChannel is a Bubble Tea powered interactive terminal channel.
// It implements the Channel interface and integrates with the message bus.
type TUIChannel struct {
	BaseChannel
	program *tea.Program
	chatID  string
	model   string // LLM model name, for display

	// replies receives agent responses from Send.
	replies chan string
}

// NewTUIChannel creates a TUIChannel backed by the given MessageBus.
// modelName is used for the header display.
func NewTUIChannel(b *bus.MessageBus, modelName string) *TUIChannel {
	return &TUIChannel{
		BaseChannel: NewBaseChannel(b, nil),
		chatID:      tuiChatIDBase,
		model:       modelName,
		replies:     make(chan string, 16),
	}
}

// Name returns "tui".
func (t *TUIChannel) Name() string { return tuiChannelName }

// Send enqueues an outbound message for the TUI to display.
func (t *TUIChannel) Send(_ context.Context, msg *bus.OutboundMessage) error {
	if msg.Content == "" {
		return nil
	}
	select {
	case t.replies <- msg.Content:
	default:
		// Buffer full; drop (rare in practice).
	}
	return nil
}

// Start launches the Bubble Tea program. It blocks until the user quits.
func (t *TUIChannel) Start(ctx context.Context) error {
	m := newTUIModel(t)
	t.program = tea.NewProgram(m, tea.WithAltScreen())

	// Listen for context cancellation and send a quit message.
	go func() {
		<-ctx.Done()
		if t.program != nil {
			t.program.Send(tea.Quit())
		}
	}()

	if _, err := t.program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Bubble Tea model
// ---------------------------------------------------------------------------

// tuiMsg types for inter-goroutine communication.
type tuiReplyMsg string
type tuiThinkingMsg bool

// chatMessage is one item in the chat history.
type chatMessage struct {
	role    string // "user" or "assistant"
	content string
}

type tuiModel struct {
	ch       *TUIChannel
	messages []chatMessage
	input    string
	thinking bool
	width    int
	height   int
}

func newTUIModel(ch *TUIChannel) tuiModel {
	return tuiModel{ch: ch}
}

// Init starts a goroutine that polls for replies from the agent.
func (m tuiModel) Init() tea.Cmd {
	return m.waitForReply()
}

// Update handles Bubble Tea messages.
func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tuiReplyMsg:
		content := string(msg)
		m.thinking = false
		m.messages = append(m.messages, chatMessage{role: "assistant", content: content})
		return m, m.waitForReply()

	case tuiThinkingMsg:
		// Used only to keep the reply poller alive; thinking state set on submit.
		return m, m.waitForReply()
	}
	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEnter:
		text := strings.TrimSpace(m.input)
		m.input = ""
		if text == "" {
			return m, nil
		}
		switch strings.ToLower(text) {
		case "/exit", "/quit":
			return m, tea.Quit
		case "/new":
			newID := fmt.Sprintf("%s:%d", tuiChatIDBase, nextTUISessionID())
			m.ch.chatID = newID
			m.messages = append(m.messages, chatMessage{role: "assistant", content: "[new conversation started]"})
			return m, nil
		}
		// Send to agent.
		m.messages = append(m.messages, chatMessage{role: "user", content: text})
		m.thinking = true
		cmd := func() tea.Msg {
			ctx := context.Background()
			_ = m.ch.HandleMessage(ctx, tuiChannelName, "user", m.ch.chatID, text, nil)
			return tuiThinkingMsg(true)
		}
		return m, cmd

	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}

	case tea.KeyRunes:
		m.input += msg.String()

	default:
		if msg.String() != "" && !strings.HasPrefix(msg.String(), "alt+") {
			m.input += msg.String()
		}
	}
	return m, nil
}

// waitForReply returns a command that blocks until a reply is available.
func (m tuiModel) waitForReply() tea.Cmd {
	return func() tea.Msg {
		reply := <-m.ch.replies
		return tuiReplyMsg(reply)
	}
}

// View renders the TUI.
func (m tuiModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Padding(0, 1)

	userStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("33")).
		Bold(true)

	assistantStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("78")).
		Bold(true)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1).
		Width(m.width - 4)

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Header
	header := headerStyle.Render(fmt.Sprintf("NanoBot  model: %s  /new = new session  /exit = quit", m.ch.model))

	// Chat history — allocate available lines.
	inputHeight := 3 // border + 1 line + padding
	statusHeight := 1
	headerHeight := 1
	chatHeight := m.height - headerHeight - inputHeight - statusHeight - 2
	if chatHeight < 1 {
		chatHeight = 1
	}

	var chatLines []string
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			chatLines = append(chatLines, userStyle.Render("You:")+" "+msg.content)
		case "assistant":
			chatLines = append(chatLines, assistantStyle.Render("Assistant:")+" "+msg.content)
		}
		chatLines = append(chatLines, "") // blank line
	}
	// Keep only the last chatHeight lines.
	if len(chatLines) > chatHeight {
		chatLines = chatLines[len(chatLines)-chatHeight:]
	}
	// Pad to fill space.
	for len(chatLines) < chatHeight {
		chatLines = append([]string{""}, chatLines...)
	}
	chatView := strings.Join(chatLines, "\n")

	// Status line
	status := ""
	if m.thinking {
		status = dimStyle.Render("⠋ thinking...")
	}

	// Input box
	prompt := inputStyle.Render("You: " + m.input + "█")

	return strings.Join([]string{header, chatView, status, prompt}, "\n")
}

// tuiSessionCounter generates unique session IDs for /new.
var tuiSessionCounter atomic.Int64

func nextTUISessionID() int64 {
	return tuiSessionCounter.Add(1)
}
