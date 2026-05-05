package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	appjobs "stds_backend/internal/application/jobs"
	appnotification "stds_backend/internal/application/notification"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbjobruns "stds_backend/internal/platform/database/jobruns"
	dbleases "stds_backend/internal/platform/database/leases"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	dbusers "stds_backend/internal/platform/database/users"
)

type jobRunStoreAdapter struct {
	repo *dbjobruns.SQLRepository
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

func newJobRunners(db *sql.DB, txRunner *dbtxrunner.Runner, notificationService *appnotification.Service) map[appjobs.JobKey]appjobs.Runner {
	billingRepo := dbbilling.NewRepository(db)
	leaseRepo := dbleases.NewRepository(db)
	userRepo := dbusers.NewRepository(db)

	return buildJobRunners(
		billingJobRepositoryAdapter{repo: billingRepo},
		leaseJobRepositoryAdapter{leases: leaseRepo, users: userRepo},
		forceTerminationJobRepositoryAdapter{repo: leaseRepo},
		jobNotificationAdapter{service: notificationService},
		txRunner,
	)
}

func buildJobRunners(billing appjobs.BillingJobRepository, leases appjobs.LeaseJobRepository, forceTerminations appjobs.ForceTerminationJobRepository, notifier appjobs.NotificationSender, txRunner appjobs.TransactionRunner) map[appjobs.JobKey]appjobs.Runner {
	return appjobs.NewRunners(
		billing,
		leases,
		forceTerminations,
		notifier,
		txRunner,
	)
}

type billingJobRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a billingJobRepositoryAdapter) ListOverdueScanCandidates(ctx context.Context, today time.Time) ([]appjobs.BillCandidate, error) {
	candidates, err := a.repo.ListOverdueScanCandidates(ctx, today)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.BillCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, appjobs.BillCandidate{ID: candidate.ID, Version: candidate.Version})
	}
	return result, nil
}

func (a billingJobRepositoryAdapter) MarkBillOverdue(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error {
	err := a.repo.MarkBillOverdue(ctx, tx, billID, expectedVersion)
	if errors.Is(err, dbbilling.ErrConcurrentUpdate) {
		return appjobs.ErrConcurrentUpdate
	}
	return err
}

func (a billingJobRepositoryAdapter) ListOverdueReminderCandidates(ctx context.Context) ([]appjobs.OverdueReminderCandidate, error) {
	candidates, err := a.repo.ListOverdueReminderCandidates(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.OverdueReminderCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, appjobs.OverdueReminderCandidate{
			ID:                 candidate.ID,
			TenantID:           candidate.TenantID,
			TenantEmail:        candidate.TenantEmail,
			Amount:             candidate.Amount,
			DueDate:            candidate.DueDate,
			OverdueNoticeCount: candidate.OverdueNoticeCount,
			Version:            candidate.Version,
		})
	}
	return result, nil
}

func (a billingJobRepositoryAdapter) IncrementOverdueNoticeCount(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error {
	err := a.repo.IncrementOverdueNoticeCount(ctx, tx, billID, expectedVersion)
	if errors.Is(err, dbbilling.ErrConcurrentUpdate) {
		return appjobs.ErrConcurrentUpdate
	}
	return err
}

func (a billingJobRepositoryAdapter) ListMonthlySnapshotProperties(ctx context.Context) ([]appjobs.MonthlySnapshotProperty, error) {
	properties, err := a.repo.ListMonthlySnapshotProperties(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.MonthlySnapshotProperty, 0, len(properties))
	for _, property := range properties {
		result = append(result, appjobs.MonthlySnapshotProperty{PropertyID: property.PropertyID})
	}
	return result, nil
}

func (a billingJobRepositoryAdapter) MonthlySnapshotExists(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) (bool, error) {
	return a.repo.MonthlySnapshotExists(ctx, tx, propertyID, year, month)
}

func (a billingJobRepositoryAdapter) CreateMonthlySnapshot(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) error {
	return a.repo.CreateMonthlySnapshot(ctx, tx, propertyID, year, month)
}

type leaseJobRepositoryAdapter struct {
	leases *dbleases.SQLRepository
	users  *dbusers.SQLRepository
}

func (a leaseJobRepositoryAdapter) ListLeaseExpiryCandidates(ctx context.Context, today time.Time) ([]appjobs.LeaseCandidate, error) {
	candidates, err := a.leases.ListLeaseExpiryCandidates(ctx, today)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.LeaseCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, appjobs.LeaseCandidate{ID: candidate.ID, Version: candidate.Version})
	}
	return result, nil
}

