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
	terminationDate := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	actualMoveOutDate := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
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
		TerminationDate:     terminationDate,
		ActualMoveOutDate:   &actualMoveOutDate,
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
	if !repo.lease.EndDate.Equal(terminationDate) {
		t.Fatalf("end date = %v, want %v", repo.lease.EndDate, terminationDate)
	}
	if repo.lease.ActualMoveOutDate == nil || !repo.lease.ActualMoveOutDate.Equal(actualMoveOutDate) {
		t.Fatalf("actual move-out date = %v, want %v", repo.lease.ActualMoveOutDate, actualMoveOutDate)
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
	terminationDate := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
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
		TerminationDate:     terminationDate,
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
	if !repo.lease.EndDate.Equal(terminationDate) {
		t.Fatalf("end date = %v, want %v", repo.lease.EndDate, terminationDate)
	}
	if repo.lease.ActualMoveOutDate != nil {
		t.Fatalf("actual move-out date = %v, want nil", repo.lease.ActualMoveOutDate)
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

func TestForceTerminateLeaseServiceRejectsMissingTerminationDate(t *testing.T) {
	service := NewForceTerminateLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:       "admin",
		ActorUserID:     "20000000-0000-0000-0000-000000000001",
		LeaseID:         terminateLeaseTestLeaseID,
		Reason:          "tenant unreachable",
		DepositHandling: "write_off",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok || details["field"] != "termination_date" {
		t.Fatalf("expected termination_date field detail, got %+v", appErr.Details)
	}
}

func TestForceTerminateLeaseServiceRejectsTerminationDateBeforeLeaseStart(t *testing.T) {
	repo := terminationRepoStub()
	service := NewForceTerminateLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ForceTerminateLeaseInput{
		ActorRole:           "admin",
		ActorUserID:         "20000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		TerminationDate:     time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant unreachable",
		DepositHandling:     "write_off",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok || details["field"] != "termination_date" {
		t.Fatalf("expected termination_date field detail, got %+v", appErr.Details)
	}
	if repo.forceTerminateCalls != 0 {
		t.Fatalf("forceTerminateCalls = %d, want 0", repo.forceTerminateCalls)
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
		TerminationDate:     time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
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

func TestPreviewCheckoutSettlementReturnsTokenAndNoWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	service := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	finalMeter := 1234

	result, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
		FinalMeterReading:   &finalMeter,
		CleaningFee:         3000,
		KeyCardLossFee:      1000,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.PreviewToken == nil || *result.PreviewToken == "" {
		t.Fatalf("expected preview token, got %+v", result.PreviewToken)
	}
	if result.NetDirection != checkoutNetRefund || result.NetAmount != 16000 {
		t.Fatalf("unexpected net result: %+v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != checkoutWarningFinalMeter {
		t.Fatalf("warnings = %+v, want final meter warning", result.Warnings)
	}
	if repo.settleDepositCalls != 0 || repo.terminateCalls != 0 {
		t.Fatalf("preview should not write, settle=%d terminate=%d", repo.settleDepositCalls, repo.terminateCalls)
	}
}

func TestPreviewCheckoutSettlementBlocksEarlyCheckoutWithoutManualRentRefundDecision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	service := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))

	result, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.PreviewToken != nil {
		t.Fatalf("expected no token for early checkout, got %q", *result.PreviewToken)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != checkoutBlockerRentRefund {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
}

func TestPreviewCheckoutSettlementAllowsEarlyCheckoutWithNoRentRefundDecision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	service := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	reason := "雙方協議不退未到期租金"

	result, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Reason:                 "tenant requested",
		ManualRentRefundReason: &reason,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.PreviewToken == nil || *result.PreviewToken == "" {
		t.Fatalf("expected preview token, got %+v", result.PreviewToken)
	}
	if len(result.Blockers) != 0 {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
	if result.ManualRentRefundAmount != 0 || result.ManualRentRefundReason == nil || *result.ManualRentRefundReason != reason {
		t.Fatalf("manual rent refund fields = amount %d reason %+v", result.ManualRentRefundAmount, result.ManualRentRefundReason)
	}
	for _, line := range result.Lines {
		if line.Kind == checkoutLineRentRefund {
			t.Fatalf("did not expect rent refund line for amount 0: %+v", line)
		}
	}
}

func TestPreviewCheckoutSettlementManualRentRefundAffectsTotalsButNotDepositInvariant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	service := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	reason := "退還 6 月未使用租金"
	actualMoveOut := time.Date(2026, 6, 15, 13, 0, 0, 0, time.UTC)

	result, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		ActualMoveOutDate:      &actualMoveOut,
		Reason:                 "tenant requested",
		CleaningFee:            3000,
		ManualRentRefundAmount: 5000,
		ManualRentRefundReason: &reason,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var rentRefundLine *CheckoutSettlementLine
	for i := range result.Lines {
		if result.Lines[i].Kind == checkoutLineRentRefund {
			rentRefundLine = &result.Lines[i]
		}
	}
	if rentRefundLine == nil || rentRefundLine.Direction != checkoutDirectionRefund || rentRefundLine.Amount != 5000 {
		t.Fatalf("rent refund line = %+v", rentRefundLine)
	}
	if result.TotalRefund != 25000 || result.TotalCharge != 3000 || result.NetDirection != checkoutNetRefund || result.NetAmount != 22000 {
		t.Fatalf("unexpected totals: refund=%d charge=%d net=%s/%d", result.TotalRefund, result.TotalCharge, result.NetDirection, result.NetAmount)
	}
	depositRefund := result.DepositAmount - result.TotalCharge
	depositDeduction := result.TotalCharge
	if depositRefund+depositDeduction != result.DepositAmount {
		t.Fatalf("deposit invariant failed: refund=%d deduction=%d deposit=%d", depositRefund, depositDeduction, result.DepositAmount)
	}
	if result.ActualMoveOutDate == nil || !result.ActualMoveOutDate.Equal(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ActualMoveOutDate = %+v", result.ActualMoveOutDate)
	}
}

func TestPreviewCheckoutSettlementRejectsNegativeFinalMeterReading(t *testing.T) {
	repo := terminationRepoStub()
	service := NewPreviewCheckoutSettlementService(repo, nil)
	finalMeter := -1

	_, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
		FinalMeterReading:   &finalMeter,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected BAD_REQUEST, got %v", err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok || details["field"] != "final_meter_reading" {
		t.Fatalf("expected final_meter_reading field detail, got %+v", appErr.Details)
	}
}

func TestPreviewCheckoutSettlementValidatesManualRentRefund(t *testing.T) {
	service := NewPreviewCheckoutSettlementService(terminationRepoStub(), nil)
	reason := "tenant requested"

	tests := []struct {
		name  string
		input CheckoutSettlementInput
		field string
	}{
		{
			name: "negative amount",
			input: CheckoutSettlementInput{
				ActorRole:              "admin",
				LeaseID:                terminateLeaseTestLeaseID,
				CheckoutDate:           time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
				Reason:                 reason,
				ManualRentRefundAmount: -1,
				ManualRentRefundReason: &reason,
			},
			field: "manual_rent_refund_amount",
		},
		{
			name: "positive amount blank reason",
			input: CheckoutSettlementInput{
				ActorRole:              "admin",
				LeaseID:                terminateLeaseTestLeaseID,
				CheckoutDate:           time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
				Reason:                 reason,
				ManualRentRefundAmount: 1,
			},
			field: "manual_rent_refund_reason",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Execute(context.Background(), tc.input)
			var appErr *apperr.Error
			if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
				t.Fatalf("expected BAD_REQUEST, got %v", err)
			}
			details, ok := appErr.Details.(map[string]interface{})
			if !ok || details["field"] != tc.field {
				t.Fatalf("expected %s field detail, got %+v", tc.field, appErr.Details)
			}
		})
	}
}

