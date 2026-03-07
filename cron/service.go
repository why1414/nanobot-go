// Package cron provides cron job scheduling for nanobot-go.
package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JobCallback is called when a job executes. Returns response text or error.
type JobCallback func(job *CronJob) (string, error)

// CronService manages and executes scheduled jobs.
type CronService struct {
	storePath string
	onJob     JobCallback
	store     *CronStore
	mu        sync.RWMutex
	timer     *time.Timer
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewCronService creates a new CronService.
func NewCronService(storePath string, onJob JobCallback) *CronService {
	return &CronService{
		storePath: storePath,
		onJob:     onJob,
	}
}

// Start starts the cron service.
func (s *CronService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true

	if err := s.loadStore(); err != nil {
		slog.Warn("failed to load cron store", "error", err)
	}

	s.recomputeNextRuns()
	s.saveStore()
	s.armTimer()

	slog.Info("Cron service started", "jobs", len(s.store.Jobs))
	return nil
}

// Stop stops the cron service.
func (s *CronService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	s.running = false
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()

	slog.Info("Cron service stopped")
}

// loadStore loads jobs from disk.
func (s *CronService) loadStore() error {
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.store = &CronStore{Version: 1}
			return nil
		}
		return fmt.Errorf("read store: %w", err)
	}

	var store CronStore
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("unmarshal store: %w", err)
	}

	s.store = &store
	return nil
}

// saveStore saves jobs to disk.
func (s *CronService) saveStore() error {
	if s.store == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.storePath), 0755); err != nil {
		return fmt.Errorf("create store dir: %w", err)
	}

	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal store: %w", err)
	}

	return os.WriteFile(s.storePath, data, 0600)
}

// recomputeNextRuns recomputes next run times for all enabled jobs.
func (s *CronService) recomputeNextRuns() {
	if s.store == nil {
		return
	}
	now := nowMs()
	for i := range s.store.Jobs {
		job := &s.store.Jobs[i]
		if job.Enabled {
			job.State.NextRunAtMs = computeNextRun(&job.Schedule, now)
		}
	}
}

// getNextWakeMs returns the earliest next run time across all jobs.
func (s *CronService) getNextWakeMs() int64 {
	if s.store == nil {
		return 0
	}
	var earliest int64
	for _, job := range s.store.Jobs {
		if job.Enabled && job.State.NextRunAtMs > 0 {
			if earliest == 0 || job.State.NextRunAtMs < earliest {
				earliest = job.State.NextRunAtMs
			}
		}
	}
	return earliest
}

// armTimer schedules the next timer tick.
func (s *CronService) armTimer() {
	if s.timer != nil {
		s.timer.Stop()
	}

	nextWake := s.getNextWakeMs()
	if nextWake == 0 || !s.running {
		return
	}

	delayMs := nextWake - nowMs()
	if delayMs < 0 {
		delayMs = 0
	}

	s.timer = time.AfterFunc(time.Duration(delayMs)*time.Millisecond, s.onTimer)
}

// onTimer handles timer tick - run due jobs.
func (s *CronService) onTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.store == nil {
		return
	}

	now := nowMs()
	var dueJobs []*CronJob
	for i := range s.store.Jobs {
		job := &s.store.Jobs[i]
		if job.Enabled && job.State.NextRunAtMs > 0 && now >= job.State.NextRunAtMs {
			dueJobs = append(dueJobs, job)
		}
	}

	for _, job := range dueJobs {
		s.executeJob(job)
	}

	s.saveStore()
	s.armTimer()
}

