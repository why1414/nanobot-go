package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// InitWorkspace initializes a nanobot workspace with default files and directories.
// This should be called on first run or when setting up a new workspace.
func InitWorkspace(workspace string) error {
	// Create workspace directory
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	// Create memory directory
	memoryDir := filepath.Join(workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return fmt.Errorf("create memory directory: %w", err)
	}

	// Create sessions directory
	sessionsDir := filepath.Join(workspace, "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return fmt.Errorf("create sessions directory: %w", err)
	}

	// Create skills directory (for custom skills)
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("create skills directory: %w", err)
	}

	// Initialize memory files if they don't exist
	if err := initMemoryFiles(memoryDir); err != nil {
		return fmt.Errorf("init memory files: %w", err)
	}

	return nil
}

// initMemoryFiles creates default MEMORY.md and HISTORY.md files.
func initMemoryFiles(memoryDir string) error {
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	historyFile := filepath.Join(memoryDir, "HISTORY.md")

	// Create default MEMORY.md if it doesn't exist
	if _, err := os.Stat(memoryFile); os.IsNotExist(err) {
		defaultMemory := `# Long-term Memory

This file stores important facts about the user, projects, and context.
The agent can read and update this file to remember information across sessions.

## User Preferences


## Projects


## Important Context

`
		if err := os.WriteFile(memoryFile, []byte(defaultMemory), 0644); err != nil {
			return fmt.Errorf("create MEMORY.md: %w", err)
		}
	}

	// Create empty HISTORY.md if it doesn't exist
	if _, err := os.Stat(historyFile); os.IsNotExist(err) {
		if err := os.WriteFile(historyFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("create HISTORY.md: %w", err)
		}
	}

	return nil
}

// CopyTemplates copies template files from templatesDir to workspace.
// This is optional - users can customize these files.
func CopyTemplates(templatesDir, workspace string) error {
	templateFiles := []string{"AGENTS.md", "SOUL.md", "USER.md", "TOOLS.md"}

	for _, filename := range templateFiles {
		src := filepath.Join(templatesDir, filename)
		dst := filepath.Join(workspace, filename)

		// Only copy if destination doesn't exist
		if _, err := os.Stat(dst); err == nil {
			continue
		}

		// Read source file
		data, err := os.ReadFile(src)
		if err != nil {
			// Skip if template doesn't exist
			continue
		}

		// Write to destination
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("copy %s: %w", filename, err)
		}
	}

	return nil
}

// EnsureWorkspace ensures the workspace exists and is initialized.
// This is a convenience function that combines InitWorkspace and CopyTemplates.
func EnsureWorkspace(workspace, templatesDir string) error {
	if err := InitWorkspace(workspace); err != nil {
		return err
	}

	if templatesDir != "" {
		if err := CopyTemplates(templatesDir, workspace); err != nil {
			// Non-fatal - templates are optional
			// Just log the error and continue
		}
	}

	return nil
}
