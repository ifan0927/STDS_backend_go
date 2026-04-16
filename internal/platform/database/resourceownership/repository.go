package resourceownership

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound indicates that no active resource matched the requested lookup.
var ErrNotFound = errors.New("resource property ownership not found")

// Repository resolves resource ids back to their owning property id for
// authorization checks.
type Repository interface {
	FindPropertyIDByRoomID(ctx context.Context, roomID string) (string, error)
	FindPropertyIDByLeaseID(ctx context.Context, leaseID string) (string, error)
	FindPropertyIDByBillID(ctx context.Context, billID string) (string, error)
	FindPropertyIDByJournalLogID(ctx context.Context, journalLogID string) (string, error)
	FindPropertyIDByRepairRequestID(ctx context.Context, repairRequestID string) (string, error)
	FindPropertyIDByForceTerminationID(ctx context.Context, forceTerminationID string) (string, error)
}

// SQLRepository resolves property ids from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) FindPropertyIDByRoomID(ctx context.Context, roomID string) (string, error) {
	const query = `
SELECT property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, roomID, "query room property by id")
}

func (r *SQLRepository) FindPropertyIDByLeaseID(ctx context.Context, leaseID string) (string, error) {
	const query = `
SELECT property_id
FROM leases
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, leaseID, "query lease property by id")
}

func (r *SQLRepository) FindPropertyIDByBillID(ctx context.Context, billID string) (string, error) {
	const query = `
SELECT property_id
FROM bills
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, billID, "query bill property by id")
}

func (r *SQLRepository) FindPropertyIDByJournalLogID(ctx context.Context, journalLogID string) (string, error) {
	const query = `
SELECT property_id
FROM journal_logs
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, journalLogID, "query journal log property by id")
}

func (r *SQLRepository) FindPropertyIDByRepairRequestID(ctx context.Context, repairRequestID string) (string, error) {
	const query = `
SELECT property_id
FROM repair_requests
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, repairRequestID, "query repair request property by id")
}

func (r *SQLRepository) FindPropertyIDByForceTerminationID(ctx context.Context, forceTerminationID string) (string, error) {
	const query = `
SELECT leases.property_id
FROM force_terminations
JOIN leases ON leases.id = force_terminations.lease_id
WHERE force_terminations.id = $1
  AND leases.deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, forceTerminationID, "query force termination property by id")
}

func (r *SQLRepository) findPropertyID(ctx context.Context, query string, id string, op string) (string, error) {
	var propertyID string
	if err := r.db.QueryRowContext(ctx, query, id).Scan(&propertyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return propertyID, nil
}
