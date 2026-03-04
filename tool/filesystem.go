package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadFileTool reads the contents of a file. Tool name: "read_file".
type ReadFileTool struct {
	workspace string
}

// NewReadFileTool creates a ReadFileTool anchored to the given workspace directory.
// Pass an empty string to use absolute paths only.
func NewReadFileTool(workspace string) *ReadFileTool {
	return &ReadFileTool{workspace: workspace}
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file at the given path." }
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to read",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Line offset to start reading from (1-based, optional)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of lines to read (optional)",
			},
		},
		"required": []string{"path"},
	}
}

// Execute reads the file and returns its content (optionally sliced by offset/limit).
func (t *ReadFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved := t.resolvePath(path)

	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Sprintf("Error reading file: %s", err.Error()), nil
	}

	content := string(data)

	// Apply optional offset/limit.
	offset := intParam(params, "offset")
	limit := intParam(params, "limit")
	if offset > 0 || limit > 0 {
		lines := strings.Split(content, "\n")
		start := 0
		if offset > 0 {
			start = offset - 1 // convert 1-based to 0-based
		}
		if start >= len(lines) {
			return "", nil
		}
		lines = lines[start:]
		if limit > 0 && limit < len(lines) {
			lines = lines[:limit]
		}
		content = strings.Join(lines, "\n")
	}
	return content, nil
}

func (t *ReadFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if t.workspace != "" {
		return filepath.Join(t.workspace, path)
	}
	return path
}

// WriteFileTool writes content to a file (overwrites). Tool name: "write_file".
type WriteFileTool struct {
	workspace string
}

// NewWriteFileTool creates a WriteFileTool anchored to the given workspace directory.
func NewWriteFileTool(workspace string) *WriteFileTool {
	return &WriteFileTool{workspace: workspace}
}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file. Creates parent directories if needed."
}
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to write to",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write",
			},
		},
		"required": []string{"path", "content"},
	}
}

// Execute writes the file, creating parent directories as needed.
func (t *WriteFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved := t.resolvePath(path)

	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Sprintf("Error creating directories: %s", err.Error()), nil
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return fmt.Sprintf("Error writing file: %s", err.Error()), nil
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), resolved), nil
}

func (t *WriteFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if t.workspace != "" {
		return filepath.Join(t.workspace, path)
	}
	return path
}

// EditFileTool replaces old_text with new_text in a file. Tool name: "edit_file".
type EditFileTool struct {
	workspace string
}

// NewEditFileTool creates an EditFileTool anchored to the given workspace directory.
func NewEditFileTool(workspace string) *EditFileTool {
	return &EditFileTool{workspace: workspace}
}

func (t *EditFileTool) Name() string { return "edit_file" }
func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. old_text must exist exactly once."
}
func (t *EditFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The file path to edit",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "The replacement text",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

// Execute replaces the first occurrence of old_text with new_text.
func (t *EditFileTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	oldText, _ := params["old_text"].(string)
	newText, _ := params["new_text"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved := t.resolvePath(path)

	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Sprintf("Error reading file: %s", err.Error()), nil
	}
	content := string(data)
	count := strings.Count(content, oldText)
	if count == 0 {
		return fmt.Sprintf("Error: old_text not found in %s", path), nil
	}
	if count > 1 {
		return fmt.Sprintf("Warning: old_text appears %d times; provide more context to make it unique", count), nil
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(resolved, []byte(newContent), 0o644); err != nil {
		return fmt.Sprintf("Error writing file: %s", err.Error()), nil
	}
	return fmt.Sprintf("Successfully edited %s", resolved), nil
}

func (t *EditFileTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if t.workspace != "" {
		return filepath.Join(t.workspace, path)
	}
	return path
}

// ListDirTool lists the contents of a directory. Tool name: "list_dir".
type ListDirTool struct {
	workspace string
}

// NewListDirTool creates a ListDirTool anchored to the given workspace directory.
func NewListDirTool(workspace string) *ListDirTool {
	return &ListDirTool{workspace: workspace}
}

func (t *ListDirTool) Name() string        { return "list_dir" }
func (t *ListDirTool) Description() string { return "List the contents of a directory." }
func (t *ListDirTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The directory path to list",
			},
		},
		"required": []string{"path"},
	}
}

// Execute lists the directory entries, prefixing dirs with "[dir]" and files with "[file]".
func (t *ListDirTool) Execute(_ context.Context, params map[string]any) (string, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved := t.resolvePath(path)

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return fmt.Sprintf("Error listing directory: %s", err.Error()), nil
	}
	if len(entries) == 0 {
		return fmt.Sprintf("Directory %s is empty", path), nil
	}

	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			sb.WriteString("[dir]  ")
		} else {
			sb.WriteString("[file] ")
		}
		sb.WriteString(e.Name())
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func (t *ListDirTool) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if t.workspace != "" {
		return filepath.Join(t.workspace, path)
	}
	return path
}

// intParam extracts an integer from params (supports float64 from JSON decoding).
func intParam(params map[string]any, key string) int {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
