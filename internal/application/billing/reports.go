package billing

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	domainbilling "stds_backend/internal/domain/billing"

	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/shared/apperr"
	"stds_backend/internal/shared/reporthtml"
)

var ErrFinancialReportNotFoundRepository = errors.New("financial report not found")

// Clock provides current time for current-month report routing.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

var taiwanReportLocation = time.FixedZone("Asia/Taipei", 8*60*60)

func currentReportPeriod(clock Clock) (int, int) {
	now := clock.Now().In(taiwanReportLocation)
	return now.Year(), int(now.Month())
}

// PendingMeterQuery defines role scoping for pending meter reads.
type PendingMeterQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
}

// MeterHistoryQuery defines role scoping and period filters for meter history reads.
type MeterHistoryQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	RoomID              string
	Year                *int
	Month               *int
}

// FinancialReportSummaryQuery defines role scoping for financial report summary reads.
type FinancialReportSummaryQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                *int
	CurrentYear         int
	CurrentMonth        int
}

// FinancialReportQuery defines role scoping for financial report detail reads.
type FinancialReportQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
}

// TenantRosterQuery defines role scoping for tenant roster report rows.
type TenantRosterQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	AsOf                time.Time
	IncludeVacant       bool
}

// BillReceiptQuery defines role scoping for bill receipt export.
type BillReceiptQuery struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
}

// FinancialReportSummary is the application read model for report summary rows.
type FinancialReportSummary struct {
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
}

// FinancialReportEntry is the application read model for one report entry.
type FinancialReportEntry struct {
	Category    string
	Description *string
	Amount      int
}

// FinancialReport is the application read model for a property monthly report.
type FinancialReport struct {
	PropertyID   string
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
	IsFinalized  bool
	Entries      []FinancialReportEntry
}

// TenantRosterRow is one room row used by the tenant roster export.
type TenantRosterRow struct {
	PropertyID         string
	PropertyName       string
	RoomID             string
	RoomName           string
	RoomStatus         string
	LeaseID            *string
	TenantName         *string
	TenantPhone        *string
	NextRentDueDate    *time.Time
	RentBillingCadence *string
	RentAmount         *int
	ReportNotes        *string
}

// BillReceipt is the application read model for one bill receipt export.
type BillReceipt struct {
	BillID               string
	BillType             string
	BillStatus           string
	Amount               *int
	PaidAmount           *int
	PeriodStart          time.Time
	PeriodEnd            time.Time
	MeterPreviousReading *int
	MeterCurrentReading  *int
	MeterUnitPrice       *float64
	PropertyName         string
	RoomName             string
	TenantName           string
}

// ReportRepository defines read operations required by billing report use cases.
type ReportRepository interface {
	ListPendingMeterBills(ctx context.Context, query PendingMeterQuery) ([]Bill, error)
	ListPropertyMeterHistory(ctx context.Context, query MeterHistoryQuery) ([]Bill, error)
	ListRoomMeterHistory(ctx context.Context, query MeterHistoryQuery) ([]Bill, error)
	ListFinancialReportSummaries(ctx context.Context, query FinancialReportSummaryQuery) ([]FinancialReportSummary, error)
	FindLiveFinancialReport(ctx context.Context, query FinancialReportQuery) (*FinancialReport, error)
	FindSnapshotFinancialReport(ctx context.Context, query FinancialReportQuery) (*FinancialReport, error)
	ListTenantRosterRows(ctx context.Context, query TenantRosterQuery) ([]TenantRosterRow, error)
	FindBillReceipt(ctx context.Context, query BillReceiptQuery) (*BillReceipt, error)
}

// ListPendingMeterInput is the use-case input for property pending meter reads.
type ListPendingMeterInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
}

// ListPendingMeterService lists bills waiting for meter readings.
type ListPendingMeterService struct {
	repo ReportRepository
}

// NewListPendingMeterService returns a ListPendingMeterService.
func NewListPendingMeterService(repo ReportRepository) *ListPendingMeterService {
	return &ListPendingMeterService{repo: repo}
}

// Execute validates report read scope and returns pending meter bills.
func (s *ListPendingMeterService) Execute(ctx context.Context, input ListPendingMeterInput) ([]Bill, error) {
	actorRole, err := normalizeMeterReportRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return nil, err
	}

	bills, err := s.repo.ListPendingMeterBills(ctx, PendingMeterQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
	})
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	return bills, nil
}

