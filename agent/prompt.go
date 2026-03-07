package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultSystemPrompt is the built-in default system prompt.
const DefaultSystemPrompt = `You are a helpful AI assistant with access to tools.

When using tools:
- Make sure to use the correct tool for the task
- Provide clear and complete parameters
- Handle errors gracefully

Be concise and helpful in your responses.`

// BootstrapFiles are loaded from workspace in order.
var BootstrapFiles = []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md", "IDENTITY.md"}

// SystemPromptBuilder builds the system prompt from multiple sources.
type SystemPromptBuilder struct {
	workspace      string
	skillsLoader   *SkillsLoader
	memoryStore    *MemoryStore
	bootstrapFiles []string
}

// NewSystemPromptBuilder creates a new SystemPromptBuilder.
func NewSystemPromptBuilder(workspace string, skillsLoader *SkillsLoader, memoryStore *MemoryStore) *SystemPromptBuilder {
	return &SystemPromptBuilder{
		workspace:      workspace,
		skillsLoader:   skillsLoader,
		memoryStore:    memoryStore,
		bootstrapFiles: BootstrapFiles,
	}
}

// Build constructs the full system prompt.
// It combines: core identity + bootstrap files + skills + memory.
func (b *SystemPromptBuilder) Build() string {
	var parts []string

	// 1. Core identity (time, runtime, workspace info)
	parts = append(parts, b.buildCoreIdentity())

	// 2. Bootstrap files (AGENTS.md, SOUL.md, USER.md, TOOLS.md, IDENTITY.md)
	bootstrap := b.loadBootstrapFiles()
	if bootstrap != "" {
		parts = append(parts, bootstrap)
	}

	// 3. Skills - progressive loading
	if b.skillsLoader != nil {
		// Always-loaded skills: full content
		alwaysSkills := b.skillsLoader.GetAlwaysSkills()
		if len(alwaysSkills) > 0 {
			alwaysContent := b.skillsLoader.LoadSkillsForContext(alwaysSkills)
			if alwaysContent != "" {
				parts = append(parts, "# Active Skills\n\n"+alwaysContent)
			}
		}

		// All skills: summary only
		skillsSummary := b.skillsLoader.BuildSkillsSummary()
		if skillsSummary != "" {
			skillsSection := fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.
Skills with available="false" need dependencies installed first - you can try installing them with apt/brew.

%s`, skillsSummary)
			parts = append(parts, skillsSection)
		}
	}

	// 4. Memory context
	if b.memoryStore != nil {
		memoryCtx := b.memoryStore.GetMemoryContext()
		if memoryCtx != "" {
			parts = append(parts, "# Memory\n\n"+memoryCtx)
		}
	}

	return strings.Join(parts, "\n\n---\n\n")
}

// buildCoreIdentity builds the core identity section with runtime info.
func (b *SystemPromptBuilder) buildCoreIdentity() string {
	now := time.Now()
	nowStr := now.Format("2006-01-02 15:04 (Monday)")
	_, offset := now.Zone()
	tz := fmt.Sprintf("UTC%+d", offset/3600)
	if name, _ := now.Zone(); name != "" {
		tz = name
	}

	workspacePath := b.workspace
	if abs, err := filepath.Abs(b.workspace); err == nil {
		workspacePath = abs
	}

	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macOS"
	}
	runtimeInfo := fmt.Sprintf("%s %s, Go %s", osName, runtime.GOARCH, runtime.Version()[2:])

	return fmt.Sprintf(`# nanobot 🐈

You are nanobot, a helpful AI assistant.

## Current Time
%s (%s)

## Runtime
%s

## Workspace
Your workspace is at: %s
- Long-term memory: %s/memory/MEMORY.md
- History log: %s/memory/HISTORY.md (grep-searchable)
- Custom skills: %s/skills/{skill-name}/SKILL.md

Reply directly with text for conversations. Only use the 'message' tool to send to a specific chat channel.

## Tool Call Guidelines
- Before calling tools, you may briefly state your intent (e.g. "Let me check that"), but NEVER predict or describe the expected result before receiving it.
- Before modifying a file, read it first to confirm its current content.
- Do not assume a file or directory exists — use list_dir or read_file to verify.
- After writing or editing a file, re-read it if accuracy matters.
- If a tool call fails, analyze the error before retrying with a different approach.

## Memory
- Remember important facts: write to %s/memory/MEMORY.md
- Recall past events: grep %s/memory/HISTORY.md`, nowStr, tz, runtimeInfo, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath)
}

// loadBootstrapFiles loads all bootstrap files from workspace.
func (b *SystemPromptBuilder) loadBootstrapFiles() string {
	var parts []string

	for _, filename := range b.bootstrapFiles {
		filePath := filepath.Join(b.workspace, filename)
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(content))
		if text != "" {
			parts = append(parts, fmt.Sprintf("## %s\n\n%s", filename, text))
		}
	}

	return strings.Join(parts, "\n\n")
}
