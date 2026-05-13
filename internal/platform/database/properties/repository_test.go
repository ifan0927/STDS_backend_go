package properties

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindOwnerIDByPropertyIDReturnsOwnerID(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT owner_id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("property-1").
		WillReturnRows(sqlmock.NewRows([]string{"owner_id"}).AddRow("owner-1"))

	ownerID, err := repo.FindOwnerIDByPropertyID(context.Background(), "property-1")
	if err != nil {
		t.Fatalf("FindOwnerIDByPropertyID: %v", err)
	}
	if ownerID != "owner-1" {
		t.Fatalf("ownerID = %q, want owner-1", ownerID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindOwnerIDByPropertyIDMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT owner_id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("property-404").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindOwnerIDByPropertyID(context.Background(), "property-404")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreatePersistsPropertyInTransaction(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	subtitle := "North wing"
	contactPhone := "02-1234-5678"
	contactEmail := "owner@example.com"
	notes := "Managed property"
	facilities := map[string]interface{}{"elevator": true}

	mock.ExpectQuery("INSERT INTO properties").
		WithArgs("Green Villa", "Green Villa Public", &subtitle, "Taipei", 4.5, "monthly", "owner-1", &contactPhone, &contactEmail, &notes, `{"elevator":true}`).
		WillReturnRows(propertyRows().AddRow(
			"property-1",
			"Green Villa",
			"Green Villa Public",
			subtitle,
			"Taipei",
			4.5,
			"monthly",
			"owner-1",
			contactPhone,
			contactEmail,
			notes,
			`{"elevator":true}`,
			createdAt,
			updatedAt,
			1,
		))

	property, err := repo.Create(context.Background(), tx, CreatePropertyParams{
		Name:                             "Green Villa",
		PropertyPublicName:               "Green Villa Public",
		Subtitle:                         &subtitle,
		Address:                          "Taipei",
		ElectricityUnitPrice:             4.5,
		DefaultElectricityBillingCadence: "monthly",
		OwnerID:                          "owner-1",
		ContactPhone:                     &contactPhone,
		ContactEmail:                     &contactEmail,
		Notes:                            &notes,
		Facilities:                       &facilities,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if property.ID != "property-1" || property.Name != "Green Villa" || property.PropertyPublicName != "Green Villa Public" || property.Address != "Taipei" || property.OwnerID != "owner-1" {
		t.Fatalf("unexpected property: %+v", property)
	}
	if property.ElectricityUnitPrice == nil || *property.ElectricityUnitPrice != 4.5 {
		t.Fatalf("ElectricityUnitPrice = %v, want 4.5", property.ElectricityUnitPrice)
	}
	if property.DefaultElectricityBillingCadence != "monthly" {
		t.Fatalf("DefaultElectricityBillingCadence = %q, want monthly", property.DefaultElectricityBillingCadence)
	}
	if property.Subtitle == nil || *property.Subtitle != subtitle || property.ContactEmail == nil || *property.ContactEmail != contactEmail {
		t.Fatalf("unexpected optional fields: %+v", property)
	}
	if property.Facilities == nil || (*property.Facilities)["elevator"] != true {
		t.Fatalf("unexpected facilities: %#v", property.Facilities)
	}
	if property.CreatedAt != createdAt || property.UpdatedAt != updatedAt || property.Version != 1 {
		t.Fatalf("unexpected audit fields: %+v", property)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDScansNullableElectricityUnitPrice(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	mock.ExpectQuery("SELECT").
		WithArgs("property-1").
		WillReturnRows(propertyRows().AddRow(
			"property-1",
			"Green Villa",
			"Green Villa",
			nil,
			"Taipei",
			nil,
			"bimonthly",
			"owner-1",
			nil,
			nil,
			nil,
			nil,
			createdAt,
			updatedAt,
			3,
		))

	property, err := repo.FindByID(context.Background(), tx, "property-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if property.ElectricityUnitPrice != nil {
		t.Fatalf("ElectricityUnitPrice = %v, want nil", property.ElectricityUnitPrice)
	}
	if property.DefaultElectricityBillingCadence != "bimonthly" {
		t.Fatalf("DefaultElectricityBillingCadence = %q, want bimonthly", property.DefaultElectricityBillingCadence)
	}
	if property.Version != 3 {
		t.Fatalf("Version = %d, want 3", property.Version)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectQuery("SELECT").
		WithArgs("property-404").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), tx, "property-404")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdatePersistsPropertyWithNullableElectricityUnitPrice(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	mock.ExpectQuery("UPDATE properties").
		WithArgs("property-1", "Green Villa Updated", "Public Green Villa", nil, "New Taipei", nil, "bimonthly", "owner-2", nil, nil, nil, nil, 3).
		WillReturnRows(propertyRows().AddRow(
			"property-1",
			"Green Villa Updated",
			"Public Green Villa",
			nil,
			"New Taipei",
			nil,
			"bimonthly",
			"owner-2",
			nil,
			nil,
			nil,
			nil,
			createdAt,
			updatedAt,
			4,
		))

	property, err := repo.Update(context.Background(), tx, UpdatePropertyParams{
		ID:                               "property-1",
		Name:                             "Green Villa Updated",
		PropertyPublicName:               "Public Green Villa",
		Address:                          "New Taipei",
		ElectricityUnitPrice:             nil,
		DefaultElectricityBillingCadence: "bimonthly",
		OwnerID:                          "owner-2",
		Version:                          3,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if property.Name != "Green Villa Updated" || property.PropertyPublicName != "Public Green Villa" || property.Address != "New Taipei" || property.OwnerID != "owner-2" {
		t.Fatalf("unexpected property: %+v", property)
	}
	if property.ElectricityUnitPrice != nil {
		t.Fatalf("ElectricityUnitPrice = %v, want nil", property.ElectricityUnitPrice)
	}
	if property.DefaultElectricityBillingCadence != "bimonthly" || property.Version != 4 {
		t.Fatalf("unexpected cadence/version: %+v", property)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	unitPrice := 5.25

	mock.ExpectQuery("UPDATE properties").
		WithArgs("property-404", "Green Villa", "Green Villa", nil, "Taipei", &unitPrice, "monthly", "owner-1", nil, nil, nil, nil, 9).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.Update(context.Background(), tx, UpdatePropertyParams{
		ID:                               "property-404",
		Name:                             "Green Villa",
		PropertyPublicName:               "Green Villa",
		Address:                          "Taipei",
		ElectricityUnitPrice:             &unitPrice,
		DefaultElectricityBillingCadence: "monthly",
		OwnerID:                          "owner-1",
		Version:                          9,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListOccupiedRoomIDsReturnsIDsInTransaction(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM rooms
WHERE property_id = $1
  AND status = 'occupied'
  AND deleted_at IS NULL
ORDER BY id
`)).
		WithArgs("property-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("room-1").AddRow("room-2"))

	roomIDs, err := repo.ListOccupiedRoomIDs(context.Background(), tx, "property-1")
	if err != nil {
		t.Fatalf("ListOccupiedRoomIDs: %v", err)
	}
	if len(roomIDs) != 2 || roomIDs[0] != "room-1" || roomIDs[1] != "room-2" {
		t.Fatalf("unexpected room ids: %+v", roomIDs)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSoftDeleteReturnsNotFoundWhenNoRowsAffected(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE properties
SET deleted_at = now(),
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
`)).
		WithArgs("property-404", 3).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.SoftDelete(context.Background(), tx, "property-404", 3)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateRoomPersistsRoomInTransaction(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	size := 12.5
	floor := "2F"
	roomType := "suite"
	facilities := map[string]interface{}{"balcony": true}
	defaultRent := 18000
	notes := "Bright room"
	zone := "A"

	mock.ExpectQuery("INSERT INTO rooms").
		WithArgs("property-1", "Room A", &size, &floor, &roomType, `{"balcony":true}`, &defaultRent, &notes, &zone).
		WillReturnRows(roomRows().AddRow("room-1", "property-1", "Room A", "vacant", size, floor, roomType, `{"balcony":true}`, defaultRent, notes, zone, createdAt, updatedAt))

	room, err := repo.CreateRoom(context.Background(), tx, CreateRoomParams{
		PropertyID:        "property-1",
		Name:              "Room A",
		Size:              &size,
		Floor:             &floor,
		RoomType:          &roomType,
		Facilities:        &facilities,
		DefaultRentAmount: &defaultRent,
		Notes:             &notes,
		Zone:              &zone,
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID != "room-1" || room.PropertyID != "property-1" || room.Name != "Room A" || room.Status != "vacant" {
		t.Fatalf("unexpected room: %+v", room)
	}
	if room.Size == nil || *room.Size != size || room.DefaultRentAmount == nil || *room.DefaultRentAmount != defaultRent {
		t.Fatalf("unexpected room writable fields: %+v", room)
	}
	if room.Facilities == nil || (*room.Facilities)["balcony"] != true {
		t.Fatalf("unexpected room facilities: %#v", room.Facilities)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindRoomByIDReturnsRoom(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	mock.ExpectQuery("SELECT").
		WithArgs("room-1").
		WillReturnRows(roomRows().AddRow("room-1", "property-1", "Room A", "occupied", nil, nil, nil, nil, nil, nil, nil, createdAt, updatedAt))

	room, err := repo.FindRoomByID(context.Background(), tx, "room-1")
	if err != nil {
		t.Fatalf("FindRoomByID: %v", err)
	}
	if room.ID != "room-1" || room.PropertyID != "property-1" || room.Status != "occupied" {
		t.Fatalf("unexpected room: %+v", room)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateRoomUsesEmptyStatusWhenStatusIsNil(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	mock.ExpectQuery("UPDATE rooms").
		WithArgs("room-1", "Room A Updated", "", nil, nil, nil, nil, nil, nil, nil).
		WillReturnRows(roomRows().AddRow("room-1", "property-1", "Room A Updated", "occupied", nil, nil, nil, nil, nil, nil, nil, createdAt, updatedAt))

	room, err := repo.UpdateRoom(context.Background(), tx, UpdateRoomParams{
		ID:   "room-1",
		Name: "Room A Updated",
	})
	if err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}
	if room.Name != "Room A Updated" || room.Status != "occupied" {
		t.Fatalf("unexpected room: %+v", room)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateRoomUsesProvidedStatusWhenStatusIsNonNil(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	status := "maintenance"

	mock.ExpectQuery("UPDATE rooms").
		WithArgs("room-1", "Room A", "maintenance", nil, nil, nil, nil, nil, nil, nil).
		WillReturnRows(roomRows().AddRow("room-1", "property-1", "Room A", "maintenance", nil, nil, nil, nil, nil, nil, nil, createdAt, updatedAt))

	room, err := repo.UpdateRoom(context.Background(), tx, UpdateRoomParams{
		ID:     "room-1",
		Name:   "Room A",
		Status: &status,
	})
	if err != nil {
		t.Fatalf("UpdateRoom: %v", err)
	}
	if room.Status != "maintenance" {
		t.Fatalf("Status = %q, want maintenance", room.Status)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateRoomMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectQuery("UPDATE rooms").
		WithArgs("room-404", "Room Missing", "", nil, nil, nil, nil, nil, nil, nil).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.UpdateRoom(context.Background(), tx, UpdateRoomParams{
		ID:   "room-404",
		Name: "Room Missing",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSoftDeleteRoomReturnsNotFoundWhenNoRowsAffected(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET deleted_at = now(),
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`)).
		WithArgs("room-404").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.SoftDeleteRoom(context.Background(), tx, "room-404")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSoftDeleteRoomMarksRoomDeleted(t *testing.T) {
	db, mock, repo := newPropertyRepoTest(t)
	defer closePropertyDB(t, db)
	tx := beginPropertyTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET deleted_at = now(),
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`)).
		WithArgs("room-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.SoftDeleteRoom(context.Background(), tx, "room-1"); err != nil {
		t.Fatalf("SoftDeleteRoom: %v", err)
	}

	mock.ExpectCommit()
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateRepairRequestScansNullableAssignmentFields(t *testing.T) {
	tests := []struct {
		name            string
		assignedTo      any
		assignedAt      any
		completedAt     any
		wantAssignedTo  *string
		wantAssignedAt  *time.Time
		wantCompletedAt *time.Time
		wantStatus      string
	}{
		{
			name:        "null assignment fields",
			assignedTo:  nil,
			assignedAt:  nil,
			completedAt: nil,
			wantStatus:  "open",
		},
		{
			name:            "populated assignment fields",
			assignedTo:      "staff-1",
			assignedAt:      time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC),
			completedAt:     time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
			wantAssignedTo:  stringPtr("staff-1"),
			wantAssignedAt:  timePtr(time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)),
			wantCompletedAt: timePtr(time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)),
			wantStatus:      "completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, repo := newPropertyRepoTest(t)
			defer closePropertyDB(t, db)
			tx := beginPropertyTx(t, db, mock)
			submittedAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
			createdAt := submittedAt.Add(time.Minute)
			updatedAt := createdAt.Add(time.Minute)

			mock.ExpectQuery(regexp.QuoteMeta(`
INSERT INTO repair_requests (
	property_id,
	room_id,
	submitted_by,
	title,
	description
) VALUES ($1, $2, $3, $4, $5)
RETURNING
	id,
	property_id,
	room_id,
	submitted_by,
	assigned_to,
	title,
	description,
	status,
	submitted_at,
	assigned_at,
	completed_at,
	created_at,
	updated_at
`)).
				WithArgs("property-1", "room-1", "tenant-1", "Leaking sink", "Kitchen sink leaks").
				WillReturnRows(repairRequestRows().AddRow(
					"repair-1",
					"property-1",
					"room-1",
					"tenant-1",
					tt.assignedTo,
					"Leaking sink",
					"Kitchen sink leaks",
					tt.wantStatus,
					submittedAt,
					tt.assignedAt,
					tt.completedAt,
					createdAt,
					updatedAt,
				))

			repairRequest, err := repo.CreateRepairRequest(context.Background(), tx, CreateRepairRequestParams{
				PropertyID:  "property-1",
				RoomID:      "room-1",
				SubmittedBy: "tenant-1",
				Title:       "Leaking sink",
				Description: "Kitchen sink leaks",
			})
			if err != nil {
				t.Fatalf("CreateRepairRequest: %v", err)
			}
			if repairRequest.ID != "repair-1" || repairRequest.PropertyID != "property-1" || repairRequest.RoomID != "room-1" || repairRequest.SubmittedBy != "tenant-1" {
				t.Fatalf("unexpected repair request: %+v", repairRequest)
			}
			assertStringPtr(t, "AssignedTo", repairRequest.AssignedTo, tt.wantAssignedTo)
			assertTimePtr(t, "AssignedAt", repairRequest.AssignedAt, tt.wantAssignedAt)
			assertTimePtr(t, "CompletedAt", repairRequest.CompletedAt, tt.wantCompletedAt)
			if repairRequest.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", repairRequest.Status, tt.wantStatus)
			}

			mock.ExpectCommit()
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func newPropertyRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closePropertyDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func beginPropertyTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return tx
}

func propertyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"name",
		"property_public_name",
		"subtitle",
		"address",
		"electricity_unit_price",
		"default_electricity_billing_cadence",
		"owner_id",
		"contact_phone",
		"contact_email",
		"notes",
		"facilities",
		"created_at",
		"updated_at",
		"version",
	})
}

func roomRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"property_id",
		"name",
		"status",
		"size",
		"floor",
		"room_type",
		"facilities",
		"default_rent_amount",
		"notes",
		"zone",
		"created_at",
		"updated_at",
	})
}

func repairRequestRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"property_id",
		"room_id",
		"submitted_by",
		"assigned_to",
		"title",
		"description",
		"status",
		"submitted_at",
		"assigned_at",
		"completed_at",
		"created_at",
		"updated_at",
	})
}

func stringPtr(value string) *string {
	return &value
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func assertStringPtr(t *testing.T, name string, got, want *string) {
	t.Helper()

	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if *got != *want {
		t.Fatalf("%s = %q, want %q", name, *got, *want)
	}
}

func assertTimePtr(t *testing.T, name string, got, want *time.Time) {
	t.Helper()

	if got == nil || want == nil {
		if got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
		return
	}
	if !got.Equal(*want) {
		t.Fatalf("%s = %v, want %v", name, *got, *want)
	}
}
