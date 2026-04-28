package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

var ErrInvalidWindowKey = errors.New("invalid scheduler window_key")

// TransactionRunner is the transaction surface required by scheduler jobs.
type TransactionRunner interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error
}

type BillingJobRepository interface {
	ListOverdueScanCandidates(ctx context.Context, today time.Time) ([]BillCandidate, error)
	MarkBillOverdue(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error
	ListOverdueReminderCandidates(ctx context.Context) ([]OverdueReminderCandidate, error)
	IncrementOverdueNoticeCount(ctx context.Context, tx *sql.Tx, billID string, expectedVersion int) error
	ListMonthlySnapshotProperties(ctx context.Context) ([]MonthlySnapshotProperty, error)
	MonthlySnapshotExists(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) (bool, error)
	CreateMonthlySnapshot(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) error
}

type LeaseJobRepository interface {
	ListLeaseExpiryCandidates(ctx context.Context, today time.Time) ([]LeaseCandidate, error)
	MarkLeaseExpired(ctx context.Context, tx *sql.Tx, leaseID string, expectedVersion int) error
	ListLeaseExpiringSoonCandidates(ctx context.Context, targetDate time.Time) ([]LeaseExpiringSoonCandidate, error)
	ListLeaseExpiringSoonRecipients(ctx context.Context, propertyID string) ([]NotificationRecipient, error)
}

type ForceTerminationJobRepository interface {
	ListInProgressForceTerminations(ctx context.Context) ([]ForceTerminationCandidate, error)
	ListPendingForceTerminationBillIDs(ctx context.Context, tx *sql.Tx, forceTerminationID string) ([]string, error)
	WriteOffBills(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) (int, error)
	MarkForceTerminationBillsDone(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error
	CompleteForceTermination(ctx context.Context, tx *sql.Tx, forceTerminationID string) error
}

type NotificationSender interface {
	SendOverdueBillReminder(ctx context.Context, recipient NotificationRecipient, bill OverdueReminderCandidate) error
	SendLeaseExpiringSoon(ctx context.Context, recipient NotificationRecipient, lease LeaseExpiringSoonCandidate) error
}

type BillCandidate struct {
	ID      string
	Version int
}

type OverdueReminderCandidate struct {
	ID                 string
	TenantID           string
	TenantEmail        *string
	Amount             *int
	DueDate            time.Time
	OverdueNoticeCount int
	Version            int
}

type MonthlySnapshotProperty struct {
	PropertyID string
}

type LeaseCandidate struct {
	ID      string
	Version int
}

type LeaseExpiringSoonCandidate struct {
	ID         string
	PropertyID string
	TenantID   string
	RoomID     string
	EndDate    time.Time
}

type ForceTerminationCandidate struct {
	ID     string
	Reason string
}

type NotificationRecipient struct {
	Email string
	Name  string
}

func NewRunners(billing BillingJobRepository, leases LeaseJobRepository, forceTerminations ForceTerminationJobRepository, notifier NotificationSender, txRunner TransactionRunner) map[JobKey]Runner {
	return map[JobKey]Runner{
		JobOverdueBillsScan:             NewOverdueBillsScanRunner(billing, txRunner),
		JobOverdueBillReminders:         NewOverdueBillReminderRunner(billing, notifier, txRunner),
		JobLeaseExpiryScan:              NewLeaseExpiryRunner(leases, txRunner),
		JobLeaseExpiringSoonReminder:    NewLeaseExpiringSoonReminderRunner(leases, notifier),
		JobMonthlySnapshot:              NewMonthlySnapshotRunner(billing, txRunner),
		JobForceTerminationCompensation: NewForceTerminationCompensationRunner(forceTerminations, txRunner),
	}
}

func NewOverdueBillsScanRunner(repo BillingJobRepository, txRunner TransactionRunner) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		today, err := parseDailyWindowKey(windowKey)
		if err != nil {
			return nil, err
		}
		candidates, err := repo.ListOverdueScanCandidates(ctx, today)
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, candidate := range candidates {
			err := txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
				return repo.MarkBillOverdue(ctx, tx, candidate.ID, candidate.Version)
			})
			switch {
			case err == nil:
				summary.ProcessedCount++
			case errors.Is(err, ErrConcurrentUpdate):
				summary.SkippedCount++
			default:
				summary.FailedCount++
			}
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

func NewOverdueBillReminderRunner(repo BillingJobRepository, notifier NotificationSender, txRunner TransactionRunner) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		if _, err := parseDailyWindowKey(windowKey); err != nil {
			return nil, err
		}
		candidates, err := repo.ListOverdueReminderCandidates(ctx)
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, candidate := range candidates {
			email := strings.TrimSpace(stringValue(candidate.TenantEmail))
			if email == "" {
				summary.SkippedCount++
				continue
			}
			if notifier == nil {
				summary.FailedCount++
				continue
			}

			err := txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
				if err := repo.IncrementOverdueNoticeCount(ctx, tx, candidate.ID, candidate.Version); err != nil {
					return err
				}
				return notifier.SendOverdueBillReminder(ctx, NotificationRecipient{Email: email}, candidate)
			})
			switch {
			case err == nil:
				summary.ProcessedCount++
			case errors.Is(err, ErrConcurrentUpdate):
				summary.SkippedCount++
			default:
				summary.FailedCount++
			}
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

