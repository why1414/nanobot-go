package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/libo/nanobot-go/provider"
)

// MemoryStore implements two-layer memory: MEMORY.md (long-term facts) + HISTORY.md (grep-searchable log).
type MemoryStore struct {
	memoryDir  string
	memoryFile string
	historyFile string
}

// NewMemoryStore creates a MemoryStore in the given workspace.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	return &MemoryStore{
		memoryDir:   memoryDir,
		memoryFile:  filepath.Join(memoryDir, "MEMORY.md"),
		historyFile: filepath.Join(memoryDir, "HISTORY.md"),
	}
}

// ReadLongTerm reads the long-term memory file.
func (m *MemoryStore) ReadLongTerm() string {
	data, err := os.ReadFile(m.memoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteLongTerm writes the long-term memory file.
func (m *MemoryStore) WriteLongTerm(content string) error {
	if err := os.MkdirAll(m.memoryDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(m.memoryFile, []byte(content), 0644)
}

// AppendHistory appends an entry to the history file.
func (m *MemoryStore) AppendHistory(entry string) error {
	if err := os.MkdirAll(m.memoryDir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(m.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.TrimSpace(entry) + "\n\n")
	return err
}

// GetMemoryContext returns the long-term memory content for system prompt injection.
func (m *MemoryStore) GetMemoryContext() string {
	longTerm := m.ReadLongTerm()
	if longTerm == "" {
		return ""
	}
	return fmt.Sprintf("## Long-term Memory\n%s", longTerm)
}

// ConsolidateOptions holds options for memory consolidation.
type ConsolidateOptions struct {
	ArchiveAll    bool
	MemoryWindow  int
}

// Consolidate consolidates old messages into MEMORY.md + HISTORY.md via LLM tool call.
// Returns true on success (including no-op), false on failure.
func (m *MemoryStore) Consolidate(
	ctx context.Context,
	session *Session,
	prov provider.LLMProvider,
	model string,
	opts ConsolidateOptions,
) bool {
	var oldMessages []SessionMessage
	var keepCount int

	if opts.ArchiveAll {
		oldMessages = session.Messages
		keepCount = 0
		slog.Info("Memory consolidation (archive_all)", "messages", len(session.Messages))
	} else {
		keepCount = opts.MemoryWindow / 2
		if len(session.Messages) <= keepCount {
			return true
		}
		if session.LastConsolidated >= len(session.Messages) {
			return true
		}

		startIdx := session.LastConsolidated
		endIdx := len(session.Messages) - keepCount
		if endIdx <= startIdx {
			return true
		}
		oldMessages = session.Messages[startIdx:endIdx]

		slog.Info("Memory consolidation", "to_consolidate", len(oldMessages), "keep", keepCount)
	}

	if len(oldMessages) == 0 {
		return true
	}

	// Build prompt
	var lines []string
	for _, msg := range oldMessages {
		if msg.Content == "" {
			continue
		}
		ts := msg.Timestamp.Format("2006-01-02 15:04")
		tools := ""
		if len(msg.ToolsUsed) > 0 {
			tools = fmt.Sprintf(" [tools: %s]", strings.Join(msg.ToolsUsed, ", "))
		}
		lines = append(lines, fmt.Sprintf("[%s] %s%s: %s", ts, strings.ToUpper(msg.Role), tools, msg.Content))
	}

	currentMemory := m.ReadLongTerm()
	prompt := fmt.Sprintf(`Process this conversation and call the save_memory tool with your consolidation.

## Current Long-term Memory
%s

## Conversation to Process
%s`, orEmpty(currentMemory, "(empty)"), strings.Join(lines, "\n"))

	// Define save_memory tool
	saveMemoryTool := []provider.Tool{
		{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        "save_memory",
				Description: "Save the memory consolidation result to persistent storage.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"history_entry": map[string]any{
							"type":        "string",
							"description": "A paragraph (2-5 sentences) summarizing key events/decisions/topics. Start with [YYYY-MM-DD HH:MM]. Include detail useful for grep search.",
						},
						"memory_update": map[string]any{
							"type":        "string",
							"description": "Full updated long-term memory as markdown. Include all existing facts plus new ones. Return unchanged if nothing new.",
						},
					},
					"required": []string{"history_entry", "memory_update"},
				},
			},
		},
	}

	// Call LLM
	messages := []provider.Message{
		{Role: "system", Content: "You are a memory consolidation agent. Call the save_memory tool with your consolidation of the conversation."},
		{Role: "user", Content: prompt},
	}

	resp, err := prov.Chat(ctx, messages, provider.ChatOptions{
		Model: model,
		Tools: saveMemoryTool,
	})
	if err != nil {
		slog.Error("Memory consolidation failed", "error", err)
		return false
	}

	if !resp.HasToolCalls() {
		slog.Warn("Memory consolidation: LLM did not call save_memory, skipping")
		return false
	}

	// Find the save_memory tool call
	var args map[string]any
	for _, tc := range resp.ToolCalls {
		if tc.Name == "save_memory" {
			args = tc.Arguments
			break
		}
	}

	if args == nil {
		slog.Warn("Memory consolidation: no save_memory tool call found")
		return false
	}

	// Extract arguments
	if entry, ok := args["history_entry"]; ok {
		var entryStr string
		switch v := entry.(type) {
		case string:
			entryStr = v
		default:
			data, _ := json.Marshal(v)
			entryStr = string(data)
		}
		if entryStr != "" {
			if err := m.AppendHistory(entryStr); err != nil {
				slog.Warn("Failed to append history", "error", err)
			}
		}
	}

	if update, ok := args["memory_update"]; ok {
		var updateStr string
		switch v := update.(type) {
		case string:
			updateStr = v
		default:
			data, _ := json.Marshal(v)
			updateStr = string(data)
		}
		if updateStr != "" && updateStr != currentMemory {
			if err := m.WriteLongTerm(updateStr); err != nil {
				slog.Warn("Failed to write long-term memory", "error", err)
			}
		}
	}

	// Update session
	if opts.ArchiveAll {
		session.LastConsolidated = 0
	} else {
		session.LastConsolidated = len(session.Messages) - keepCount
	}

	slog.Info("Memory consolidation done", "messages", len(session.Messages), "last_consolidated", session.LastConsolidated)
	return true
}

func orEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Update the Session struct to include LastConsolidated and ToolsUsed
// We need to extend the SessionMessage struct

// SessionMessageExtended extends SessionMessage with additional fields for memory.
type SessionMessageExtended struct {
	Role       string
	Content    string
	ToolCalls  []provider.ToolCall
	ToolCallID string
	Name       string
	Timestamp  time.Time
	ToolsUsed  []string // Tools used in this message
}
