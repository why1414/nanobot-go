// Package agent implements the core agent loop, session management, and context building.
package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/libo/nanobot-go/provider"
)

// SessionMessage is a persisted message in a session's history.
type SessionMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCalls  []provider.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string              `json:"toolCallId,omitempty"`
	Name       string              `json:"name,omitempty"`
	Timestamp  time.Time           `json:"timestamp"`
	ToolsUsed  []string            `json:"toolsUsed,omitempty"` // Tools used in this message (for memory consolidation)
}

// SessionMetadata is the first line in a session JSONL file.
type SessionMetadata struct {
	Type             string    `json:"_type"`
	Key              string    `json:"key"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	LastConsolidated int       `json:"lastConsolidated"`
}

// Session holds the conversation history for a single chat session.
type Session struct {
	Key              string           `json:"key"`
	Messages         []SessionMessage `json:"messages"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	LastConsolidated int              `json:"lastConsolidated"` // Index of last consolidated message
}

// SessionManager stores sessions in memory and persists to JSONL files.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	sessionsDir string
}

// NewSessionManager creates a SessionManager with JSONL persistence.
func NewSessionManager(workspace string) *SessionManager {
	sessionsDir := filepath.Join(workspace, "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		slog.Warn("failed to create sessions directory", "error", err)
	}
	return &SessionManager{
		sessions:    make(map[string]*Session),
		sessionsDir: sessionsDir,
	}
}

// GetOrCreate returns the existing session for key, or creates a new empty one.
func (m *SessionManager) GetOrCreate(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		return s
	}

	// Try to load from disk
	s := m.load(key)
	if s == nil {
		s = &Session{
			Key:       key,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	m.sessions[key] = s
	return s
}

// GetHistory returns up to maxMessages most-recent messages from the session.
// If maxMessages is 0 or negative, all messages are returned.
func (m *SessionManager) GetHistory(key string, maxMessages int) []SessionMessage {
	m.mu.RLock()
	s := m.sessions[key]
	m.mu.RUnlock()

	if s == nil {
		return nil
	}

	msgs := s.Messages
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	return msgs
}

// AppendMessages appends the given messages to the named session.
func (m *SessionManager) AppendMessages(key string, msgs []SessionMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[key]
	if !ok {
		s = &Session{
			Key:       key,
			CreatedAt: time.Now(),
		}
		m.sessions[key] = s
	}

	s.Messages = append(s.Messages, msgs...)
	s.UpdatedAt = time.Now()

	// Persist to disk
	if err := m.save(s); err != nil {
		slog.Warn("failed to save session", "key", key, "error", err)
	}
}

// getSessionPath returns the JSONL file path for a session.
func (m *SessionManager) getSessionPath(key string) string {
	// Replace : with _ for safe filename
	safeKey := safeFilename(key)
	return filepath.Join(m.sessionsDir, safeKey+".jsonl")
}

// load loads a session from JSONL file.
func (m *SessionManager) load(key string) *Session {
	path := m.getSessionPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	session := &Session{
		Key:       key,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try to parse as metadata
		var meta SessionMetadata
		if err := json.Unmarshal([]byte(line), &meta); err == nil && meta.Type == "metadata" {
			session.Key = meta.Key
			session.CreatedAt = meta.CreatedAt
			session.UpdatedAt = meta.UpdatedAt
			session.LastConsolidated = meta.LastConsolidated
			continue
		}

		// Parse as message
		var msg SessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err == nil {
			session.Messages = append(session.Messages, msg)
		}
	}

	return session
}

// save saves a session to JSONL file.
func (m *SessionManager) save(session *Session) error {
	path := m.getSessionPath(session.Key)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	defer f.Close()

	// Write metadata line
	meta := SessionMetadata{
		Type:             "metadata",
		Key:              session.Key,
		CreatedAt:        session.CreatedAt,
		UpdatedAt:        session.UpdatedAt,
		LastConsolidated: session.LastConsolidated,
	}
	metaJSON, _ := json.Marshal(meta)
	if _, err := fmt.Fprintln(f, string(metaJSON)); err != nil {
		return err
	}

	// Write message lines
	for _, msg := range session.Messages {
		msgJSON, _ := json.Marshal(msg)
		if _, err := fmt.Fprintln(f, string(msgJSON)); err != nil {
			return err
		}
	}

	return nil
}

// safeFilename converts a session key to a safe filename.
func safeFilename(key string) string {
	// Replace : with _ (e.g., "cli:default" -> "cli_default")
	return strings.ReplaceAll(key, ":", "_")
}
