package property

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	domainproperty "stds_backend/internal/domain/property"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

func TestCreateRoomServiceCreatesRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	service := NewCreateRoomService(propertyRepoStub{
		current: &Property{ID: "property-1"},
		createdRoom: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusVacant,
			CreatedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
			UpdatedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
		},
	}, dbtxrunner.New(db, nil))

	room, err := service.Execute(context.Background(), CreateRoomInput{
		PropertyID: "property-1",
		Name:       " 101 Room ",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if room.Name != "101 Room" {
		t.Fatalf("expected normalized room name, got %q", room.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeleteRoomServiceRejectsMaintenanceRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewDeleteRoomService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusMaintenance,
		},
	}, dbtxrunner.New(db, nil))

	err = service.Execute(context.Background(), DeleteRoomInput{ID: "room-1"})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codeRoomIsInMaintenance {
		t.Fatalf("expected %s, got %s", codeRoomIsInMaintenance, appErr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestDeleteRoomServiceRejectsOccupiedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewDeleteRoomService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusOccupied,
		},
	}, dbtxrunner.New(db, nil))

	err = service.Execute(context.Background(), DeleteRoomInput{ID: "room-1"})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codeRoomIsOccupied {
		t.Fatalf("expected %s, got %s", codeRoomIsOccupied, appErr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateRoomServiceUpdatesName(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	service := NewUpdateRoomService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusVacant,
		},
		updatedRoom: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room Renamed",
			Status:     domainproperty.RoomStatusVacant,
			CreatedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
			UpdatedAt:  time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		},
	}, dbtxrunner.New(db, nil))

	name := " 101 Room Renamed "
	room, err := service.Execute(context.Background(), UpdateRoomInput{
		ID:   "room-1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if room.Name != "101 Room Renamed" {
		t.Fatalf("expected normalized room name, got %q", room.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestMapDomainErrorMapsBadRoomStatusToInternalServerError(t *testing.T) {
	err := mapDomainError(domainproperty.ErrBadRoomStatus)

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != apperr.ErrInternalServerError.Code {
		t.Fatalf("expected %s, got %s", apperr.ErrInternalServerError.Code, appErr.Code)
	}
}

func TestSetRoomMaintenanceServicePublishesEventAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	publisher := &recordingPublisher{}
	service := NewSetRoomMaintenanceService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusVacant,
		},
		updatedRoom: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusMaintenance,
			CreatedAt:  time.Date(2026, 4, 23, 9, 0, 0, 0, time.UTC),
			UpdatedAt:  time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		},
		repairRequest: &RepairRequest{
			ID:          "repair-1",
			PropertyID:  "property-1",
			RoomID:      "room-1",
			SubmittedBy: "user-1",
			Title:       "Leak",
			Description: "Bathroom leak",
			Status:      "submitted",
			SubmittedAt: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
			CreatedAt:   time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		},
	}, dbtxrunner.New(db, publisher))

	result, err := service.Execute(context.Background(), SetRoomMaintenanceInput{
		RoomID:      "room-1",
		OperatorID:  "user-1",
		Title:       "Leak",
		Description: "Bathroom leak",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Room.Status != domainproperty.RoomStatusMaintenance {
		t.Fatalf("expected maintenance status, got %q", result.Room.Status)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(publisher.events))
	}
	if _, ok := publisher.events[0].(domainevents.RoomSetToMaintenance); !ok {
		t.Fatalf("expected RoomSetToMaintenance event, got %T", publisher.events[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSetRoomMaintenanceServiceRejectsOccupiedRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewSetRoomMaintenanceService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusOccupied,
		},
	}, dbtxrunner.New(db, nil))

	_, err = service.Execute(context.Background(), SetRoomMaintenanceInput{
		RoomID:      "room-1",
		OperatorID:  "user-1",
		Title:       "Leak",
		Description: "Bathroom leak",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codeRoomIsOccupied {
		t.Fatalf("expected %s, got %s", codeRoomIsOccupied, appErr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSetRoomMaintenanceServiceRejectsMaintenanceRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewSetRoomMaintenanceService(propertyRepoStub{
		room: &Room{
			ID:         "room-1",
			PropertyID: "property-1",
			Name:       "101 Room",
			Status:     domainproperty.RoomStatusMaintenance,
		},
	}, dbtxrunner.New(db, nil))

	_, err = service.Execute(context.Background(), SetRoomMaintenanceInput{
		RoomID:      "room-1",
		OperatorID:  "user-1",
		Title:       "Leak",
		Description: "Bathroom leak",
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected app error, got %v", err)
	}
	if appErr.Code != codeRoomIsInMaintenance {
		t.Fatalf("expected %s, got %s", codeRoomIsInMaintenance, appErr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
