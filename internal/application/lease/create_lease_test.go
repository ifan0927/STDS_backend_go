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
		})
		if !errors.Is(err, errLeaseRentAmountZero) {
			t.Fatalf("expected errLeaseRentAmountZero, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})
}

type leaseRepositoryStub struct {
	tenant            *Tenant
	tenantErr         error
	room              *Room
	roomErr           error
	createdLease      *Lease
	createLeaseErr    error
	createBillsErr    error
	createLeaseParams *CreateLeaseParams
	createdBills      []CreateBillParams
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

func (s *leaseRepositoryStub) CreateLease(_ context.Context, _ *sql.Tx, params CreateLeaseParams) (*Lease, error) {
	s.createLeaseParams = &params
	if s.createLeaseErr != nil {
		return nil, s.createLeaseErr
	}
	return s.createdLease, nil
}

func (s *leaseRepositoryStub) CreateBills(_ context.Context, _ *sql.Tx, params []CreateBillParams) error {
	s.createdBills = append([]CreateBillParams(nil), params...)
	return s.createBillsErr
}

func (s *leaseRepositoryStub) MarkRoomOccupied(context.Context, *sql.Tx, string) error {
	return nil
}

func (s *leaseRepositoryStub) ActivateTenant(context.Context, *sql.Tx, string) error {
	return nil
}

type recordingPublisher struct {
	events []any
}

func (p *recordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}
