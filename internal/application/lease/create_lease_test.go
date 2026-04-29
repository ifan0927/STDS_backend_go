package lease

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

func TestCreateLeaseServiceCreatesLeaseAndPreGeneratesBills(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	repo := &leaseRepositoryStub{
		tenant: &Tenant{ID: "tenant-1", Status: "inactive"},
		room: &Room{
			ID:                               "room-1",
			PropertyID:                       "property-1",
			Status:                           "vacant",
			DefaultElectricityBillingCadence: "monthly",
		},
		createdLease: &Lease{
			ID:                        "lease-1",
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "monthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
			CreatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
			UpdatedAt:                 time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
			Version:                   1,
		},
	}

	service := NewCreateLeaseService(repo, dbtxrunner.New(db, publisher))
	lease, err := service.Execute(context.Background(), CreateLeaseInput{
		TenantID:      "tenant-1",
		RoomID:        "room-1",
		RentAmount:    18000,
		StartDate:     time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		EndDate:       time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		DepositAmount: 36000,
		ActorRole:     "admin",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if lease.ID != "lease-1" {
		t.Fatalf("expected lease-1, got %q", lease.ID)
	}
	if len(repo.createdBills) != 6 {
		t.Fatalf("expected 6 created bills, got %d", len(repo.createdBills))
	}

	firstRent := repo.createdBills[0]
	if firstRent.Type != billTypeRent || firstRent.Status != billStatusPendingPayment || firstRent.Amount == nil || *firstRent.Amount != 18000 {
		t.Fatalf("unexpected first rent bill: %+v", firstRent)
	}
	if !firstRent.PeriodStart.Equal(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)) || !firstRent.PeriodEnd.Equal(time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)) || !firstRent.DueDate.Equal(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first rent bill period: %+v", firstRent)
	}

	lastElectricity := repo.createdBills[len(repo.createdBills)-1]
	if lastElectricity.Type != billTypeElectricity || lastElectricity.Status != billStatusPendingMeter || lastElectricity.Amount != nil {
		t.Fatalf("unexpected last electricity bill: %+v", lastElectricity)
	}
	if !lastElectricity.PeriodStart.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) || !lastElectricity.PeriodEnd.Equal(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)) || !lastElectricity.DueDate.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected last electricity period: %+v", lastElectricity)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.LeaseCreated)
	if !ok {
		t.Fatalf("expected LeaseCreated event, got %T", publisher.events[0])
	}
	if event.LeaseID != "lease-1" || event.RoomID != "room-1" || event.TenantID != "tenant-1" || event.ElectricityBillingCadence != "monthly" {
		t.Fatalf("unexpected event: %+v", event)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateLeaseServiceUsesExplicitCadenceOverride(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	override := "bimonthly"
	repo := &leaseRepositoryStub{
		tenant: &Tenant{ID: "tenant-1", Status: "active"},
		room: &Room{
			ID:                               "room-1",
			PropertyID:                       "property-1",
			Status:                           "vacant",
			DefaultElectricityBillingCadence: "monthly",
		},
		createdLease: &Lease{
			ID:                        "lease-1",
			TenantID:                  "tenant-1",
			PropertyID:                "property-1",
			RoomID:                    "room-1",
			RentAmount:                18000,
			StartDate:                 time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
			EndDate:                   time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
			ElectricityBillingCadence: "bimonthly",
			Status:                    "active",
			DepositAmount:             36000,
			DepositStatus:             "held",
		},
	}

	service := NewCreateLeaseService(repo, dbtxrunner.New(db, nil))
	_, err = service.Execute(context.Background(), CreateLeaseInput{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		RentAmount:                18000,
		StartDate:                 time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		EndDate:                   time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		DepositAmount:             36000,
		ElectricityBillingCadence: &override,
		ActorRole:                 "admin",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.createLeaseParams == nil || repo.createLeaseParams.ElectricityBillingCadence != "bimonthly" {
		t.Fatalf("expected bimonthly cadence, got %+v", repo.createLeaseParams)
	}
	if len(repo.createdBills) != 9 {
		t.Fatalf("expected 9 bills for 6 rent periods and 3 electricity periods, got %d", len(repo.createdBills))
	}
	firstElectricity := repo.createdBills[6]
	if firstElectricity.Type != billTypeElectricity || firstElectricity.Status != billStatusPendingMeter || firstElectricity.Amount != nil {
		t.Fatalf("unexpected first electricity bill: %+v", firstElectricity)
	}
	if !firstElectricity.PeriodStart.Equal(time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)) || !firstElectricity.PeriodEnd.Equal(time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first electricity period: %+v", firstElectricity)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateLeaseServiceRejectsBusinessRuleViolationsAndMissingReferences(t *testing.T) {
	t.Run("missing tenant id", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{RoomID: "room-1"})
		if !errors.Is(err, errValidationTenantIDRequired) {
			t.Fatalf("expected errValidationTenantIDRequired, got %v", err)
		}
	})

	t.Run("missing room id", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{TenantID: "tenant-1"})
		if !errors.Is(err, errValidationRoomIDRequired) {
			t.Fatalf("expected errValidationRoomIDRequired, got %v", err)
		}
	})

	t.Run("zero tenant id", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{
			TenantID: "00000000-0000-0000-0000-000000000000",
			RoomID:   "room-1",
		})
		if !errors.Is(err, errValidationTenantIDRequired) {
			t.Fatalf("expected errValidationTenantIDRequired, got %v", err)
		}
	})

	t.Run("zero start date", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{
			TenantID: "tenant-1",
			RoomID:   "room-1",
			EndDate:  time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
		})
		if !errors.Is(err, errValidationStartDateRequired) {
			t.Fatalf("expected errValidationStartDateRequired, got %v", err)
		}
	})

	t.Run("tenant not found", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectRollback()

		service := NewCreateLeaseService(&leaseRepositoryStub{tenantErr: ErrTenantNotFound}, dbtxrunner.New(db, nil))
		_, err = service.Execute(context.Background(), CreateLeaseInput{
			TenantID:      "tenant-1",
			RoomID:        "room-1",
			RentAmount:    18000,
			StartDate:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			DepositAmount: 36000,
			ActorRole:     "admin",
		})
		if !errors.Is(err, apperr.ErrTenantNotFound) {
			t.Fatalf("expected tenant not found, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	t.Run("room not vacant", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectRollback()

		service := NewCreateLeaseService(&leaseRepositoryStub{
			tenant: &Tenant{ID: "tenant-1", Status: "active"},
			room: &Room{
				ID:                               "room-1",
				PropertyID:                       "property-1",
				Status:                           "occupied",
				DefaultElectricityBillingCadence: "monthly",
			},
		}, dbtxrunner.New(db, nil))
		_, err = service.Execute(context.Background(), CreateLeaseInput{
			TenantID:      "tenant-1",
			RoomID:        "room-1",
			RentAmount:    18000,
			StartDate:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			DepositAmount: 36000,
			ActorRole:     "admin",
		})
		if !errors.Is(err, errRoomNotVacant) {
			t.Fatalf("expected errRoomNotVacant, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	t.Run("negative rent", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectRollback()

		service := NewCreateLeaseService(&leaseRepositoryStub{
			tenant: &Tenant{ID: "tenant-1", Status: "active"},
			room: &Room{
				ID:                               "room-1",
				PropertyID:                       "property-1",
				Status:                           "vacant",
				DefaultElectricityBillingCadence: "monthly",
			},
		}, dbtxrunner.New(db, nil))
		_, err = service.Execute(context.Background(), CreateLeaseInput{
			TenantID:      "tenant-1",
			RoomID:        "room-1",
			RentAmount:    -1,
			StartDate:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			DepositAmount: 36000,
			ActorRole:     "admin",
		})
		if !errors.Is(err, errLeaseRentAmountNonPositive) {
			t.Fatalf("expected errLeaseRentAmountNonPositive, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	t.Run("empty actor role", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{
			TenantID:      "tenant-1",
			RoomID:        "room-1",
			RentAmount:    18000,
			StartDate:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			DepositAmount: 36000,
		})
		if !errors.Is(err, apperr.ErrForbidden) {
			t.Fatalf("expected forbidden, got %v", err)
		}
	})

	t.Run("unknown actor role", func(t *testing.T) {
		service := NewCreateLeaseService(nil, nil)
		_, err := service.Execute(context.Background(), CreateLeaseInput{
			TenantID:      "tenant-1",
			RoomID:        "room-1",
			RentAmount:    18000,
			StartDate:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			EndDate:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			DepositAmount: 36000,
			ActorRole:     "superuser",
		})
		if !errors.Is(err, apperr.ErrForbidden) {
			t.Fatalf("expected forbidden, got %v", err)
		}
	})
}

type leaseRepositoryStub struct {
	tenant              *Tenant
	tenantErr           error
	room                *Room
	roomErr             error
	lease               *Lease
	leaseErr            error
	createdLease        *Lease
	createLeaseErr      error
	createBillsErr      error
	updatedLease        *Lease
	settledLease        *Lease
	lockedRentBills     bool
	voidRentDueDate     *time.Time
	createLeaseParams   *CreateLeaseParams
	terminateCalls      int
	forceTerminateCalls int
	forceTermination    *ForceTermination
	voidBillsBoundary   *time.Time
	updateLeaseCalls    int
	settleDepositCalls  int
	writeOffBillIDs     []string
	createdBills        []CreateBillParams
	replacementBills    []Bill
}

type depositAccountingRepositoryStub struct {
	entries     []DepositAccountingEntryParams
	createErr   error
	createCalls int
}

func (s *depositAccountingRepositoryStub) CreateDepositAccountingEntry(_ context.Context, _ *sql.Tx, params DepositAccountingEntryParams) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	s.entries = append(s.entries, params)
	return nil
}

func (s *leaseRepositoryStub) FindTenantByID(context.Context, *sql.Tx, string) (*Tenant, error) {
	if s.tenantErr != nil {
		return nil, s.tenantErr
	}
	return s.tenant, nil
}

func (s *leaseRepositoryStub) FindRoomByIDForUpdate(context.Context, *sql.Tx, string) (*Room, error) {
	if s.roomErr != nil {
		return nil, s.roomErr
	}
	return s.room, nil
}

func (s *leaseRepositoryStub) FindLeaseByIDForUpdate(context.Context, *sql.Tx, string) (*Lease, error) {
	if s.leaseErr != nil {
		return nil, s.leaseErr
	}
	return s.lease, nil
}

func (s *leaseRepositoryStub) CreateLease(_ context.Context, _ *sql.Tx, params CreateLeaseParams) (*Lease, error) {
	s.createLeaseParams = &params
	if s.createLeaseErr != nil {
		return nil, s.createLeaseErr
	}
	return s.createdLease, nil
}

func (s *leaseRepositoryStub) UpdateLeaseConditions(_ context.Context, _ *sql.Tx, params UpdateLeaseParams) (*Lease, error) {
	s.updateLeaseCalls++
	if s.updatedLease != nil {
		s.updatedLease.RentAmount = params.RentAmount
		return s.updatedLease, nil
	}
	if s.lease == nil {
		return nil, ErrLeaseNotFound
	}
	s.lease.RentAmount = params.RentAmount
	return s.lease, nil
}

func (s *leaseRepositoryStub) SettleDeposit(_ context.Context, _ *sql.Tx, params SettleDepositParams) (*Lease, error) {
	s.settleDepositCalls++
	if s.settledLease != nil {
		s.settledLease.DepositRefundAmount = &params.RefundAmount
		s.settledLease.DepositDeductionAmount = &params.DeductionAmount
		s.settledLease.DepositDeductionReason = params.DepositDeductionReason
		s.settledLease.DepositStatus = "settled"
		return s.settledLease, nil
	}
	if s.lease == nil {
		return nil, ErrLeaseNotFound
	}
	s.lease.DepositRefundAmount = &params.RefundAmount
	s.lease.DepositDeductionAmount = &params.DeductionAmount
	s.lease.DepositDeductionReason = params.DepositDeductionReason
	s.lease.DepositStatus = "settled"
	return s.lease, nil
}

func (s *leaseRepositoryStub) TerminateLease(_ context.Context, _ *sql.Tx, params TerminateLeaseParams) (*Lease, error) {
	s.terminateCalls++
	if s.lease == nil {
		return nil, ErrLeaseNotFound
	}
	s.lease.Status = "terminated"
	s.lease.EndDate = params.EndDate
	s.lease.TerminationReason = &params.TerminationReason
	return s.lease, nil
}

func (s *leaseRepositoryStub) ForceTerminateLease(_ context.Context, _ *sql.Tx, params ForceTerminateLeaseParams) (*Lease, error) {
	s.forceTerminateCalls++
	if s.lease == nil {
		return nil, ErrLeaseNotFound
	}
	s.lease.Status = "force_terminated"
	s.lease.TerminationReason = &params.TerminationReason
	s.lease.DepositStatus = params.DepositStatus
	return s.lease, nil
}

func (s *leaseRepositoryStub) ListBillsByLeaseIDForUpdate(context.Context, *sql.Tx, string) ([]Bill, error) {
	return s.replacementBills, nil
}

func (s *leaseRepositoryStub) CreateForceTermination(_ context.Context, _ *sql.Tx, params CreateForceTerminationParams) (*ForceTermination, error) {
	s.forceTermination = &ForceTermination{
		ID:              "80000000-0000-0000-0000-000000000001",
		LeaseID:         params.LeaseID,
		Status:          "in_progress",
		InitiatedBy:     params.InitiatedBy,
		Reason:          params.Reason,
		DepositHandling: params.DepositHandling,
		CreatedAt:       time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
	}
	return s.forceTermination, nil
}

func (s *leaseRepositoryStub) CreateForceTerminationBills(_ context.Context, _ *sql.Tx, forceTerminationID string, billIDs []string) error {
	if s.forceTermination != nil && s.forceTermination.ID == forceTerminationID {
		for _, billID := range billIDs {
			s.forceTermination.Bills = append(s.forceTermination.Bills, ForceTerminationBill{BillID: billID, Status: "pending"})
		}
	}
	return nil
}

func (s *leaseRepositoryStub) WriteOffBills(_ context.Context, _ *sql.Tx, billIDs []string, _ string) error {
	s.writeOffBillIDs = append([]string(nil), billIDs...)
	return nil
}

func (s *leaseRepositoryStub) MarkForceTerminationBillsDone(_ context.Context, _ *sql.Tx, forceTerminationID string, billIDs []string) error {
	if s.forceTermination != nil && s.forceTermination.ID == forceTerminationID {
		for i := range s.forceTermination.Bills {
			for _, billID := range billIDs {
				if s.forceTermination.Bills[i].BillID == billID {
					s.forceTermination.Bills[i].Status = "done"
				}
			}
		}
	}
	return nil
}

func (s *leaseRepositoryStub) CompleteForceTermination(context.Context, *sql.Tx, string) error {
	if s.forceTermination != nil {
		s.forceTermination.Status = "completed"
	}
	return nil
}

func (s *leaseRepositoryStub) FindForceTerminationByID(context.Context, *sql.Tx, string) (*ForceTermination, error) {
	if s.forceTermination == nil {
		return nil, ErrForceTerminationNotFound
	}
	return s.forceTermination, nil
}

func (s *leaseRepositoryStub) HasLockedRentBillsFromDueDate(context.Context, *sql.Tx, string, time.Time) (bool, error) {
	return s.lockedRentBills, nil
}

func (s *leaseRepositoryStub) VoidRentBillsFromDueDate(_ context.Context, _ *sql.Tx, _ string, dueDate time.Time) error {
	s.voidRentDueDate = &dueDate
	return nil
}

func (s *leaseRepositoryStub) VoidBillsOverlappingOrAfter(_ context.Context, _ *sql.Tx, _ string, boundary time.Time) error {
	s.voidBillsBoundary = &boundary
	return nil
}

func (s *leaseRepositoryStub) CreateBills(_ context.Context, _ *sql.Tx, params []CreateBillParams) error {
	s.createdBills = append([]CreateBillParams(nil), params...)
	return s.createBillsErr
}

func (s *leaseRepositoryStub) MarkRoomOccupied(context.Context, *sql.Tx, string) error {
	if s.room != nil {
		s.room.Status = "occupied"
	}
	return nil
}

func (s *leaseRepositoryStub) MarkRoomVacant(context.Context, *sql.Tx, string) error {
	if s.room != nil {
		s.room.Status = "vacant"
	}
	return nil
}

func (s *leaseRepositoryStub) ActivateTenant(context.Context, *sql.Tx, string) error {
	if s.tenant != nil && s.tenant.Status == "inactive" {
		s.tenant.Status = "active"
	}
	return nil
}

func (s *leaseRepositoryStub) DeactivateTenantIfNoActiveLeases(context.Context, *sql.Tx, string) error {
	if s.tenant == nil {
		return nil
	}
	if s.lease != nil && (s.lease.Status == "active" || s.lease.Status == "expired") {
		return nil
	}
	if s.createdLease != nil && (s.createdLease.Status == "active" || s.createdLease.Status == "expired") {
		return nil
	}
	s.tenant.Status = "inactive"
	return nil
}

type recordingPublisher struct {
	events []any
}

func (p *recordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}
