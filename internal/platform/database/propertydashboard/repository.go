package propertydashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	appproperty "stds_backend/internal/application/property"
)

// SQLRepository reads the property dashboard from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a PostgreSQL-backed dashboard repository.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// GetDashboard returns one property's dashboard read model.
func (r *SQLRepository) GetDashboard(ctx context.Context, propertyID string, year int, month int) (*appproperty.Dashboard, error) {
	if err := r.ensurePropertyExists(ctx, propertyID); err != nil {
		return nil, err
	}

	rooms, err := r.listRooms(ctx, propertyID)
	if err != nil {
		return nil, err
	}
	summary, err := r.getMonthlySummary(ctx, propertyID, year, month)
	if err != nil {
		return nil, err
	}
	journals, err := r.listRecentJournals(ctx, propertyID)
	if err != nil {
		return nil, err
	}

	return &appproperty.Dashboard{
		PropertyID:     propertyID,
		Rooms:          rooms,
		MonthlySummary: summary,
		RecentJournals: journals,
	}, nil
}

func (r *SQLRepository) ensurePropertyExists(ctx context.Context, propertyID string) error {
	const query = `
SELECT 1
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`
	var exists int
	if err := r.db.QueryRowContext(ctx, query, propertyID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appproperty.ErrPropertyNotFound
		}
		return fmt.Errorf("query dashboard property: %w", err)
	}

	return nil
}

func (r *SQLRepository) listRooms(ctx context.Context, propertyID string) ([]appproperty.DashboardRoom, error) {
	const query = `
SELECT id, name, status
FROM rooms
WHERE property_id = $1
  AND deleted_at IS NULL
ORDER BY created_at DESC, id DESC
`
	rows, err := r.db.QueryContext(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list dashboard rooms: %w", err)
	}
	defer rows.Close()

	rooms := make([]appproperty.DashboardRoom, 0)
	for rows.Next() {
		var room appproperty.DashboardRoom
		if err := rows.Scan(&room.ID, &room.Name, &room.Status); err != nil {
			return nil, fmt.Errorf("scan dashboard room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard rooms: %w", err)
	}

	return rooms, nil
}

func (r *SQLRepository) getMonthlySummary(ctx context.Context, propertyID string, year int, month int) (appproperty.DashboardMonthlySummary, error) {
	periodStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	const query = `
SELECT
	COALESCE(SUM(CASE WHEN type = 'rent' AND status NOT IN ('voided', 'written_off') AND period_start >= $2 AND period_start < $3 THEN amount ELSE 0 END), 0) AS expected_rent,
	COALESCE(SUM(CASE WHEN type = 'rent' AND period_start >= $2 AND period_start < $3 AND status = 'paid' THEN paid_amount ELSE 0 END), 0) AS collected_rent,
	COUNT(*) FILTER (WHERE status = 'overdue' AND period_start >= $2 AND period_start < $3) AS overdue_bill_count
FROM bills
WHERE property_id = $1
  AND deleted_at IS NULL
`
	var summary appproperty.DashboardMonthlySummary
	if err := r.db.QueryRowContext(ctx, query, propertyID, periodStart, periodEnd).Scan(
		&summary.ExpectedRent,
		&summary.CollectedRent,
		&summary.OverdueBillCount,
	); err != nil {
		return appproperty.DashboardMonthlySummary{}, fmt.Errorf("query dashboard monthly summary: %w", err)
	}

	return summary, nil
}

func (r *SQLRepository) listRecentJournals(ctx context.Context, propertyID string) ([]appproperty.DashboardRecentJournal, error) {
	const query = `
SELECT id, type, content, created_at
FROM (
	SELECT id, 'journal_log' AS type, content, created_at
	FROM journal_logs
	WHERE property_id = $1
	  AND deleted_at IS NULL
	UNION ALL
	SELECT id, 'repair_request' AS type, title AS content, created_at
	FROM repair_requests
	WHERE property_id = $1
	  AND deleted_at IS NULL
) recent_activity
ORDER BY created_at DESC, id DESC
LIMIT 5
`
	rows, err := r.db.QueryContext(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list dashboard recent journals: %w", err)
	}
	defer rows.Close()

	journals := make([]appproperty.DashboardRecentJournal, 0)
	for rows.Next() {
		var journal appproperty.DashboardRecentJournal
		if err := rows.Scan(&journal.ID, &journal.Type, &journal.Content, &journal.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan dashboard recent journal: %w", err)
		}
		journals = append(journals, journal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard recent journals: %w", err)
	}

	return journals, nil
}
