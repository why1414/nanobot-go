// Package cron provides cron job scheduling for nanobot-go.
package cron

import "time"

// ScheduleKind represents the type of schedule.
type ScheduleKind string

const (
	ScheduleKindAt    ScheduleKind = "at"
	ScheduleKindEvery ScheduleKind = "every"
	ScheduleKindCron  ScheduleKind = "cron"
)

// CronSchedule defines when a job should run.
type CronSchedule struct {
	Kind ScheduleKind `json:"kind"`
	// For "at": timestamp in ms
	AtMs int64 `json:"atMs,omitempty"`
	// For "every": interval in ms
	EveryMs int64 `json:"everyMs,omitempty"`
	// For "cron": cron expression (e.g. "0 9 * * *")
	Expr string `json:"expr,omitempty"`
	// Timezone for cron expressions
	TZ string `json:"tz,omitempty"`
}

// PayloadKind represents what to do when the job runs.
type PayloadKind string

const (
	PayloadKindSystemEvent PayloadKind = "system_event"
	PayloadKindAgentTurn   PayloadKind = "agent_turn"
)

// CronPayload defines what to do when the job runs.
type CronPayload struct {
	Kind    PayloadKind `json:"kind"`
	Message string      `json:"message"`
	// Deliver response to channel
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"` // e.g. "whatsapp"
	To      string `json:"to,omitempty"`      // e.g. phone number
}

// JobStatus represents the last run status.
type JobStatus string

const (
	JobStatusOK      JobStatus = "ok"
	JobStatusError   JobStatus = "error"
	JobStatusSkipped JobStatus = "skipped"
)

// CronJobState holds runtime state of a job.
type CronJobState struct {
	NextRunAtMs  int64      `json:"nextRunAtMs,omitempty"`
	LastRunAtMs  int64      `json:"lastRunAtMs,omitempty"`
	LastStatus   JobStatus  `json:"lastStatus,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
}

// CronJob represents a scheduled job.
type CronJob struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Enabled         bool          `json:"enabled"`
	Schedule        CronSchedule  `json:"schedule"`
	Payload         CronPayload   `json:"payload"`
	State           CronJobState  `json:"state"`
	CreatedAtMs     int64         `json:"createdAtMs"`
	UpdatedAtMs     int64         `json:"updatedAtMs"`
	DeleteAfterRun  bool          `json:"deleteAfterRun"`
}

// CronStore holds persistent store for cron jobs.
type CronStore struct {
	Version int        `json:"version"`
	Jobs    []CronJob  `json:"jobs"`
}

// nowMs returns current time in milliseconds.
func nowMs() int64 {
	return time.Now().UnixMilli()
}