// executeJob executes a single job.
func (s *CronService) executeJob(job *CronJob) {
	startMs := nowMs()
	slog.Info("Cron: executing job", "name", job.Name, "id", job.ID)

	var lastErr string
	var lastStatus JobStatus = JobStatusOK

	if s.onJob != nil {
		_, err := s.onJob(job)
		if err != nil {
			lastStatus = JobStatusError
			lastErr = err.Error()
			slog.Error("Cron: job failed", "name", job.Name, "id", job.ID, "error", err)
		} else {
			slog.Info("Cron: job completed", "name", job.Name, "id", job.ID)
		}
	}

	job.State.LastRunAtMs = startMs
	job.State.LastStatus = lastStatus
	job.State.LastError = lastErr
	job.UpdatedAtMs = nowMs()

	// Handle one-shot jobs
	if job.Schedule.Kind == ScheduleKindAt {
		if job.DeleteAfterRun {
			// Remove job from list
			for i, j := range s.store.Jobs {
				if j.ID == job.ID {
					s.store.Jobs = append(s.store.Jobs[:i], s.store.Jobs[i+1:]...)
					break
				}
			}
		} else {
			job.Enabled = false
			job.State.NextRunAtMs = 0
		}
	} else {
		// Compute next run
		job.State.NextRunAtMs = computeNextRun(&job.Schedule, nowMs())
	}
}

// ListJobs returns all jobs, optionally including disabled ones.
func (s *CronService) ListJobs(includeDisabled bool) []CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.store == nil {
		return nil
	}

	var jobs []CronJob
	for _, job := range s.store.Jobs {
		if includeDisabled || job.Enabled {
			jobs = append(jobs, job)
		}
	}

	// Sort by next run time
	return jobs
}

// AddJob adds a new job.
func (s *CronService) AddJob(name string, schedule CronSchedule, message string, deliver bool, channel, to string, deleteAfterRun bool) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil {
		s.store = &CronStore{Version: 1}
	}

	// Validate schedule
	if err := validateSchedule(&schedule); err != nil {
		return nil, err
	}

	now := nowMs()
	job := &CronJob{
		ID:             generateID(),
		Name:           name,
		Enabled:        true,
		Schedule:       schedule,
		Payload:        CronPayload{Kind: PayloadKindAgentTurn, Message: message, Deliver: deliver, Channel: channel, To: to},
		State:          CronJobState{NextRunAtMs: computeNextRun(&schedule, now)},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	s.store.Jobs = append(s.store.Jobs, *job)
	s.saveStore()
	s.armTimer()

	slog.Info("Cron: added job", "name", name, "id", job.ID)
	return job, nil
}

// RemoveJob removes a job by ID.
func (s *CronService) RemoveJob(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil {
		return false
	}

	for i, job := range s.store.Jobs {
		if job.ID == jobID {
			s.store.Jobs = append(s.store.Jobs[:i], s.store.Jobs[i+1:]...)
			s.saveStore()
			s.armTimer()
			slog.Info("Cron: removed job", "id", jobID)
			return true
		}
	}
	return false
}

// EnableJob enables or disables a job.
func (s *CronService) EnableJob(jobID string, enabled bool) *CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil {
		return nil
	}

	for i := range s.store.Jobs {
		if s.store.Jobs[i].ID == jobID {
			s.store.Jobs[i].Enabled = enabled
			s.store.Jobs[i].UpdatedAtMs = nowMs()
			if enabled {
				s.store.Jobs[i].State.NextRunAtMs = computeNextRun(&s.store.Jobs[i].Schedule, nowMs())
			} else {
				s.store.Jobs[i].State.NextRunAtMs = 0
			}
			s.saveStore()
			s.armTimer()
			job := s.store.Jobs[i]
			return &job
		}
	}
	return nil
}

// RunJob manually runs a job.
func (s *CronService) RunJob(jobID string, force bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store == nil {
		return false
	}

	for i := range s.store.Jobs {
		if s.store.Jobs[i].ID == jobID {
			if !force && !s.store.Jobs[i].Enabled {
				return false
			}
			s.executeJob(&s.store.Jobs[i])
			s.saveStore()
			s.armTimer()
			return true
		}
	}
	return false
}

// Status returns service status.
func (s *CronService) Status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobCount := 0
	if s.store != nil {
		jobCount = len(s.store.Jobs)
	}

	return map[string]any{
		"enabled":        s.running,
		"jobs":           jobCount,
		"nextWakeAtMs":   s.getNextWakeMs(),
	}
}

// computeNextRun computes the next run time in ms.
func computeNextRun(schedule *CronSchedule, nowMs int64) int64 {
	switch schedule.Kind {
	case ScheduleKindAt:
		if schedule.AtMs > nowMs {
			return schedule.AtMs
		}
		return 0

	case ScheduleKindEvery:
		if schedule.EveryMs <= 0 {
			return 0
		}
		return nowMs + schedule.EveryMs

	case ScheduleKindCron:
		if schedule.Expr == "" {
			return 0
		}
		return computeNextCronRun(schedule.Expr, schedule.TZ, nowMs)
	}
	return 0
}

