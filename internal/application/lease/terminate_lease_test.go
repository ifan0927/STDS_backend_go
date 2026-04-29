package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const terminateLeaseTestLeaseID = "40000000-0000-0000-0000-000000000003"

func TestTerminateLeaseServiceSettlesDepositAndPublishesEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)

	mock.ExpectBegin()
	mock.ExpectCommit()

	refundAmount := 15000
	deductionAmount := 5000
	reason := "wall repair"
	publisher := &recordingPublisher{}
	repo := terminationRepoStub()
	accountingRepo := &depositAccountingRepositoryStub{}
	service := NewTerminateLeaseService(repo, accountingRepo, dbtxrunner.New(db, publisher))

	lease, err := service.Execute(context.Background(), TerminateLeaseInput{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
		DeductionAmount:     &deductionAmount,
		DeductionReason:     &reason,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if lease.Status != "terminated" {
		t.Fatalf("status = %q, want terminated", lease.Status)
	}
	if repo.settleDepositCalls != 1 || repo.terminateCalls != 1 {
		t.Fatalf("settleDepositCalls=%d terminateCalls=%d, want 1/1", repo.settleDepositCalls, repo.terminateCalls)
	}
	if len(accountingRepo.entries) != 2 {
		t.Fatalf("accounting entries = %d, want 2", len(accountingRepo.entries))
	}
	if accountingRepo.entries[0].Category != depositAccountingCategoryRefund || accountingRepo.entries[0].Amount != -refundAmount {
		t.Fatalf("refund accounting entry = %+v", accountingRepo.entries[0])
	}
	if accountingRepo.entries[1].Category != depositAccountingCategoryDeduction || accountingRepo.entries[1].Amount != deductionAmount {
		t.Fatalf("deduction accounting entry = %+v", accountingRepo.entries[1])
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events = %d, want 3", len(publisher.events))
	}
	terminated, ok := publisher.events[2].(domainevents.LeaseTerminated)
	if !ok {
		t.Fatalf("third event = %T, want LeaseTerminated", publisher.events[2])
	}
	if terminated.Forced || terminated.IsReplacement || terminated.LeaseID != terminateLeaseTestLeaseID {
		t.Fatalf("unexpected LeaseTerminated event: %+v", terminated)
	}
}

func TestTerminateLeaseServiceRejectsUnpaidBills(t *testing.T) {
	repo := terminationRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "50000000-0000-0000-0000-000000000010",
		Type:        "rent",
		Status:      "pending_payment",
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	})
	service := NewTerminateLeaseService(repo, &depositAccountingRepositoryStub{}, txRunnerForRollback(t))

	refundAmount := 20000
	_, err := service.Execute(context.Background(), TerminateLeaseInput{
		ActorRole:    "admin",
		LeaseID:      terminateLeaseTestLeaseID,
		RefundAmount: &refundAmount,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeLeaseHasUnpaidBills {
		t.Fatalf("expected LEASE_HAS_UNPAID_BILLS, got %v", err)
	}
	if repo.settleDepositCalls != 0 || repo.terminateCalls != 0 {
		t.Fatalf("unexpected writes: settle=%d terminate=%d", repo.settleDepositCalls, repo.terminateCalls)
	}
}

func TestTerminateLeaseServiceRejectsDeductionWithoutReason(t *testing.T) {
	repo := terminationRepoStub()
	service := NewTerminateLeaseService(repo, &depositAccountingRepositoryStub{}, txRunnerForRollback(t))

	refundAmount := 15000
	deductionAmount := 5000
	_, err := service.Execute(context.Background(), TerminateLeaseInput{
		ActorRole:       "admin",
		LeaseID:         terminateLeaseTestLeaseID,
		RefundAmount:    &refundAmount,
		DeductionAmount: &deductionAmount,
	})
	if !errors.Is(err, errDepositDeductionReasonRequired) {
		t.Fatalf("expected errDepositDeductionReasonRequired, got %v", err)
	}
}

func TestTerminateLeaseServiceRollsBackWhenAccountingEntryFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := terminationRepoStub()
	accountingRepo := &depositAccountingRepositoryStub{createErr: errors.New("accounting failed")}
	service := NewTerminateLeaseService(repo, accountingRepo, dbtxrunner.New(db, nil))

	refundAmount := 20000
	_, err = service.Execute(context.Background(), TerminateLeaseInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
	})
	if err == nil {
		t.Fatal("Execute error = nil, want error")
	}
	if repo.settleDepositCalls != 1 || repo.terminateCalls != 1 {
		t.Fatalf("settleDepositCalls=%d terminateCalls=%d, want 1/1", repo.settleDepositCalls, repo.terminateCalls)
	}
	if accountingRepo.createCalls != 1 {
		t.Fatalf("CreateDepositAccountingEntry calls = %d, want 1", accountingRepo.createCalls)
	}
}

