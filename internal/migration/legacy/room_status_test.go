package legacy

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReconcileRoomStatusWritesReport(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	reportDir := t.TempDir()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET status = $1,
    updated_at = now()
WHERE deleted_at IS NULL
  AND status <> $1
  AND id IN (
    SELECT DISTINCT room_id
    FROM leases
    WHERE status = $2
      AND deleted_at IS NULL
  )
`)).
		WithArgs(roomStatusOccupied, roomStatusActiveLease).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET status = $1,
    updated_at = now()
WHERE deleted_at IS NULL
  AND status = $2
  AND id NOT IN (
    SELECT DISTINCT room_id
    FROM leases
    WHERE status = $3
      AND deleted_at IS NULL
  )
`)).
		WithArgs(roomStatusVacant, roomStatusOccupied, roomStatusActiveLease).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(DISTINCT room_id)
FROM leases
WHERE status = $1
  AND deleted_at IS NULL
`)).
		WithArgs(roomStatusActiveLease).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT status, COUNT(*)
FROM rooms
WHERE deleted_at IS NULL
GROUP BY status
`)).
		WillReturnRows(
			sqlmock.NewRows([]string{"status", "count"}).
				AddRow(roomStatusOccupied, 2).
				AddRow(roomStatusVacant, 5).
				AddRow(roomStatusMaintenance, 1),
		)
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*)
FROM rooms r
WHERE r.deleted_at IS NULL
  AND (
    (
      EXISTS (
        SELECT 1
        FROM leases l
        WHERE l.room_id = r.id
          AND l.status = $1
          AND l.deleted_at IS NULL
      )
      AND r.status <> $2
    )
    OR
    (
      NOT EXISTS (
        SELECT 1
        FROM leases l
        WHERE l.room_id = r.id
          AND l.status = $1
          AND l.deleted_at IS NULL
      )
      AND r.status = $2
    )
  )
`)).
		WithArgs(roomStatusActiveLease, roomStatusOccupied).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectCommit()

	report, err := ReconcileRoomStatus(context.Background(), db, ReconcileRoomStatusOptions{
		ReportDir: reportDir,
	})
	if err != nil {
		t.Fatalf("ReconcileRoomStatus() error = %v", err)
	}

	if report.RoomsUpdatedToOccupied != 2 {
		t.Fatalf("RoomsUpdatedToOccupied = %d, want 2", report.RoomsUpdatedToOccupied)
	}
	if report.RoomsUpdatedToVacant != 1 {
		t.Fatalf("RoomsUpdatedToVacant = %d, want 1", report.RoomsUpdatedToVacant)
	}
	if report.ActiveLeaseRooms != 2 {
		t.Fatalf("ActiveLeaseRooms = %d, want 2", report.ActiveLeaseRooms)
	}
	if report.OccupiedRooms != 2 {
		t.Fatalf("OccupiedRooms = %d, want 2", report.OccupiedRooms)
	}
	if report.MaintenanceRooms != 1 {
		t.Fatalf("MaintenanceRooms = %d, want 1", report.MaintenanceRooms)
	}
	if report.ImpossibleStatusRows != 0 {
		t.Fatalf("ImpossibleStatusRows = %d, want 0", report.ImpossibleStatusRows)
	}
	if report.ReportPath == "" {
		t.Fatal("ReportPath should not be empty")
	}

	content, err := os.ReadFile(report.ReportPath)
	if err != nil {
		t.Fatalf("os.ReadFile(report.ReportPath) error = %v", err)
	}

	var persisted RoomStatusReconciliationReport
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("json.Unmarshal(report) error = %v", err)
	}
	if persisted.ActiveLeaseRooms != 2 {
		t.Fatalf("persisted.ActiveLeaseRooms = %d, want 2", persisted.ActiveLeaseRooms)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestReconcileRoomStatusFailsVerificationMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET status = $1,
    updated_at = now()
WHERE deleted_at IS NULL
  AND status <> $1
  AND id IN (
    SELECT DISTINCT room_id
    FROM leases
    WHERE status = $2
      AND deleted_at IS NULL
  )
`)).
		WithArgs(roomStatusOccupied, roomStatusActiveLease).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`
UPDATE rooms
SET status = $1,
    updated_at = now()
WHERE deleted_at IS NULL
  AND status = $2
  AND id NOT IN (
    SELECT DISTINCT room_id
    FROM leases
    WHERE status = $3
      AND deleted_at IS NULL
  )
`)).
		WithArgs(roomStatusVacant, roomStatusOccupied, roomStatusActiveLease).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(DISTINCT room_id)
FROM leases
WHERE status = $1
  AND deleted_at IS NULL
`)).
		WithArgs(roomStatusActiveLease).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT status, COUNT(*)
FROM rooms
WHERE deleted_at IS NULL
GROUP BY status
`)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).AddRow(roomStatusOccupied, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT COUNT(*)
FROM rooms r
WHERE r.deleted_at IS NULL
  AND (
    (
      EXISTS (
        SELECT 1
        FROM leases l
        WHERE l.room_id = r.id
          AND l.status = $1
          AND l.deleted_at IS NULL
      )
      AND r.status <> $2
    )
    OR
    (
      NOT EXISTS (
        SELECT 1
        FROM leases l
        WHERE l.room_id = r.id
          AND l.status = $1
          AND l.deleted_at IS NULL
      )
      AND r.status = $2
    )
  )
`)).
		WithArgs(roomStatusActiveLease, roomStatusOccupied).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	_, err = ReconcileRoomStatus(context.Background(), db, ReconcileRoomStatusOptions{
		ReportDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("ReconcileRoomStatus() error = nil, want error")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}
