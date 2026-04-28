package jobs

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

func TestOverdueBillsScanSkipsConcurrentUpdate(t *testing.T) {
	repo := &batchBillingRepoStub{
		overdueScanCandidates: []BillCandidate{{ID: "bill-1", Version: 3}},
		markBillErr:           ErrConcurrentUpdate,
	}
	runner := NewOverdueBillsScanRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.SkippedCount != 1 || summary.ProcessedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.markBillID != "bill-1" || repo.markBillVersion != 3 {
		t.Fatalf("unexpected mark call: bill=%s version=%d", repo.markBillID, repo.markBillVersion)
	}
}

func TestOverdueBillReminderSkipsMissingTenantEmail(t *testing.T) {
	repo := &batchBillingRepoStub{
		overdueReminderCandidates: []OverdueReminderCandidate{
			{ID: "bill-1", Version: 1},
		},
	}
	notifier := &jobNotifierStub{}
	runner := NewOverdueBillReminderRunner(repo, notifier, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.SkippedCount != 1 || summary.ProcessedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if notifier.overdueCalls != 0 {
		t.Fatalf("expected no notification calls, got %d", notifier.overdueCalls)
	}
	if repo.incrementCalls != 0 {
		t.Fatalf("expected no increment calls, got %d", repo.incrementCalls)
	}
}

func TestOverdueBillReminderIncrementsAfterSuccessfulSend(t *testing.T) {
	email := "tenant@example.com"
	repo := &batchBillingRepoStub{
		overdueReminderCandidates: []OverdueReminderCandidate{
			{ID: "bill-1", TenantEmail: &email, Version: 7},
		},
	}
	notifier := &jobNotifierStub{}
	runner := NewOverdueBillReminderRunner(repo, notifier, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.SkippedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if notifier.overdueCalls != 1 {
		t.Fatalf("expected one notification call, got %d", notifier.overdueCalls)
	}
	if repo.incrementBillID != "bill-1" || repo.incrementVersion != 7 {
		t.Fatalf("unexpected increment call: bill=%s version=%d", repo.incrementBillID, repo.incrementVersion)
	}
}

func TestOverdueBillReminderSkipsConcurrentUpdateWithoutSending(t *testing.T) {
	email := "tenant@example.com"
	repo := &batchBillingRepoStub{
		overdueReminderCandidates: []OverdueReminderCandidate{
			{ID: "bill-1", TenantEmail: &email, Version: 7},
		},
		incrementErr: ErrConcurrentUpdate,
	}
	notifier := &jobNotifierStub{}
	runner := NewOverdueBillReminderRunner(repo, notifier, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.SkippedCount != 1 || summary.ProcessedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if notifier.overdueCalls != 0 {
		t.Fatalf("expected no notification calls, got %d", notifier.overdueCalls)
	}
}

func TestLeaseExpiringSoonReminderSendsToFilteredRecipients(t *testing.T) {
	repo := &batchLeaseRepoStub{
		expiringSoonCandidates: []LeaseExpiringSoonCandidate{
			{ID: "lease-1", PropertyID: "property-1", EndDate: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC)},
		},
		recipients: []NotificationRecipient{
			{Email: "organizer@example.com", Name: "Organizer"},
			{Email: "staff@example.com", Name: "Staff"},
			{Email: "staff@example.com", Name: "Staff"},
			{Email: " "},
		},
	}
	notifier := &jobNotifierStub{}
	runner := NewLeaseExpiringSoonReminderRunner(repo, notifier)

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.SkippedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if notifier.leaseCalls != 2 {
		t.Fatalf("expected two unique notification calls, got %d", notifier.leaseCalls)
	}
	if repo.recipientPropertyID != "property-1" {
		t.Fatalf("expected property-1 recipient lookup, got %q", repo.recipientPropertyID)
	}
}

func TestLeaseExpiryMarksEligibleLeaseExpired(t *testing.T) {
	repo := &batchLeaseRepoStub{
		expiryCandidates: []LeaseCandidate{{ID: "lease-1", Version: 4}},
	}
	runner := NewLeaseExpiryRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.SkippedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.markLeaseID != "lease-1" || repo.markLeaseVersion != 4 {
		t.Fatalf("unexpected mark call: lease=%s version=%d", repo.markLeaseID, repo.markLeaseVersion)
	}
}

func TestMonthlySnapshotSkipsExistingSnapshot(t *testing.T) {
	repo := &batchBillingRepoStub{
		snapshotProperties: []MonthlySnapshotProperty{{PropertyID: "property-1"}},
		snapshotExists:     true,
	}
	runner := NewMonthlySnapshotRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.SkippedCount != 1 || summary.ProcessedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.createSnapshotCalls != 0 {
		t.Fatalf("expected no snapshot creation, got %d", repo.createSnapshotCalls)
	}
}

func TestMonthlySnapshotCreatesMissingSnapshot(t *testing.T) {
	repo := &batchBillingRepoStub{
		snapshotProperties: []MonthlySnapshotProperty{{PropertyID: "property-1"}},
	}
	runner := NewMonthlySnapshotRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.SkippedCount != 0 || summary.FailedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.createSnapshotCalls != 1 {
		t.Fatalf("expected one snapshot creation, got %d", repo.createSnapshotCalls)
	}
}

func TestDailyRunnerRejectsInvalidWindowKey(t *testing.T) {
	runner := NewLeaseExpiryRunner(&batchLeaseRepoStub{}, txRunnerStub{})

	_, err := runner(context.Background(), "2026-04")
	if !errors.Is(err, ErrInvalidWindowKey) {
		t.Fatalf("expected ErrInvalidWindowKey, got %v", err)
	}
}

func TestForceTerminationCompensationFailsWhenWriteOffDoesNotCoverAllBills(t *testing.T) {
	repo := &batchForceTerminationRepoStub{
		candidates: []ForceTerminationCandidate{{ID: "force-termination-1", Reason: "legacy cleanup"}},
		billIDs:    []string{"bill-1", "bill-2"},
		writeOffs:  1,
	}
	runner := NewForceTerminationCompensationRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.FailedCount != 1 || summary.ProcessedCount != 0 || summary.SkippedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.markDoneCalls != 0 {
		t.Fatalf("expected no done markers, got %d", repo.markDoneCalls)
	}
	if repo.completeCalls != 0 {
		t.Fatalf("expected no completion, got %d", repo.completeCalls)
	}
}

func TestForceTerminationCompensationCompletesWhenAllPendingBillsWrittenOff(t *testing.T) {
	repo := &batchForceTerminationRepoStub{
		candidates: []ForceTerminationCandidate{{ID: "force-termination-1", Reason: "legacy cleanup"}},
		billIDs:    []string{"bill-1", "bill-2"},
		writeOffs:  2,
	}
	runner := NewForceTerminationCompensationRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.FailedCount != 0 || summary.SkippedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.markDoneCalls != 1 {
		t.Fatalf("expected one done marker, got %d", repo.markDoneCalls)
	}
	if repo.completeCalls != 1 {
		t.Fatalf("expected one completion, got %d", repo.completeCalls)
	}
}

func TestForceTerminationCompensationCompletesWhenNoPendingBillsRemain(t *testing.T) {
	repo := &batchForceTerminationRepoStub{
		candidates: []ForceTerminationCandidate{{ID: "force-termination-1", Reason: "legacy cleanup"}},
	}
	runner := NewForceTerminationCompensationRunner(repo, txRunnerStub{})

	summary, err := runner(context.Background(), "2026-04-16")
	if err != nil {
		t.Fatalf("runner: %v", err)
	}

	if summary.ProcessedCount != 1 || summary.FailedCount != 0 || summary.SkippedCount != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if repo.completeCalls != 1 {
		t.Fatalf("expected one completion, got %d", repo.completeCalls)
	}
}

type txRunnerStub struct{}

func (txRunnerStub) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error {
	return fn(ctx, nil, nil)
}

type batchBillingRepoStub struct {
	overdueScanCandidates     []BillCandidate
	overdueReminderCandidates []OverdueReminderCandidate
	snapshotProperties        []MonthlySnapshotProperty
	snapshotExists            bool
	markBillID                string
	markBillVersion           int
	markBillErr               error
	incrementCalls            int
	incrementBillID           string
	incrementVersion          int
	incrementErr              error
	createSnapshotCalls       int
}

func (s *batchBillingRepoStub) ListOverdueScanCandidates(context.Context, time.Time) ([]BillCandidate, error) {
	return s.overdueScanCandidates, nil
}

func (s *batchBillingRepoStub) MarkBillOverdue(_ context.Context, _ *sql.Tx, billID string, expectedVersion int) error {
	s.markBillID = billID
	s.markBillVersion = expectedVersion
	return s.markBillErr
}

func (s *batchBillingRepoStub) ListOverdueReminderCandidates(context.Context) ([]OverdueReminderCandidate, error) {
	return s.overdueReminderCandidates, nil
}

func (s *batchBillingRepoStub) IncrementOverdueNoticeCount(_ context.Context, _ *sql.Tx, billID string, expectedVersion int) error {
	s.incrementCalls++
	s.incrementBillID = billID
	s.incrementVersion = expectedVersion
	return s.incrementErr
}

func (s *batchBillingRepoStub) ListMonthlySnapshotProperties(context.Context) ([]MonthlySnapshotProperty, error) {
	return s.snapshotProperties, nil
}

func (s *batchBillingRepoStub) MonthlySnapshotExists(context.Context, *sql.Tx, string, int, int) (bool, error) {
	return s.snapshotExists, nil
}

func (s *batchBillingRepoStub) CreateMonthlySnapshot(context.Context, *sql.Tx, string, int, int) error {
	s.createSnapshotCalls++
	return nil
}

type batchLeaseRepoStub struct {
	expiryCandidates       []LeaseCandidate
	expiringSoonCandidates []LeaseExpiringSoonCandidate
	recipients             []NotificationRecipient
	recipientPropertyID    string
	markLeaseID            string
	markLeaseVersion       int
	markLeaseErr           error
}

func (s *batchLeaseRepoStub) ListLeaseExpiryCandidates(context.Context, time.Time) ([]LeaseCandidate, error) {
	return s.expiryCandidates, nil
}

func (s *batchLeaseRepoStub) MarkLeaseExpired(_ context.Context, _ *sql.Tx, leaseID string, expectedVersion int) error {
	s.markLeaseID = leaseID
	s.markLeaseVersion = expectedVersion
	return s.markLeaseErr
}

func (s *batchLeaseRepoStub) ListLeaseExpiringSoonCandidates(context.Context, time.Time) ([]LeaseExpiringSoonCandidate, error) {
	return s.expiringSoonCandidates, nil
}

func (s *batchLeaseRepoStub) ListLeaseExpiringSoonRecipients(_ context.Context, propertyID string) ([]NotificationRecipient, error) {
	s.recipientPropertyID = propertyID
	return s.recipients, nil
}

type batchForceTerminationRepoStub struct {
	candidates    []ForceTerminationCandidate
	billIDs       []string
	writeOffs     int
	markDoneCalls int
	completeCalls int
}

func (s *batchForceTerminationRepoStub) ListInProgressForceTerminations(context.Context) ([]ForceTerminationCandidate, error) {
	return s.candidates, nil
}

func (s *batchForceTerminationRepoStub) ListPendingForceTerminationBillIDs(context.Context, *sql.Tx, string) ([]string, error) {
	return s.billIDs, nil
}

func (s *batchForceTerminationRepoStub) WriteOffBills(context.Context, *sql.Tx, []string, string) (int, error) {
	return s.writeOffs, nil
}

func (s *batchForceTerminationRepoStub) MarkForceTerminationBillsDone(context.Context, *sql.Tx, string, []string) error {
	s.markDoneCalls++
	return nil
}

func (s *batchForceTerminationRepoStub) CompleteForceTermination(context.Context, *sql.Tx, string) error {
	s.completeCalls++
	return nil
}

type jobNotifierStub struct {
	overdueCalls int
	leaseCalls   int
}

func (s *jobNotifierStub) SendOverdueBillReminder(context.Context, NotificationRecipient, OverdueReminderCandidate) error {
	s.overdueCalls++
	return nil
}

func (s *jobNotifierStub) SendLeaseExpiringSoon(context.Context, NotificationRecipient, LeaseExpiringSoonCandidate) error {
	s.leaseCalls++
	return nil
}