// ListPropertyMeterHistoryInput is the use-case input for property meter history reads.
type ListPropertyMeterHistoryInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                *int
}

// ListPropertyMeterHistoryService lists property-level meter history.
type ListPropertyMeterHistoryService struct {
	repo ReportRepository
}

// NewListPropertyMeterHistoryService returns a ListPropertyMeterHistoryService.
func NewListPropertyMeterHistoryService(repo ReportRepository) *ListPropertyMeterHistoryService {
	return &ListPropertyMeterHistoryService{repo: repo}
}

// Execute validates report read scope and returns property meter history.
func (s *ListPropertyMeterHistoryService) Execute(ctx context.Context, input ListPropertyMeterHistoryInput) ([]Bill, error) {
	actorRole, err := normalizeMeterReportRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return nil, err
	}
	year, err := normalizeOptionalYear(input.Year)
	if err != nil {
		return nil, err
	}

	bills, err := s.repo.ListPropertyMeterHistory(ctx, MeterHistoryQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		Year:                year,
	})
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	return bills, nil
}

// ListRoomMeterHistoryInput is the use-case input for room meter history reads.
type ListRoomMeterHistoryInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	RoomID              string
	Year                *int
	Month               *int
}

// ListRoomMeterHistoryService lists room-level meter history.
type ListRoomMeterHistoryService struct {
	repo ReportRepository
}

// NewListRoomMeterHistoryService returns a ListRoomMeterHistoryService.
func NewListRoomMeterHistoryService(repo ReportRepository) *ListRoomMeterHistoryService {
	return &ListRoomMeterHistoryService{repo: repo}
}

// Execute validates report read scope and returns room meter history.
func (s *ListRoomMeterHistoryService) Execute(ctx context.Context, input ListRoomMeterHistoryInput) ([]Bill, error) {
	actorRole, err := normalizeMeterReportRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	roomID, err := normalizeRequiredUUID(input.RoomID, "room_id", apperr.ErrRoomNotFound)
	if err != nil {
		return nil, err
	}
	year, err := normalizeOptionalYear(input.Year)
	if err != nil {
		return nil, err
	}
	month, err := normalizeOptionalMonthInt(input.Month)
	if err != nil {
		return nil, err
	}
	if month != nil && year == nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "year",
			"reason": "is required when month is provided",
		})
	}

	bills, err := s.repo.ListRoomMeterHistory(ctx, MeterHistoryQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		RoomID:              roomID,
		Year:                year,
		Month:               month,
	})
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	return bills, nil
}

// ListFinancialReportSummariesInput is the use-case input for report summary reads.
type ListFinancialReportSummariesInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                *int
}

// ListFinancialReportSummariesService lists report summaries.
type ListFinancialReportSummariesService struct {
	repo  ReportRepository
	clock Clock
}

// NewListFinancialReportSummariesService returns a ListFinancialReportSummariesService.
func NewListFinancialReportSummariesService(repo ReportRepository, clocks ...Clock) *ListFinancialReportSummariesService {
	var clock Clock = systemClock{}
	if len(clocks) > 0 && clocks[0] != nil {
		clock = clocks[0]
	}
	return &ListFinancialReportSummariesService{repo: repo, clock: clock}
}

// Execute validates financial report read scope and returns summaries.
func (s *ListFinancialReportSummariesService) Execute(ctx context.Context, input ListFinancialReportSummariesInput) ([]FinancialReportSummary, error) {
	actorRole, err := normalizeFinancialReportReadRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return nil, err
	}
	year, err := normalizeOptionalYear(input.Year)
	if err != nil {
		return nil, err
	}
	currentYear, currentMonth := currentReportPeriod(s.clock)

	summaries, err := s.repo.ListFinancialReportSummaries(ctx, FinancialReportSummaryQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		Year:                year,
		CurrentYear:         currentYear,
		CurrentMonth:        currentMonth,
	})
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	return summaries, nil
}

// GetFinancialReportInput is the use-case input for report detail reads.
type GetFinancialReportInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
}

// GetFinancialReportService retrieves one financial report.
type GetFinancialReportService struct {
	repo  ReportRepository
	clock Clock
}

