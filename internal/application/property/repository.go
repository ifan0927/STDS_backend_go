package property

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrPropertyNotFound indicates that no active property matched the requested lookup.
var ErrPropertyNotFound = errors.New("property not found")

// Property is the application-facing property shape for property use cases.
type Property struct {
	ID                               string
	Name                             string
	PropertyPublicName               string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	CreatedAt                        time.Time
	UpdatedAt                        time.Time
	Version                          int
}

// Room is the application-facing room shape for room command use cases.
type Room struct {
	ID                string
	PropertyID        string
	Name              string
	Status            string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// RepairRequest is the application-facing repair request shape for maintenance entry.
type RepairRequest struct {
	ID          string
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
	Status      string
	SubmittedAt time.Time
	AssignedAt  *time.Time
	AssignedTo  *string
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Dashboard contains the property owner dashboard read model.
type Dashboard struct {
	PropertyID     string
	Rooms          []DashboardRoom
	MonthlySummary DashboardMonthlySummary
	Occupancy      OccupancySummary
	RecentJournals []DashboardRecentJournal
}

// HomeDashboard contains the role-scoped dashboard read model for the home page.
type HomeDashboard struct {
	PortfolioSummary      OccupancySummary
	MonthlyBillingSummary DashboardMonthlySummary
	PropertySummaries     []HomeDashboardPropertySummary
	RecentJournals        []HomeDashboardRecentJournal
}

// HomeDashboardPropertySummary contains one property's home dashboard row.
type HomeDashboardPropertySummary struct {
	PropertyID     string
	PropertyName   string
	Occupancy      OccupancySummary
	MonthlySummary DashboardMonthlySummary
}

// HomeDashboardRecentJournal contains one recent activity item for the home dashboard.
type HomeDashboardRecentJournal struct {
	ID           string
	PropertyID   string
	PropertyName string
	Type         string
	Content      string
	CreatedAt    time.Time
}

// OccupancySummary contains room status totals and occupied ratio.
type OccupancySummary struct {
	TotalRooms       int
	OccupiedRooms    int
	VacantRooms      int
	MaintenanceRooms int
	OccupancyRate    float64
}

// DashboardRoom contains one room row in the dashboard.
type DashboardRoom struct {
	ID     string
	Name   string
	Status string
}

// DashboardMonthlySummary contains current-month rent and overdue bill totals.
type DashboardMonthlySummary struct {
	ExpectedRent     int
	CollectedRent    int
	OverdueBillCount int
}

// DashboardRecentJournal contains one recent journal entry in the dashboard.
type DashboardRecentJournal struct {
	ID        string
	Type      string
	Content   string
	CreatedAt time.Time
}

// CreatePropertyParams contains the writable fields required to create a
// property.
type CreatePropertyParams struct {
	Name                             string
	PropertyPublicName               string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
}

// CreatePropertyAccountParams contains the fields required to create the
// accounting lifecycle record for a property.
type CreatePropertyAccountParams struct {
	PropertyID string
}

// UpdatePropertyParams contains the writable fields required to persist a
// property update.
type UpdatePropertyParams struct {
	ID                               string
	Name                             string
	PropertyPublicName               string
	Subtitle                         *string
	Address                          string
	ElectricityUnitPrice             *float64
	DefaultElectricityBillingCadence string
	OwnerID                          string
	ContactPhone                     *string
	ContactEmail                     *string
	Notes                            *string
	Facilities                       *map[string]interface{}
	Version                          int
}

// CreateRoomParams contains the writable fields required to create a room.
type CreateRoomParams struct {
	PropertyID        string
	Name              string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
}

// UpdateRoomParams contains the writable fields required to persist a room update.
type UpdateRoomParams struct {
	ID                string
	Name              string
	Status            *string
	Size              *float64
	Floor             *string
	RoomType          *string
	Facilities        *map[string]interface{}
	DefaultRentAmount *int
	Notes             *string
	Zone              *string
}

// CreateRepairRequestParams contains the writable fields required to create a room-scoped repair request.
type CreateRepairRequestParams struct {
	PropertyID  string
	RoomID      string
	SubmittedBy string
	Title       string
	Description string
}

// Repository defines persistence needed by property application services.
type Repository interface {
	Create(ctx context.Context, tx *sql.Tx, params CreatePropertyParams) (*Property, error)
	FindByID(ctx context.Context, tx *sql.Tx, id string) (*Property, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdatePropertyParams) (*Property, error)
	ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error)
	SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error
	CreateRoom(ctx context.Context, tx *sql.Tx, params CreateRoomParams) (*Room, error)
	FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*Room, error)
	UpdateRoom(ctx context.Context, tx *sql.Tx, params UpdateRoomParams) (*Room, error)
	SoftDeleteRoom(ctx context.Context, tx *sql.Tx, id string) error
	CreateRepairRequest(ctx context.Context, tx *sql.Tx, params CreateRepairRequestParams) (*RepairRequest, error)
}

// PropertyAccountRepository defines the strong-consistency lifecycle operation
// property creation needs from billing.
type PropertyAccountRepository interface {
	CreatePropertyAccount(ctx context.Context, tx *sql.Tx, params CreatePropertyAccountParams) error
}

// DashboardRepository defines the read model needed by the property dashboard.
type DashboardRepository interface {
	GetDashboard(ctx context.Context, propertyID string, year int, month int) (*Dashboard, error)
	GetHomeDashboard(ctx context.Context, scope DashboardScope, year int, month int) (*HomeDashboard, error)
}

// DashboardScope defines role and property visibility for dashboard reads.
type DashboardScope struct {
	Role                string
	UserID              string
	AssignedPropertyIDs []string
}