func TestForceTerminateLeaseServiceWritesOffBillsAndPublishesEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := terminationRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "50000000-0000-0000-0000-000000000010",
		Type:        "rent",
		Status:      "overdue",
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	})
	service := NewForceTerminateLeaseService(repo, dbtxrunner.New(db, publisher))

	result, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:           "organizer",
		ActorUserID:         "20000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		Reason:              "tenant unreachable",
		DepositHandling:     "write_off",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("force termination status = %q, want completed", result.Status)
	}
	if len(result.Bills) != 1 || result.Bills[0].Status != "done" {
		t.Fatalf("unexpected force termination bills: %+v", result.Bills)
	}
	if len(repo.writeOffBillIDs) != 1 || repo.writeOffBillIDs[0] != "50000000-0000-0000-0000-000000000010" {
		t.Fatalf("writeOffBillIDs = %+v", repo.writeOffBillIDs)
	}
	if repo.forceTerminateCalls != 1 {
		t.Fatalf("forceTerminateCalls = %d, want 1", repo.forceTerminateCalls)
	}
	if repo.lease.DepositStatus != "written_off" {
		t.Fatalf("deposit status = %q, want written_off", repo.lease.DepositStatus)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d, want 1", len(publisher.events))
	}
	terminated, ok := publisher.events[0].(domainevents.LeaseTerminated)
	if !ok || !terminated.Forced || terminated.IsReplacement {
		t.Fatalf("event = %+v, want forced LeaseTerminated", publisher.events[0])
	}
}

func TestForceTerminateLeaseServiceKeepsDepositHeld(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := terminationRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "50000000-0000-0000-0000-000000000010",
		Type:        "rent",
		Status:      "overdue",
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	})
	service := NewForceTerminateLeaseService(repo, dbtxrunner.New(db, publisher))

	result, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:           "admin",
		ActorUserID:         "20000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		Reason:              "tenant unreachable",
		DepositHandling:     "keep_held",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Status != "completed" {
		t.Fatalf("force termination status = %q, want completed", result.Status)
	}
	if len(repo.writeOffBillIDs) != 1 || repo.writeOffBillIDs[0] != "50000000-0000-0000-0000-000000000010" {
		t.Fatalf("writeOffBillIDs = %+v", repo.writeOffBillIDs)
	}
	if repo.forceTerminateCalls != 1 {
		t.Fatalf("forceTerminateCalls = %d, want 1", repo.forceTerminateCalls)
	}
	if repo.lease.DepositStatus != "held" {
		t.Fatalf("deposit status = %q, want held", repo.lease.DepositStatus)
	}
	if result.DepositHandling != "keep_held" {
		t.Fatalf("deposit handling = %q, want keep_held", result.DepositHandling)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d, want 1", len(publisher.events))
	}
	terminated, ok := publisher.events[0].(domainevents.LeaseTerminated)
	if !ok || !terminated.Forced || terminated.IsReplacement {
		t.Fatalf("event = %+v, want forced LeaseTerminated", publisher.events[0])
	}
}

func TestForceTerminateLeaseServiceRejectsLowPrivilegeRole(t *testing.T) {
	service := NewForceTerminateLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:       "staff",
		ActorUserID:     "20000000-0000-0000-0000-000000000001",
		LeaseID:         terminateLeaseTestLeaseID,
		Reason:          "tenant unreachable",
		DepositHandling: "write_off",
	})
	if !errors.Is(err, errForbiddenForceTermination) {
		t.Fatalf("expected errForbiddenForceTermination, got %v", err)
	}
}

func TestForceTerminateLeaseServiceRejectsMissingReason(t *testing.T) {
	service := NewForceTerminateLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:       "admin",
		ActorUserID:     "20000000-0000-0000-0000-000000000001",
		LeaseID:         terminateLeaseTestLeaseID,
		DepositHandling: "write_off",
	})
	if !errors.Is(err, errForceTerminationReasonRequired) {
		t.Fatalf("expected errForceTerminationReasonRequired, got %v", err)
	}
}

func TestForceTerminateLeaseServiceRejectsMissingDepositHandling(t *testing.T) {
	service := NewForceTerminateLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:   "admin",
		ActorUserID: "20000000-0000-0000-0000-000000000001",
		LeaseID:     terminateLeaseTestLeaseID,
		Reason:      "tenant unreachable",
	})
	if !errors.Is(err, errForceTerminationDepositHandlingRequired) {
		t.Fatalf("expected errForceTerminationDepositHandlingRequired, got %v", err)
	}
}

func TestForceTerminateLeaseServiceRejectsInvalidDepositHandling(t *testing.T) {
	repo := terminationRepoStub()
	service := NewForceTerminateLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:           "admin",
		ActorUserID:         "20000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		Reason:              "tenant unreachable",
		DepositHandling:     "refund",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok || details["field"] != "deposit_handling" {
		t.Fatalf("expected deposit_handling field detail, got %+v", appErr.Details)
	}
}

func TestGetForceTerminationServiceReturnsDetail(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	repo.forceTermination = &ForceTermination{
		ID:          "80000000-0000-0000-0000-000000000001",
		LeaseID:     terminateLeaseTestLeaseID,
		Status:      "completed",
		InitiatedBy: "20000000-0000-0000-0000-000000000001",
		Reason:      "tenant unreachable",
		Bills: []ForceTerminationBill{
			{BillID: "50000000-0000-0000-0000-000000000010", Status: "done"},
		},
	}
	service := NewGetForceTerminationService(repo, dbtxrunner.New(db, nil))

	result, err := service.Execute(context.Background(), GetForceTerminationInput{
		ActorRole:          "organizer",
		ForceTerminationID: "80000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ID != "80000000-0000-0000-0000-000000000001" || len(result.Bills) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func terminationRepoStub() *leaseRepositoryStub {
	return &leaseRepositoryStub{
		lease: &Lease{
			ID:                        terminateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             20000,
			DepositStatus:             "held",
		},
		replacementBills: []Bill{
			{
				ID:          "50000000-0000-0000-0000-000000000001",
				Type:        "rent",
				Status:      "paid",
				PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				PeriodEnd:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
			},
		},
	}
}

func verifySQLMockExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