func (a leaseJobRepositoryAdapter) MarkLeaseExpired(ctx context.Context, tx *sql.Tx, leaseID string, expectedVersion int) error {
	err := a.leases.MarkLeaseExpired(ctx, tx, leaseID, expectedVersion)
	if errors.Is(err, dbleases.ErrLeaseNotFound) {
		return appjobs.ErrConcurrentUpdate
	}
	return err
}

func (a leaseJobRepositoryAdapter) ListLeaseExpiringSoonCandidates(ctx context.Context, targetDate time.Time) ([]appjobs.LeaseExpiringSoonCandidate, error) {
	candidates, err := a.leases.ListLeaseExpiringSoonCandidates(ctx, targetDate)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.LeaseExpiringSoonCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, appjobs.LeaseExpiringSoonCandidate{
			ID:         candidate.ID,
			PropertyID: candidate.PropertyID,
			TenantID:   candidate.TenantID,
			RoomID:     candidate.RoomID,
			EndDate:    candidate.EndDate,
		})
	}
	return result, nil
}

func (a leaseJobRepositoryAdapter) ListLeaseExpiringSoonRecipients(ctx context.Context, propertyID string) ([]appjobs.NotificationRecipient, error) {
	recipients, err := a.users.ListLeaseExpiringSoonRecipients(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.NotificationRecipient, 0, len(recipients))
	for _, recipient := range recipients {
		result = append(result, appjobs.NotificationRecipient{Email: recipient.Email, Name: recipient.Name})
	}
	return result, nil
}

type forceTerminationJobRepositoryAdapter struct {
	repo *dbleases.SQLRepository
}

func (a forceTerminationJobRepositoryAdapter) ListInProgressForceTerminations(ctx context.Context) ([]appjobs.ForceTerminationCandidate, error) {
	candidates, err := a.repo.ListInProgressForceTerminations(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]appjobs.ForceTerminationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, appjobs.ForceTerminationCandidate{ID: candidate.ID, Reason: candidate.Reason})
	}
	return result, nil
}

func (a forceTerminationJobRepositoryAdapter) ListPendingForceTerminationBillIDs(ctx context.Context, tx *sql.Tx, forceTerminationID string) ([]string, error) {
	return a.repo.ListPendingForceTerminationBillIDs(ctx, tx, forceTerminationID)
}

func (a forceTerminationJobRepositoryAdapter) WriteOffBills(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) (int, error) {
	return a.repo.WriteOffBillsWithCount(ctx, tx, billIDs, reason)
}

func (a forceTerminationJobRepositoryAdapter) MarkForceTerminationBillsDone(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error {
	return a.repo.MarkForceTerminationBillsDone(ctx, tx, forceTerminationID, billIDs)
}

func (a forceTerminationJobRepositoryAdapter) CompleteForceTermination(ctx context.Context, tx *sql.Tx, forceTerminationID string) error {
	return a.repo.CompleteForceTermination(ctx, tx, forceTerminationID)
}

type jobNotificationAdapter struct {
	service *appnotification.Service
}

func (a jobNotificationAdapter) SendOverdueBillReminder(ctx context.Context, recipient appjobs.NotificationRecipient, bill appjobs.OverdueReminderCandidate) error {
	return a.service.SendOverdueBillReminderEmail(ctx, appnotification.OverdueBillReminderEmailInput{
		Email:   recipient.Email,
		Amount:  bill.Amount,
		DueDate: bill.DueDate,
	})
}

func (a jobNotificationAdapter) SendLeaseExpiringSoon(ctx context.Context, recipient appjobs.NotificationRecipient, lease appjobs.LeaseExpiringSoonCandidate) error {
	return a.service.SendLeaseExpiringSoonEmail(ctx, appnotification.LeaseExpiringSoonEmailInput{
		Email:   recipient.Email,
		Name:    recipient.Name,
		LeaseID: lease.ID,
		EndDate: lease.EndDate,
	})
}
