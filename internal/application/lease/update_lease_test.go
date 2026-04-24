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
			ID:                        "lease-1",
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
		LeaseID:             "lease-1",
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
	if event.LeaseID != "lease-1" || event.NewRentAmount != 20000 || !event.EffectiveDate.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected event: %+v", event)
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
		LeaseID:    "lease-1",
		RentAmount: &rentAmount,
		EndDate:    &endDate,
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeLeaseUnsupportedUpdate {
		t.Fatalf("expected %s, got %v", codeLeaseUnsupportedUpdate, err)
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
			ID:                        "lease-1",
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
		LeaseID:             "lease-1",
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
			ID:                        "lease-1",
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
	service := NewUpdateDepositService(repo, dbtxrunner.New(db, publisher))

	refundAmount := 30000
	deductionAmount := 6000
	reason := "cleaning"
	lease, err := service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             "lease-1",
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
	if len(publisher.events) != 2 {
		t.Fatalf("events = %d, want 2", len(publisher.events))
	}
	if _, ok := publisher.events[0].(domainevents.DepositRefunded); !ok {
		t.Fatalf("first event = %T, want DepositRefunded", publisher.events[0])
	}
	if _, ok := publisher.events[1].(domainevents.DepositDeducted); !ok {
		t.Fatalf("second event = %T, want DepositDeducted", publisher.events[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
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
			ID:                        "lease-1",
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
	service := NewUpdateDepositService(repo, dbtxrunner.New(db, nil))

	refundAmount := 30000
	deductionAmount := 6000
	_, err = service.Execute(context.Background(), UpdateDepositInput{
		ActorRole:           "admin",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             "lease-1",
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
