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

const replaceLeaseTestLeaseID = "40000000-0000-0000-0000-000000000001"

func TestReplaceLeaseServiceCreatesSuccessorAndPublishesEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := replacementRepoStub()
	service := NewReplaceLeaseService(repo, dbtxrunner.New(db, publisher))

	result, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:           "staff",
		AssignedPropertyIDs: []string{"property-1"},
		LeaseID:             replaceLeaseTestLeaseID,
		Reason:              "cadence_change",
		EffectiveStartDate:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:     "carry_over",
		NewEndDate:          time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:       20000,
		NewCadence:          "bimonthly",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.OldLease.Status != "terminated" {
		t.Fatalf("old status = %q, want terminated", result.OldLease.Status)
	}
	if repo.createLeaseParams == nil || repo.createLeaseParams.TenantID != repo.lease.TenantID || repo.createLeaseParams.RoomID != repo.lease.RoomID {
		t.Fatalf("successor did not inherit scope: %+v", repo.createLeaseParams)
	}
	if repo.voidBillsBoundary == nil || !repo.voidBillsBoundary.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("voidBillsBoundary = %v, want 2026-06-01", repo.voidBillsBoundary)
	}
	if len(repo.createdBills) != 11 {
		t.Fatalf("created bills = %d, want 11", len(repo.createdBills))
	}
	if len(publisher.events) != 3 {
		t.Fatalf("events = %d, want 3", len(publisher.events))
	}
	terminated, ok := publisher.events[0].(domainevents.LeaseTerminated)
	if !ok || !terminated.IsReplacement {
		t.Fatalf("first event = %+v, want replacement LeaseTerminated", publisher.events[0])
	}
	if _, ok := publisher.events[1].(domainevents.LeaseCreated); !ok {
		t.Fatalf("second event = %T, want LeaseCreated", publisher.events[1])
	}
	replaced, ok := publisher.events[2].(domainevents.LeaseReplaced)
	if !ok {
		t.Fatalf("third event = %T, want LeaseReplaced", publisher.events[2])
	}
	if replaced.OldLeaseID != replaceLeaseTestLeaseID || replaced.NewLeaseID != "lease-successor" || replaced.DepositHandling != "carry_over" {
		t.Fatalf("unexpected LeaseReplaced event: %+v", replaced)
	}
}

func TestReplaceLeaseServiceRejectsNonBoundaryDate(t *testing.T) {
	service := NewReplaceLeaseService(replacementRepoStub(), txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:          "admin",
		LeaseID:            replaceLeaseTestLeaseID,
		Reason:             "cadence_change",
		EffectiveStartDate: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		DepositHandling:    "carry_over",
		NewEndDate:         time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:      20000,
		NewCadence:         "bimonthly",
	})
	if !errors.Is(err, errReplacementNotAtBillingBoundary) {
		t.Fatalf("expected errReplacementNotAtBillingBoundary, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsUnsettledBoundaryBeforeBills(t *testing.T) {
	repo := replacementRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "bill-unsettled",
		Type:        "rent",
		Status:      "pending_payment",
		PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	})
	service := NewReplaceLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:          "admin",
		LeaseID:            replaceLeaseTestLeaseID,
		Reason:             "cadence_change",
		EffectiveStartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:    "carry_over",
		NewEndDate:         time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:      20000,
		NewCadence:         "bimonthly",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeReplacementUnsettledBills {
		t.Fatalf("expected errReplacementHasUnsettledBills, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsUnsupportedDepositHandling(t *testing.T) {
	service := NewReplaceLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		LeaseID:            replaceLeaseTestLeaseID,
		EffectiveStartDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:    "refund",
		NewEndDate:         time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:      20000,
		NewCadence:         "bimonthly",
	})
	if !errors.Is(err, errReplacementDepositHandlingUnsupported) {
		t.Fatalf("expected errReplacementDepositHandlingUnsupported, got %v", err)
	}
}

func TestReplaceLeaseServiceVoidsBillsOverlappingBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := replacementRepoStub()
	repo.replacementBills = append(repo.replacementBills, Bill{
		ID:          "bill-electricity-paid-second",
		Type:        "electricity",
		Status:      "paid",
		PeriodStart: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
	}, Bill{
		ID:          "bill-rent-overlap",
		Type:        "rent",
		Status:      "pending_payment",
		PeriodStart: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	})
	service := NewReplaceLeaseService(repo, dbtxrunner.New(db, nil))

	_, err = service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:          "admin",
		LeaseID:            replaceLeaseTestLeaseID,
		Reason:             "cadence_change",
		EffectiveStartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:    "carry_over",
		NewEndDate:         time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:      20000,
		NewCadence:         "bimonthly",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.voidBillsBoundary == nil || !repo.voidBillsBoundary.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("voidBillsBoundary = %v, want 2026-07-01", repo.voidBillsBoundary)
	}
}

func replacementRepoStub() *leaseRepositoryStub {
	return &leaseRepositoryStub{
		tenant: &Tenant{ID: "tenant-1", Status: "active"},
		room: &Room{
			ID:                               "room-1",
			PropertyID:                       "property-1",
			Status:                           "occupied",
			DefaultElectricityBillingCadence: "monthly",
		},
		lease: &Lease{
			ID:                        replaceLeaseTestLeaseID,
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
		createdLease: &Lease{
			ID:                        "lease-successor",
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                20000,
			StartDate:                 time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "bimonthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
		replacementBills: []Bill{
			{
				ID:          "bill-electricity-paid",
				Type:        "electricity",
				Status:      "paid",
				PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:          "bill-rent-paid",
				Type:        "rent",
				Status:      "paid",
				PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
				PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			},
		},
	}
}

func txRunnerForRollback(t *testing.T) *dbtxrunner.Runner {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	mock.ExpectBegin()
	mock.ExpectRollback()

	return dbtxrunner.New(db, nil)
}