// NewGetFinancialReportService returns a GetFinancialReportService.
func NewGetFinancialReportService(repo ReportRepository, clock Clock) *GetFinancialReportService {
	if clock == nil {
		clock = systemClock{}
	}
	return &GetFinancialReportService{repo: repo, clock: clock}
}

// Execute validates financial report read scope and chooses live or snapshot data.
func (s *GetFinancialReportService) Execute(ctx context.Context, input GetFinancialReportInput) (*FinancialReport, error) {
	query, err := s.normalizeFinancialReportQuery(input)
	if err != nil {
		return nil, err
	}
	return s.findFinancialReport(ctx, query)
}

func (s *GetFinancialReportService) normalizeFinancialReportQuery(input GetFinancialReportInput) (FinancialReportQuery, error) {
	actorRole, err := normalizeFinancialReportReadRole(input.ActorRole)
	if err != nil {
		return FinancialReportQuery{}, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return FinancialReportQuery{}, err
	}
	if err := validateYear(input.Year); err != nil {
		return FinancialReportQuery{}, err
	}
	if err := validateMonth(input.Month); err != nil {
		return FinancialReportQuery{}, err
	}
	return FinancialReportQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		Year:                input.Year,
		Month:               input.Month,
	}, nil
}

func (s *GetFinancialReportService) findFinancialReport(ctx context.Context, query FinancialReportQuery) (*FinancialReport, error) {
	currentYear, currentMonth := currentReportPeriod(s.clock)
	var report *FinancialReport
	var err error
	if query.Year == currentYear && query.Month == currentMonth {
		report, err = s.repo.FindLiveFinancialReport(ctx, query)
	} else {
		report, err = s.repo.FindSnapshotFinancialReport(ctx, query)
	}
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	if report == nil {
		return nil, ErrFinancialReportNotFound
	}
	return report, nil
}

// SendFinancialReportInput is the use-case input for report send requests.
type SendFinancialReportInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
}

// SendFinancialReportService validates a report and emits FinancialReportSendRequested.
type SendFinancialReportService struct {
	repo      ReportRepository
	publisher domainevents.Publisher
	clock     Clock
}

// NewSendFinancialReportService returns a SendFinancialReportService.
func NewSendFinancialReportService(repo ReportRepository, publisher domainevents.Publisher, clock Clock) *SendFinancialReportService {
	if clock == nil {
		clock = systemClock{}
	}
	return &SendFinancialReportService{repo: repo, publisher: publisher, clock: clock}
}

// Execute validates the report exists, then publishes FinancialReportSendRequested.
func (s *SendFinancialReportService) Execute(ctx context.Context, input SendFinancialReportInput) (*FinancialReport, error) {
	query, err := s.normalizeFinancialReportSendQuery(input)
	if err != nil {
		return nil, err
	}

	report, err := s.findFinancialReport(ctx, query)
	if err != nil {
		return nil, err
	}
	if s.publisher == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "financial_report_publisher"})
	}
	if err := s.publisher.Publish(ctx, domainevents.FinancialReportSendRequested{
		PropertyID: query.PropertyID,
		Year:       query.Year,
		Month:      query.Month,
		OccurredAt: s.clock.Now().UTC(),
	}); err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}
	return report, nil
}

func (s *SendFinancialReportService) normalizeFinancialReportSendQuery(input SendFinancialReportInput) (FinancialReportQuery, error) {
	actorRole, err := normalizeFinancialReportSendRole(input.ActorRole)
	if err != nil {
		return FinancialReportQuery{}, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return FinancialReportQuery{}, err
	}
	if err := validateYear(input.Year); err != nil {
		return FinancialReportQuery{}, err
	}
	if err := validateMonth(input.Month); err != nil {
		return FinancialReportQuery{}, err
	}
	return FinancialReportQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		Year:                input.Year,
		Month:               input.Month,
	}, nil
}

func (s *SendFinancialReportService) findFinancialReport(ctx context.Context, query FinancialReportQuery) (*FinancialReport, error) {
	currentYear, currentMonth := currentReportPeriod(s.clock)
	var report *FinancialReport
	var err error
	if query.Year == currentYear && query.Month == currentMonth {
		report, err = s.repo.FindLiveFinancialReport(ctx, query)
	} else {
		report, err = s.repo.FindSnapshotFinancialReport(ctx, query)
	}
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	if report == nil {
		return nil, ErrFinancialReportNotFound
	}
	return report, nil
}

