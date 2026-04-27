package repair

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	apprepair "stds_backend/internal/application/repair"
)

func TestListScopesOrganizerToAssignedPropertiesAndFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("FROM repair_requests rr")).
		WithArgs(testPropertyID, testPropertyID, testRoomID, "submitted", testStaffID, 20, 0).
		WillReturnRows(repairRows().AddRow(
			testRepairID,
			testPropertyID,
			testRoomID,
			testStaffID,
			nil,
			"Leak",
			"Bathroom leak",
			"submitted",
			now,
			nil,
			nil,
			nil,
			now,
			now,
		))

	repo := NewRepository(db)
	status := "submitted"
	roomID := testRoomID
	assignedTo := testStaffID
	propertyID := testPropertyID
	items, err := repo.List(context.Background(), apprepair.ListQuery{
		ActorRole:           "organizer",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          &propertyID,
		RoomID:              &roomID,
		Status:              &status,
		AssignedTo:          &assignedTo,
		Limit:               20,
		Offset:              0,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != testRepairID {
		t.Fatalf("unexpected items: %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCancelPersistsCancelReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	cancelReason := "owner deferred"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE repair_requests")).
		WithArgs(testRepairID, cancelReason).
		WillReturnRows(repairRows().AddRow(
			testRepairID,
			testPropertyID,
			testRoomID,
			testStaffID,
			nil,
			"Leak",
			"Bathroom leak",
			"cancelled",
			now,
			nil,
			nil,
			cancelReason,
			now,
			now,
		))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	repo := NewRepository(db)
	repairRequest, err := repo.Cancel(context.Background(), tx, apprepair.CancelParams{
		ID:           testRepairID,
		CancelReason: &cancelReason,
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if repairRequest.CancelReason == nil || *repairRequest.CancelReason != cancelReason {
		t.Fatalf("expected cancel reason %q, got %#v", cancelReason, repairRequest.CancelReason)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestFindUserByIDLoadsAssignedPropertyIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(testStaffID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"role",
			"assigned_property_ids",
		}).AddRow(
			testStaffID,
			"staff",
			`["`+testPropertyID+`"]`,
		))
	mock.ExpectCommit()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	repo := NewRepository(db)
	user, err := repo.FindUserByID(context.Background(), tx, testStaffID)
	if err != nil {
		t.Fatalf("FindUserByID: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(user.AssignedPropertyIDs) != 1 || user.AssignedPropertyIDs[0] != testPropertyID {
		t.Fatalf("unexpected assigned property ids: %#v", user.AssignedPropertyIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func repairRows() *sqlmock.Rows {
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
		"cancel_reason",
		"created_at",
		"updated_at",
	})
}

const (
	testRepairID   = "70000000-0000-0000-0000-000000000001"
	testPropertyID = "10000000-0000-0000-0000-000000000001"
	testRoomID     = "20000000-0000-0000-0000-000000000001"
	testStaffID    = "00000000-0000-0000-0000-000000000002"
)
