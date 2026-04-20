package server

import (
	"context"

	appjobs "stds_backend/internal/application/jobs"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
)

type jobRunStoreAdapter struct {
	repo dbjobruns.Repository
}

func (a jobRunStoreAdapter) Start(ctx context.Context, jobKey string, windowKey string, requestID string, maxRetries int) (*appjobs.StartResult, error) {
	result, err := a.repo.Start(ctx, jobKey, windowKey, requestID, maxRetries)
	if err != nil {
		return nil, err
	}

	return &appjobs.StartResult{
		RunID:      result.RunID,
		Status:     string(result.Status),
		Acquired:   result.Acquired,
		Message:    result.Message,
		StartedAt:  result.StartedAt,
		RetryCount: result.RetryCount,
	}, nil
}

func (a jobRunStoreAdapter) Complete(ctx context.Context, runID string, message string) error {
	return a.repo.Complete(ctx, runID, message)
}

func (a jobRunStoreAdapter) Fail(ctx context.Context, runID string, message string) error {
	return a.repo.Fail(ctx, runID, message)
}

func (a jobRunStoreAdapter) Skip(ctx context.Context, runID string, message string) error {
	return a.repo.Skip(ctx, runID, message)
}