// ExportTenantRosterInput is the use-case input for tenant roster HTML export.
type ExportTenantRosterInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	AsOf                *time.Time
	IncludeVacant       bool
	Format              string
}

// ExportTenantRosterService renders the property tenant roster report as HTML.
type ExportTenantRosterService struct {
	repo     ReportRepository
	renderer reporthtml.Renderer
	clock    Clock
}

// NewExportTenantRosterService returns an ExportTenantRosterService.
func NewExportTenantRosterService(repo ReportRepository, renderer reporthtml.Renderer, clock Clock) *ExportTenantRosterService {
	if clock == nil {
		clock = systemClock{}
	}
	return &ExportTenantRosterService{repo: repo, renderer: renderer, clock: clock}
}

// Execute validates tenant roster export scope and renders the HTML document.
func (s *ExportTenantRosterService) Execute(ctx context.Context, input ExportTenantRosterInput) (*reporthtml.Document, error) {
	query, err := s.normalizeTenantRosterQuery(input)
	if err != nil {
		return nil, err
	}
	if s.renderer == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "tenant_roster_renderer"})
	}

	rows, err := s.repo.ListTenantRosterRows(ctx, query)
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}

	view := newTenantRosterView(query.AsOf, rows)
	html, err := s.renderer.Render("tenant_roster.html", view)
	if err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "tenant_roster_renderer"})
	}

	return &reporthtml.Document{
		HTML:     html,
		Filename: reporthtml.HTMLFilename("tenant-roster", view.PropertyName, query.AsOf.Format("2006-01-02")),
	}, nil
}

func (s *ExportTenantRosterService) normalizeTenantRosterQuery(input ExportTenantRosterInput) (TenantRosterQuery, error) {
	actorRole, err := normalizeFinancialReportReadRole(input.ActorRole)
	if err != nil {
		return TenantRosterQuery{}, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return TenantRosterQuery{}, err
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "html"
	}
	if format != "html" {
		return TenantRosterQuery{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "format"})
	}
	asOf := currentReportDate(s.clock)
	if input.AsOf != nil {
		asOf = dateOnly(*input.AsOf)
	}
	return TenantRosterQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		AsOf:                asOf,
		IncludeVacant:       input.IncludeVacant,
	}, nil
}

// ExportBillReceiptInput is the use-case input for bill receipt HTML export.
type ExportBillReceiptInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
	Format              string
}

// ExportBillReceiptService renders one paid rent or electricity bill receipt as HTML.
type ExportBillReceiptService struct {
	repo     ReportRepository
	renderer reporthtml.Renderer
}

// NewExportBillReceiptService returns an ExportBillReceiptService.
func NewExportBillReceiptService(repo ReportRepository, renderer reporthtml.Renderer) *ExportBillReceiptService {
	return &ExportBillReceiptService{repo: repo, renderer: renderer}
}

// Execute validates bill receipt export scope and renders the HTML document.
func (s *ExportBillReceiptService) Execute(ctx context.Context, input ExportBillReceiptInput) (*reporthtml.Document, error) {
	query, err := normalizeBillReceiptQuery(input)
	if err != nil {
		return nil, err
	}
	if s.renderer == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "bill_receipt_renderer"})
	}

	receipt, err := s.repo.FindBillReceipt(ctx, query)
	if err != nil {
		return nil, mapReportRepositoryError(err)
	}
	if receipt == nil {
		return nil, apperr.ErrBillNotFound
	}
	if err := validateBillReceiptExportable(receipt); err != nil {
		return nil, err
	}

	view := newBillReceiptView(*receipt)
	html, err := s.renderer.Render("bill_receipt.html", view)
	if err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err).WithDetails(map[string]interface{}{"dependency": "bill_receipt_renderer"})
	}

	return &reporthtml.Document{
		HTML:     html,
		Filename: reporthtml.HTMLFilename("bill-receipt", receipt.BillType, receipt.RoomName, receipt.PeriodStart.Format("2006-01-02")),
	}, nil
}

