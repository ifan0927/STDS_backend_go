package billing

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"stds_backend/internal/platform/database/txrunner"
)

var (
	ErrBillNotFound             = errors.New("bill not found")
	ErrConcurrentUpdateConflict = errors.New("concurrent update conflict")
	ErrPropertyAccountNotFound  = errors.New("property account not found")
)

// TransactionRunner is the txrunner.Runner surface required by billing use cases.
type TransactionRunner interface {
	WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error
}

// Bill is the application read model for API bill responses.
type Bill struct {
	ID                   string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
	Type                 string
	Amount               *int
	DueDate              time.Time
	Status               string
	PaymentMethod        *string
	PaidAt               *time.Time
	PaidAmount           *int
	MeterPreviousReading *int
	MeterCurrentReading  *int
	MeterUnitPrice       *float64
	MeterRecordedAt      *time.Time
	WrittenOffReason     *string
	OverdueNoticeCount   int
	PeriodStart          time.Time
	PeriodEnd            time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// ListBillsQuery defines filtering, pagination, and role scoping for bill lists.
type ListBillsQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          *string
	LeaseID             *string
	TenantID            *string
	Status              *string
	Month               *time.Time
	Limit               int
	Offset              int
}

// GetBillQuery defines role scoping for bill detail retrieval.
type GetBillQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
}

// UpdateMeterParams contains persisted fields after meter recording.
type UpdateMeterParams struct {
	BillID          string
	PreviousReading int
	CurrentReading  int
	UnitPrice       float64
	Amount          int
	Status          string
	MeterRecordedAt time.Time
	ExpectedVersion int
}

// UpdatePaymentParams contains persisted fields after payment recording.
type UpdatePaymentParams struct {
	BillID          string
	PaidAmount      int
	PaymentMethod   string
	PaidAt          time.Time
	Status          string
	ExpectedVersion int
}

// Repository defines database operations required by billing use cases.
type Repository interface {
	ListBills(ctx context.Context, query ListBillsQuery) ([]Bill, error)
	FindBillByID(ctx context.Context, query GetBillQuery) (*Bill, error)
	FindBillByIDForUpdate(ctx context.Context, tx *sql.Tx, billID string) (*Bill, error)
	FindPreviousElectricityReading(ctx context.Context, tx *sql.Tx, roomID string, beforePeriodStart time.Time) (int, error)
	FindPropertyElectricityUnitPrice(ctx context.Context, tx *sql.Tx, propertyID string) (*float64, error)
	UpdateBillMeter(ctx context.Context, tx *sql.Tx, params UpdateMeterParams) (*Bill, error)
	UpdateBillPayment(ctx context.Context, tx *sql.Tx, params UpdatePaymentParams) (*Bill, error)
}

// AccountingEntryParams contains accounting entry data derived from BillPaid.
type AccountingEntryParams struct {
	PropertyID  string
	Category    string
	Amount      int
	Description *string
	SourceRef   map[string]interface{}
	Year        int
	Month       int
}

// AccountingRepository defines persistence required to complete bill payment.
type AccountingRepository interface {
	CreateAccountingEntry(ctx context.Context, tx *sql.Tx, params AccountingEntryParams) error
}
