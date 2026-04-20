package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeJobRunsRepo struct {
	result *StartResult
	err    error
}

func (f fakeJobRunsRepo) Start(_ context.Context, _, _, _ string, _ int) (*StartResult, error) {
	return f.result, f.err
}
func (fakeJobRunsRepo) Complete(context.Context, string, string) error { return nil }
func (fakeJobRunsRepo) Fail(context.Context, string, string) error     { return nil }
func (fakeJobRunsRepo) Skip(context.Context, string, string) error     { return nil }

func TestTriggerServiceReturnsSkippedForDuplicateWindow(t *testing.T) {
	called := false
	service := NewTriggerService(fakeJobRunsRepo{
		result: &StartResult{
			RunID:     "run-1",
			Status:    JobRunStatusCompleted,
			Acquired:  false,
			StartedAt: time.Unix(1, 0),
		},
	}, map[JobKey]Runner{
		JobOverdueBillsScan: func(context.Context) (*ExecutionSummary, error) {
			called = true
			return &ExecutionSummary{}, nil
		},
	}, time.Minute, 3)

	result, err := service.Execute(context.Background(), JobOverdueBillsScan, "2026-04-16", "req-1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if called {
		t.Fatal("expected duplicate window not to execute runner")
	}
	if result.Status != "skipped" {
		t.Fatalf("expected skipped, got %q", result.Status)
	}
}

func TestTriggerServiceReturnsRunnerError(t *testing.T) {
	expectedErr := errors.New("boom")
	service := NewTriggerService(fakeJobRunsRepo{
		result: &StartResult{
			RunID:     "run-1",
			Status:    JobRunStatusStarted,
			Acquired:  true,
			StartedAt: time.Unix(1, 0),
		},
	}, map[JobKey]Runner{
		JobOverdueBillsScan: func(context.Context) (*ExecutionSummary, error) {
			return nil, expectedErr
		},
	}, time.Minute, 3)

	_, err := service.Execute(context.Background(), JobOverdueBillsScan, "2026-04-16", "req-1")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}
