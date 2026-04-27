package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	appjournal "stds_backend/internal/application/journal"
)

// SQLRepository persists and reads journal log state.
type SQLRepository struct {
	db *sql.DB
}

// NewRepository returns a journal repository backed by db.
func NewRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// List returns active journal logs visible to the actor scope.
func (r *SQLRepository) List(ctx context.Context, query appjournal.ListQuery) ([]appjournal.JournalLog, error) {
	base := selectJournalLogColumns + `
FROM journal_logs jl
WHERE jl.deleted_at IS NULL
`
	args := make([]any, 0)

	switch strings.TrimSpace(query.ActorRole) {
	case "organizer", "staff":
		if len(query.AssignedPropertyIDs) == 0 {
			return []appjournal.JournalLog{}, nil
		}
		placeholders := make([]string, 0, len(query.AssignedPropertyIDs))
		for _, id := range query.AssignedPropertyIDs {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		base += "\n  AND jl.property_id IN (" + strings.Join(placeholders, ", ") + ")"
	case "admin":
	default:
		return []appjournal.JournalLog{}, nil
	}

	if query.PropertyID != nil {
		args = append(args, *query.PropertyID)
		base += fmt.Sprintf("\n  AND jl.property_id = $%d", len(args))
	}
	if query.RoomID != nil {
		args = append(args, *query.RoomID)
		base += fmt.Sprintf("\n  AND jl.room_id = $%d", len(args))
	}
	if query.DateFrom != nil {
		args = append(args, query.DateFrom.UTC())
		base += fmt.Sprintf("\n  AND jl.created_at >= $%d", len(args))
	}
	if query.DateTo != nil {
		args = append(args, query.DateTo.UTC().AddDate(0, 0, 1))
		base += fmt.Sprintf("\n  AND jl.created_at < $%d", len(args))
	}

	args = append(args, query.Limit, query.Offset)
	base += fmt.Sprintf("\nORDER BY jl.created_at DESC, jl.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, fmt.Errorf("list journal logs: %w", err)
	}
	defer rows.Close()

	items := []appjournal.JournalLog{}
	for rows.Next() {
		item, err := scanJournalLog(rows)
		if err != nil {
			return nil, fmt.Errorf("scan journal log: %w", err)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate journal logs: %w", err)
	}

	return items, nil
}

// FindByID returns one active journal log.
func (r *SQLRepository) FindByID(ctx context.Context, id string) (*appjournal.JournalLog, error) {
	query := selectJournalLogColumns + `
FROM journal_logs jl
WHERE jl.id = $1
  AND jl.deleted_at IS NULL
LIMIT 1
`
	journalLog, err := scanJournalLog(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appjournal.ErrJournalLogNotFound
		}
		return nil, fmt.Errorf("query journal log by id: %w", err)
	}

	return journalLog, nil
}

// FindByIDForUpdate locks one active journal log for command use.
func (r *SQLRepository) FindByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*appjournal.JournalLog, error) {
	query := selectJournalLogColumns + `
FROM journal_logs jl
WHERE jl.id = $1
  AND jl.deleted_at IS NULL
FOR UPDATE
`
	journalLog, err := scanJournalLog(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appjournal.ErrJournalLogNotFound
		}
		return nil, fmt.Errorf("query journal log by id for update: %w", err)
	}

	return journalLog, nil
}

// EnsurePropertyExists verifies that an active property exists for journal writes.
func (r *SQLRepository) EnsurePropertyExists(ctx context.Context, tx *sql.Tx, id string) error {
	const query = `
SELECT 1
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`
	var exists int
	if err := tx.QueryRowContext(ctx, query, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appjournal.ErrPropertyNotFound
		}
		return fmt.Errorf("query journal property by id: %w", err)
	}

	return nil
}

// FindRoomByID returns an active room's owning property.
func (r *SQLRepository) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*appjournal.Room, error) {
	const query = `
SELECT id, property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`
	var room appjournal.Room
	if err := tx.QueryRowContext(ctx, query, id).Scan(&room.ID, &room.PropertyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appjournal.ErrRoomNotFound
		}
		return nil, fmt.Errorf("query journal room by id: %w", err)
	}

	return &room, nil
}

// Create inserts a journal log.
func (r *SQLRepository) Create(ctx context.Context, tx *sql.Tx, params appjournal.CreateParams) (*appjournal.JournalLog, error) {
	query := `
INSERT INTO journal_logs (
	property_id,
	room_id,
	author_id,
	content,
	expense_amount,
	expense_description
) VALUES ($1, $2, $3, $4, $5, $6)
` + returningJournalLogColumns

	journalLog, err := scanJournalLog(tx.QueryRowContext(ctx, query,
		params.PropertyID,
		nullableString(params.RoomID),
		params.AuthorID,
		params.Content,
		nullableInt(params.ExpenseAmount),
		nullableString(params.ExpenseDescription),
	))
	if err != nil {
		return nil, fmt.Errorf("create journal log: %w", err)
	}

	return journalLog, nil
}

// Update persists mutable journal log fields.
func (r *SQLRepository) Update(ctx context.Context, tx *sql.Tx, params appjournal.UpdateParams) (*appjournal.JournalLog, error) {
	query := `
UPDATE journal_logs
SET content = $2,
    expense_amount = $3,
    expense_description = $4,
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
` + returningJournalLogColumns

	journalLog, err := scanJournalLog(tx.QueryRowContext(ctx, query,
		params.ID,
		params.Content,
		nullableInt(params.ExpenseAmount),
		nullableString(params.ExpenseDescription),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, appjournal.ErrJournalLogNotFound
		}
		return nil, fmt.Errorf("update journal log: %w", err)
	}

	return journalLog, nil
}

// SoftDelete marks a journal log deleted.
func (r *SQLRepository) SoftDelete(ctx context.Context, tx *sql.Tx, id string) error {
	const query = `
UPDATE journal_logs
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete journal log: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("soft delete journal log rows affected: %w", err)
	}
	if affected == 0 {
		return appjournal.ErrJournalLogNotFound
	}

	return nil
}

const selectJournalLogColumns = `
SELECT
	jl.id,
	jl.property_id,
	jl.room_id,
	jl.author_id,
	jl.content,
	jl.expense_amount,
	jl.expense_description,
	jl.created_at,
	jl.updated_at
`

const returningJournalLogColumns = `
RETURNING
	id,
	property_id,
	room_id,
	author_id,
	content,
	expense_amount,
	expense_description,
	created_at,
	updated_at
`

type journalLogScanner interface {
	Scan(dest ...any) error
}

func scanJournalLog(scanner journalLogScanner) (*appjournal.JournalLog, error) {
	var item appjournal.JournalLog
	var roomID sql.NullString
	var expenseAmount sql.NullInt64
	var expenseDescription sql.NullString

	if err := scanner.Scan(
		&item.ID,
		&item.PropertyID,
		&roomID,
		&item.AuthorID,
		&item.Content,
		&expenseAmount,
		&expenseDescription,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if roomID.Valid {
		item.RoomID = &roomID.String
	}
	if expenseAmount.Valid {
		value := int(expenseAmount.Int64)
		item.ExpenseAmount = &value
	}
	if expenseDescription.Valid {
		item.ExpenseDescription = &expenseDescription.String
	}

	return &item, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}

	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}

	return *value
}
