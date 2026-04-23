package propertyquery

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListRoomsByPropertyReturnsActiveRoomsWithStatusFilterAndPagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, property_id, name, status, size, floor, room_type, facilities::text, default_rent_amount, notes, zone, created_at, updated_at
FROM rooms
WHERE property_id = $1
  AND deleted_at IS NULL
 AND status = $2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`)).
		WithArgs("10000000-0000-0000-0000-000000000001", "vacant", 10, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "property_id", "name", "status", "size", "floor", "room_type", "facilities", "default_rent_amount", "notes", "zone", "created_at", "updated_at",
		}).AddRow(
			"20000000-0000-0000-0000-000000000001",
			"10000000-0000-0000-0000-000000000001",
			"101 Room",
			"vacant",
			10.5,
			"1F",
			"suite",
			`{"ac":true}`,
			12000,
			"Window room",
			"A",
			now,
			now,
		))

	rooms, err := repo.ListRoomsByProperty(context.Background(), "10000000-0000-0000-0000-000000000001", "vacant", 10, 20)
	if err != nil {
		t.Fatalf("ListRoomsByProperty: %v", err)
	}

	if len(rooms) != 1 {
		t.Fatalf("expected 1 room, got %d", len(rooms))
	}
	if rooms[0].Name != "101 Room" {
		t.Fatalf("expected room name 101 Room, got %s", rooms[0].Name)
	}
	if rooms[0].Facilities == nil || (*rooms[0].Facilities)["ac"] != true {
		t.Fatalf("expected facilities to include ac=true, got %#v", rooms[0].Facilities)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindRoomByIDWrapsQueryErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, property_id, name, status, size, floor, room_type, facilities::text, default_rent_amount, notes, zone, created_at, updated_at
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1`)).
		WithArgs("20000000-0000-0000-0000-000000000099").
		WillReturnError(sqlmock.ErrCancelled)

	_, err = repo.FindRoomByID(context.Background(), "20000000-0000-0000-0000-000000000099")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected wrapped query error, got not found")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindRoomByIDMapsNoRowsToRoomNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, property_id, name, status, size, floor, room_type, facilities::text, default_rent_amount, notes, zone, created_at, updated_at
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1`)).
		WithArgs("20000000-0000-0000-0000-000000000099").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "property_id", "name", "status", "size", "floor", "room_type", "facilities", "default_rent_amount", "notes", "zone", "created_at", "updated_at",
		}))

	_, err = repo.FindRoomByID(context.Background(), "20000000-0000-0000-0000-000000000099")
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("expected ErrRoomNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
