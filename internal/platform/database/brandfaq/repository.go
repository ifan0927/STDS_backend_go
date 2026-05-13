package brandfaq

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound indicates that a brand FAQ item does not exist.
var ErrNotFound = errors.New("brand FAQ item not found")

// Item is the persisted brand FAQ item.
type Item struct {
	ID        string
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

// CreateItemParams contains fields for inserting a brand FAQ item.
type CreateItemParams struct {
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
}

// UpdateItemParams contains fields for updating a brand FAQ item.
type UpdateItemParams struct {
	ID        string
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
	Version   int
}

// SQLRepository loads and persists brand FAQ items.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a brand FAQ repository backed by db.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// List returns brand FAQ items ordered by sort_order.
func (r *SQLRepository) List(ctx context.Context, includeInactive bool) ([]Item, error) {
	const query = `
SELECT id, question, answer, sort_order, is_active, created_at, updated_at, version
FROM brand_faq_items
WHERE deleted_at IS NULL
  AND ($1::boolean OR is_active = true)
ORDER BY sort_order
`

	rows, err := r.db.QueryContext(ctx, query, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list brand FAQ items: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan brand FAQ item: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brand FAQ items: %w", err)
	}

	return items, nil
}

// FindForUpdate locks and returns a brand FAQ item inside tx.
func (r *SQLRepository) FindForUpdate(ctx context.Context, tx *sql.Tx, id string) (*Item, error) {
	const query = `
SELECT id, question, answer, sort_order, is_active, created_at, updated_at, version
FROM brand_faq_items
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE
`

	item, err := scanItem(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find brand FAQ item for update: %w", err)
	}

	return item, nil
}

// Create inserts a brand FAQ item inside tx.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params CreateItemParams) (*Item, error) {
	const query = `
INSERT INTO brand_faq_items (question, answer, sort_order, is_active)
VALUES ($1, $2, $3, $4)
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`

	item, err := scanItem(tx.QueryRowContext(ctx, query, params.Question, params.Answer, params.SortOrder, params.IsActive))
	if err != nil {
		return nil, fmt.Errorf("create brand FAQ item: %w", err)
	}

	return item, nil
}

// Update updates a brand FAQ item inside tx.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params UpdateItemParams) (*Item, error) {
	const query = `
UPDATE brand_faq_items
SET question = $1,
	answer = $2,
	sort_order = $3,
	is_active = $4,
	updated_at = now(),
	version = version + 1
WHERE id = $5
  AND version = $6
  AND deleted_at IS NULL
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`

	item, err := scanItem(tx.QueryRowContext(ctx, query, params.Question, params.Answer, params.SortOrder, params.IsActive, params.ID, params.Version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update brand FAQ item: %w", err)
	}

	return item, nil
}

// Deactivate marks a brand FAQ item inactive inside tx.
func (r *SQLRepository) Deactivate(ctx context.Context, tx *sql.Tx, id string, version int) (*Item, error) {
	const query = `
UPDATE brand_faq_items
SET is_active = false,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND deleted_at IS NULL
RETURNING id, question, answer, sort_order, is_active, created_at, updated_at, version
`

	item, err := scanItem(tx.QueryRowContext(ctx, query, id, version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("deactivate brand FAQ item: %w", err)
	}

	return item, nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanItem(row scanner) (*Item, error) {
	var item Item
	if err := row.Scan(
		&item.ID,
		&item.Question,
		&item.Answer,
		&item.SortOrder,
		&item.IsActive,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.Version,
	); err != nil {
		return nil, err
	}

	return &item, nil
}
