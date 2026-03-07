package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultSystemPrompt is the built-in default system prompt.
const DefaultSystemPrompt = `You are a helpful AI assistant with access to tools.

When using tools:
- Make sure to use the correct tool for the task
- Provide clear and complete parameters
- Handle errors gracefully

Be concise and helpful in your responses.`

// SystemPromptBuilder builds the system prompt from multiple sources.
type SystemPromptBuilder struct {
	workspace       string
	skillsLoader    *SkillsLoader
	memoryStore     *MemoryStore
}

// NewSystemPromptBuilder creates a new SystemPromptBuilder.
func NewSystemPromptBuilder(workspace string, skillsLoader *SkillsLoader, memoryStore *MemoryStore) *SystemPromptBuilder {
	return &SystemPromptBuilder{
		workspace:    workspace,
		skillsLoader: skillsLoader,
		memoryStore:  memoryStore,
	}
}

// Build constructs the full system prompt.
// It combines: built-in prompt + workspace file + skills + memory.
func (b *SystemPromptBuilder) Build() string {
	var parts []string

	// 1. Base system prompt from file or default
	basePrompt := b.loadWorkspacePrompt()
	if basePrompt == "" {
		basePrompt = DefaultSystemPrompt
	}
	parts = append(parts, basePrompt)

	// 2. Skills summary (for progressive loading)
	if b.skillsLoader != nil {
		skillsSummary := b.skillsLoader.BuildSkillsSummary()
		if skillsSummary != "" {
			parts = append(parts, "\n\n## Available Skills\n\n"+skillsSummary)
		}

		// Load always-active skills
		alwaysSkills := b.skillsLoader.GetAlwaysSkills()
		if len(alwaysSkills) > 0 {
			alwaysContent := b.skillsLoader.LoadSkillsForContext(alwaysSkills)
			if alwaysContent != "" {
				parts = append(parts, "\n\n## Active Skills\n\n"+alwaysContent)
			}
		}
	}

	// 3. Memory context
	if b.memoryStore != nil {
		memoryCtx := b.memoryStore.GetMemoryContext()
		if memoryCtx != "" {
			parts = append(parts, "\n\n"+memoryCtx)
		}
	}

	return strings.Join(parts, "")
}

// loadWorkspacePrompt loads the system prompt from AGENT.md or SYSTEM.md in the workspace.
func (b *SystemPromptBuilder) loadWorkspacePrompt() string {
	// Try AGENT.md first
	if content, err := os.ReadFile(filepath.Join(b.workspace, "AGENT.md")); err == nil {
		return string(content)
	}

	// Try SYSTEM.md
	if content, err := os.ReadFile(filepath.Join(b.workspace, "SYSTEM.md")); err == nil {
		return string(content)
	}

	return ""
}

// LoadSystemPrompt loads the system prompt from the workspace directory.
// Returns the content of AGENT.md or SYSTEM.md if found, otherwise returns empty string.
func LoadSystemPrompt(workspace string) string {
	// Try AGENT.md first
	if content, err := os.ReadFile(filepath.Join(workspace, "AGENT.md")); err == nil {
		return string(content)
	}

	// Try SYSTEM.md
	if content, err := os.ReadFile(filepath.Join(workspace, "SYSTEM.md")); err == nil {
		return string(content)
	}

	return ""
}

// BuildSystemPrompt builds a system prompt from workspace file, skills, and memory.
// This is a convenience function for simple use cases.
func BuildSystemPrompt(workspace string, skillsLoader *SkillsLoader, memoryStore *MemoryStore) string {
	builder := NewSystemPromptBuilder(workspace, skillsLoader, memoryStore)
	return builder.Build()
}

// FormatTimestamp formats a timestamp for the given time.
func FormatTimestamp(t interface{}) string {
	switch v := t.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", t)
	}
}
