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

const updateLeaseTestLeaseID = "40000000-0000-0000-0000-000000000001"

func TestUpdateLeaseServiceUpdatesRentAndRegeneratesFutureRentBills(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	service := NewUpdateLeaseService(repo, dbtxrunner.New(db, publisher))

	rentAmount := 20000
	lease, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:           "organizer",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RentAmount:          &rentAmount,
		OperationDate:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lease.RentAmount != 20000 {
		t.Fatalf("RentAmount = %d, want 20000", lease.RentAmount)
	}
	if repo.voidRentDueDate == nil || !repo.voidRentDueDate.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("voidRentDueDate = %v, want 2026-06-01", repo.voidRentDueDate)
	}
	if len(repo.createdBills) != 2 {
		t.Fatalf("created bills = %d, want 2", len(repo.createdBills))
	}
	for _, bill := range repo.createdBills {
		if bill.Type != billTypeRent || bill.Amount == nil || *bill.Amount != 20000 {
			t.Fatalf("unexpected bill: %+v", bill)
		}
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d, want 1", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.LeaseConditionChanged)
	if !ok {
		t.Fatalf("event = %T, want LeaseConditionChanged", publisher.events[0])
	}
	if event.LeaseID != updateLeaseTestLeaseID || event.NewRentAmount != 20000 || !event.EffectiveDate.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected event: %+v", event)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateLeaseServiceRegeneratesRentFromNextQuarterlyPeriod(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                54000,
			StartDate:                 time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
			RentBillingCadence:        "quarterly",
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	service := NewUpdateLeaseService(repo, dbtxrunner.New(db, publisher))

	rentAmount := 60000
	lease, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:           "organizer",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RentAmount:          &rentAmount,
		OperationDate:       time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lease.RentAmount != 60000 {
		t.Fatalf("RentAmount = %d, want 60000", lease.RentAmount)
	}
	if repo.voidRentDueDate == nil || !repo.voidRentDueDate.Equal(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("voidRentDueDate = %v, want 2026-08-15", repo.voidRentDueDate)
	}
	if len(repo.createdBills) != 2 {
		t.Fatalf("created bills = %d, want 2", len(repo.createdBills))
	}
	wantPeriods := []struct {
		start time.Time
		end   time.Time
	}{
		{time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC)},
	}
	for i, bill := range repo.createdBills {
		if bill.Type != billTypeRent || bill.Amount == nil || *bill.Amount != 60000 {
			t.Fatalf("unexpected bill %d: %+v", i, bill)
		}
		if !bill.PeriodStart.Equal(wantPeriods[i].start) || !bill.PeriodEnd.Equal(wantPeriods[i].end) || !bill.DueDate.Equal(wantPeriods[i].start) {
			t.Fatalf("bill %d period = %s..%s due %s, want %s..%s due %s", i, bill.PeriodStart, bill.PeriodEnd, bill.DueDate, wantPeriods[i].start, wantPeriods[i].end, wantPeriods[i].start)
		}
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d, want 1", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.LeaseConditionChanged)
	if !ok {
		t.Fatalf("event = %T, want LeaseConditionChanged", publisher.events[0])
	}
	if !event.EffectiveDate.Equal(time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("effective date = %s, want 2026-08-15", event.EffectiveDate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateLeaseServiceRejectsEndDateUpdate(t *testing.T) {
	service := NewUpdateLeaseService(nil, nil)
	rentAmount := 20000
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)

	_, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:  "admin",
		LeaseID:    updateLeaseTestLeaseID,
		RentAmount: &rentAmount,
		EndDate:    &endDate,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeLeaseUnsupportedUpdate {
		t.Fatalf("expected %s, got %v", codeLeaseUnsupportedUpdate, err)
	}
}

func TestUpdateLeaseServiceRejectsRentCadenceUpdate(t *testing.T) {
	service := NewUpdateLeaseService(nil, nil)
	rentCadence := "quarterly"

	_, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:          "admin",
		LeaseID:            updateLeaseTestLeaseID,
		RentBillingCadence: &rentCadence,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeLeaseUnsupportedUpdate {
		t.Fatalf("expected %s, got %v", codeLeaseUnsupportedUpdate, err)
	}
}

func TestUpdateLeaseServiceRejectsMalformedLeaseID(t *testing.T) {
	service := NewUpdateLeaseService(nil, nil)
	rentAmount := 20000

	_, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:  "admin",
		LeaseID:    "not-a-uuid",
		RentAmount: &rentAmount,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected bad request, got %v", err)
	}
}

func TestUpdateLeaseServiceTreatsUnchangedRentAsNoOp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	service := NewUpdateLeaseService(repo, dbtxrunner.New(db, publisher))

	rentAmount := 18000
	lease, err := service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:           "organizer",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RentAmount:          &rentAmount,
		OperationDate:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lease != repo.lease {
		t.Fatalf("lease pointer changed for no-op update")
	}
	if repo.updateLeaseCalls != 0 {
		t.Fatalf("UpdateLeaseConditions calls = %d, want 0", repo.updateLeaseCalls)
	}
	if repo.voidRentDueDate != nil {
		t.Fatalf("voidRentDueDate = %v, want nil", repo.voidRentDueDate)
	}
	if len(repo.createdBills) != 0 {
		t.Fatalf("created bills = %d, want 0", len(repo.createdBills))
	}
	if len(publisher.events) != 0 {
		t.Fatalf("events = %d, want 0", len(publisher.events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateLeaseServiceRejectsLockedFutureRentBills(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
		lockedRentBills: true,
	}
	service := NewUpdateLeaseService(repo, dbtxrunner.New(db, nil))

	rentAmount := 20000
	_, err = service.Execute(context.Background(), UpdateLeaseInput{
		ActorRole:           "organizer",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RentAmount:          &rentAmount,
		OperationDate:       time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, errLeaseUpdateHasLockedBills) {
		t.Fatalf("expected errLeaseUpdateHasLockedBills, got %v", err)
	}
	if repo.voidRentDueDate != nil {
		t.Fatalf("voidRentDueDate = %v, want nil", repo.voidRentDueDate)
	}
	if len(repo.createdBills) != 0 {
		t.Fatalf("created bills = %d, want 0", len(repo.createdBills))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceSettlesDepositAndPublishesEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	accountingRepo := &depositAccountingRepositoryStub{}
	service := NewUpdateDepositService(repo, accountingRepo, dbtxrunner.New(db, publisher))

	refundAmount := 30000
	deductionAmount := 6000
	reason := "cleaning"
	lease, err := service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
		DeductionAmount:     &deductionAmount,
		DeductionReason:     &reason,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if lease.DepositStatus != "settled" {
		t.Fatalf("DepositStatus = %q, want settled", lease.DepositStatus)
	}
	if lease.DepositRefundAmount == nil || *lease.DepositRefundAmount != refundAmount {
		t.Fatalf("DepositRefundAmount = %v, want %d", lease.DepositRefundAmount, refundAmount)
	}
	if lease.DepositDeductionAmount == nil || *lease.DepositDeductionAmount != deductionAmount {
		t.Fatalf("DepositDeductionAmount = %v, want %d", lease.DepositDeductionAmount, deductionAmount)
	}
	if lease.DepositDeductionReason == nil || *lease.DepositDeductionReason != reason {
		t.Fatalf("DepositDeductionReason = %v, want %q", lease.DepositDeductionReason, reason)
	}
	if len(accountingRepo.entries) != 2 {
		t.Fatalf("accounting entries = %d, want 2", len(accountingRepo.entries))
	}
	if accountingRepo.entries[0].Category != depositAccountingCategoryRefund || accountingRepo.entries[0].Amount != -refundAmount {
		t.Fatalf("refund accounting entry = %+v", accountingRepo.entries[0])
	}
	if accountingRepo.entries[0].SourceRef["type"] != "DepositRefunded" || accountingRepo.entries[0].SourceRef["lease_id"] != updateLeaseTestLeaseID {
		t.Fatalf("refund source ref = %#v", accountingRepo.entries[0].SourceRef)
	}
	if accountingRepo.entries[1].Category != depositAccountingCategoryDeduction || accountingRepo.entries[1].Amount != deductionAmount {
		t.Fatalf("deduction accounting entry = %+v", accountingRepo.entries[1])
	}
	if accountingRepo.entries[1].Description == nil || *accountingRepo.entries[1].Description != reason {
		t.Fatalf("deduction description = %v, want %q", accountingRepo.entries[1].Description, reason)
	}
	if accountingRepo.entries[1].SourceRef["type"] != "DepositDeducted" || accountingRepo.entries[1].SourceRef["lease_id"] != updateLeaseTestLeaseID || accountingRepo.entries[1].SourceRef["reason"] != reason {
		t.Fatalf("deduction source ref = %#v", accountingRepo.entries[1].SourceRef)
	}
	if len(publisher.events) != 2 {
		t.Fatalf("events = %d, want 2", len(publisher.events))
	}
	refunded, ok := publisher.events[0].(domainevents.DepositRefunded)
	if !ok {
		t.Fatalf("first event = %T, want DepositRefunded", publisher.events[0])
	}
	if refunded.LeaseID != updateLeaseTestLeaseID || refunded.Amount != refundAmount {
		t.Fatalf("unexpected refund event: %+v", refunded)
	}
	deducted, ok := publisher.events[1].(domainevents.DepositDeducted)
	if !ok {
		t.Fatalf("second event = %T, want DepositDeducted", publisher.events[1])
	}
	if deducted.LeaseID != updateLeaseTestLeaseID || deducted.Amount != deductionAmount || deducted.Reason != reason {
		t.Fatalf("unexpected deduction event: %+v", deducted)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceCreatesRefundAccountingEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	accountingRepo := &depositAccountingRepositoryStub{}
	service := NewUpdateDepositService(repo, accountingRepo, dbtxrunner.New(db, nil))

	refundAmount := 36000
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accountingRepo.entries) != 1 {
		t.Fatalf("accounting entries = %d, want 1", len(accountingRepo.entries))
	}
	entry := accountingRepo.entries[0]
	if entry.Category != depositAccountingCategoryRefund || entry.AccountingTitleCode != depositAccountingTitleCodeRefund || entry.Amount != -refundAmount {
		t.Fatalf("accounting entry = %+v", entry)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceCreatesDeductionAccountingEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	accountingRepo := &depositAccountingRepositoryStub{}
	service := NewUpdateDepositService(repo, accountingRepo, dbtxrunner.New(db, nil))

	deductionAmount := 36000
	reason := "wall repair"
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		DeductionAmount:     &deductionAmount,
		DeductionReason:     &reason,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(accountingRepo.entries) != 1 {
		t.Fatalf("accounting entries = %d, want 1", len(accountingRepo.entries))
	}
	entry := accountingRepo.entries[0]
	if entry.Category != depositAccountingCategoryDeduction || entry.AccountingTitleCode != depositAccountingTitleCodeDeduction || entry.Amount != deductionAmount {
		t.Fatalf("accounting entry = %+v", entry)
	}
	if entry.Description == nil || *entry.Description != reason {
		t.Fatalf("description = %v, want %q", entry.Description, reason)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceRejectsMalformedLeaseID(t *testing.T) {
	service := NewUpdateDepositService(nil, nil, nil)
	refundAmount := 36000

	_, err := service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:    "admin",
		LeaseID:      "not-a-uuid",
		RefundAmount: &refundAmount,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeBadRequest {
		t.Fatalf("expected bad request, got %v", err)
	}
}

func TestUpdateDepositServiceRejectsDeductionWithoutReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	service := NewUpdateDepositService(repo, &depositAccountingRepositoryStub{}, dbtxrunner.New(db, nil))

	refundAmount := 30000
	deductionAmount := 6000
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
		DeductionAmount:     &deductionAmount,
	})
	if !errors.Is(err, errDepositDeductionReasonRequired) {
		t.Fatalf("expected errDepositDeductionReasonRequired, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceRejectsNegativeSettlementAmount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	service := NewUpdateDepositService(repo, &depositAccountingRepositoryStub{}, dbtxrunner.New(db, nil))

	refundAmount := -1
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
	})
	if !errors.Is(err, errSettlementNegative) {
		t.Fatalf("expected errSettlementNegative, got %v", err)
	}
	if repo.settleDepositCalls != 0 {
		t.Fatalf("SettleDeposit calls = %d, want 0", repo.settleDepositCalls)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateDepositServiceRollsBackWhenAccountingEntryFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &leaseRepositoryStub{
		lease: &Lease{
			ID:                        updateLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}
	accountingRepo := &depositAccountingRepositoryStub{createErr: errors.New("accounting failed")}
	service := NewUpdateDepositService(repo, accountingRepo, dbtxrunner.New(db, nil))

	refundAmount := 36000
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             updateLeaseTestLeaseID,
		RefundAmount:        &refundAmount,
	})
	if err == nil {
		t.Fatal("Execute error = nil, want error")
	}
	if repo.settleDepositCalls != 1 {
		t.Fatalf("SettleDeposit calls = %d, want 1", repo.settleDepositCalls)
	}
	if accountingRepo.createCalls != 1 {
		t.Fatalf("CreateDepositAccountingEntry calls = %d, want 1", accountingRepo.createCalls)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
