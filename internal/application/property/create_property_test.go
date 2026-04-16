package property

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
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
		WithArgs("Property A", "Address A", 5, "owner-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "address", "electricity_unit_price", "owner_id", "created_at", "updated_at", "version",
		}).AddRow("property-1", "Property A", "Address A", 5, "owner-1", time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC), 1))
	mock.ExpectCommit()

	service := NewCreatePropertyService(dbproperties.NewRepository(db), dbtxrunner.New(db, domainevents.NoopPublisher{}))
	property, err := service.Execute(context.Background(), CreatePropertyInput{
		Name:                 "Property A",
		Address:              "Address A",
		ElectricityUnitPrice: 5,
		OwnerID:              "owner-1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if property.ID != "property-1" {
		t.Fatalf("expected property-1, got %q", property.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreatePropertyServiceValidatesInput(t *testing.T) {
	service := NewCreatePropertyService(nil, nil)

	if _, err := service.Execute(context.Background(), CreatePropertyInput{}); err == nil {
		t.Fatal("expected validation error, got nil")
	}
}
