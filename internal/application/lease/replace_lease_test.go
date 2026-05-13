package lease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/platform/eventbus"
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
		ActorRole:             "staff",
		AssignedPropertyIDs:   []string{"property-1"},
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.OldLease.Status != "terminated" {
		t.Fatalf("old status = %q, want terminated", result.OldLease.Status)
	}
	expectedOldEndDate := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	if !result.OldLease.EndDate.Equal(expectedOldEndDate) {
		t.Fatalf("old end date = %v, want %v", result.OldLease.EndDate, expectedOldEndDate)
	}
	if result.OldLease.ActualMoveOutDate != nil {
		t.Fatalf("old actual move-out date = %v, want nil", result.OldLease.ActualMoveOutDate)
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

func TestReplaceLeaseServiceSubscribersKeepReplacementRoomAndTenantActive(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := replacementRepoStub()
	bus := eventbus.New()
	txRunner := dbtxrunner.New(db, bus)
	eventbus.Subscribe(bus, NewReleaseRoomOnLeaseTerminatedHandler(repo, txRunner).HandleLeaseTerminated)
	eventbus.Subscribe(bus, NewDeactivateTenantOnLeaseTerminatedHandler(repo, txRunner).HandleLeaseTerminated)
	eventbus.Subscribe(bus, NewOccupyRoomOnLeaseCreatedHandler(repo, txRunner).HandleLeaseCreated)
	eventbus.Subscribe(bus, NewActivateTenantOnLeaseCreatedHandler(repo, txRunner).HandleLeaseCreated)
	service := NewReplaceLeaseService(repo, txRunner)

	_, err = service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "staff",
		AssignedPropertyIDs:   []string{"property-1"},
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.room.Status != "occupied" {
		t.Fatalf("room status = %q, want occupied", repo.room.Status)
	}
	if repo.tenant.Status != "active" {
		t.Fatalf("tenant status = %q, want active", repo.tenant.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestReplaceLeaseServiceRejectsNonBoundaryDate(t *testing.T) {
	service := NewReplaceLeaseService(replacementRepoStub(), txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if !errors.Is(err, errReplacementNotAtBillingBoundary) {
		t.Fatalf("expected errReplacementNotAtBillingBoundary, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsMissingRentBillingCadence(t *testing.T) {
	service := NewReplaceLeaseService(nil, nil)

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
	if !errors.As(err, &appErr) {
		t.Fatalf("expected application error, got %v", err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok || details["field"] != "rent_billing_cadence" {
		t.Fatalf("expected rent_billing_cadence field detail, got %+v", appErr.Details)
	}
}

func TestReplaceLeaseServiceSupportsRentCadenceChangeAndReportsChangedField(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := replacementRepoStub()
	repo.lease.RentBillingCadence = "monthly"
	repo.createdLease.RentBillingCadence = "quarterly"
	service := NewReplaceLeaseService(repo, dbtxrunner.New(db, nil))

	result, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "quarterly",
		NewCadence:            "monthly",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.createLeaseParams == nil || repo.createLeaseParams.RentBillingCadence != "quarterly" || repo.createLeaseParams.ElectricityBillingCadence != "monthly" {
		t.Fatalf("unexpected create lease params: %+v", repo.createLeaseParams)
	}
	if !hasString(result.ChangedFields, "rent_billing_cadence") {
		t.Fatalf("changed fields = %#v, want rent_billing_cadence", result.ChangedFields)
	}
	if len(repo.createdBills) != 10 {
		t.Fatalf("created bills = %d, want 10 for 3 quarterly rent periods and 7 monthly electricity periods", len(repo.createdBills))
	}
	firstRent := repo.createdBills[0]
	if firstRent.Type != billTypeRent || !firstRent.PeriodStart.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) || !firstRent.PeriodEnd.Equal(time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first rent bill: %+v", firstRent)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
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
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != codeReplacementUnsettledBills {
		t.Fatalf("expected errReplacementHasUnsettledBills, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsUnsupportedDepositHandling(t *testing.T) {
	service := NewReplaceLeaseService(nil, nil)

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		LeaseID:               replaceLeaseTestLeaseID,
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "refund",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if !errors.Is(err, errReplacementDepositHandlingUnsupported) {
		t.Fatalf("expected errReplacementDepositHandlingUnsupported, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsScopeMismatch(t *testing.T) {
	repo := replacementRepoStub()
	repo.room.PropertyID = "property-2"
	service := NewReplaceLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if !errors.Is(err, errReplacementScopeMismatch) {
		t.Fatalf("expected errReplacementScopeMismatch, got %v", err)
	}
}

func TestReplaceLeaseServiceRejectsBoundaryThatCutsRentPeriod(t *testing.T) {
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
	service := NewReplaceLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if !errors.Is(err, errReplacementNotAtBillingBoundary) {
		t.Fatalf("expected errReplacementNotAtBillingBoundary, got %v", err)
	}
	if repo.voidBillsBoundary != nil {
		t.Fatalf("voidBillsBoundary = %v, want nil", repo.voidBillsBoundary)
	}
}

func TestReplaceLeaseServiceRejectsLeaseEndShortPeriodAsBoundary(t *testing.T) {
	repo := replacementRepoStub()
	repo.lease.StartDate = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.lease.EndDate = time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	repo.lease.RentBillingCadence = "quarterly"
	repo.replacementBills = []Bill{
		{
			ID:          "bill-rent-paid-full",
			Type:        "rent",
			Status:      "paid",
			PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          "bill-electricity-paid-full",
			Type:        "electricity",
			Status:      "paid",
			PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          "bill-rent-final-short",
			Type:        "rent",
			Status:      "paid",
			PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          "bill-electricity-final-short",
			Type:        "electricity",
			Status:      "paid",
			PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		},
	}
	service := NewReplaceLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "quarterly",
		NewCadence:            "monthly",
	})
	if !errors.Is(err, errReplacementNotAtBillingBoundary) {
		t.Fatalf("expected errReplacementNotAtBillingBoundary, got %v", err)
	}
	if repo.voidBillsBoundary != nil {
		t.Fatalf("voidBillsBoundary = %v, want nil", repo.voidBillsBoundary)
	}
}

func TestReplaceLeaseServiceRejectsBoundaryThatCutsElectricityPeriod(t *testing.T) {
	repo := replacementRepoStub()
	repo.replacementBills = []Bill{
		{
			ID:          "bill-rent-paid-second",
			Type:        "rent",
			Status:      "paid",
			PeriodStart: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          "bill-electricity-overlap",
			Type:        "electricity",
			Status:      "pending_meter",
			PeriodStart: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC),
		},
	}
	service := NewReplaceLeaseService(repo, txRunnerForRollback(t))

	_, err := service.Execute(context.Background(), ReplaceLeaseInput{
		ActorRole:             "admin",
		LeaseID:               replaceLeaseTestLeaseID,
		Reason:                "cadence_change",
		EffectiveStartDate:    time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		DepositHandling:       "carry_over",
		NewEndDate:            time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		NewRentAmount:         20000,
		NewRentBillingCadence: "monthly",
		NewCadence:            "bimonthly",
	})
	if !errors.Is(err, errReplacementNotAtBillingBoundary) {
		t.Fatalf("expected errReplacementNotAtBillingBoundary, got %v", err)
	}
	if repo.voidBillsBoundary != nil {
		t.Fatalf("voidBillsBoundary = %v, want nil", repo.voidBillsBoundary)
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
			RentBillingCadence:        "monthly",
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
			RentBillingCadence:        "monthly",
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

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func txRunnerForRollback(t *testing.T) *dbtxrunner.Runner {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
		_ = db.Close()
	})
	mock.ExpectBegin()
	mock.ExpectRollback()

	return dbtxrunner.New(db, nil)
}
