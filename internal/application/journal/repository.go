package journal

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

var (
	ErrJournalLogNotFound      = errors.New("journal log not found")
	ErrPropertyNotFound        = errors.New("journal property not found")
	ErrRoomNotFound            = errors.New("journal room not found")
	ErrPropertyAccountNotFound = errors.New("journal property account not found")
	ErrAccountingTitleNotFound = errors.New("journal accounting title not found")
)

// TransactionRunner is the txrunner.Runner surface required by journal use cases.
type TransactionRunner interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error
}

// JournalLog is the application-facing journal log shape.
type JournalLog struct {
	ID                         string
	PropertyID                 string
	RoomID                     *string
	AuthorID                   string
	PropertyLabel              string
	RoomLabel                  *string
	AuthorLabel                string
	Content                    string
	ExpenseAmount              *int
	ExpenseDescription         *string
	ExpenseAccountingTitleID   *string
	ExpenseAccountingTitleCode *string
	ExpenseAccountingTitleName *string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// Room captures room ownership needed by journal creation.
type Room struct {
	ID         string
	PropertyID string
}

// AccountingTitle is an active accounting title selectable for journal expenses.
type AccountingTitle struct {
	ID   string
	Code string
	Name string
	Kind string
}

// ListQuery defines filtering, pagination, and role scoping for journal lists.
type ListQuery struct {
	ActorRole           string
	AssignedPropertyIDs []string
	PropertyID          *string
	RoomID              *string
	DateFrom            *time.Time
	DateTo              *time.Time
	Limit               int
	Offset              int
}

// ListResult contains journal logs and the total matching rows before pagination.
type ListResult struct {
	Items []JournalLog
	Total int
}

// CreateParams contains fields for journal log creation.
type CreateParams struct {
	PropertyID                 string
	RoomID                     *string
	AuthorID                   string
	Content                    string
	ExpenseAmount              *int
	ExpenseDescription         *string
	ExpenseAccountingTitleID   *string
	ExpenseAccountingTitleCode *string
	ExpenseAccountingTitleName *string
}

// ExpenseAccountingEntryParams contains accounting data derived from journal expense creation.
type ExpenseAccountingEntryParams struct {
	PropertyID          string
	Category            string
	AccountingTitleCode string
	Amount              int
	Description         *string
	SourceRef           map[string]interface{}
	Year                int
	Month               int
	SourceDate          *time.Time
	DisplayNote         *string
}

// UpdateParams contains mutable journal log fields.
type UpdateParams struct {
	ID                         string
	Content                    string
	ExpenseAmount              *int
	ExpenseDescription         *string
	ExpenseAccountingTitleID   *string
	ExpenseAccountingTitleCode *string
	ExpenseAccountingTitleName *string
}

// Repository defines persistence required by journal use cases.
type Repository interface {
	List(ctx context.Context, query ListQuery) (ListResult, error)
	FindByID(ctx context.Context, id string) (*JournalLog, error)
	FindByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*JournalLog, error)
	ListExpenseAccountingTitles(ctx context.Context) ([]AccountingTitle, error)
	FindExpenseAccountingTitleByID(ctx context.Context, tx *sql.Tx, id string) (*AccountingTitle, error)
	FindExpenseAccountingTitleByCode(ctx context.Context, tx *sql.Tx, code string) (*AccountingTitle, error)
	EnsurePropertyExists(ctx context.Context, tx *sql.Tx, id string) error
	FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*Room, error)
	Create(ctx context.Context, tx *sql.Tx, params CreateParams) (*JournalLog, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateParams) (*JournalLog, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string) error
}

// ExpenseAccountingRepository defines persistence required for journal expense accounting.
type ExpenseAccountingRepository interface {
	CreateExpenseAccountingEntry(ctx context.Context, tx *sql.Tx, params ExpenseAccountingEntryParams) error
	SyncExpenseAccountingEntry(ctx context.Context, tx *sql.Tx, journalLogID string, params *ExpenseAccountingEntryParams) error
	JournalExpenseSnapshotExists(ctx context.Context, tx *sql.Tx, propertyID string, year int, month int) (bool, error)
}
