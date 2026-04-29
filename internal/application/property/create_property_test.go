package property

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
)

func TestCreatePropertyServiceCreatesProperty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO properties").
		WithArgs("Property A", "Address A", 4.5, "monthly", "owner-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "address", "electricity_unit_price", "default_electricity_billing_cadence", "owner_id", "created_at", "updated_at", "version",
		}).AddRow("property-1", "Property A", "Address A", 4.5, "monthly", "owner-1", time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), 1))
	mock.ExpectExec("INSERT INTO property_accounts").
		WithArgs("property-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	service := NewCreatePropertyService(sqlPropertyRepositoryAdapter{repo: dbproperties.NewRepository(db)}, sqlPropertyAccountRepositoryAdapter{repo: dbbilling.NewRepository(db)}, dbtxrunner.New(db, domainevents.NoopPublisher{}))
	property, err := service.Execute(context.Background(), CreatePropertyInput{
		Name:                             "Property A",
		Address:                          "Address A",
		ElectricityUnitPrice:             4.5,
		DefaultElectricityBillingCadence: "monthly",
		OwnerID:                          "owner-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if property.ID != "property-1" {
		t.Fatalf("expected property-1, got %q", property.ID)
	}
	if property.ElectricityUnitPrice == nil || *property.ElectricityUnitPrice != 4.5 {
		t.Fatalf("expected electricity_unit_price 4.5, got %v", property.ElectricityUnitPrice)
	}
	if property.DefaultElectricityBillingCadence != "monthly" {
		t.Fatalf("expected default cadence monthly, got %q", property.DefaultElectricityBillingCadence)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

type sqlPropertyRepositoryAdapter struct {
	repo dbproperties.CommandRepository
}

type sqlPropertyAccountRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a sqlPropertyAccountRepositoryAdapter) CreatePropertyAccount(ctx context.Context, tx *sql.Tx, params CreatePropertyAccountParams) error {
	return a.repo.CreatePropertyAccount(ctx, tx, params.PropertyID)
}

func (a sqlPropertyRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error) {
	property, err := a.repo.Create(ctx, tx, dbproperties.CreatePropertyParams{
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
	})
	if err != nil {
		return nil, err
	}

	return &Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}, nil
}

func TestCreatePropertyServiceValidatesInput(t *testing.T) {
	service := NewCreatePropertyService(nil, nil, nil)

	if _, err := service.Execute(context.Background(), CreatePropertyInput{}); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func (a sqlPropertyRepositoryAdapter) FindByID(context.Context, *sql.Tx, string) (*Property, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) Update(context.Context, *sql.Tx, UpdatePropertyParams) (*Property, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) ListOccupiedRoomIDs(context.Context, *sql.Tx, string) ([]string, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) SoftDelete(context.Context, *sql.Tx, string, int) error {
	return nil
}

func (a sqlPropertyRepositoryAdapter) CreateRoom(context.Context, *sql.Tx, CreateRoomParams) (*Room, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) FindRoomByID(context.Context, *sql.Tx, string) (*Room, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) UpdateRoom(context.Context, *sql.Tx, UpdateRoomParams) (*Room, error) {
	return nil, nil
}

func (a sqlPropertyRepositoryAdapter) SoftDeleteRoom(context.Context, *sql.Tx, string) error {
	return nil
}

func (a sqlPropertyRepositoryAdapter) CreateRepairRequest(context.Context, *sql.Tx, CreateRepairRequestParams) (*RepairRequest, error) {
	return nil, nil
}