func normalizeBillReceiptQuery(input ExportBillReceiptInput) (BillReceiptQuery, error) {
	actorRole, err := normalizeFinancialReportReadRole(input.ActorRole)
	if err != nil {
		return BillReceiptQuery{}, err
	}
	billID, err := normalizeRequiredUUID(input.BillID, "id", apperr.ErrBillNotFound)
	if err != nil {
		return BillReceiptQuery{}, err
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "html"
	}
	if format != "html" {
		return BillReceiptQuery{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "format"})
	}
	return BillReceiptQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		BillID:              billID,
	}, nil
}

func validateBillReceiptExportable(receipt *BillReceipt) error {
	if receipt.BillStatus != domainbilling.StatusPaid {
		return ErrBillReceiptNotExportable.WithDetails(map[string]interface{}{"status": receipt.BillStatus})
	}
	switch receipt.BillType {
	case domainbilling.TypeRent:
		if receipt.Amount == nil {
			return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "bill_receipt_amount"})
		}
	case domainbilling.TypeElectricity:
		if receipt.Amount == nil || receipt.MeterPreviousReading == nil || receipt.MeterCurrentReading == nil || receipt.MeterUnitPrice == nil {
			return ErrBillReceiptNotExportable.WithDetails(map[string]interface{}{"type": receipt.BillType})
		}
	default:
		return ErrBillReceiptNotExportable.WithDetails(map[string]interface{}{"type": receipt.BillType})
	}
	return nil
}

