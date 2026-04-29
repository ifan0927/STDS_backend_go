package property

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	domainproperty "stds_backend/internal/domain/property"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

type recordingPublisher struct {
	events []any
}

func (p *recordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}

type propertyRepoStub struct {
	created         *Property
	current         *Property
	updated         *Property
	room            *Room
	createdRoom     *Room
	updatedRoom     *Room
	repairRequest   *RepairRequest
	occupiedRoomIDs []string
	createErr       error
	findErr         error
	updateErr       error
	findRoomErr     error
	createRoomErr   error
	updateRoomErr   error
	repairErr       error
	listOccupiedErr error
	deleteErr       error
	deleteRoomErr   error
}

type propertyAccountRepoStub struct {
	propertyID string
	err        error
}

func (s *propertyAccountRepoStub) CreatePropertyAccount(_ context.Context, _ *sql.Tx, params CreatePropertyAccountParams) error {
	s.propertyID = params.PropertyID
	return s.err
}

func (s propertyRepoStub) Create(context.Context, *sql.Tx, CreatePropertyParams) (*Property, error) {
	return s.created, s.createErr
}

func (s propertyRepoStub) FindByID(context.Context, *sql.Tx, string) (*Property, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.current, nil
}

func (s propertyRepoStub) Update(context.Context, *sql.Tx, UpdatePropertyParams) (*Property, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.updated, nil
}

func (s propertyRepoStub) ListOccupiedRoomIDs(context.Context, *sql.Tx, string) ([]string, error) {
	return s.occupiedRoomIDs, s.listOccupiedErr
}

func (s propertyRepoStub) SoftDelete(context.Context, *sql.Tx, string, int) error {
	return s.deleteErr
}

func (s propertyRepoStub) CreateRoom(context.Context, *sql.Tx, CreateRoomParams) (*Room, error) {
	return s.createdRoom, s.createRoomErr
}

func (s propertyRepoStub) FindRoomByID(context.Context, *sql.Tx, string) (*Room, error) {
	if s.findRoomErr != nil {
		return nil, s.findRoomErr
	}
	return s.room, nil
}

func (s propertyRepoStub) UpdateRoom(context.Context, *sql.Tx, UpdateRoomParams) (*Room, error) {
	if s.updateRoomErr != nil {
		return nil, s.updateRoomErr
	}
	return s.updatedRoom, nil
}

func (s propertyRepoStub) SoftDeleteRoom(context.Context, *sql.Tx, string) error {
	return s.deleteRoomErr
}

func (s propertyRepoStub) CreateRepairRequest(context.Context, *sql.Tx, CreateRepairRequestParams) (*RepairRequest, error) {
	if s.repairErr != nil {
		return nil, s.repairErr
	}
	return s.repairRequest, nil
}

func TestCreatePropertyServicePublishesPropertyCreatedAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	electricityUnitPrice := 4.5
	accountRepo := &propertyAccountRepoStub{}
	service := NewCreatePropertyService(propertyRepoStub{
		created: &Property{
			ID:                               "property-1",
			Name:                             "Property A",
			Address:                          "Address A",
			ElectricityUnitPrice:             &electricityUnitPrice,
			DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
			OwnerID:                          "owner-1",
			CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Version:                          1,
		},
	}, accountRepo, dbtxrunner.New(db, publisher))

	if _, err := service.Execute(context.Background(), CreatePropertyInput{
		Name:                             "Property A",
		Address:                          "Address A",
		ElectricityUnitPrice:             4.5,
		DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
		OwnerID:                          "owner-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(publisher.events))
	}
	if _, ok := publisher.events[0].(domainevents.PropertyCreated); !ok {
		t.Fatalf("expected PropertyCreated event, got %T", publisher.events[0])
	}
	if accountRepo.propertyID != "property-1" {
		t.Fatalf("expected property account for property-1, got %q", accountRepo.propertyID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreatePropertyServiceRollsBackWhenPropertyAccountCreateFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	publisher := &recordingPublisher{}
	electricityUnitPrice := 4.5
	service := NewCreatePropertyService(propertyRepoStub{
		created: &Property{
			ID:                               "property-1",
			Name:                             "Property A",
			Address:                          "Address A",
			ElectricityUnitPrice:             &electricityUnitPrice,
			DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
			OwnerID:                          "owner-1",
			CreatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			UpdatedAt:                        time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC),
			Version:                          1,
		},
	}, &propertyAccountRepoStub{err: errors.New("create property account failed")}, dbtxrunner.New(db, publisher))

	if _, err := service.Execute(context.Background(), CreatePropertyInput{
		Name:                             "Property A",
		Address:                          "Address A",
		ElectricityUnitPrice:             4.5,
		DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
		OwnerID:                          "owner-1",
	}); err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(publisher.events) != 0 {
		t.Fatalf("expected no published events, got %d", len(publisher.events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdatePropertyServiceRejectsStaffElectricityPriceMutation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	electricityUnitPrice := 4.0
	service := NewUpdatePropertyService(propertyRepoStub{
		current: &Property{
			ID:                               "property-1",
			Name:                             "Property A",
			Address:                          "Address A",
			ElectricityUnitPrice:             &electricityUnitPrice,
			DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
			OwnerID:                          "owner-1",
			Version:                          1,
		},
	}, dbtxrunner.New(db, nil))

	price := 5.0
	_, err = service.Execute(context.Background(), UpdatePropertyInput{
		ID:                   "property-1",
		ActorRole:            "staff",
		ElectricityUnitPrice: &price,
	})

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codeForbiddenElectricityPriceUpdate {
		t.Fatalf("expected %s, got %s", codeForbiddenElectricityPriceUpdate, appErr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeletePropertyServiceReturnsOccupiedRoomDetails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	electricityUnitPrice := 4.0
	service := NewDeletePropertyService(propertyRepoStub{
		current: &Property{
			ID:                               "property-1",
			Name:                             "Property A",
			Address:                          "Address A",
			ElectricityUnitPrice:             &electricityUnitPrice,
			DefaultElectricityBillingCadence: domainproperty.BillingCadenceMonthly,
			OwnerID:                          "owner-1",
			Version:                          1,
		},
		occupiedRoomIDs: []string{"room-1", "room-2"},
	}, dbtxrunner.New(db, nil))

	err = service.Execute(context.Background(), DeletePropertyInput{ID: "property-1"})

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codePropertyHasOccupiedRooms {
		t.Fatalf("expected %s, got %s", codePropertyHasOccupiedRooms, appErr.Code)
	}

	details, ok := appErr.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details map, got %T", appErr.Details)
	}
	if got := details["occupied_room_ids"]; got == nil {
		t.Fatalf("expected occupied_room_ids detail, got %#v", details)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeletePropertyServiceMapsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewDeletePropertyService(propertyRepoStub{
		findErr: ErrPropertyNotFound,
	}, dbtxrunner.New(db, nil))

	err = service.Execute(context.Background(), DeletePropertyInput{ID: "missing"})
	if !errors.Is(err, apperr.ErrPropertyNotFound) {
		t.Fatalf("expected ErrPropertyNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
