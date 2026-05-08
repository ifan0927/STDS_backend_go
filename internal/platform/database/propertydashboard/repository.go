package propertydashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

	occupancy, err := r.getOccupancySummary(ctx, propertyID)
	if err != nil {
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
		Occupancy:      occupancy,
		RecentJournals: journals,
	}, nil
}

// GetHomeDashboard returns cross-property dashboard summaries for the caller's scope.
func (r *SQLRepository) GetHomeDashboard(ctx context.Context, scope appproperty.DashboardScope, year int, month int) (*appproperty.HomeDashboard, error) {
	summaries, err := r.listHomePropertySummaries(ctx, scope, year, month)
	if err != nil {
		return nil, err
	}
	journals, err := r.listHomeRecentJournals(ctx, scope)
	if err != nil {
		return nil, err
	}

	dashboard := appproperty.HomeDashboard{
		PropertySummaries: summaries,
		RecentJournals:    journals,
	}
	for i := range summaries {
		dashboard.PortfolioSummary.TotalRooms += summaries[i].Occupancy.TotalRooms
		dashboard.PortfolioSummary.OccupiedRooms += summaries[i].Occupancy.OccupiedRooms
		dashboard.PortfolioSummary.VacantRooms += summaries[i].Occupancy.VacantRooms
		dashboard.PortfolioSummary.MaintenanceRooms += summaries[i].Occupancy.MaintenanceRooms
		dashboard.MonthlyBillingSummary.ExpectedRent += summaries[i].MonthlySummary.ExpectedRent
		dashboard.MonthlyBillingSummary.CollectedRent += summaries[i].MonthlySummary.CollectedRent
		dashboard.MonthlyBillingSummary.OverdueBillCount += summaries[i].MonthlySummary.OverdueBillCount
	}
	if dashboard.PortfolioSummary.TotalRooms > 0 {
		dashboard.PortfolioSummary.OccupancyRate = float64(dashboard.PortfolioSummary.OccupiedRooms) / float64(dashboard.PortfolioSummary.TotalRooms)
	}

	return &dashboard, nil
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

func (r *SQLRepository) getOccupancySummary(ctx context.Context, propertyID string) (appproperty.OccupancySummary, error) {
	const query = `
SELECT
	COUNT(*)::int AS total_rooms,
	COUNT(*) FILTER (WHERE status = 'occupied')::int AS occupied_rooms,
	COUNT(*) FILTER (WHERE status = 'vacant')::int AS vacant_rooms,
	COUNT(*) FILTER (WHERE status = 'maintenance')::int AS maintenance_rooms,
	CASE WHEN COUNT(*) = 0 THEN 0 ELSE COUNT(*) FILTER (WHERE status = 'occupied')::float / COUNT(*)::float END AS occupancy_rate
FROM rooms
WHERE property_id = $1
  AND deleted_at IS NULL
`
	var summary appproperty.OccupancySummary
	if err := r.db.QueryRowContext(ctx, query, propertyID).Scan(
		&summary.TotalRooms,
		&summary.OccupiedRooms,
		&summary.VacantRooms,
		&summary.MaintenanceRooms,
		&summary.OccupancyRate,
	); err != nil {
		return appproperty.OccupancySummary{}, fmt.Errorf("query dashboard occupancy summary: %w", err)
	}

	return summary, nil
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

func (r *SQLRepository) listHomePropertySummaries(ctx context.Context, scope appproperty.DashboardScope, year int, month int) ([]appproperty.HomeDashboardPropertySummary, error) {
	periodStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	args := []any{periodStart, periodEnd}
	scopeClause, args, ok := appendDashboardScope("p", scope, args)
	if !ok {
		return []appproperty.HomeDashboardPropertySummary{}, nil
	}

	query := `
WITH room_summary AS (
	SELECT
		property_id,
		COUNT(*)::int AS total_rooms,
		COUNT(*) FILTER (WHERE status = 'occupied')::int AS occupied_rooms,
		COUNT(*) FILTER (WHERE status = 'vacant')::int AS vacant_rooms,
		COUNT(*) FILTER (WHERE status = 'maintenance')::int AS maintenance_rooms
	FROM rooms
	WHERE deleted_at IS NULL
	GROUP BY property_id
),
bill_summary AS (
	SELECT
		property_id,
		COALESCE(SUM(CASE WHEN type = 'rent' AND status NOT IN ('voided', 'written_off') AND period_start >= $1 AND period_start < $2 THEN amount ELSE 0 END), 0)::int AS expected_rent,
		COALESCE(SUM(CASE WHEN type = 'rent' AND status = 'paid' AND period_start >= $1 AND period_start < $2 THEN paid_amount ELSE 0 END), 0)::int AS collected_rent,
		COUNT(*) FILTER (WHERE status = 'overdue' AND period_start >= $1 AND period_start < $2)::int AS overdue_bill_count
	FROM bills
	WHERE deleted_at IS NULL
	GROUP BY property_id
)
SELECT
	p.id,
	p.name,
	COALESCE(room_summary.total_rooms, 0) AS total_rooms,
	COALESCE(room_summary.occupied_rooms, 0) AS occupied_rooms,
	COALESCE(room_summary.vacant_rooms, 0) AS vacant_rooms,
	COALESCE(room_summary.maintenance_rooms, 0) AS maintenance_rooms,
	CASE WHEN COALESCE(room_summary.total_rooms, 0) = 0 THEN 0 ELSE room_summary.occupied_rooms::float / room_summary.total_rooms::float END AS occupancy_rate,
	COALESCE(bill_summary.expected_rent, 0) AS expected_rent,
	COALESCE(bill_summary.collected_rent, 0) AS collected_rent,
	COALESCE(bill_summary.overdue_bill_count, 0) AS overdue_bill_count
FROM properties p
LEFT JOIN room_summary ON room_summary.property_id = p.id
LEFT JOIN bill_summary ON bill_summary.property_id = p.id
WHERE p.deleted_at IS NULL
` + scopeClause + `
ORDER BY p.created_at DESC, p.id DESC
`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list home dashboard property summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]appproperty.HomeDashboardPropertySummary, 0)
	for rows.Next() {
		var summary appproperty.HomeDashboardPropertySummary
		if err := rows.Scan(
			&summary.PropertyID,
			&summary.PropertyName,
			&summary.Occupancy.TotalRooms,
			&summary.Occupancy.OccupiedRooms,
			&summary.Occupancy.VacantRooms,
			&summary.Occupancy.MaintenanceRooms,
			&summary.Occupancy.OccupancyRate,
			&summary.MonthlySummary.ExpectedRent,
			&summary.MonthlySummary.CollectedRent,
			&summary.MonthlySummary.OverdueBillCount,
		); err != nil {
			return nil, fmt.Errorf("scan home dashboard property summary: %w", err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate home dashboard property summaries: %w", err)
	}

	return summaries, nil
}

func (r *SQLRepository) listHomeRecentJournals(ctx context.Context, scope appproperty.DashboardScope) ([]appproperty.HomeDashboardRecentJournal, error) {
	args := []any{}
	journalScopeClause, args, ok := appendDashboardScope("p", scope, args)
	if !ok {
		return []appproperty.HomeDashboardRecentJournal{}, nil
	}
	repairScopeClause := strings.ReplaceAll(journalScopeClause, "p.", "rp.")

	query := `
SELECT id, property_id, property_name, type, content, created_at
FROM (
	SELECT jl.id, jl.property_id, p.name AS property_name, 'journal_log' AS type, jl.content, jl.created_at
	FROM journal_logs jl
	JOIN properties p ON p.id = jl.property_id AND p.deleted_at IS NULL
	WHERE jl.deleted_at IS NULL
` + journalScopeClause + `
	UNION ALL
	SELECT rr.id, rr.property_id, rp.name AS property_name, 'repair_request' AS type, rr.title AS content, rr.created_at
	FROM repair_requests rr
	JOIN properties rp ON rp.id = rr.property_id AND rp.deleted_at IS NULL
	WHERE rr.deleted_at IS NULL
` + repairScopeClause + `
) recent_activity
ORDER BY created_at DESC, id DESC
LIMIT 5
`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list home dashboard recent journals: %w", err)
	}
	defer rows.Close()

	journals := make([]appproperty.HomeDashboardRecentJournal, 0)
	for rows.Next() {
		var journal appproperty.HomeDashboardRecentJournal
		if err := rows.Scan(
			&journal.ID,
			&journal.PropertyID,
			&journal.PropertyName,
			&journal.Type,
			&journal.Content,
			&journal.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan home dashboard recent journal: %w", err)
		}
		journals = append(journals, journal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate home dashboard recent journals: %w", err)
	}

	return journals, nil
}

func appendDashboardScope(alias string, scope appproperty.DashboardScope, args []any) (string, []any, bool) {
	switch scope.Role {
	case "owner":
		args = append(args, scope.UserID)
		return fmt.Sprintf(" AND %s.owner_id = $%d", alias, len(args)), args, true
	case "organizer", "staff":
		if len(scope.AssignedPropertyIDs) == 0 {
			return "", args, false
		}
		placeholders := make([]string, 0, len(scope.AssignedPropertyIDs))
		for _, propertyID := range scope.AssignedPropertyIDs {
			args = append(args, propertyID)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		return fmt.Sprintf(" AND %s.id IN (%s)", alias, strings.Join(placeholders, ", ")), args, true
	default:
		return "", args, true
	}
}