func currentReportDate(clock Clock) time.Time {
	now := clock.Now().In(taiwanReportLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, taiwanReportLocation)
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

type tenantRosterView struct {
	Title        string
	PropertyName string
	AsOfLabel    string
	Rows         []tenantRosterViewRow
}

type tenantRosterViewRow struct {
	RoomName             string
	StatusLabel          string
	TenantName           string
	TenantPhone          string
	NextRentDueDateLabel string
	RentCadenceLabel     string
	RentAmountLabel      string
	Notes                string
}

type billReceiptView struct {
	Title         string
	EnglishTitle  string
	Copies        []billReceiptCopyView
	IsRent        bool
	IsElectricity bool
}

type billReceiptCopyView struct {
	CopyLabel            string
	PropertyName         string
	RoomName             string
	TenantName           string
	PeriodLabel          string
	ReceiptDate          string
	Collector            string
	ItemLabel            string
	Description          string
	AmountLabel          string
	PreviousReadingLabel string
	CurrentReadingLabel  string
	UsageLabel           string
	UnitPriceLabel       string
}

func newTenantRosterView(asOf time.Time, rows []TenantRosterRow) tenantRosterView {
	propertyName := ""
	if len(rows) > 0 {
		propertyName = rows[0].PropertyName
	}
	view := tenantRosterView{
		Title:        "租客名冊",
		PropertyName: propertyName,
		AsOfLabel:    asOf.Format("2006-01-02"),
		Rows:         make([]tenantRosterViewRow, 0, len(rows)),
	}
	for _, row := range rows {
		view.Rows = append(view.Rows, tenantRosterViewRow{
			RoomName:             row.RoomName,
			StatusLabel:          tenantRosterStatusLabel(row),
			TenantName:           stringValue(row.TenantName),
			TenantPhone:          stringValue(row.TenantPhone),
			NextRentDueDateLabel: dateValue(row.NextRentDueDate),
			RentCadenceLabel:     rentCadenceLabel(row.RentBillingCadence),
			RentAmountLabel:      intValue(row.RentAmount),
			// TODO: Decide the report-owned notes source before populating this field.
			Notes: stringValue(row.ReportNotes),
		})
	}
	return view
}

func newBillReceiptView(receipt BillReceipt) billReceiptView {
	view := billReceiptView{
		Title:         billReceiptTitle(receipt.BillType),
		EnglishTitle:  billReceiptEnglishTitle(receipt.BillType),
		IsRent:        receipt.BillType == domainbilling.TypeRent,
		IsElectricity: receipt.BillType == domainbilling.TypeElectricity,
		Copies:        make([]billReceiptCopyView, 0, 2),
	}
	for _, copyLabel := range []string{"客戶聯", "存根聯"} {
		view.Copies = append(view.Copies, newBillReceiptCopyView(receipt, copyLabel))
	}
	return view
}

func newBillReceiptCopyView(receipt BillReceipt, copyLabel string) billReceiptCopyView {
	amount := 0
	if receipt.Amount != nil {
		amount = *receipt.Amount
	}
	view := billReceiptCopyView{
		CopyLabel:    copyLabel,
		PropertyName: receipt.PropertyName,
		RoomName:     receipt.RoomName,
		TenantName:   receipt.TenantName,
		PeriodLabel:  receipt.PeriodStart.Format("2006-01-02") + " 至 " + receipt.PeriodEnd.Format("2006-01-02"),
		ReceiptDate:  "",
		Collector:    "",
		ItemLabel:    billReceiptItemLabel(receipt.BillType),
		Description:  billReceiptDescription(receipt),
		AmountLabel:  moneyLabel(amount),
	}
	if receipt.BillType == domainbilling.TypeElectricity {
		previous := intPtrValue(receipt.MeterPreviousReading)
		current := intPtrValue(receipt.MeterCurrentReading)
		view.PreviousReadingLabel = numberLabel(previous)
		view.CurrentReadingLabel = numberLabel(current)
		view.UsageLabel = numberLabel(current - previous)
		view.UnitPriceLabel = unitPriceLabel(receipt.MeterUnitPrice)
	}
	return view
}

func billReceiptTitle(billType string) string {
	if billType == domainbilling.TypeElectricity {
		return "電費收據"
	}
	return "租金收據"
}

func billReceiptEnglishTitle(billType string) string {
	if billType == domainbilling.TypeElectricity {
		return "Electricity Receipt"
	}
	return "Rent Receipt"
}

func billReceiptItemLabel(billType string) string {
	if billType == domainbilling.TypeElectricity {
		return "電費"
	}
	return "租金"
}

func billReceiptDescription(receipt BillReceipt) string {
	if receipt.BillType == domainbilling.TypeElectricity {
		return receipt.PeriodStart.Format("2006 年 1 月") + "電費"
	}
	return receipt.PeriodStart.Format("2006 年 1 月") + "租金"
}

func tenantRosterStatusLabel(row TenantRosterRow) string {
	if row.LeaseID == nil {
		return "空房"
	}
	return "出租中"
}

func rentCadenceLabel(value *string) string {
	switch strings.ToLower(strings.TrimSpace(stringValue(value))) {
	case "monthly":
		return "月繳"
	case "quarterly":
		return "季繳"
	case "semiannual":
		return "半年繳"
	case "annual":
		return "年繳"
	default:
		return ""
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func dateValue(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

func intValue(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func intPtrValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func moneyLabel(value int) string {
	return "NT$ " + numberLabel(value)
}

func numberLabel(value int) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	text := strconv.Itoa(value)
	if len(text) <= 3 {
		return sign + text
	}
	parts := make([]string, 0, len(text)/3+1)
	for len(text) > 3 {
		parts = append([]string{text[len(text)-3:]}, parts...)
		text = text[:len(text)-3]
	}
	parts = append([]string{text}, parts...)
	return sign + strings.Join(parts, ",")
}

func unitPriceLabel(value *float64) string {
	if value == nil {
		return ""
	}
	return "NT$ " + strconv.FormatFloat(*value, 'f', -1, 64)
}

func normalizeMeterReportRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer", "staff":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeFinancialReportReadRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer", "staff", "owner":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeFinancialReportSendRole(role string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "admin", "organizer":
		return normalized, nil
	default:
		return "", apperr.ErrForbidden
	}
}

func normalizeRequiredUUID(value string, field string, emptyErr error) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", emptyErr
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}
	return trimmed, nil
}

func normalizeOptionalYear(value *int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if err := validateYear(*value); err != nil {
		return nil, err
	}
	year := *value
	return &year, nil
}

func normalizeOptionalMonthInt(value *int) (*int, error) {
	if value == nil {
		return nil, nil
	}
	if err := validateMonth(*value); err != nil {
		return nil, err
	}
	month := *value
	return &month, nil
}

func validateYear(value int) error {
	if value < 1 {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "year"})
	}
	return nil
}

func validateMonth(value int) error {
	if value < 1 || value > 12 {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "month"})
	}
	return nil
}

func mapReportRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrBillNotFound):
		return apperr.ErrBillNotFound
	case errors.Is(err, ErrFinancialReportNotFoundRepository):
		return ErrFinancialReportNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
