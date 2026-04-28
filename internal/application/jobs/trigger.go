package jobs

import (
	"context"
	"fmt"
	"time"
)

// JobKey identifies a scheduler-triggered job.
type JobKey string

const (
	JobOverdueBillsScan             JobKey = "overdue_bills_scan"
	JobOverdueBillReminders         JobKey = "overdue_bill_reminders"
	JobLeaseExpiryScan              JobKey = "lease_expiry_scan"
	JobLeaseExpiringSoonReminder    JobKey = "lease_expiring_soon_reminder"
	JobMonthlySnapshot              JobKey = "monthly_snapshot"
	JobForceTerminationCompensation JobKey = "force_termination_compensation"
)

// ExecutionSummary is a normalized batch execution summary for job responses
// and logs.
type ExecutionSummary struct {
	ProcessedCount int
	SkippedCount   int
	FailedCount    int
	Note           string
}

// Runner executes a concrete job for one scheduler window.
type Runner func(context.Context, string) (*ExecutionSummary, error)

// TriggerResult is the transport-neutral result for a scheduler trigger call.
type TriggerResult struct {
	JobKey      string
	Status      string
	WindowKey   string
	Message     string
	RequestedAt time.Time
	RetryCount  int
	DurationMs  int64
	Summary     *ExecutionSummary
}

// TriggerService dispatches scheduler-triggered jobs with row-locked
// deduplication, bounded execution timeout, and retry-aware run tracking.
type TriggerService struct {
	jobRuns    JobRunStore
	runners    map[JobKey]Runner
	now        func() time.Time
	timeout    time.Duration
	maxRetries int
}

// NewTriggerService returns a TriggerService with the provided dependencies.
func NewTriggerService(jobRuns JobRunStore, runners map[JobKey]Runner, timeout time.Duration, maxRetries int) *TriggerService {
	if runners == nil {
		runners = defaultRunners()
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	return &TriggerService{
		jobRuns:    jobRuns,
		runners:    runners,
		now:        time.Now,
		timeout:    timeout,
		maxRetries: maxRetries,
	}
}

// Execute runs the requested job and returns a normalized trigger response.
func (s *TriggerService) Execute(ctx context.Context, jobKey JobKey, windowKey string, requestID string) (*TriggerResult, error) {
	runner, ok := s.runners[jobKey]
	if !ok {
		return nil, fmt.Errorf("job runner %q is not configured", jobKey)
	}
	if s.jobRuns == nil {
		return nil, fmt.Errorf("job run repository is not configured")
	}

	started, err := s.jobRuns.Start(ctx, string(jobKey), windowKey, requestID, s.maxRetries)
	if err != nil {
		return nil, err
	}

	result := &TriggerResult{
		JobKey:      string(jobKey),
		WindowKey:   windowKey,
		RequestedAt: s.now().UTC(),
		RetryCount:  started.RetryCount,
	}

	if !started.Acquired {
		message := fmt.Sprintf("job window already processed with status %s", started.Status)
		result.Status = "skipped"
		result.Message = message
		result.Summary = &ExecutionSummary{Note: message}
		return result, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	startedAt := time.Now()
	summary, err := runner(runCtx, windowKey)
	result.DurationMs = time.Since(startedAt).Milliseconds()
	if summary == nil {
		summary = &ExecutionSummary{}
	}
	result.Summary = summary

	if err != nil {
		failMessage := fmt.Sprintf("job failed: %v", err)
		_ = s.jobRuns.Fail(ctx, started.RunID, failMessage)
		result.Status = "failed"
		result.Message = failMessage
		return nil, err
	}

	completeMessage := summary.Note
	if completeMessage == "" {
		completeMessage = "job completed"
	}
	if err := s.jobRuns.Complete(ctx, started.RunID, completeMessage); err != nil {
		return nil, err
	}

	result.Status = "accepted"
	result.Message = completeMessage
	return result, nil
}

func defaultRunners() map[JobKey]Runner {
	return map[JobKey]Runner{
		JobOverdueBillsScan: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
		JobOverdueBillReminders: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
		JobLeaseExpiryScan: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
		JobLeaseExpiringSoonReminder: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
		JobMonthlySnapshot: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
		JobForceTerminationCompensation: func(context.Context, string) (*ExecutionSummary, error) {
			return &ExecutionSummary{Note: "job completed"}, nil
		},
	}
}