func TestPreviewCheckoutSettlementReturnsBlockersForUnsettledBills(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "50000000-0000-0000-0000-000000000010",
		Type:        "electricity",
		Status:      "pending_meter",
		PeriodStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
	})
	service := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))

	result, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.PreviewToken != nil {
		t.Fatalf("expected no token when blocked, got %q", *result.PreviewToken)
	}
	if len(result.Blockers) != 1 || result.Blockers[0].Code != checkoutBlockerPendingMeter {
		t.Fatalf("unexpected blockers: %+v", result.Blockers)
	}
}

func TestFinalizeCheckoutSettlementRejectsStalePreviewToken(t *testing.T) {
	repo := terminationRepoStub()
	service := NewFinalizeCheckoutSettlementService(repo, &depositAccountingRepositoryStub{}, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
		CleaningFee:         3000,
		PreviewToken:        "stale",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeCheckoutSettlementStale {
		t.Fatalf("expected stale checkout error, got %v", err)
	}
	if repo.settleDepositCalls != 0 || repo.terminateCalls != 0 {
		t.Fatalf("stale finalize should not write, settle=%d terminate=%d", repo.settleDepositCalls, repo.terminateCalls)
	}
}

func TestFinalizeCheckoutSettlementRejectsStalePreviewTokenWhenManualRefundFieldsChange(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	previewService := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	reason := "退還未使用租金"
	actualMoveOut := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	preview, err := previewService.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		ActualMoveOutDate:      &actualMoveOut,
		Reason:                 "tenant requested",
		ManualRentRefundAmount: 5000,
		ManualRentRefundReason: &reason,
	})
	if err != nil {
		t.Fatalf("preview Execute: %v", err)
	}
	changedReason := "改為不退租金"
	service := NewFinalizeCheckoutSettlementService(repo, &depositAccountingRepositoryStub{}, txRunnerForRollback(t))

	_, err = service.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		ActualMoveOutDate:      &actualMoveOut,
		Reason:                 "tenant requested",
		ManualRentRefundAmount: 0,
		ManualRentRefundReason: &changedReason,
		PreviewToken:           *preview.PreviewToken,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeCheckoutSettlementStale {
		t.Fatalf("expected stale checkout error, got %v", err)
	}
	if repo.settleDepositCalls != 0 || repo.terminateCalls != 0 {
		t.Fatalf("stale finalize should not write, settle=%d terminate=%d", repo.settleDepositCalls, repo.terminateCalls)
	}
}