// computeNextCronRun computes the next run time for a cron expression.
func computeNextCronRun(expr, tz string, nowMs int64) int64 {
	// Parse cron expression (5 fields: minute hour day month weekday)
	fields := parseCronExpr(expr)
	if fields == nil {
		return 0
	}

	// Get current time in the specified timezone
	loc := time.Local
	if tz != "" {
		var err error
		loc, err = time.LoadLocation(tz)
		if err != nil {
			slog.Warn("invalid timezone", "tz", tz, "error", err)
			loc = time.Local
		}
	}

	now := time.UnixMilli(nowMs).In(loc)

	// Find next occurrence (check next 366 days)
	for i := 0; i < 366; i++ {
		// Start from next minute
		next := now.Add(time.Duration(i) * 24 * time.Hour)
		next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)

		for hour := 0; hour < 24; hour++ {
			for minute := 0; minute < 60; minute++ {
				candidate := next.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
				if candidate.UnixMilli() <= nowMs {
					continue
				}

				if matchesCron(fields, candidate) {
					return candidate.UnixMilli()
				}
			}
		}
	}

	return 0
}

// cronFields holds parsed cron expression fields.
type cronFields struct {
	minute  map[int]bool
	hour    map[int]bool
	day     map[int]bool
	month   map[int]bool
	weekday map[int]bool
}

// parseCronExpr parses a 5-field cron expression.
func parseCronExpr(expr string) *cronFields {
	parts := splitWhitespace(expr)
	if len(parts) != 5 {
		return nil
	}

	fields := &cronFields{
		minute:  parseCronField(parts[0], 0, 59),
		hour:    parseCronField(parts[1], 0, 23),
		day:     parseCronField(parts[2], 1, 31),
		month:   parseCronField(parts[3], 1, 12),
		weekday: parseCronField(parts[4], 0, 6),
	}

	return fields
}

// parseCronField parses a single cron field.
func parseCronField(field string, min, max int) map[int]bool {
	result := make(map[int]bool)

	if field == "*" {
		for i := min; i <= max; i++ {
			result[i] = true
		}
		return result
	}

	// Handle step (e.g., "*/5")
	if len(field) > 2 && field[:2] == "*/" {
		step := parseInt(field[2:])
		if step > 0 {
			for i := min; i <= max; i += step {
				result[i] = true
			}
		}
		return result
	}

	// Handle range (e.g., "1-5")
	for _, part := range splitComma(field) {
		if idx := indexByte(part, '-'); idx >= 0 {
			start := parseInt(part[:idx])
			end := parseInt(part[idx+1:])
			for i := start; i <= end; i++ {
				if i >= min && i <= max {
					result[i] = true
				}
			}
		} else {
			val := parseInt(part)
			if val >= min && val <= max {
				result[val] = true
			}
		}
	}

	return result
}

// matchesCron checks if a time matches the cron fields.
func matchesCron(fields *cronFields, t time.Time) bool {
	return fields.minute[t.Minute()] &&
		fields.hour[t.Hour()] &&
		fields.day[t.Day()] &&
		fields.month[int(t.Month())] &&
		fields.weekday[int(t.Weekday())]
}

// validateSchedule validates the schedule fields.
func validateSchedule(schedule *CronSchedule) error {
	if schedule.TZ != "" && schedule.Kind != ScheduleKindCron {
		return fmt.Errorf("tz can only be used with cron schedules")
	}

	if schedule.Kind == ScheduleKindCron && schedule.TZ != "" {
		if _, err := time.LoadLocation(schedule.TZ); err != nil {
			return fmt.Errorf("unknown timezone '%s'", schedule.TZ)
		}
	}

	return nil
}

func generateID() string {
	return fmt.Sprintf("%08x", nowMs()&0xffffffff)[:8]
}

func parseInt(s string) int {
	var n int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func splitWhitespace(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' || s[i] == '\t' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	return result
}

func splitComma(s string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	return result
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