func NewLeaseExpiryRunner(repo LeaseJobRepository, txRunner TransactionRunner) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		today, err := parseDailyWindowKey(windowKey)
		if err != nil {
			return nil, err
		}
		candidates, err := repo.ListLeaseExpiryCandidates(ctx, today)
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, candidate := range candidates {
			err := txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
				return repo.MarkLeaseExpired(ctx, tx, candidate.ID, candidate.Version)
			})
			switch {
			case err == nil:
				summary.ProcessedCount++
			case errors.Is(err, ErrConcurrentUpdate):
				summary.SkippedCount++
			default:
				summary.FailedCount++
			}
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

func NewLeaseExpiringSoonReminderRunner(repo LeaseJobRepository, notifier NotificationSender) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		today, err := parseDailyWindowKey(windowKey)
		if err != nil {
			return nil, err
		}
		candidates, err := repo.ListLeaseExpiringSoonCandidates(ctx, today.AddDate(0, 0, 30))
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, candidate := range candidates {
			recipients, err := repo.ListLeaseExpiringSoonRecipients(ctx, candidate.PropertyID)
			if err != nil {
				summary.FailedCount++
				continue
			}
			recipients = filterRecipients(recipients)
			if len(recipients) == 0 {
				summary.SkippedCount++
				continue
			}
			if notifier == nil {
				summary.FailedCount++
				continue
			}

			failed := false
			for _, recipient := range recipients {
				if err := notifier.SendLeaseExpiringSoon(ctx, recipient, candidate); err != nil {
					failed = true
				}
			}
			if failed {
				summary.FailedCount++
				continue
			}
			summary.ProcessedCount++
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

func NewMonthlySnapshotRunner(repo BillingJobRepository, txRunner TransactionRunner) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		year, month, err := parseMonthlyWindowKey(windowKey)
		if err != nil {
			return nil, err
		}
		properties, err := repo.ListMonthlySnapshotProperties(ctx)
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, property := range properties {
			err := txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
				exists, err := repo.MonthlySnapshotExists(ctx, tx, property.PropertyID, year, month)
				if err != nil {
					return err
				}
				if exists {
					return ErrAlreadyProcessed
				}
				return repo.CreateMonthlySnapshot(ctx, tx, property.PropertyID, year, month)
			})
			switch {
			case err == nil:
				summary.ProcessedCount++
			case errors.Is(err, ErrAlreadyProcessed):
				summary.SkippedCount++
			default:
				summary.FailedCount++
			}
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

func NewForceTerminationCompensationRunner(repo ForceTerminationJobRepository, txRunner TransactionRunner) Runner {
	return func(ctx context.Context, windowKey string) (*ExecutionSummary, error) {
		if _, err := parseDailyWindowKey(windowKey); err != nil {
			return nil, err
		}
		candidates, err := repo.ListInProgressForceTerminations(ctx)
		if err != nil {
			return nil, err
		}

		summary := &ExecutionSummary{}
		for _, candidate := range candidates {
			err := txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
				billIDs, err := repo.ListPendingForceTerminationBillIDs(ctx, tx, candidate.ID)
				if err != nil {
					return err
				}
				updated, err := repo.WriteOffBills(ctx, tx, billIDs, candidate.Reason)
				if err != nil {
					return err
				}
				if updated != len(billIDs) {
					return ErrConcurrentUpdate
				}
				if err := repo.MarkForceTerminationBillsDone(ctx, tx, candidate.ID, billIDs); err != nil {
					return err
				}
				return repo.CompleteForceTermination(ctx, tx, candidate.ID)
			})
			if err != nil {
				summary.FailedCount++
				continue
			}
			summary.ProcessedCount++
		}
		summary.Note = "job completed"
		return summary, nil
	}
}

var (
	ErrConcurrentUpdate = errors.New("concurrent update conflict")
	ErrAlreadyProcessed = errors.New("job item already processed")
)

func parseDailyWindowKey(windowKey string) (time.Time, error) {
	value := strings.TrimSpace(windowKey)
	if value == "" {
		return time.Time{}, ErrInvalidWindowKey
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: expected YYYY-MM-DD", ErrInvalidWindowKey)
	}
	return parsed, nil
}

func parseMonthlyWindowKey(windowKey string) (int, int, error) {
	value := strings.TrimSpace(windowKey)
	if value == "" {
		return 0, 0, ErrInvalidWindowKey
	}
	parsed, err := time.Parse("2006-01", value)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: expected YYYY-MM", ErrInvalidWindowKey)
	}
	return parsed.Year(), int(parsed.Month()), nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func filterRecipients(recipients []NotificationRecipient) []NotificationRecipient {
	filtered := make([]NotificationRecipient, 0, len(recipients))
	seen := map[string]struct{}{}
	for _, recipient := range recipients {
		email := strings.TrimSpace(recipient.Email)
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		recipient.Email = email
		filtered = append(filtered, recipient)
	}
	return filtered
}
