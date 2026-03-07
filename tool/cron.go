package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/libo/nanobot-go/cron"
)

// CronTool is a tool to schedule reminders and recurring tasks.
type CronTool struct {
	cron    *cron.CronService
	channel string
	chatID  string
}

// NewCronTool creates a new CronTool.
func NewCronTool(cronService *cron.CronService) *CronTool {
	return &CronTool{cron: cronService}
}

// SetContext sets the current session context for delivery.
func (t *CronTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

func (t *CronTool) Name() string { return "cron" }

func (t *CronTool) Description() string {
	return "Schedule reminders and recurring tasks. Actions: add, list, remove."
}

func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "remove"},
				"description": "Action to perform",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Reminder message (for add)",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Interval in seconds (for recurring tasks)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression like '0 9 * * *' (for scheduled tasks)",
			},
			"tz": map[string]any{
				"type":        "string",
				"description": "IANA timezone for cron expressions (e.g. 'America/Vancouver')",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "ISO datetime for one-time execution (e.g. '2026-02-12T10:30:00')",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (for remove)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	action, _ := params["action"].(string)

	switch action {
	case "add":
		return t.addJob(params), nil
	case "list":
		return t.listJobs(), nil
	case "remove":
		return t.removeJob(params), nil
	default:
		return fmt.Sprintf("Unknown action: %s", action), nil
	}
}

func (t *CronTool) addJob(params map[string]any) string {
	message, _ := params["message"].(string)
	if message == "" {
		return "Error: message is required for add"
	}

	if t.channel == "" || t.chatID == "" {
		return "Error: no session context (channel/chat_id)"
	}

	tz, _ := params["tz"].(string)
	cronExpr, _ := params["cron_expr"].(string)

	if tz != "" && cronExpr == "" {
		return "Error: tz can only be used with cron_expr"
	}

	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Sprintf("Error: unknown timezone '%s'", tz)
		}
	}

	var schedule cron.CronSchedule
	var deleteAfter bool

	if everySeconds, ok := params["every_seconds"]; ok {
		var seconds int64
		switch v := everySeconds.(type) {
		case float64:
			seconds = int64(v)
		case int:
			seconds = int64(v)
		}
		if seconds <= 0 {
			return "Error: every_seconds must be positive"
		}
		schedule = cron.CronSchedule{
			Kind:    cron.ScheduleKindEvery,
			EveryMs: seconds * 1000,
		}
	} else if cronExpr != "" {
		schedule = cron.CronSchedule{
			Kind: cron.ScheduleKindCron,
			Expr: cronExpr,
			TZ:   tz,
		}
	} else if at, ok := params["at"].(string); ok {
		dt, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return fmt.Sprintf("Error: invalid ISO datetime format: %v", err)
		}
		schedule = cron.CronSchedule{
			Kind:  cron.ScheduleKindAt,
			AtMs:  dt.UnixMilli(),
		}
		deleteAfter = true
	} else {
		return "Error: either every_seconds, cron_expr, or at is required"
	}

	// Truncate name to 30 chars
	name := message
	if len(name) > 30 {
		name = name[:30]
	}

	job, err := t.cron.AddJob(name, schedule, message, true, t.channel, t.chatID, deleteAfter)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	return fmt.Sprintf("Created job '%s' (id: %s)", job.Name, job.ID)
}

func (t *CronTool) listJobs() string {
	jobs := t.cron.ListJobs(false)
	if len(jobs) == 0 {
		return "No scheduled jobs."
	}

	var lines []string
	for _, job := range jobs {
		lines = append(lines, fmt.Sprintf("- %s (id: %s, %s)", job.Name, job.ID, job.Schedule.Kind))
	}
	return "Scheduled jobs:\n" + joinLines(lines)
}

func (t *CronTool) removeJob(params map[string]any) string {
	jobID, _ := params["job_id"].(string)
	if jobID == "" {
		return "Error: job_id is required for remove"
	}

	if t.cron.RemoveJob(jobID) {
		return fmt.Sprintf("Removed job %s", jobID)
	}
	return fmt.Sprintf("Job %s not found", jobID)
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
