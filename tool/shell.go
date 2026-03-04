package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	defaultExecTimeout = 30 * time.Second
	maxOutputLen       = 10000
)

// ShellTool executes shell commands and returns stdout + stderr.
// Tool name: "exec".
type ShellTool struct {
	workdir string
	timeout time.Duration
}

// NewShellTool creates a ShellTool.
// workdir is the default working directory (uses os.Getwd if empty).
// timeout is the command timeout (uses defaultExecTimeout if zero).
func NewShellTool(workdir string, timeout time.Duration) *ShellTool {
	if timeout == 0 {
		timeout = defaultExecTimeout
	}
	return &ShellTool{workdir: workdir, timeout: timeout}
}

func (s *ShellTool) Name() string        { return "exec" }
func (s *ShellTool) Description() string { return "Execute a shell command and return its output." }
func (s *ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in seconds (default 30)",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the shell command and returns captured output.
func (s *ShellTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	command, _ := params["command"].(string)
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Determine working directory.
	workdir := s.workdir
	if wd, ok := params["working_dir"].(string); ok && wd != "" {
		workdir = wd
	}
	if workdir == "" {
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			workdir = "."
		}
	}

	// Determine timeout.
	timeout := s.timeout
	if ts, ok := params["timeout_seconds"]; ok {
		switch v := ts.(type) {
		case float64:
			timeout = time.Duration(v) * time.Second
		case int:
			timeout = time.Duration(v) * time.Second
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = workdir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var parts []string
	if out := stdout.String(); out != "" {
		parts = append(parts, out)
	}
	if errOut := stderr.String(); errOut != "" {
		parts = append(parts, "STDERR:\n"+errOut)
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("Error: command timed out after %v", timeout), nil
		}
		if exitCode := cmd.ProcessState; exitCode != nil {
			parts = append(parts, fmt.Sprintf("\nExit code: %d", exitCode.ExitCode()))
		}
	}

	result := joinParts(parts)
	if result == "" {
		result = "(no output)"
	}
	if len(result) > maxOutputLen {
		result = result[:maxOutputLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(result)-maxOutputLen)
	}
	return result, nil
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(p)
	}
	return buf.String()
}