func TestFinalizeCheckoutSettlementPersistsSnapshotAndPublishesEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	previewService := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	preview, err := previewService.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
		CleaningFee:         3000,
	})
	if err != nil {
		t.Fatalf("preview Execute: %v", err)
	}
	token := *preview.PreviewToken

	db2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db2.Close()
	defer verifySQLMockExpectations(t, mock2)
	mock2.ExpectBegin()
	mock2.ExpectCommit()

	publisher := &recordingPublisher{}
	accountingRepo := &depositAccountingRepositoryStub{}
	finalizeService := NewFinalizeCheckoutSettlementService(repo, accountingRepo, dbtxrunner.New(db2, publisher))
	finalized, err := finalizeService.Execute(context.Background(), CheckoutSettlementInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             terminateLeaseTestLeaseID,
		CheckoutDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Reason:              "tenant requested",
		CleaningFee:         3000,
		PreviewToken:        token,
	})
	if err != nil {
		t.Fatalf("finalize Execute: %v", err)
	}
	if finalized.PreviewToken != nil || !finalized.ExportAvailable || finalized.FinalizedAt == nil {
		t.Fatalf("unexpected finalized response: %+v", finalized)
	}
	if repo.lease.SettlementDetail == nil {
		t.Fatal("expected settlement detail to be persisted")
	}
	if repo.settleDepositCalls != 1 || repo.terminateCalls != 1 {
		t.Fatalf("settleDepositCalls=%d terminateCalls=%d, want 1/1", repo.settleDepositCalls, repo.terminateCalls)
	}
	if len(accountingRepo.entries) != 2 {
		t.Fatalf("accounting entries = %d, want 2", len(accountingRepo.entries))
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events = %d, want deposit refund, deduction, termination", len(publisher.events))
	}
}

