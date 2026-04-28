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
	FindPropertyIDByPropertyID(ctx context.Context, propertyID string) (string, error)
	FindPropertyIDByRoomID(ctx context.Context, roomID string) (string, error)
	FindPropertyIDByTenantID(ctx context.Context, tenantID string) (string, error)
	FindPropertyIDByLeaseID(ctx context.Context, leaseID string) (string, error)
	FindPropertyIDByBillID(ctx context.Context, billID string) (string, error)
	FindPropertyIDByJournalLogID(ctx context.Context, journalLogID string) (string, error)
	FindPropertyIDByRepairRequestID(ctx context.Context, repairRequestID string) (string, error)
	FindPropertyIDByForceTerminationID(ctx context.Context, forceTerminationID string) (string, error)
	FindPropertyIDByAttachmentID(ctx context.Context, attachmentID string) (string, error)
	EnsureTenantExists(ctx context.Context, tenantID string) error
}

// SQLRepository resolves property ids from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

func (r *SQLRepository) FindPropertyIDByPropertyID(ctx context.Context, propertyID string) (string, error) {
	const query = `
	SELECT id
	FROM properties
	WHERE id = $1
	  AND deleted_at IS NULL
	LIMIT 1
	`

	return r.findPropertyID(ctx, query, propertyID, "query property by id")
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

func (r *SQLRepository) FindPropertyIDByTenantID(ctx context.Context, tenantID string) (string, error) {
	const query = `
SELECT property_id
FROM leases
WHERE tenant_id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	return r.findPropertyID(ctx, query, tenantID, "query tenant property by id")
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

func (r *SQLRepository) FindPropertyIDByAttachmentID(ctx context.Context, attachmentID string) (string, error) {
	const query = `
SELECT property_id
FROM (
    SELECT property_id AS property_id
    FROM property_attachments
    WHERE id = $1
      AND deleted_at IS NULL

    UNION ALL

    SELECT rooms.property_id AS property_id
    FROM room_attachments
    JOIN rooms ON rooms.id = room_attachments.room_id
    WHERE room_attachments.id = $1
      AND room_attachments.deleted_at IS NULL
      AND rooms.deleted_at IS NULL

    UNION ALL

	    SELECT leases.property_id AS property_id
	    FROM tenant_attachments
	    JOIN leases ON leases.tenant_id = tenant_attachments.tenant_id
	    WHERE tenant_attachments.id = $1
	      AND tenant_attachments.deleted_at IS NULL
	      AND leases.deleted_at IS NULL

    UNION ALL

    SELECT leases.property_id AS property_id
    FROM lease_attachments
    JOIN leases ON leases.id = lease_attachments.lease_id
    WHERE lease_attachments.id = $1
      AND lease_attachments.deleted_at IS NULL
      AND leases.deleted_at IS NULL

    UNION ALL

    SELECT journal_logs.property_id AS property_id
    FROM journal_log_attachments
    JOIN journal_logs ON journal_logs.id = journal_log_attachments.journal_log_id
    WHERE journal_log_attachments.id = $1
      AND journal_log_attachments.deleted_at IS NULL
      AND journal_logs.deleted_at IS NULL

    UNION ALL

    SELECT repair_requests.property_id AS property_id
    FROM repair_request_attachments
    JOIN repair_requests ON repair_requests.id = repair_request_attachments.repair_request_id
    WHERE repair_request_attachments.id = $1
      AND repair_request_attachments.deleted_at IS NULL
      AND repair_requests.deleted_at IS NULL

    UNION ALL

    SELECT bills.property_id AS property_id
    FROM bill_attachments
    JOIN bills ON bills.id = bill_attachments.bill_id
    WHERE bill_attachments.id = $1
      AND bill_attachments.deleted_at IS NULL
      AND bills.deleted_at IS NULL
) AS attachment_properties
LIMIT 1
`

	return r.findPropertyID(ctx, query, attachmentID, "query attachment property by id")
}

func (r *SQLRepository) EnsureTenantExists(ctx context.Context, tenantID string) error {
	const query = `
	SELECT id
	FROM tenants
	WHERE id = $1
	  AND deleted_at IS NULL
	LIMIT 1
	`

	var id string
	if err := r.db.QueryRowContext(ctx, query, tenantID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("query tenant by id: %w", err)
	}

	return nil
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
