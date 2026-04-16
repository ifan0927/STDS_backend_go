package properties

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound indicates that no active property matched the requested lookup.
var ErrNotFound = errors.New("property not found")

// Repository defines the property ownership lookup required by authorization.
type Repository interface {
	FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error)
}

// SQLRepository loads property ownership data from PostgreSQL.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by the provided database handle.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// FindOwnerIDByPropertyID returns the owner user id for the given property
// when it has not been soft-deleted.
func (r *SQLRepository) FindOwnerIDByPropertyID(ctx context.Context, propertyID string) (string, error) {
	const query = `
SELECT owner_id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`

	var ownerID string
	if err := r.db.QueryRowContext(ctx, query, propertyID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("query property owner by id: %w", err)
	}

	return ownerID, nil
}