func TestFinalizeCheckoutSettlementPersistsManualRentRefundAndAccounting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	previewService := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	reason := "退還 6 月未使用租金"
	actualMoveOut := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	input := CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		ActualMoveOutDate:      &actualMoveOut,
		Reason:                 "tenant requested",
		CleaningFee:            3000,
		ManualRentRefundAmount: 5000,
		ManualRentRefundReason: &reason,
	}
	preview, err := previewService.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("preview Execute: %v", err)
	}

	db2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db2.Close()
	defer verifySQLMockExpectations(t, mock2)
	mock2.ExpectBegin()
	mock2.ExpectCommit()

	publisher := &recordingPublisher{}
	accountingRepo := &depositAccountingRepositoryStub{}
	finalizeService := NewFinalizeCheckoutSettlementService(repo, accountingRepo, dbtxrunner.New(db2, publisher))
	input.PreviewToken = *preview.PreviewToken
	finalized, err := finalizeService.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("finalize Execute: %v", err)
	}
	if finalized.ActualMoveOutDate == nil || finalized.ManualRentRefundReason == nil || finalized.ManualRentRefundAmount != 5000 {
		t.Fatalf("finalized manual refund fields = %+v", finalized)
	}
	if len(accountingRepo.entries) != 3 {
		t.Fatalf("accounting entries = %d, want 3", len(accountingRepo.entries))
	}
	entry := accountingRepo.entries[2]
	if entry.Category != rentRefundAccountingCategory || entry.AccountingTitleCode != rentRefundAccountingTitleCode || entry.Amount != -5000 {
		t.Fatalf("rent refund accounting entry = %+v", entry)
	}
	if entry.Year != 2026 || entry.Month != 6 || entry.SourceDate == nil || !entry.SourceDate.Equal(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("rent refund source period = year %d month %d date %+v", entry.Year, entry.Month, entry.SourceDate)
	}
	if entry.Description == nil || *entry.Description != reason || entry.DisplayNote == nil || *entry.DisplayNote != reason {
		t.Fatalf("rent refund description/display = %+v/%+v", entry.Description, entry.DisplayNote)
	}
	if repo.lease.SettlementDetail == nil {
		t.Fatal("expected settlement detail to be persisted")
	}
	decoded, err := checkoutSettlementFromDetailMap(*repo.lease.SettlementDetail)
	if err != nil {
		t.Fatalf("decode settlement detail: %v", err)
	}
	if decoded.ActualMoveOutDate == nil || decoded.ManualRentRefundReason == nil || decoded.ManualRentRefundAmount != 5000 {
		t.Fatalf("decoded snapshot fields = %+v", decoded)
	}
	if decoded.DepositAmount != 20000 || repo.lease.DepositRefundAmount == nil || repo.lease.DepositDeductionAmount == nil || *repo.lease.DepositRefundAmount+*repo.lease.DepositDeductionAmount != decoded.DepositAmount {
		t.Fatalf("deposit invariant failed: lease=%+v decoded=%+v", repo.lease, decoded)
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events = %d, want deposit refund, deduction, termination", len(publisher.events))
	}
}

func TestFinalizeCheckoutSettlementRollsBackWhenRentRefundAccountingFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	defer verifySQLMockExpectations(t, mock)
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := terminationRepoStub()
	previewService := NewPreviewCheckoutSettlementService(repo, dbtxrunner.New(db, nil))
	reason := "退還未使用租金"
	input := CheckoutSettlementInput{
		ActorRole:              "admin",
		AssignedPropertyIDs:    []string{"property-1"},
		LeaseID:                terminateLeaseTestLeaseID,
		CheckoutDate:           time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
		Reason:                 "tenant requested",
		ManualRentRefundAmount: 5000,
		ManualRentRefundReason: &reason,
	}
	preview, err := previewService.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("preview Execute: %v", err)
	}

	db2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db2.Close()
	defer verifySQLMockExpectations(t, mock2)
	mock2.ExpectBegin()
	mock2.ExpectRollback()

	publisher := &recordingPublisher{}
	accountingRepo := &depositAccountingRepositoryStub{createErrByCategory: map[string]error{rentRefundAccountingCategory: errors.New("insert failed")}}
	finalizeService := NewFinalizeCheckoutSettlementService(repo, accountingRepo, dbtxrunner.New(db2, publisher))
	input.PreviewToken = *preview.PreviewToken
	_, err = finalizeService.Execute(context.Background(), input)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeInternalServerError {
		t.Fatalf("expected internal server error, got %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("events should not publish on rollback: %+v", publisher.events)
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
