// Package agent implements the core agent loop, session management, and context building.
package agent

import (
	"sync"
	"time"

	"github.com/libo/nanobot-go/provider"
)

// SessionMessage is a persisted message in a session's history.
type SessionMessage struct {
	Role       string
	Content    string
	ToolCalls  []provider.ToolCall
	ToolCallID string
	Name       string
	Timestamp  time.Time
}

// Session holds the conversation history for a single chat session.
type Session struct {
	Key      string
	Messages []SessionMessage
}

// SessionManager stores sessions in memory, keyed by session key.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager creates an empty SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// GetOrCreate returns the existing session for key, or creates a new empty one.
func (m *SessionManager) GetOrCreate(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		return s
	}
	s := &Session{Key: key}
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
		s = &Session{Key: key}
		m.sessions[key] = s
	}
	s.Messages = append(s.Messages, msgs...)
}
