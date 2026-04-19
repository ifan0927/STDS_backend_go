package legacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	roomStatusOccupied    = "occupied"
	roomStatusMaintenance = "maintenance"
	roomStatusActiveLease = "active"
)

// ReconcileRoomStatusOptions controls Task 10 execution.
type ReconcileRoomStatusOptions struct {
	ReportDir string
}

// RoomStatusReconciliationReport captures Task 10 execution details.
type RoomStatusReconciliationReport struct {
	GeneratedAt            time.Time `json:"generated_at"`
	ReportPath             string    `json:"report_path"`
	ActiveLeaseRooms       int       `json:"active_lease_rooms"`
	OccupiedRooms          int       `json:"occupied_rooms"`
	VacantRooms            int       `json:"vacant_rooms"`
	MaintenanceRooms       int       `json:"maintenance_rooms"`
	RoomsUpdatedToOccupied int       `json:"rooms_updated_to_occupied"`
	RoomsUpdatedToVacant   int       `json:"rooms_updated_to_vacant"`
	ImpossibleStatusRows   int       `json:"impossible_status_rows"`
	Assumptions            []string  `json:"assumptions"`
}

// ReconcileRoomStatus executes Task 10 against the configured database.
func ReconcileRoomStatus(ctx context.Context, db *sql.DB, options ReconcileRoomStatusOptions) (*RoomStatusReconciliationReport, error) {
	if options.ReportDir == "" {
		return nil, fmt.Errorf("report dir is required")
	}

	report := &RoomStatusReconciliationReport{
		GeneratedAt: time.Now().UTC(),
		Assumptions: []string{
			"Task 7 remains authoritative for the initial room import: rooms start as vacant before this stage runs.",
			"Task 9 lease status remains authoritative for occupancy: any non-deleted room with at least one active migrated lease is reconciled to occupied.",
			"Existing maintenance status is preserved only for rooms without active leases; a room with an active lease cannot remain maintenance after reconciliation.",
		},
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin room status reconciliation tx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	updatedToOccupied, err := markRoomsOccupiedFromActiveLeases(ctx, tx)
	if err != nil {
		return nil, err
	}
	report.RoomsUpdatedToOccupied = updatedToOccupied

	updatedToVacant, err := markRoomsVacantWithoutActiveLeases(ctx, tx)
	if err != nil {
		return nil, err
	}
	report.RoomsUpdatedToVacant = updatedToVacant

	report.ActiveLeaseRooms, err = countDistinctRoomsWithActiveLeases(ctx, tx)
	if err != nil {
		return nil, err
	}

	report.OccupiedRooms, report.VacantRooms, report.MaintenanceRooms, err = loadRoomStatusCounts(ctx, tx)
	if err != nil {
		return nil, err
	}

	report.ImpossibleStatusRows, err = countImpossibleRoomStatusRows(ctx, tx)
	if err != nil {
		return nil, err
	}

	if report.OccupiedRooms != report.ActiveLeaseRooms {
		return nil, fmt.Errorf(
			"room status reconciliation verification failed: occupied rooms = %d, active lease rooms = %d",
			report.OccupiedRooms,
			report.ActiveLeaseRooms,
		)
	}
	if report.ImpossibleStatusRows != 0 {
		return nil, fmt.Errorf(
			"room status reconciliation verification failed: impossible_status_rows = %d",
			report.ImpossibleStatusRows,
		)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit room status reconciliation: %w", err)
	}
	committed = true

	reportPath, err := writeRoomStatusReconciliationReport(options.ReportDir, report)
	if err != nil {
		return nil, err
	}
	report.ReportPath = reportPath

	return report, nil
}

func markRoomsOccupiedFromActiveLeases(ctx context.Context, tx *sql.Tx) (int, error) {
	const query = `
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
`

	result, err := tx.ExecContext(ctx, query, roomStatusOccupied, roomStatusActiveLease)
	if err != nil {
		return 0, fmt.Errorf("mark rooms occupied from active leases: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read occupied reconciliation rows affected: %w", err)
	}

	return int(rows), nil
}

func markRoomsVacantWithoutActiveLeases(ctx context.Context, tx *sql.Tx) (int, error) {
	const query = `
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
`

	result, err := tx.ExecContext(ctx, query, roomStatusVacant, roomStatusOccupied, roomStatusActiveLease)
	if err != nil {
		return 0, fmt.Errorf("mark rooms vacant without active leases: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read vacant reconciliation rows affected: %w", err)
	}

	return int(rows), nil
}

func countDistinctRoomsWithActiveLeases(ctx context.Context, tx *sql.Tx) (int, error) {
	const query = `
SELECT COUNT(DISTINCT room_id)
FROM leases
WHERE status = $1
  AND deleted_at IS NULL
`

	var count int
	if err := tx.QueryRowContext(ctx, query, roomStatusActiveLease).Scan(&count); err != nil {
		return 0, fmt.Errorf("count distinct rooms with active leases: %w", err)
	}

	return count, nil
}

func loadRoomStatusCounts(ctx context.Context, tx *sql.Tx) (int, int, int, error) {
	const query = `
SELECT status, COUNT(*)
FROM rooms
WHERE deleted_at IS NULL
GROUP BY status
`

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query room status counts: %w", err)
	}
	defer rows.Close()

	var occupied int
	var vacant int
	var maintenance int

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, 0, fmt.Errorf("scan room status count: %w", err)
		}

		switch status {
		case roomStatusOccupied:
			occupied = count
		case roomStatusVacant:
			vacant = count
		case roomStatusMaintenance:
			maintenance = count
		}
	}

	if err := rows.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("iterate room status counts: %w", err)
	}

	return occupied, vacant, maintenance, nil
}

func countImpossibleRoomStatusRows(ctx context.Context, tx *sql.Tx) (int, error) {
	const query = `
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
`

	var count int
	if err := tx.QueryRowContext(ctx, query, roomStatusActiveLease, roomStatusOccupied).Scan(&count); err != nil {
		return 0, fmt.Errorf("count impossible room status rows: %w", err)
	}

	return count, nil
}

func writeRoomStatusReconciliationReport(reportDir string, report *RoomStatusReconciliationReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}

	path := filepath.Join(reportDir, "task10_room_status_report.json")
	report.ReportPath = path
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal room status reconciliation report: %w", err)
	}

	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write room status reconciliation report: %w", err)
	}

	return path, nil
}
