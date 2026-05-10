package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appattachment "stds_backend/internal/application/attachment"
	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appjournal "stds_backend/internal/application/journal"
	applease "stds_backend/internal/application/lease"
	appproperty "stds_backend/internal/application/property"
	apprepair "stds_backend/internal/application/repair"
	apptenant "stds_backend/internal/application/tenant"
	domainusers "stds_backend/internal/domain/users"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/queryparams"
	"stds_backend/internal/http/requestctx"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbtenantquery "stds_backend/internal/platform/database/tenantquery"
	"stds_backend/internal/platform/database/users"
	"stds_backend/internal/shared/apperr"
	"stds_backend/internal/shared/reporthtml"
)

var _ api.ServerInterface = (*APIServer)(nil)

// APIServer is the placeholder implementation for the generated API server
// interface.
type APIServer struct {
	userRepo          users.Repository
	createUserService *appiam.CreateUserService
	sendPasswordReset *appiam.SendUserPasswordResetService
	syncAuthService   *appiam.SyncAuthService
	updateCurrentUser *appiam.UpdateCurrentUserService
	updateUser        *appiam.UpdateUserService
	assignProperties  *appiam.AssignUserPropertiesService
	jobTriggerService *appjobs.TriggerService
	propertyQueryRepo dbpropertyquery.Repository
	propertyDashboard *appproperty.DashboardService
	leaseQueryRepo    dbleasequery.Repository
	repairQueryRepo   RepairQueryRepository
	tenantQueryRepo   dbtenantquery.Repository
	createPropertySvc *appproperty.CreatePropertyService
	updatePropertySvc *appproperty.UpdatePropertyService
	deletePropertySvc *appproperty.DeletePropertyService
	createRoomSvc     *appproperty.CreateRoomService
	updateRoomSvc     *appproperty.UpdateRoomService
	deleteRoomSvc     *appproperty.DeleteRoomService
	setMaintenanceSvc *appproperty.SetRoomMaintenanceService
	createTenantSvc   *apptenant.CreateTenantService
	updateTenantSvc   *apptenant.UpdateTenantService
	createLeaseSvc    *applease.CreateLeaseService
	updateLeaseSvc    *applease.UpdateLeaseService
	updateDepositSvc  *applease.UpdateDepositService
	replaceLeaseSvc   *applease.ReplaceLeaseService
	terminateLeaseSvc *applease.TerminateLeaseService
	previewCheckout   *applease.PreviewCheckoutSettlementService
	finalizeCheckout  *applease.FinalizeCheckoutSettlementService
	exportCheckout    *applease.ExportCheckoutSettlementService
	forceTerminateSvc *applease.ForceTerminateLeaseService
	getForceTermSvc   *applease.GetForceTerminationService
	billing           BillingServices
	journal           JournalServices
	repair            RepairServices
	attachment        *appattachment.Service
}

// APIServerDeps groups the application entry points required by exposed API
// endpoints.
type APIServerDeps struct {
	UserRepo          users.Repository
	CreateUser        *appiam.CreateUserService
	SendPasswordReset *appiam.SendUserPasswordResetService
	SyncAuth          *appiam.SyncAuthService
	UpdateCurrentUser *appiam.UpdateCurrentUserService
	UpdateUser        *appiam.UpdateUserService
	AssignProperties  *appiam.AssignUserPropertiesService
	JobTrigger        *appjobs.TriggerService
	PropertyQuery     dbpropertyquery.Repository
	PropertyDashboard *appproperty.DashboardService
	LeaseQuery        dbleasequery.Repository
	RepairQuery       RepairQueryRepository
	TenantQuery       dbtenantquery.Repository
	CreateProperty    *appproperty.CreatePropertyService
	UpdateProperty    *appproperty.UpdatePropertyService
	DeleteProperty    *appproperty.DeletePropertyService
	CreateRoom        *appproperty.CreateRoomService
	UpdateRoom        *appproperty.UpdateRoomService
	DeleteRoom        *appproperty.DeleteRoomService
	SetMaintenance    *appproperty.SetRoomMaintenanceService
	CreateTenant      *apptenant.CreateTenantService
	UpdateTenant      *apptenant.UpdateTenantService
	CreateLease       *applease.CreateLeaseService
	Leases            LeaseServices
	Billing           BillingServices
	Journal           JournalServices
	Repair            RepairServices
	Attachment        *appattachment.Service
}

// LeaseServices groups lease command services beyond creation.
type LeaseServices struct {
	UpdateLease         *applease.UpdateLeaseService
	UpdateDeposit       *applease.UpdateDepositService
	ReplaceLease        *applease.ReplaceLeaseService
	TerminateLease      *applease.TerminateLeaseService
	PreviewCheckout     *applease.PreviewCheckoutSettlementService
	FinalizeCheckout    *applease.FinalizeCheckoutSettlementService
	ExportCheckout      *applease.ExportCheckoutSettlementService
	ForceTerminateLease *applease.ForceTerminateLeaseService
	GetForceTermination *applease.GetForceTerminationService
}

// BillingServices groups the billing application entry points used by the
// transport layer.
type BillingServices struct {
	Query             BillingQueryService
	Meter             BillingMeterService
	Payment           BillingPaymentService
	PropertyMeters    BillingPropertyMeterService
	RoomMeters        BillingRoomMeterService
	TenantLeaseRoster BillingTenantLeaseRosterService
	FinancialReports  BillingFinancialReportService
}

// JournalServices groups journal log application services used by the transport layer.
type JournalServices struct {
	List                     *appjournal.ListService
	Get                      *appjournal.GetService
	ListExpenseAccountTitles *appjournal.ListExpenseAccountingTitlesService
	Create                   *appjournal.CreateService
	Update                   *appjournal.UpdateService
	Delete                   *appjournal.DeleteService
}

// RepairServices groups repair request application services used by the transport layer.
type RepairServices struct {
	Create   *apprepair.CreateService
	Update   *apprepair.UpdateService
	Delete   *apprepair.DeleteService
	Workflow *apprepair.WorkflowService
}

// RepairQueryRepository serves repair request read endpoints.
type RepairQueryRepository interface {
	List(ctx context.Context, query apprepair.ListQuery) (apprepair.ListResult, error)
	FindByID(ctx context.Context, id string) (*apprepair.RepairRequest, error)
}

type BillingQueryService interface {
	ListBills(ctx context.Context, input BillingListInput) (BillingListResult, error)
	GetBill(ctx context.Context, input BillingGetInput) (*BillingBill, error)
}

type BillingMeterService interface {
	SubmitBillMeter(ctx context.Context, input BillingMeterInput) (*BillingBill, error)
}

type BillingPaymentService interface {
	RecordBillPayment(ctx context.Context, input BillingPaymentInput) (*BillingBill, error)
}

type BillingPropertyMeterService interface {
	ListPropertyPendingMeters(ctx context.Context, input BillingPropertyMetersInput) ([]BillingBill, error)
	ListPropertyMeterHistory(ctx context.Context, input BillingPropertyMeterHistoryInput) ([]BillingPropertyMeterHistoryRow, error)
}

type BillingRoomMeterService interface {
	ListRoomMeterHistory(ctx context.Context, input BillingRoomMeterHistoryInput) ([]BillingBill, error)
}

type BillingTenantLeaseRosterService interface {
	ListTenantLeaseRoster(ctx context.Context, input BillingTenantLeaseRosterInput) (BillingTenantLeaseRosterResult, error)
}

type BillingFinancialReportService interface {
	ListFinancialReportSummaries(ctx context.Context, input BillingFinancialReportSummaryInput) ([]BillingFinancialReportSummary, error)
	GetFinancialReport(ctx context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error)
	SendFinancialReport(ctx context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error)
	ExportTenantRoster(ctx context.Context, input BillingTenantRosterInput) (*reporthtml.Document, error)
	ExportBillReceipt(ctx context.Context, input BillingReceiptInput) (*reporthtml.Document, error)
	ExportMonthlyCashflow(ctx context.Context, input BillingMonthlyCashflowInput) (*reporthtml.Document, error)
	ExportProfitLoss(ctx context.Context, input BillingProfitLossInput) (*reporthtml.Document, error)
	ExportOperationReport(ctx context.Context, input BillingOperationReportInput) (*reporthtml.Document, error)
}

type BillingListInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          *string
	LeaseID             *string
	TenantID            *string
	Type                string
	Status              string
	Month               *string
	Limit               int
	Offset              int
}

type BillingListResult struct {
	Items []BillingBill
	Total int
}

type BillingGetInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
}

type BillingMeterInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	BillID              string
	CurrentReading      int
}

type BillingPaymentInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	BillID              string
	PaidAmount          int
	PaymentMethod       string
	PaidAt              *time.Time
}

type BillingPropertyMetersInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
}

type BillingPropertyMeterHistoryInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                *int
}

type BillingRoomMeterHistoryInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	RoomID              string
	Year                *int
	Month               *int
}

type BillingTenantLeaseRosterInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	IncludeVacant       bool
	Limit               int
	Offset              int
}

type BillingTenantLeaseRosterResult struct {
	Items []BillingTenantLeaseRosterRow
	Total int
}

type BillingFinancialReportSummaryInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                *int
}

type BillingFinancialReportInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
}

type BillingTenantRosterInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	AsOf                *time.Time
	IncludeVacant       bool
	Format              string
}

type BillingMonthlyCashflowInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
	Format              string
}

type BillingProfitLossInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
	Format              string
}

type BillingOperationReportInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	Year                int
	Month               int
	Format              string
}

type BillingReceiptInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
	Format              string
}

type BillingBill struct {
	ID                   string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
	PropertyLabel        string
	RoomLabel            string
	TenantLabel          string
	PeriodLabel          string
	Type                 string
	Amount               *int
	PeriodStart          time.Time
	PeriodEnd            time.Time
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
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

type BillingPropertyMeterHistoryRow struct {
	BillID          string
	PropertyID      string
	RoomID          string
	RoomLabel       string
	TenantID        string
	TenantLabel     string
	LeaseID         string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	PeriodLabel     string
	DueDate         time.Time
	PreviousReading int
	CurrentReading  int
	Usage           int
	UnitPrice       float64
	Amount          *int
	Status          string
	MeterRecordedAt *time.Time
}

type BillingTenantLeaseRosterRow struct {
	PropertyID         string
	RoomID             string
	RoomLabel          string
	RoomStatus         string
	LeaseID            *string
	LeaseStatus        *string
	TenantID           *string
	TenantLabel        *string
	TenantPhone        *string
	StartDate          *time.Time
	EndDate            *time.Time
	RentAmount         *int
	RentBillingCadence *string
	DepositAmount      *int
	DepositStatus      *string
	NextRentDueDate    *time.Time
	NextRentStatus     *string
	Notes              *string
}

type BillingFinancialReportSummary struct {
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
}

type BillingFinancialReport struct {
	PropertyID   string
	Year         int
	Month        int
	TotalIncome  int
	TotalExpense int
	Net          int
	IsFinalized  bool
	Entries      []BillingFinancialReportEntry
}

type BillingFinancialReportEntry struct {
	ID                  string
	Category            string
	AccountingTitleID   *string
	AccountingTitleCode *string
	AccountingTitleName *string
	SourceDate          *time.Time
	RoomLabel           *string
	TenantLabel         *string
	PeriodLabel         *string
	DisplayNote         *string
	Description         *string
	Amount              int
	Source              *BillingFinancialReportEntrySource
}

type BillingFinancialReportEntrySource struct {
	Type   string
	ID     string
	Detail *string
}

// NewAPIServer returns an API server with all exposed endpoint dependencies
// wired. Missing dependencies fail during construction instead of at request
// time.
func NewAPIServer(deps APIServerDeps) *APIServer {
	validateAPIServerDeps(deps)

	return &APIServer{
		userRepo:          deps.UserRepo,
		createUserService: deps.CreateUser,
		sendPasswordReset: deps.SendPasswordReset,
		syncAuthService:   deps.SyncAuth,
		updateCurrentUser: deps.UpdateCurrentUser,
		updateUser:        deps.UpdateUser,
		assignProperties:  deps.AssignProperties,
		jobTriggerService: deps.JobTrigger,
		propertyQueryRepo: deps.PropertyQuery,
		propertyDashboard: deps.PropertyDashboard,
		leaseQueryRepo:    deps.LeaseQuery,
		repairQueryRepo:   deps.RepairQuery,
		tenantQueryRepo:   deps.TenantQuery,
		createPropertySvc: deps.CreateProperty,
		updatePropertySvc: deps.UpdateProperty,
		deletePropertySvc: deps.DeleteProperty,
		createRoomSvc:     deps.CreateRoom,
		updateRoomSvc:     deps.UpdateRoom,
		deleteRoomSvc:     deps.DeleteRoom,
		setMaintenanceSvc: deps.SetMaintenance,
		createTenantSvc:   deps.CreateTenant,
		updateTenantSvc:   deps.UpdateTenant,
		createLeaseSvc:    deps.CreateLease,
		updateLeaseSvc:    deps.Leases.UpdateLease,
		updateDepositSvc:  deps.Leases.UpdateDeposit,
		replaceLeaseSvc:   deps.Leases.ReplaceLease,
		terminateLeaseSvc: deps.Leases.TerminateLease,
		previewCheckout:   deps.Leases.PreviewCheckout,
		finalizeCheckout:  deps.Leases.FinalizeCheckout,
		exportCheckout:    deps.Leases.ExportCheckout,
		forceTerminateSvc: deps.Leases.ForceTerminateLease,
		getForceTermSvc:   deps.Leases.GetForceTermination,
		billing:           deps.Billing,
		journal:           deps.Journal,
		repair:            deps.Repair,
		attachment:        deps.Attachment,
	}
}

func validateAPIServerDeps(deps APIServerDeps) {
	required := []struct {
		name string
		dep  any
	}{
		{"user_repo", deps.UserRepo},
		{"create_user", deps.CreateUser},
		{"send_password_reset", deps.SendPasswordReset},
		{"sync_auth", deps.SyncAuth},
		{"update_current_user", deps.UpdateCurrentUser},
		{"update_user", deps.UpdateUser},
		{"assign_properties", deps.AssignProperties},
		{"job_trigger", deps.JobTrigger},
		{"property_query", deps.PropertyQuery},
		{"property_dashboard", deps.PropertyDashboard},
		{"lease_query", deps.LeaseQuery},
		{"repair_query", deps.RepairQuery},
		{"tenant_query", deps.TenantQuery},
		{"create_property", deps.CreateProperty},
		{"update_property", deps.UpdateProperty},
		{"delete_property", deps.DeleteProperty},
		{"create_room", deps.CreateRoom},
		{"update_room", deps.UpdateRoom},
		{"delete_room", deps.DeleteRoom},
		{"set_maintenance", deps.SetMaintenance},
		{"create_tenant", deps.CreateTenant},
		{"update_tenant", deps.UpdateTenant},
		{"create_lease", deps.CreateLease},
		{"update_lease", deps.Leases.UpdateLease},
		{"update_deposit", deps.Leases.UpdateDeposit},
		{"replace_lease", deps.Leases.ReplaceLease},
		{"terminate_lease", deps.Leases.TerminateLease},
		{"preview_checkout", deps.Leases.PreviewCheckout},
		{"finalize_checkout", deps.Leases.FinalizeCheckout},
		{"export_checkout", deps.Leases.ExportCheckout},
		{"force_terminate", deps.Leases.ForceTerminateLease},
		{"get_force_termination", deps.Leases.GetForceTermination},
		{"billing_query", deps.Billing.Query},
		{"billing_meter", deps.Billing.Meter},
		{"billing_payment", deps.Billing.Payment},
		{"billing_property_meters", deps.Billing.PropertyMeters},
		{"billing_room_meters", deps.Billing.RoomMeters},
		{"billing_tenant_lease_roster", deps.Billing.TenantLeaseRoster},
		{"billing_financial_reports", deps.Billing.FinancialReports},
		{"journal_list", deps.Journal.List},
		{"journal_get", deps.Journal.Get},
		{"journal_accounting_titles", deps.Journal.ListExpenseAccountTitles},
		{"journal_create", deps.Journal.Create},
		{"journal_update", deps.Journal.Update},
		{"journal_delete", deps.Journal.Delete},
		{"repair_create", deps.Repair.Create},
		{"repair_update", deps.Repair.Update},
		{"repair_delete", deps.Repair.Delete},
		{"repair_workflow", deps.Repair.Workflow},
		{"attachment", deps.Attachment},
	}
	for _, item := range required {
		if item.dep == nil {
			panic("handler.NewAPIServer missing dependency: " + item.name)
		}
	}
}

func (s *APIServer) listAttachments(c *gin.Context, resourceType appattachment.ResourceType, id openapi_types.UUID) {
	attachments, err := s.attachment.ListAttachments(c.Request.Context(), resourceType, id.String())
	if err != nil {
		c.Error(err)
		return
	}

	items := make([]api.AttachmentResponse, 0, len(attachments))
	for i := range attachments {
		items = append(items, toAttachmentResponse(&attachments[i]))
	}

	c.JSON(http.StatusOK, api.AttachmentListResponse{Data: &items})
}

func (s *APIServer) createAttachment(c *gin.Context, resourceType appattachment.ResourceType, id openapi_types.UUID) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.RegisterAttachmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	attachment, err := s.attachment.RegisterAttachment(c.Request.Context(), appattachment.RegisterAttachmentInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		ResourceType:        resourceType,
		ResourceID:          id.String(),
		Nonce:               request.Nonce,
		FileName:            request.FileName,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toAttachmentResponse(attachment))
}

func toAttachmentResponse(attachment *appattachment.Attachment) api.AttachmentResponse {
	id := uuid.MustParse(attachment.ID)
	objectPath := attachment.ObjectPath
	fileName := attachment.FileName
	createdAt := attachment.CreatedAt

	var uploadedBy *openapi_types.UUID
	if attachment.UploadedBy != nil {
		parsed := uuid.MustParse(*attachment.UploadedBy)
		uploadedBy = &parsed
	}
	var photoStage *api.AttachmentResponsePhotoStage
	if attachment.PhotoStage != nil {
		stage := api.AttachmentResponsePhotoStage(*attachment.PhotoStage)
		photoStage = &stage
	}

	return api.AttachmentResponse{
		Id:         &id,
		ObjectPath: &objectPath,
		FileName:   &fileName,
		UploadedBy: uploadedBy,
		SortOrder:  attachment.SortOrder,
		PhotoStage: photoStage,
		CreatedAt:  &createdAt,
	}
}

func mapRepairQueryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apprepair.ErrRepairRequestNotFound) {
		return apperr.ErrRepairRequestNotFound
	}

	return apperr.ErrInternalServerError.WithCause(err)
}

// SyncAuth handles the auth sync endpoint.
func (s *APIServer) SyncAuth(c *gin.Context) {
	firebaseUID, ok := requestctx.GetFirebaseUID(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	user, err := s.syncAuthService.Execute(c.Request.Context(), firebaseUID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toIAMUserResponse(user))
}

// ListBills handles the bill listing endpoint.
func (s *APIServer) ListBills(c *gin.Context, params api.ListBillsParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	if params.Month != nil && !queryparams.IsYYYYMM(*params.Month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must use YYYY-MM format",
		}))
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	billType := ""
	if params.Type != nil {
		billType = string(*params.Type)
	}

	var tenantID *string
	if params.TenantId != nil {
		value := params.TenantId.String()
		tenantID = &value
	}

	result, err := s.billing.Query.ListBills(c.Request.Context(), BillingListInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          params.PropertyId,
		LeaseID:             params.LeaseId,
		TenantID:            tenantID,
		Type:                billType,
		Status:              status,
		Month:               params.Month,
		Limit:               pagination.Limit,
		Offset:              pagination.Offset,
	})
	if err != nil {
		c.Error(err)
		return
	}

	items := make([]api.BillResponse, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toBillResponse(&result.Items[i]))
	}

	c.JSON(http.StatusOK, api.BillListResponse{Data: &items, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateAttachmentUploadURL handles attachment upload URL creation.
func (s *APIServer) CreateAttachmentUploadURL(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.AttachmentUploadURLRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	result, err := s.attachment.CreateUploadURL(c.Request.Context(), appattachment.CreateUploadURLInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		ResourceType:        appattachment.ResourceType(request.ResourceType),
		ResourceID:          request.ResourceId.String(),
		FileName:            request.FileName,
		ContentType:         string(request.ContentType),
		FileSize:            request.FileSize,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, api.AttachmentUploadURLResponse{
		UploadUrl: &result.UploadURL,
		Nonce:     &result.Nonce,
		ExpiresAt: &result.ExpiresAt,
	})
}

// DeleteAttachment handles attachment deletion.
func (s *APIServer) DeleteAttachment(c *gin.Context, id openapi_types.UUID) {
	if err := s.attachment.DeleteAttachment(c.Request.Context(), id.String()); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListBillAttachments handles bill attachment listing.
func (s *APIServer) ListBillAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeBill, id)
}

// CreateBillAttachment handles bill attachment registration.
func (s *APIServer) CreateBillAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeBill, id)
}

// GetBill handles the bill detail endpoint.
func (s *APIServer) GetBill(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	bill, err := s.billing.Query.GetBill(c.Request.Context(), BillingGetInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		BillID:              id,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillResponse(bill))
}

// SubmitBillMeter handles meter submission for a bill.
func (s *APIServer) SubmitBillMeter(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request struct {
		CurrentReading *int `json:"current_reading"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}
	if request.CurrentReading == nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "current_reading"}))
		return
	}

	bill, err := s.billing.Meter.SubmitBillMeter(c.Request.Context(), BillingMeterInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		BillID:              id,
		CurrentReading:      *request.CurrentReading,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillResponse(bill))
}

// RecordBillPayment handles payment recording for a bill.
func (s *APIServer) RecordBillPayment(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.RecordPaymentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	bill, err := s.billing.Payment.RecordBillPayment(c.Request.Context(), BillingPaymentInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		BillID:              id,
		PaidAmount:          request.PaidAmount,
		PaymentMethod:       string(request.PaymentMethod),
		PaidAt:              request.PaidAt,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillResponse(bill))
}

// GetForceTermination handles force-termination detail retrieval.
func (s *APIServer) GetForceTermination(c *gin.Context, id openapi_types.UUID) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	forceTermination, err := s.getForceTermSvc.Execute(c.Request.Context(), applease.GetForceTerminationInput{
		ActorRole:          principal.Role,
		ForceTerminationID: id.String(),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toForceTerminationResponse(forceTermination))
}

// RunForceTerminationCompensationJob handles the force-termination compensation job trigger.
func (s *APIServer) RunForceTerminationCompensationJob(c *gin.Context, params api.RunForceTerminationCompensationJobParams) {
	s.runJob(c, appjobs.JobForceTerminationCompensation, params.WindowKey)
}

// RunLeaseExpiryJob handles the lease expiry scan job trigger.
func (s *APIServer) RunLeaseExpiryJob(c *gin.Context, params api.RunLeaseExpiryJobParams) {
	s.runJob(c, appjobs.JobLeaseExpiryScan, params.WindowKey)
}

// RunLeaseExpiringSoonReminderJob handles the lease expiring soon reminder job trigger.
func (s *APIServer) RunLeaseExpiringSoonReminderJob(c *gin.Context, params api.RunLeaseExpiringSoonReminderJobParams) {
	s.runJob(c, appjobs.JobLeaseExpiringSoonReminder, params.WindowKey)
}

// RunMonthlySnapshotJob handles the monthly snapshot job trigger.
func (s *APIServer) RunMonthlySnapshotJob(c *gin.Context, params api.RunMonthlySnapshotJobParams) {
	s.runJob(c, appjobs.JobMonthlySnapshot, params.WindowKey)
}

// RunOverdueBillReminderJob handles the overdue bill reminder job trigger.
func (s *APIServer) RunOverdueBillReminderJob(c *gin.Context, params api.RunOverdueBillReminderJobParams) {
	s.runJob(c, appjobs.JobOverdueBillReminders, params.WindowKey)
}

// RunOverdueBillsScanJob handles the overdue bills scan job trigger.
func (s *APIServer) RunOverdueBillsScanJob(c *gin.Context, params api.RunOverdueBillsScanJobParams) {
	s.runJob(c, appjobs.JobOverdueBillsScan, params.WindowKey)
}

// ListJournalExpenseAccountingTitles handles the journal expense accounting title option endpoint.
func (s *APIServer) ListJournalExpenseAccountingTitles(c *gin.Context) {
	titles, err := s.journal.ListExpenseAccountTitles.Execute(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	responses := make([]api.AccountingTitleOption, 0, len(titles))
	for i := range titles {
		responses = append(responses, toAccountingTitleOption(titles[i]))
	}
	c.JSON(http.StatusOK, api.AccountingTitleOptionListResponse{Data: &responses})
}

// ListJournalLogs handles the journal log listing endpoint.
func (s *APIServer) ListJournalLogs(c *gin.Context, params api.ListJournalLogsParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}
	var propertyID *string
	if authorizedPropertyID := requestctx.GetPropertyID(c); authorizedPropertyID != "" {
		propertyID = &authorizedPropertyID
	}
	roomID, err := queryparams.NormalizeOptionalUUID(params.RoomId, "room_id")
	if err != nil {
		c.Error(err)
		return
	}
	var dateFrom *time.Time
	if params.DateFrom != nil {
		value := params.DateFrom.Time
		dateFrom = &value
	}
	var dateTo *time.Time
	if params.DateTo != nil {
		value := params.DateTo.Time
		dateTo = &value
	}

	result, err := s.journal.List.Execute(c.Request.Context(), appjournal.ListInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          propertyID,
		RoomID:              roomID,
		DateFrom:            dateFrom,
		DateTo:              dateTo,
		Limit:               pagination.Limit,
		Offset:              pagination.Offset,
	})
	if err != nil {
		c.Error(err)
		return
	}

	responses := make([]api.JournalLogResponse, 0, len(result.Items))
	for i := range result.Items {
		responses = append(responses, toJournalLogResponse(&result.Items[i]))
	}
	c.JSON(http.StatusOK, api.JournalLogListResponse{Data: &responses, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateJournalLog handles journal log creation.
func (s *APIServer) CreateJournalLog(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.CreateJournalLogRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var roomID *string
	if request.RoomId != nil {
		value := request.RoomId.String()
		roomID = &value
	}
	var expenseAccountingTitleID *string
	if request.ExpenseAccountingTitleId != nil {
		value := request.ExpenseAccountingTitleId.String()
		expenseAccountingTitleID = &value
	}
	journalLog, err := s.journal.Create.Execute(c.Request.Context(), appjournal.CreateInput{
		ActorRole:                principal.Role,
		ActorUserID:              principal.UserID,
		AssignedPropertyIDs:      principal.AssignedPropertyIDs,
		PropertyID:               request.PropertyId.String(),
		RoomID:                   roomID,
		Content:                  request.Content,
		ExpenseAmount:            request.ExpenseAmount,
		ExpenseDescription:       request.ExpenseDescription,
		ExpenseAccountingTitleID: expenseAccountingTitleID,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toJournalLogResponse(journalLog))
}

// ListJournalLogAttachments handles journal log attachment listing.
func (s *APIServer) ListJournalLogAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeJournalLog, id)
}

// CreateJournalLogAttachment handles journal log attachment registration.
func (s *APIServer) CreateJournalLogAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeJournalLog, id)
}

// DeleteJournalLog handles journal log deletion.
func (s *APIServer) DeleteJournalLog(c *gin.Context, id string) {
	if err := s.journal.Delete.Execute(c.Request.Context(), appjournal.DeleteInput{ID: id}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetJournalLog handles journal log detail retrieval.
func (s *APIServer) GetJournalLog(c *gin.Context, id string) {
	journalLog, err := s.journal.Get.Execute(c.Request.Context(), appjournal.GetInput{ID: id})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toJournalLogResponse(journalLog))
}

// UpdateJournalLog handles journal log updates.
func (s *APIServer) UpdateJournalLog(c *gin.Context, id string) {
	var request api.UpdateJournalLogRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var expenseAccountingTitleID *string
	if request.ExpenseAccountingTitleId != nil {
		value := request.ExpenseAccountingTitleId.String()
		expenseAccountingTitleID = &value
	}
	journalLog, err := s.journal.Update.Execute(c.Request.Context(), appjournal.UpdateInput{
		ID:                       id,
		Content:                  request.Content,
		ExpenseAmount:            request.ExpenseAmount,
		ExpenseDescription:       request.ExpenseDescription,
		ExpenseAccountingTitleID: expenseAccountingTitleID,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toJournalLogResponse(journalLog))
}

// ListLeases handles the lease listing endpoint.
func (s *APIServer) ListLeases(c *gin.Context, params api.ListLeasesParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}

	result, err := s.leaseQueryRepo.ListAccessible(c.Request.Context(), principal.Role, principal.AssignedPropertyIDs, dbleasequery.ListParams{
		PropertyID: params.PropertyId,
		RoomID:     params.RoomId,
		TenantID:   params.TenantId,
		Status:     status,
		Limit:      pagination.Limit,
		Offset:     pagination.Offset,
	})
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	items := make([]api.LeaseResponse, 0, len(result.Items))
	for _, lease := range result.Items {
		items = append(items, toLeaseResponse(&lease))
	}

	c.JSON(http.StatusOK, api.LeaseListResponse{Data: &items, Pagination: toPaginationResponse(pagination, result.Total)})
}

// ListLeaseCheckoutReviews handles the checkout review read-model endpoint.
func (s *APIServer) ListLeaseCheckoutReviews(c *gin.Context, params api.ListLeaseCheckoutReviewsParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	var propertyID *string
	if params.PropertyId != nil {
		value := params.PropertyId.String()
		propertyID = &value
	}

	result, err := s.leaseQueryRepo.ListCheckoutReviewsAccessible(c.Request.Context(), principal.Role, principal.AssignedPropertyIDs, dbleasequery.CheckoutReviewListParams{
		PropertyID: propertyID,
		Status:     status,
		Limit:      pagination.Limit,
		Offset:     pagination.Offset,
	})
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	items := make([]api.LeaseCheckoutReviewResponse, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toLeaseCheckoutReviewResponse(&result.Items[i]))
	}

	c.JSON(http.StatusOK, api.LeaseCheckoutReviewListResponse{Data: &items, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateLease handles lease creation.
func (s *APIServer) CreateLease(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.CreateLeaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var cadence *string
	if request.ElectricityBillingCadence != nil {
		value := string(*request.ElectricityBillingCadence)
		cadence = &value
	}
	var rentCadence string
	if request.RentBillingCadence != nil {
		rentCadence = string(*request.RentBillingCadence)
	}
	if request.StartingMeterReading == nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "starting_meter_reading"}))
		return
	}

	lease, err := s.createLeaseSvc.Execute(c.Request.Context(), applease.CreateLeaseInput{
		ActorRole:                 principal.Role,
		AssignedPropertyIDs:       principal.AssignedPropertyIDs,
		TenantID:                  request.TenantId.String(),
		RoomID:                    request.RoomId.String(),
		RentAmount:                request.RentAmount,
		StartDate:                 request.StartDate.Time,
		EndDate:                   request.EndDate.Time,
		DepositAmount:             request.DepositAmount,
		RentBillingCadence:        rentCadence,
		ElectricityBillingCadence: cadence,
		StartingMeterReading:      *request.StartingMeterReading,
		Notes:                     request.Notes,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedLeaseResponse(lease))
}

// ListLeaseAttachments handles lease attachment listing.
func (s *APIServer) ListLeaseAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeLease, id)
}

// CreateLeaseAttachment handles lease attachment registration.
func (s *APIServer) CreateLeaseAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeLease, id)
}

// GetLease handles lease detail retrieval.
func (s *APIServer) GetLease(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	lease, err := s.leaseQueryRepo.FindByIDAccessible(c.Request.Context(), id, principal.Role, principal.AssignedPropertyIDs)
	if err != nil {
		switch {
		case errors.Is(err, dbleasequery.ErrNotFound):
			c.Error(apperr.ErrLeaseNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toLeaseResponse(lease))
}

// UpdateLease handles lease updates.
func (s *APIServer) UpdateLease(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.UpdateLeaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	lease, err := s.updateLeaseSvc.Execute(c.Request.Context(), applease.UpdateLeaseInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		RentAmount:          request.RentAmount,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedLeaseResponse(lease))
}

// UpdateLeaseDeposit handles lease deposit updates.
func (s *APIServer) UpdateLeaseDeposit(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.UpdateDepositRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	lease, err := s.updateDepositSvc.Execute(c.Request.Context(), applease.UpdateDepositInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		RefundAmount:        request.RefundAmount,
		DeductionAmount:     request.DeductionAmount,
		DeductionReason:     request.DeductionReason,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedLeaseResponse(lease))
}

// ReplaceLease handles lease replacement.
func (s *APIServer) ReplaceLease(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.LeaseReplaceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var notes *string
	if request.NewLease.Notes != nil {
		value := strings.TrimSpace(*request.NewLease.Notes)
		if value != "" {
			notes = &value
		}
	}

	result, err := s.replaceLeaseSvc.Execute(c.Request.Context(), applease.ReplaceLeaseInput{
		ActorRole:             principal.Role,
		AssignedPropertyIDs:   principal.AssignedPropertyIDs,
		LeaseID:               id,
		Reason:                string(request.Reason),
		EffectiveStartDate:    request.EffectiveStartDate.Time,
		DepositHandling:       string(request.DepositHandling),
		NewEndDate:            request.NewLease.EndDate.Time,
		NewRentAmount:         request.NewLease.RentAmount,
		NewRentBillingCadence: string(request.NewLease.RentBillingCadence),
		NewCadence:            string(request.NewLease.ElectricityBillingCadence),
		NewNotes:              notes,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toLeaseReplaceResponse(result))
}

// ForceTerminateLease handles forced lease termination.
func (s *APIServer) ForceTerminateLease(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.ForceTerminateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	forceTermination, err := s.forceTerminateSvc.Execute(c.Request.Context(), applease.ForceTerminateLeaseInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		Reason:              request.Reason,
		DepositHandling:     string(request.DepositHandling),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toForceTerminationResponse(forceTermination))
}

// TerminateLease handles lease termination.
func (s *APIServer) TerminateLease(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.TerminateLeaseRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	lease, err := s.terminateLeaseSvc.Execute(c.Request.Context(), applease.TerminateLeaseInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		RefundAmount:        request.RefundAmount,
		DeductionAmount:     request.DeductionAmount,
		DeductionReason:     request.DepositDeductionReason,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedLeaseResponse(lease))
}

// PreviewLeaseCheckoutSettlement handles checkout settlement preview.
func (s *APIServer) PreviewLeaseCheckoutSettlement(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.CheckoutSettlementPreviewRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	settlement, err := s.previewCheckout.Execute(c.Request.Context(), toCheckoutSettlementInput(principal, id, request, ""))
	if err != nil {
		c.Error(err)
		return
	}

	response, err := toCheckoutSettlementResponse(settlement)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// FinalizeLeaseCheckoutSettlement handles checkout settlement finalization.
func (s *APIServer) FinalizeLeaseCheckoutSettlement(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.CheckoutSettlementFinalizeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	settlement, err := s.finalizeCheckout.Execute(c.Request.Context(), toCheckoutSettlementInput(principal, id, api.CheckoutSettlementInput{
		CheckoutDate:      request.CheckoutDate,
		CleaningFee:       request.CleaningFee,
		FinalMeterReading: request.FinalMeterReading,
		KeyCardLossFee:    request.KeyCardLossFee,
		Notes:             request.Notes,
		OtherFee:          request.OtherFee,
		OtherFeeReason:    request.OtherFeeReason,
		Reason:            request.Reason,
	}, request.PreviewToken))
	if err != nil {
		c.Error(err)
		return
	}

	response, err := toCheckoutSettlementResponse(settlement)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// ExportLeaseCheckoutSettlement handles finalized checkout settlement HTML export.
func (s *APIServer) ExportLeaseCheckoutSettlement(c *gin.Context, id string, params api.ExportLeaseCheckoutSettlementParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	if params.Format != nil && string(*params.Format) != "html" {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "format"}))
		return
	}

	document, err := s.exportCheckout.Execute(c.Request.Context(), applease.CheckoutSettlementInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
	})
	if err != nil {
		c.Error(err)
		return
	}
	writeHTMLDocument(c, document)
}

// ListProperties handles the property listing endpoint.
func (s *APIServer) ListProperties(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	properties, err := s.propertyQueryRepo.ListAccessible(c.Request.Context(), principal.Role, principal.UserID, principal.AssignedPropertyIDs)
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	items := make([]api.PropertyResponse, 0, len(properties))
	for _, property := range properties {
		items = append(items, toPropertyResponse(&property))
	}

	c.JSON(http.StatusOK, api.PropertyListResponse{Data: &items})
}

// GetDashboard handles home dashboard retrieval.
func (s *APIServer) GetDashboard(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	dashboard, err := s.propertyDashboard.ExecuteHome(c.Request.Context(), appproperty.HomeDashboardInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toHomeDashboardResponse(dashboard))
}

// CreateProperty handles property creation.
func (s *APIServer) CreateProperty(c *gin.Context) {
	var request api.CreatePropertyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}
	if request.OwnerId == uuid.Nil {
		c.Error(apperr.ErrValidationOwnerIDRequired)
		return
	}

	property, err := s.createPropertySvc.Execute(c.Request.Context(), appproperty.CreatePropertyInput{
		Name:                             request.Name,
		Subtitle:                         request.Subtitle,
		Address:                          request.Address,
		ElectricityUnitPrice:             request.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: string(request.DefaultElectricityBillingCadence),
		OwnerID:                          request.OwnerId.String(),
		ContactPhone:                     request.ContactPhone,
		ContactEmail:                     emailPtrToStringPtr(request.ContactEmail),
		Notes:                            request.Notes,
		Facilities:                       request.Facilities,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedPropertyResponse(property))
}

// ListPropertyAttachments handles property attachment listing.
func (s *APIServer) ListPropertyAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeProperty, id)
}

// CreatePropertyAttachment handles property attachment registration.
func (s *APIServer) CreatePropertyAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeProperty, id)
}

// DeleteProperty handles property deletion.
func (s *APIServer) DeleteProperty(c *gin.Context, id string) {
	if err := s.deletePropertySvc.Execute(c.Request.Context(), appproperty.DeletePropertyInput{
		ID: id,
	}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetProperty handles property detail retrieval.
func (s *APIServer) GetProperty(c *gin.Context, id string) {
	property, err := s.propertyQueryRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		switch err {
		case dbpropertyquery.ErrNotFound:
			c.Error(apperr.ErrPropertyNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toPropertyResponse(property))
}

// UpdateProperty handles property updates.
func (s *APIServer) UpdateProperty(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.UpdatePropertyRequest
	fields, err := bindJSONWithFields(c, &request)
	if err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var cadence *string
	if request.DefaultElectricityBillingCadence != nil {
		value := string(*request.DefaultElectricityBillingCadence)
		cadence = &value
	}

	property, err := s.updatePropertySvc.Execute(c.Request.Context(), appproperty.UpdatePropertyInput{
		ID:                               id,
		ActorRole:                        principal.Role,
		Name:                             request.Name,
		Subtitle:                         request.Subtitle,
		ClearSubtitle:                    jsonFieldIsNull(fields, "subtitle"),
		Address:                          request.Address,
		ElectricityUnitPrice:             request.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: cadence,
		ContactPhone:                     request.ContactPhone,
		ClearContactPhone:                jsonFieldIsNull(fields, "contact_phone"),
		ContactEmail:                     emailPtrToStringPtr(request.ContactEmail),
		ClearContactEmail:                jsonFieldIsNull(fields, "contact_email"),
		Notes:                            request.Notes,
		ClearNotes:                       jsonFieldIsNull(fields, "notes"),
		Facilities:                       request.Facilities,
		ClearFacilities:                  jsonFieldIsNull(fields, "facilities"),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedPropertyResponse(property))
}

// GetPropertyDashboard handles property dashboard retrieval.
func (s *APIServer) GetPropertyDashboard(c *gin.Context, id string) {
	dashboard, err := s.propertyDashboard.Execute(c.Request.Context(), appproperty.DashboardInput{
		PropertyID: id,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toDashboardResponse(dashboard))
}

// GetPropertyFinancialReportSummary handles financial report summary retrieval
// for a property.
func (s *APIServer) GetPropertyFinancialReportSummary(c *gin.Context, id string, params api.GetPropertyFinancialReportSummaryParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	summaries, err := s.billing.FinancialReports.ListFinancialReportSummaries(c.Request.Context(), BillingFinancialReportSummaryInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                params.Year,
	})
	if err != nil {
		c.Error(err)
		return
	}

	items := make([]api.FinancialReportSummaryItem, 0, len(summaries))
	for i := range summaries {
		items = append(items, toFinancialReportSummaryItem(summaries[i]))
	}

	c.JSON(http.StatusOK, api.FinancialReportListResponse{Data: &items})
}

// GetPropertyFinancialReport handles financial report retrieval for a property
// month.
func (s *APIServer) GetPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	if !isValidMonth(month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	report, err := s.billing.FinancialReports.GetFinancialReport(c.Request.Context(), BillingFinancialReportInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                year,
		Month:               month,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toFinancialReportResponse(report))
}

// SendPropertyFinancialReport handles sending a property's financial report.
func (s *APIServer) SendPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	if !isValidMonth(month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	report, err := s.billing.FinancialReports.SendFinancialReport(c.Request.Context(), BillingFinancialReportInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                year,
		Month:               month,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toFinancialReportResponse(report))
}

// ExportPropertyFinancialReportCashflow handles monthly cashflow HTML export.
func (s *APIServer) ExportPropertyFinancialReportCashflow(c *gin.Context, id string, year int, month int, params api.ExportPropertyFinancialReportCashflowParams) {
	if !isValidMonth(month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}

	document, err := s.billing.FinancialReports.ExportMonthlyCashflow(c.Request.Context(), BillingMonthlyCashflowInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                year,
		Month:               month,
		Format:              format,
	})
	if err != nil {
		c.Error(err)
		return
	}

	writeHTMLDocument(c, document)
}

// ExportPropertyFinancialReportProfitLoss handles profit and loss HTML export.
func (s *APIServer) ExportPropertyFinancialReportProfitLoss(c *gin.Context, id string, year int, month int, params api.ExportPropertyFinancialReportProfitLossParams) {
	if !isValidMonth(month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}

	document, err := s.billing.FinancialReports.ExportProfitLoss(c.Request.Context(), BillingProfitLossInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                year,
		Month:               month,
		Format:              format,
	})
	if err != nil {
		c.Error(err)
		return
	}

	writeHTMLDocument(c, document)
}

// ExportPropertyOperationReport handles monthly operation report HTML export.
func (s *APIServer) ExportPropertyOperationReport(c *gin.Context, id string, year int, month int, params api.ExportPropertyOperationReportParams) {
	if !isValidMonth(month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}

	document, err := s.billing.FinancialReports.ExportOperationReport(c.Request.Context(), BillingOperationReportInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                year,
		Month:               month,
		Format:              format,
	})
	if err != nil {
		c.Error(err)
		return
	}

	writeHTMLDocument(c, document)
}

// ExportPropertyTenantRoster handles tenant roster runtime HTML export.
func (s *APIServer) ExportPropertyTenantRoster(c *gin.Context, id string, params api.ExportPropertyTenantRosterParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var asOf *time.Time
	if params.AsOf != nil {
		value := params.AsOf.Time
		asOf = &value
	}
	includeVacant := false
	if params.IncludeVacant != nil {
		includeVacant = *params.IncludeVacant
	}
	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}

	document, err := s.billing.FinancialReports.ExportTenantRoster(c.Request.Context(), BillingTenantRosterInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		AsOf:                asOf,
		IncludeVacant:       includeVacant,
		Format:              format,
	})
	if err != nil {
		c.Error(err)
		return
	}

	writeHTMLDocument(c, document)
}

// ListPropertyTenantLeaseRoster handles property tenant/lease roster JSON reads.
func (s *APIServer) ListPropertyTenantLeaseRoster(c *gin.Context, id string, params api.ListPropertyTenantLeaseRosterParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	includeVacant := false
	if params.IncludeVacant != nil {
		includeVacant = *params.IncludeVacant
	}

	result, err := s.billing.TenantLeaseRoster.ListTenantLeaseRoster(c.Request.Context(), BillingTenantLeaseRosterInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		IncludeVacant:       includeVacant,
		Limit:               pagination.Limit,
		Offset:              pagination.Offset,
	})
	if err != nil {
		c.Error(err)
		return
	}

	response := toPropertyTenantLeaseRosterResponse(result.Items)
	response.Pagination = toPaginationResponse(pagination, result.Total)
	c.JSON(http.StatusOK, response)
}

// ExportBillReceipt handles bill receipt runtime HTML export.
func (s *APIServer) ExportBillReceipt(c *gin.Context, id string, params api.ExportBillReceiptParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	format := ""
	if params.Format != nil {
		format = string(*params.Format)
	}

	document, err := s.billing.FinancialReports.ExportBillReceipt(c.Request.Context(), BillingReceiptInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		BillID:              id,
		Format:              format,
	})
	if err != nil {
		c.Error(err)
		return
	}

	writeHTMLDocument(c, document)
}

// ListPropertyMeterHistory handles property meter history retrieval.
func (s *APIServer) ListPropertyMeterHistory(c *gin.Context, id string, params api.ListPropertyMeterHistoryParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	rows, err := s.billing.PropertyMeters.ListPropertyMeterHistory(c.Request.Context(), BillingPropertyMeterHistoryInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
		Year:                params.Year,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toPropertyMeterHistoryResponse(rows))
}

func writeHTMLDocument(c *gin.Context, document *reporthtml.Document) {
	if document == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "html_document"}))
		return
	}
	c.Header("Content-Disposition", reporthtml.InlineContentDisposition(document.Filename))
	c.Data(http.StatusOK, reporthtml.ContentType, document.HTML)
}

// ListPropertyPendingMeters handles pending meter retrieval for a property.
func (s *APIServer) ListPropertyPendingMeters(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	bills, err := s.billing.PropertyMeters.ListPropertyPendingMeters(c.Request.Context(), BillingPropertyMetersInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          id,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillListResponse(bills))
}

// ListPropertyRooms handles room listing for a property.
func (s *APIServer) ListPropertyRooms(c *gin.Context, id string, params api.ListPropertyRoomsParams) {
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
		if !isSupportedRoomStatus(status) {
			c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
				"field":  "status",
				"reason": "must be one of vacant, occupied, maintenance",
			}))
			return
		}
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	result, err := s.propertyQueryRepo.ListRoomsByProperty(c.Request.Context(), id, status, pagination.Limit, pagination.Offset)
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	if len(result.Items) == 0 {
		_, err := s.propertyQueryRepo.FindByID(c.Request.Context(), id)
		if err != nil {
			switch err {
			case dbpropertyquery.ErrNotFound:
				c.Error(apperr.ErrPropertyNotFound)
			default:
				c.Error(apperr.ErrInternalServerError.WithCause(err))
			}
			return
		}
	}

	items := make([]api.RoomResponse, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toRoomResponse(&result.Items[i]))
	}

	c.JSON(http.StatusOK, api.RoomListResponse{Data: &items, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreatePropertyRoom handles room creation within a property.
func (s *APIServer) CreatePropertyRoom(c *gin.Context, id string) {
	var request api.CreateRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	room, err := s.createRoomSvc.Execute(c.Request.Context(), appproperty.CreateRoomInput{
		PropertyID:        id,
		Name:              request.Name,
		Size:              request.Size,
		Floor:             request.Floor,
		RoomType:          request.RoomType,
		Facilities:        request.Facilities,
		DefaultRentAmount: request.DefaultRentAmount,
		Notes:             request.Notes,
		Zone:              request.Zone,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedRoomResponse(room))
}

// ListRepairRequests handles the repair request listing endpoint.
func (s *APIServer) ListRepairRequests(c *gin.Context, params api.ListRepairRequestsParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	var statusPtr *string
	if status != "" {
		statusPtr = &status
	}
	var propertyID *string
	if authorizedPropertyID := requestctx.GetPropertyID(c); authorizedPropertyID != "" {
		propertyID = &authorizedPropertyID
	}
	roomID, err := queryparams.NormalizeOptionalUUID(params.RoomId, "room_id")
	if err != nil {
		c.Error(err)
		return
	}
	assignedTo, err := queryparams.NormalizeOptionalUUID(params.AssignedTo, "assigned_to")
	if err != nil {
		c.Error(err)
		return
	}

	result, err := s.repairQueryRepo.List(c.Request.Context(), apprepair.ListQuery{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          propertyID,
		RoomID:              roomID,
		Status:              statusPtr,
		AssignedTo:          assignedTo,
		Limit:               pagination.Limit,
		Offset:              pagination.Offset,
	})
	if err != nil {
		c.Error(mapRepairQueryError(err))
		return
	}

	responses := make([]api.RepairRequestResponse, 0, len(result.Items))
	for _, item := range result.Items {
		responses = append(responses, toApplicationRepairRequestResponse(&item))
	}
	c.JSON(http.StatusOK, api.RepairRequestListResponse{Data: &responses, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateRepairRequest handles repair request creation.
func (s *APIServer) CreateRepairRequest(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.CreateRepairRequestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	repairRequest, err := s.repair.Create.Execute(c.Request.Context(), apprepair.CreateInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          request.PropertyId.String(),
		RoomID:              request.RoomId.String(),
		Title:               request.Title,
		Description:         request.Description,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toApplicationRepairRequestResponse(repairRequest))
}

// ListRepairRequestAttachments handles repair request attachment listing.
func (s *APIServer) ListRepairRequestAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeRepairRequest, id)
}

// CreateRepairRequestAttachment handles repair request attachment registration.
func (s *APIServer) CreateRepairRequestAttachment(c *gin.Context, id openapi_types.UUID) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.RegisterRepairRequestAttachmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var photoStage *appattachment.PhotoStage
	if request.PhotoStage != nil {
		stage := appattachment.PhotoStage(*request.PhotoStage)
		photoStage = &stage
	}
	attachment, err := s.attachment.RegisterAttachment(c.Request.Context(), appattachment.RegisterAttachmentInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		ResourceType:        appattachment.ResourceTypeRepairRequest,
		ResourceID:          id.String(),
		Nonce:               request.Nonce,
		FileName:            request.FileName,
		SortOrder:           request.SortOrder,
		PhotoStage:          photoStage,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toAttachmentResponse(attachment))
}

// DeleteRepairRequest handles repair request deletion.
func (s *APIServer) DeleteRepairRequest(c *gin.Context, id string) {
	if err := s.repair.Delete.Execute(c.Request.Context(), apprepair.DeleteInput{ID: id}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetRepairRequest handles repair request detail retrieval.
func (s *APIServer) GetRepairRequest(c *gin.Context, id string) {
	repairRequest, err := s.repairQueryRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.Error(mapRepairQueryError(err))
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// UpdateRepairRequest handles repair request updates.
func (s *APIServer) UpdateRepairRequest(c *gin.Context, id string) {
	var request api.UpdateRepairRequestRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	repairRequest, err := s.repair.Update.Execute(c.Request.Context(), apprepair.UpdateInput{
		ID:          id,
		Title:       request.Title,
		Description: request.Description,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// AssignRepairRequest handles repair request assignment.
func (s *APIServer) AssignRepairRequest(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}
	var request api.AssignRepairRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	repairRequest, err := s.repair.Workflow.Assign(c.Request.Context(), apprepair.AssignInput{
		ActorRole:   principal.Role,
		ActorUserID: principal.UserID,
		ID:          id,
		AssignedTo:  request.AssignedTo.String(),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// CancelRepairRequest handles repair request cancellation.
func (s *APIServer) CancelRepairRequest(c *gin.Context, id string) {
	var request api.CancelRepairRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.Error(apperr.ErrBadRequest.WithCause(err))
			return
		}
	}

	repairRequest, err := s.repair.Workflow.Cancel(c.Request.Context(), apprepair.CancelInput{
		ID:     id,
		Reason: request.Reason,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// CompleteRepairRequest handles repair request completion.
func (s *APIServer) CompleteRepairRequest(c *gin.Context, id string) {
	repairRequest, err := s.repair.Workflow.Complete(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// ProgressRepairRequest handles repair request progress updates.
func (s *APIServer) ProgressRepairRequest(c *gin.Context, id string) {
	repairRequest, err := s.repair.Workflow.Progress(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// DeleteRoom handles room deletion.
func (s *APIServer) DeleteRoom(c *gin.Context, id string) {
	if err := s.deleteRoomSvc.Execute(c.Request.Context(), appproperty.DeleteRoomInput{ID: id}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetRoom handles room detail retrieval.
func (s *APIServer) GetRoom(c *gin.Context, id string) {
	room, err := s.propertyQueryRepo.FindRoomByID(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, apperr.ErrRoomNotFound):
			c.Error(apperr.ErrRoomNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toRoomResponse(room))
}

// ListRoomAttachments handles room attachment listing.
func (s *APIServer) ListRoomAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeRoom, id)
}

// CreateRoomAttachment handles room attachment registration.
func (s *APIServer) CreateRoomAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeRoom, id)
}

// UpdateRoom handles room updates.
func (s *APIServer) UpdateRoom(c *gin.Context, id string) {
	var request api.UpdateRoomRequest
	fields, err := bindJSONWithFields(c, &request)
	if err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	room, err := s.updateRoomSvc.Execute(c.Request.Context(), appproperty.UpdateRoomInput{
		ID:                id,
		Name:              request.Name,
		Size:              request.Size,
		ClearSize:         jsonFieldIsNull(fields, "size"),
		Floor:             request.Floor,
		ClearFloor:        jsonFieldIsNull(fields, "floor"),
		RoomType:          request.RoomType,
		ClearRoomType:     jsonFieldIsNull(fields, "room_type"),
		Facilities:        request.Facilities,
		ClearFacilities:   jsonFieldIsNull(fields, "facilities"),
		DefaultRentAmount: request.DefaultRentAmount,
		ClearDefaultRent:  jsonFieldIsNull(fields, "default_rent_amount"),
		Notes:             request.Notes,
		ClearNotes:        jsonFieldIsNull(fields, "notes"),
		Zone:              request.Zone,
		ClearZone:         jsonFieldIsNull(fields, "zone"),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedRoomResponse(room))
}

// CreateRoomMaintenance handles setting room maintenance state.
func (s *APIServer) CreateRoomMaintenance(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.SetMaintenanceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	result, err := s.setMaintenanceSvc.Execute(c.Request.Context(), appproperty.SetRoomMaintenanceInput{
		RoomID:      id,
		OperatorID:  principal.UserID,
		Title:       request.Title,
		Description: request.Description,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, api.SetMaintenanceResponse{
		Room:          toCreatedRoomResponse(result.Room),
		RepairRequest: toPropertyRepairRequestResponse(result.RepairRequest),
	})
}

// ListRoomMeterHistory handles room meter history retrieval.
func (s *APIServer) ListRoomMeterHistory(c *gin.Context, id string, params api.ListRoomMeterHistoryParams) {
	if params.Month != nil && params.Year == nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "year",
			"reason": "is required when month is provided",
		}))
		c.Status(http.StatusBadRequest)
		return
	}
	if params.Month != nil && !isValidMonth(*params.Month) {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "month",
			"reason": "must be between 1 and 12",
		}))
		c.Status(http.StatusBadRequest)
		return
	}
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	bills, err := s.billing.RoomMeters.ListRoomMeterHistory(c.Request.Context(), BillingRoomMeterHistoryInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		RoomID:              id,
		Year:                params.Year,
		Month:               params.Month,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillListResponse(bills))
}

// ListTenants handles the tenant listing endpoint.
func (s *APIServer) ListTenants(c *gin.Context, params api.ListTenantsParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}

	result, err := s.tenantQueryRepo.ListAccessible(c.Request.Context(), principal.Role, principal.AssignedPropertyIDs, params.PropertyId, status, pagination.Limit, pagination.Offset)
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	items := make([]api.TenantResponse, 0, len(result.Items))
	for _, tenant := range result.Items {
		items = append(items, toTenantResponse(&tenant))
	}

	c.JSON(http.StatusOK, api.TenantListResponse{Data: &items, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateTenant handles tenant creation.
func (s *APIServer) CreateTenant(c *gin.Context) {
	var request api.CreateTenantRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var phone *string
	if request.Phone != nil {
		value := *request.Phone
		phone = &value
	}

	tenant, err := s.createTenantSvc.Execute(c.Request.Context(), apptenant.CreateTenantInput{
		Name:       request.Name,
		Email:      string(request.Email),
		Phone:      phone,
		Contacts:   tenantContactsToMaps(request.Contacts),
		BirthDate:  datePtrToTimePtr(request.BirthDate),
		NationalID: request.NationalId,
		Address:    request.Address,
		Occupation: request.Occupation,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedTenantResponse(tenant))
}

// ListTenantAttachments handles tenant attachment listing.
func (s *APIServer) ListTenantAttachments(c *gin.Context, id openapi_types.UUID) {
	s.listAttachments(c, appattachment.ResourceTypeTenant, id)
}

// CreateTenantAttachment handles tenant attachment registration.
func (s *APIServer) CreateTenantAttachment(c *gin.Context, id openapi_types.UUID) {
	s.createAttachment(c, appattachment.ResourceTypeTenant, id)
}

// GetTenant handles tenant detail retrieval.
func (s *APIServer) GetTenant(c *gin.Context, id string) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	tenant, err := s.tenantQueryRepo.FindByIDAccessible(c.Request.Context(), id, principal.Role, principal.AssignedPropertyIDs)
	if err != nil {
		switch {
		case errors.Is(err, dbtenantquery.ErrNotFound):
			c.Error(apperr.ErrTenantNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toTenantResponse(tenant))
}

// UpdateTenant handles tenant updates.
func (s *APIServer) UpdateTenant(c *gin.Context, id string) {
	var request api.UpdateTenantRequest
	fields, err := bindJSONWithFields(c, &request)
	if err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var email *string
	if request.Email != nil {
		value := string(*request.Email)
		email = &value
	}

	tenant, err := s.updateTenantSvc.Execute(c.Request.Context(), apptenant.UpdateTenantInput{
		ID:              id,
		Name:            request.Name,
		Email:           email,
		Phone:           request.Phone,
		Contacts:        tenantContactsToMaps(request.Contacts),
		BirthDate:       datePtrToTimePtr(request.BirthDate),
		ClearBirthDate:  jsonFieldIsNull(fields, "birth_date"),
		NationalID:      request.NationalId,
		ClearNationalID: jsonFieldIsNull(fields, "national_id"),
		Address:         request.Address,
		ClearAddress:    jsonFieldIsNull(fields, "address"),
		Occupation:      request.Occupation,
		ClearOccupation: jsonFieldIsNull(fields, "occupation"),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedTenantResponse(tenant))
}

// ListTenantLeases handles lease listing for a tenant.
func (s *APIServer) ListTenantLeases(c *gin.Context, id string, params api.ListTenantLeasesParams) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}

	leases, err := s.tenantQueryRepo.ListLeasesByTenantAccessible(c.Request.Context(), id, principal.Role, principal.AssignedPropertyIDs, status)
	if err != nil {
		switch {
		case errors.Is(err, dbtenantquery.ErrNotFound):
			c.Error(apperr.ErrTenantNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	items := make([]api.LeaseResponse, 0, len(leases))
	for _, lease := range leases {
		items = append(items, toTenantLeaseResponse(&lease))
	}

	c.JSON(http.StatusOK, api.LeaseListResponse{Data: &items})
}

// ListUsers handles the user listing endpoint.
func (s *APIServer) ListUsers(c *gin.Context, params api.ListUsersParams) {
	role := ""
	if params.Role != nil {
		role = string(*params.Role)
	}

	pagination, err := queryparams.NormalizePagination(params.Page, params.Limit)
	if err != nil {
		c.Error(err)
		return
	}

	result, err := s.userRepo.List(c.Request.Context(), users.ListParams{
		Role:   role,
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	})
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	responseItems := make([]api.UserResponse, 0, len(result.Items))
	for _, user := range result.Items {
		responseItems = append(responseItems, toUserResponse(&user))
	}

	c.JSON(http.StatusOK, api.UserListResponse{Data: &responseItems, Pagination: toPaginationResponse(pagination, result.Total)})
}

// CreateUser handles user creation.
func (s *APIServer) CreateUser(c *gin.Context) {
	var request api.CreateUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	user, err := s.createUserService.Execute(c.Request.Context(), appiam.CreateUserInput{
		Email: string(request.Email),
		Name:  request.Name,
		Role:  string(request.Role),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toIAMUserResponse(user))
}

// GetCurrentUser handles retrieval of the current authenticated user.
func (s *APIServer) GetCurrentUser(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	user, err := s.userRepo.FindByID(c.Request.Context(), principal.UserID)
	if err != nil {
		switch err {
		case users.ErrNotFound:
			c.Error(apperr.ErrUserNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toUserResponse(user))
}

// UpdateCurrentUser handles self-service user profile updates.
func (s *APIServer) UpdateCurrentUser(c *gin.Context) {
	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.UpdateCurrentUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	user, err := s.updateCurrentUser.Execute(c.Request.Context(), appiam.UpdateCurrentUserInput{
		UserID: principal.UserID,
		Name:   request.Name,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCurrentUserResponse(user))
}

// TriggerUserPasswordReset handles admin-triggered password reset emails.
func (s *APIServer) TriggerUserPasswordReset(c *gin.Context, id string) {
	if err := s.sendPasswordReset.Execute(c.Request.Context(), id); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetUser handles user detail retrieval.
func (s *APIServer) GetUser(c *gin.Context, id string) {
	if _, err := uuid.Parse(id); err != nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "id",
			"reason": "must be a valid UUID",
		}))
		return
	}

	user, err := s.userRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		switch err {
		case users.ErrNotFound:
			c.Error(apperr.ErrUserNotFound)
		default:
			c.Error(apperr.ErrInternalServerError.WithCause(err))
		}
		return
	}

	c.JSON(http.StatusOK, toUserResponse(user))
}

// UpdateUser handles user updates.
func (s *APIServer) UpdateUser(c *gin.Context, id string) {
	if _, err := uuid.Parse(id); err != nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "id",
			"reason": "must be a valid UUID",
		}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.UpdateUserRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var role *string
	if request.Role != nil {
		value := string(*request.Role)
		role = &value
	}

	user, err := s.updateUser.Execute(c.Request.Context(), appiam.UpdateUserInput{
		ActorUserID:  principal.UserID,
		TargetUserID: id,
		Name:         request.Name,
		Role:         role,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toIAMUserResponse(user))
}

// AssignUserProperties handles property assignment updates for a user.
func (s *APIServer) AssignUserProperties(c *gin.Context, id string) {
	if _, err := uuid.Parse(id); err != nil {
		c.Error(apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "id",
			"reason": "must be a valid UUID",
		}))
		return
	}

	var request api.PropertyAssignmentRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	propertyIDs := make([]string, 0, len(request.PropertyIds))
	for _, propertyID := range request.PropertyIds {
		propertyIDs = append(propertyIDs, propertyID.String())
	}

	user, err := s.assignProperties.Execute(c.Request.Context(), appiam.AssignUserPropertiesInput{
		TargetUserID: id,
		PropertyIDs:  propertyIDs,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toManagedUserResponse(user))
}

func toUserResponse(user *users.User) api.UserResponse {
	id, ok := parseUUID(user.ID)
	email := openapi_types.Email(user.Email)
	role := api.UserResponseRole(user.Role)
	createdAt := user.CreatedAt
	updatedAt := user.UpdatedAt
	version := user.Version
	firebaseUID := user.FirebaseUID
	name := user.Name

	response := api.UserResponse{
		AssignedPropertyIds: toUUIDList(user.AssignedPropertyIDs),
		CreatedAt:           &createdAt,
		Email:               &email,
		FirebaseUid:         &firebaseUID,
		Name:                &name,
		PermissionOverrides: &user.PermissionOverrides,
		Role:                &role,
		UpdatedAt:           &updatedAt,
		Version:             &version,
	}

	if ok {
		response.Id = &id
	}

	return response
}

func toIAMUserResponse(user *appiam.UserAccount) api.UserResponse {
	adapted := &users.User{
		ID:                  user.ID,
		FirebaseUID:         user.FirebaseUID,
		Email:               user.Email,
		Name:                user.Name,
		Role:                user.Role,
		PermissionOverrides: user.PermissionOverrides,
		AssignedPropertyIDs: user.AssignedPropertyIDs,
		CreatedAt:           user.CreatedAt,
		UpdatedAt:           user.UpdatedAt,
		Version:             user.Version,
	}

	return toUserResponse(adapted)
}

func toCurrentUserResponse(user *domainusers.User) api.UserResponse {
	id, ok := parseUUID(user.ID)
	email := openapi_types.Email(user.Email)
	role := api.UserResponseRole(user.Role)
	createdAt := user.CreatedAt
	updatedAt := user.UpdatedAt
	version := user.Version
	firebaseUID := user.FirebaseUID
	name := user.Name

	response := api.UserResponse{
		AssignedPropertyIds: toUUIDList(user.AssignedPropertyIDs),
		CreatedAt:           &createdAt,
		Email:               &email,
		FirebaseUid:         &firebaseUID,
		Name:                &name,
		PermissionOverrides: &user.PermissionOverrides,
		Role:                &role,
		UpdatedAt:           &updatedAt,
		Version:             &version,
	}

	if ok {
		response.Id = &id
	}

	return response
}

func toManagedUserResponse(user *appiam.ManagedUser) api.UserResponse {
	id, ok := parseUUID(user.ID)
	email := openapi_types.Email(user.Email)
	role := api.UserResponseRole(user.Role)
	createdAt := user.CreatedAt
	updatedAt := user.UpdatedAt
	version := user.Version
	firebaseUID := user.FirebaseUID
	name := user.Name

	response := api.UserResponse{
		AssignedPropertyIds: toUUIDList(user.AssignedPropertyIDs),
		CreatedAt:           &createdAt,
		Email:               &email,
		FirebaseUid:         &firebaseUID,
		Name:                &name,
		PermissionOverrides: &user.PermissionOverrides,
		Role:                &role,
		UpdatedAt:           &updatedAt,
		Version:             &version,
	}

	if ok {
		response.Id = &id
	}

	return response
}

func toUUIDList(values []string) *[]openapi_types.UUID {
	result := make([]openapi_types.UUID, 0, len(values))
	for _, value := range values {
		parsed, ok := parseUUID(value)
		if ok {
			result = append(result, parsed)
		}
	}

	if len(result) == 0 {
		empty := []openapi_types.UUID{}
		return &empty
	}

	return &result
}

func parseUUID(value string) (openapi_types.UUID, bool) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return openapi_types.UUID{}, false
	}

	return parsed, true
}

func emailPtrToStringPtr(value *openapi_types.Email) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func bindJSONWithFields(c *gin.Context, target any) (map[string]json.RawMessage, error) {
	body, err := c.GetRawData()
	if err != nil {
		return nil, err
	}

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return nil, err
	}

	return fields, nil
}

func jsonFieldIsNull(fields map[string]json.RawMessage, name string) bool {
	value, ok := fields[name]
	if !ok {
		return false
	}

	return strings.TrimSpace(string(value)) == "null"
}

func datePtrToTimePtr(value *openapi_types.Date) *time.Time {
	if value == nil {
		return nil
	}
	result := value.Time
	return &result
}

func tenantContactsToMaps(value *[]api.TenantContact) *[]map[string]interface{} {
	if value == nil {
		return nil
	}
	contacts := make([]map[string]interface{}, 0, len(*value))
	for _, contact := range *value {
		item := map[string]interface{}{}
		if contact.Name != nil {
			item["name"] = *contact.Name
		}
		if contact.Phone != nil {
			item["phone"] = *contact.Phone
		}
		if contact.Email != nil {
			item["email"] = string(*contact.Email)
		}
		if contact.Relation != nil {
			item["relation"] = *contact.Relation
		}
		if contact.Notes != nil {
			item["notes"] = *contact.Notes
		}
		contacts = append(contacts, item)
	}
	return &contacts
}

func tenantMapsToContacts(value []map[string]interface{}) *[]api.TenantContact {
	contacts := make([]api.TenantContact, 0, len(value))
	for _, item := range value {
		contact := api.TenantContact{}
		contact.Name = optionalStringFromMap(item, "name")
		contact.Phone = optionalStringFromMap(item, "phone")
		if email := optionalStringFromMap(item, "email"); email != nil {
			value := openapi_types.Email(*email)
			contact.Email = &value
		}
		contact.Relation = optionalStringFromMap(item, "relation")
		contact.Notes = optionalStringFromMap(item, "notes")
		contacts = append(contacts, contact)
	}
	return &contacts
}

func optionalStringFromMap(values map[string]interface{}, key string) *string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil
	}
	return &value
}

func isValidMonth(month int) bool {
	return month >= 1 && month <= 12
}

func toBillListResponse(bills []BillingBill) api.BillListResponse {
	items := make([]api.BillResponse, 0, len(bills))
	for i := range bills {
		items = append(items, toBillResponse(&bills[i]))
	}

	return api.BillListResponse{Data: &items}
}

func toPropertyMeterHistoryResponse(rows []BillingPropertyMeterHistoryRow) api.PropertyMeterHistoryResponse {
	items := make([]api.PropertyMeterHistoryRow, 0, len(rows))
	for i := range rows {
		items = append(items, toPropertyMeterHistoryRowResponse(rows[i]))
	}

	return api.PropertyMeterHistoryResponse{Data: &items}
}

func toPropertyTenantLeaseRosterResponse(rows []BillingTenantLeaseRosterRow) api.PropertyTenantLeaseRosterResponse {
	items := make([]api.PropertyTenantLeaseRosterRow, 0, len(rows))
	for i := range rows {
		items = append(items, toPropertyTenantLeaseRosterRowResponse(rows[i]))
	}

	return api.PropertyTenantLeaseRosterResponse{Data: &items}
}

func toPropertyTenantLeaseRosterRowResponse(row BillingTenantLeaseRosterRow) api.PropertyTenantLeaseRosterRow {
	propertyID, propertyOK := parseUUID(row.PropertyID)
	roomID, roomOK := parseUUID(row.RoomID)
	roomStatus := row.RoomStatus

	response := api.PropertyTenantLeaseRosterRow{
		DepositAmount: row.DepositAmount,
		Notes:         row.Notes,
		RentAmount:    row.RentAmount,
		RoomLabel:     &row.RoomLabel,
		RoomStatus:    &roomStatus,
		TenantLabel:   row.TenantLabel,
		TenantPhone:   row.TenantPhone,
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if row.LeaseID != nil {
		if leaseID, ok := parseUUID(*row.LeaseID); ok {
			response.LeaseId = &leaseID
		}
	}
	if row.LeaseStatus != nil {
		status := *row.LeaseStatus
		response.LeaseStatus = &status
	}
	if row.TenantID != nil {
		if tenantID, ok := parseUUID(*row.TenantID); ok {
			response.TenantId = &tenantID
		}
	}
	if row.StartDate != nil {
		startDate := openapi_types.Date{Time: *row.StartDate}
		response.StartDate = &startDate
	}
	if row.EndDate != nil {
		endDate := openapi_types.Date{Time: *row.EndDate}
		response.EndDate = &endDate
	}
	if row.RentBillingCadence != nil {
		cadence := *row.RentBillingCadence
		response.RentBillingCadence = &cadence
	}
	if row.DepositStatus != nil {
		status := *row.DepositStatus
		response.DepositStatus = &status
	}
	if row.NextRentDueDate != nil {
		dueDate := openapi_types.Date{Time: *row.NextRentDueDate}
		response.NextRentDueDate = &dueDate
	}
	if row.NextRentStatus != nil {
		status := *row.NextRentStatus
		response.NextRentStatus = &status
	}

	return response
}

func toPropertyMeterHistoryRowResponse(row BillingPropertyMeterHistoryRow) api.PropertyMeterHistoryRow {
	billID, billOK := parseUUID(row.BillID)
	propertyID, propertyOK := parseUUID(row.PropertyID)
	roomID, roomOK := parseUUID(row.RoomID)
	tenantID, tenantOK := parseUUID(row.TenantID)
	leaseID, leaseOK := parseUUID(row.LeaseID)
	status := api.PropertyMeterHistoryRowStatus(row.Status)
	periodStart := openapi_types.Date{Time: row.PeriodStart}
	periodEnd := openapi_types.Date{Time: row.PeriodEnd}
	dueDate := openapi_types.Date{Time: row.DueDate}

	response := api.PropertyMeterHistoryRow{
		Amount:          row.Amount,
		CurrentReading:  &row.CurrentReading,
		DueDate:         &dueDate,
		MeterRecordedAt: row.MeterRecordedAt,
		PeriodEnd:       &periodEnd,
		PeriodLabel:     &row.PeriodLabel,
		PeriodStart:     &periodStart,
		PreviousReading: &row.PreviousReading,
		RoomLabel:       &row.RoomLabel,
		Status:          &status,
		TenantLabel:     &row.TenantLabel,
		UnitPrice:       &row.UnitPrice,
		Usage:           &row.Usage,
	}
	if billOK {
		response.BillId = &billID
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if tenantOK {
		response.TenantId = &tenantID
	}
	if leaseOK {
		response.LeaseId = &leaseID
	}

	return response
}

func toPaginationResponse(pagination queryparams.Pagination, total int) *api.PaginationResponse {
	totalPages := 0
	if total > 0 {
		totalPages = (total + pagination.Limit - 1) / pagination.Limit
	}
	hasNext := pagination.Page < totalPages
	page := pagination.Page
	limit := pagination.Limit
	return &api.PaginationResponse{
		Page:       &page,
		Limit:      &limit,
		Total:      &total,
		TotalPages: &totalPages,
		HasNext:    &hasNext,
	}
}

func toBillResponse(bill *BillingBill) api.BillResponse {
	id, idOK := parseUUID(bill.ID)
	leaseID, leaseOK := parseUUID(bill.LeaseID)
	tenantID, tenantOK := parseUUID(bill.TenantID)
	roomID, roomOK := parseUUID(bill.RoomID)
	propertyID, propertyOK := parseUUID(bill.PropertyID)
	billType := api.BillResponseType(bill.Type)
	status := api.BillResponseStatus(bill.Status)
	periodStart := openapi_types.Date{Time: bill.PeriodStart}
	periodEnd := openapi_types.Date{Time: bill.PeriodEnd}
	dueDate := openapi_types.Date{Time: bill.DueDate}
	createdAt := bill.CreatedAt
	updatedAt := bill.UpdatedAt
	overdueNoticeCount := bill.OverdueNoticeCount
	version := bill.Version

	response := api.BillResponse{
		Amount:               bill.Amount,
		CreatedAt:            &createdAt,
		DueDate:              &dueDate,
		MeterCurrentReading:  bill.MeterCurrentReading,
		MeterPreviousReading: bill.MeterPreviousReading,
		MeterRecordedAt:      bill.MeterRecordedAt,
		MeterUnitPrice:       bill.MeterUnitPrice,
		OverdueNoticeCount:   &overdueNoticeCount,
		PaidAmount:           bill.PaidAmount,
		PaidAt:               bill.PaidAt,
		PeriodEnd:            &periodEnd,
		PeriodLabel:          &bill.PeriodLabel,
		PeriodStart:          &periodStart,
		PropertyLabel:        &bill.PropertyLabel,
		RoomLabel:            &bill.RoomLabel,
		Status:               &status,
		TenantLabel:          &bill.TenantLabel,
		Type:                 &billType,
		UpdatedAt:            &updatedAt,
		Version:              &version,
		WrittenOffReason:     bill.WrittenOffReason,
	}
	if idOK {
		response.Id = &id
	}
	if leaseOK {
		response.LeaseId = &leaseID
	}
	if tenantOK {
		response.TenantId = &tenantID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if bill.PaymentMethod != nil {
		paymentMethod := api.BillResponsePaymentMethod(*bill.PaymentMethod)
		response.PaymentMethod = &paymentMethod
	}

	return response
}

func toFinancialReportSummaryItem(summary BillingFinancialReportSummary) api.FinancialReportSummaryItem {
	year := summary.Year
	month := summary.Month
	totalIncome := summary.TotalIncome
	totalExpense := summary.TotalExpense
	net := summary.Net

	return api.FinancialReportSummaryItem{
		Year:         &year,
		Month:        &month,
		TotalIncome:  &totalIncome,
		TotalExpense: &totalExpense,
		Net:          &net,
	}
}

func toJournalLogResponse(journalLog *appjournal.JournalLog) api.JournalLogResponse {
	if journalLog == nil {
		return api.JournalLogResponse{}
	}

	id, idOK := parseUUID(journalLog.ID)
	propertyID, propertyOK := parseUUID(journalLog.PropertyID)
	authorID, authorOK := parseUUID(journalLog.AuthorID)
	content := journalLog.Content
	createdAt := journalLog.CreatedAt
	updatedAt := journalLog.UpdatedAt

	response := api.JournalLogResponse{
		AuthorLabel:                &journalLog.AuthorLabel,
		Content:                    &content,
		CreatedAt:                  &createdAt,
		ExpenseAccountingTitleCode: journalLog.ExpenseAccountingTitleCode,
		ExpenseAccountingTitleName: journalLog.ExpenseAccountingTitleName,
		ExpenseAmount:              journalLog.ExpenseAmount,
		ExpenseDescription:         journalLog.ExpenseDescription,
		PropertyLabel:              &journalLog.PropertyLabel,
		RoomLabel:                  journalLog.RoomLabel,
		UpdatedAt:                  &updatedAt,
	}
	if idOK {
		response.Id = &id
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if authorOK {
		response.AuthorId = &authorID
	}
	if journalLog.RoomID != nil {
		roomID, roomOK := parseUUID(*journalLog.RoomID)
		if roomOK {
			response.RoomId = &roomID
		}
	}
	if journalLog.ExpenseAccountingTitleID != nil {
		titleID, titleOK := parseUUID(*journalLog.ExpenseAccountingTitleID)
		if titleOK {
			response.ExpenseAccountingTitleId = &titleID
		}
	}

	return response
}

func toAccountingTitleOption(title appjournal.AccountingTitle) api.AccountingTitleOption {
	id, idOK := parseUUID(title.ID)
	code := title.Code
	name := title.Name
	kind := api.AccountingTitleOptionKind(title.Kind)
	response := api.AccountingTitleOption{
		Code: &code,
		Kind: &kind,
		Name: &name,
	}
	if idOK {
		response.Id = &id
	}
	return response
}

func toFinancialReportResponse(report *BillingFinancialReport) api.FinancialReportResponse {
	propertyID, propertyOK := parseUUID(report.PropertyID)
	year := report.Year
	month := report.Month
	totalIncome := report.TotalIncome
	totalExpense := report.TotalExpense
	net := report.Net
	isFinalized := report.IsFinalized
	entries := make([]api.FinancialReportEntryItem, 0, len(report.Entries))
	for i := range report.Entries {
		entries = append(entries, toFinancialReportEntryItem(report.Entries[i]))
	}

	response := api.FinancialReportResponse{
		Year:         &year,
		Month:        &month,
		TotalIncome:  &totalIncome,
		TotalExpense: &totalExpense,
		Net:          &net,
		IsFinalized:  &isFinalized,
		Entries:      &entries,
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}

	return response
}

func toFinancialReportEntryItem(entry BillingFinancialReportEntry) api.FinancialReportEntryItem {
	amount := entry.Amount
	category := api.FinancialReportEntryItemCategory(entry.Category)
	entryID, entryIDOK := parseUUID(entry.ID)

	response := api.FinancialReportEntryItem{
		AccountingTitleCode: entry.AccountingTitleCode,
		AccountingTitleName: entry.AccountingTitleName,
		Amount:              &amount,
		Category:            &category,
		Description:         entry.Description,
		DisplayNote:         entry.DisplayNote,
		PeriodLabel:         entry.PeriodLabel,
		RoomLabel:           entry.RoomLabel,
		Source:              toFinancialReportEntrySource(entry.Source),
		TenantLabel:         entry.TenantLabel,
	}
	if entryIDOK {
		response.EntryId = &entryID
	}
	if entry.AccountingTitleID != nil {
		if accountingTitleID, ok := parseUUID(*entry.AccountingTitleID); ok {
			response.AccountingTitleId = &accountingTitleID
		}
	}
	if entry.SourceDate != nil {
		sourceDate := openapi_types.Date{Time: *entry.SourceDate}
		response.SourceDate = &sourceDate
	}

	return response
}

func toFinancialReportEntrySource(source *BillingFinancialReportEntrySource) *api.FinancialReportEntrySource {
	if source == nil {
		return nil
	}
	sourceID, ok := parseUUID(source.ID)
	if !ok {
		return nil
	}
	sourceType := api.FinancialReportEntrySourceType(source.Type)
	response := api.FinancialReportEntrySource{
		Id:   &sourceID,
		Type: &sourceType,
	}
	if source.Detail != nil {
		detail := api.FinancialReportEntrySourceDetail(*source.Detail)
		response.Detail = &detail
	}
	return &response
}

func toDashboardResponse(dashboard *appproperty.Dashboard) api.DashboardResponse {
	propertyID, propertyOK := parseUUID(dashboard.PropertyID)
	rooms := make([]api.DashboardRoomItem, 0, len(dashboard.Rooms))
	for i := range dashboard.Rooms {
		rooms = append(rooms, toDashboardRoomItem(dashboard.Rooms[i]))
	}
	recentJournals := make([]api.DashboardRecentJournalItem, 0, len(dashboard.RecentJournals))
	for i := range dashboard.RecentJournals {
		recentJournals = append(recentJournals, toDashboardRecentJournalItem(dashboard.RecentJournals[i]))
	}

	expectedRent := dashboard.MonthlySummary.ExpectedRent
	collectedRent := dashboard.MonthlySummary.CollectedRent
	overdueBillCount := dashboard.MonthlySummary.OverdueBillCount
	response := api.DashboardResponse{
		MonthlySummary: &api.DashboardMonthlySummary{
			ExpectedRent:     &expectedRent,
			CollectedRent:    &collectedRent,
			OverdueBillCount: &overdueBillCount,
		},
		OccupancySummary: toOccupancySummaryResponse(dashboard.Occupancy),
		RecentJournals:   &recentJournals,
		Rooms:            &rooms,
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}

	return response
}

func toHomeDashboardResponse(dashboard *appproperty.HomeDashboard) api.HomeDashboardResponse {
	propertySummaries := make([]api.HomeDashboardPropertySummary, 0, len(dashboard.PropertySummaries))
	for i := range dashboard.PropertySummaries {
		propertySummaries = append(propertySummaries, toHomeDashboardPropertySummary(dashboard.PropertySummaries[i]))
	}
	recentJournals := make([]api.HomeDashboardRecentJournalItem, 0, len(dashboard.RecentJournals))
	for i := range dashboard.RecentJournals {
		recentJournals = append(recentJournals, toHomeDashboardRecentJournalItem(dashboard.RecentJournals[i]))
	}

	return api.HomeDashboardResponse{
		PortfolioSummary:      *toOccupancySummaryResponse(dashboard.PortfolioSummary),
		MonthlyBillingSummary: toHomeDashboardBillingSummary(dashboard.MonthlyBillingSummary),
		PropertySummaries:     propertySummaries,
		RecentJournals:        recentJournals,
	}
}

func toHomeDashboardPropertySummary(summary appproperty.HomeDashboardPropertySummary) api.HomeDashboardPropertySummary {
	propertyID, propertyOK := parseUUID(summary.PropertyID)
	propertyName := summary.PropertyName
	response := api.HomeDashboardPropertySummary{
		MonthlyBillingSummary: toHomeDashboardBillingSummary(summary.MonthlySummary),
		OccupancySummary:      *toOccupancySummaryResponse(summary.Occupancy),
		PropertyName:          propertyName,
	}
	if propertyOK {
		response.PropertyId = propertyID
	}
	return response
}

func toHomeDashboardBillingSummary(summary appproperty.DashboardMonthlySummary) api.HomeDashboardBillingSummary {
	return api.HomeDashboardBillingSummary{
		ExpectedRent:     summary.ExpectedRent,
		CollectedRent:    summary.CollectedRent,
		OverdueBillCount: summary.OverdueBillCount,
	}
}

func toHomeDashboardRecentJournalItem(journal appproperty.HomeDashboardRecentJournal) api.HomeDashboardRecentJournalItem {
	id, idOK := parseUUID(journal.ID)
	propertyID, propertyOK := parseUUID(journal.PropertyID)
	createdAt := journal.CreatedAt

	response := api.HomeDashboardRecentJournalItem{
		Content:      journal.Content,
		CreatedAt:    createdAt,
		PropertyName: journal.PropertyName,
		Type:         api.HomeDashboardRecentJournalItemType(journal.Type),
	}
	if idOK {
		response.Id = id
	}
	if propertyOK {
		response.PropertyId = propertyID
	}
	return response
}

func toOccupancySummaryResponse(summary appproperty.OccupancySummary) *api.OccupancySummary {
	return &api.OccupancySummary{
		TotalRooms:       summary.TotalRooms,
		OccupiedRooms:    summary.OccupiedRooms,
		VacantRooms:      summary.VacantRooms,
		MaintenanceRooms: summary.MaintenanceRooms,
		OccupancyRate:    summary.OccupancyRate,
	}
}

func toDBPropertyOccupancySummaryResponse(summary dbpropertyquery.OccupancySummary) *api.OccupancySummary {
	return toOccupancySummaryResponse(appproperty.OccupancySummary{
		TotalRooms:       summary.TotalRooms,
		OccupiedRooms:    summary.OccupiedRooms,
		VacantRooms:      summary.VacantRooms,
		MaintenanceRooms: summary.MaintenanceRooms,
		OccupancyRate:    summary.OccupancyRate,
	})
}

func toDashboardRoomItem(room appproperty.DashboardRoom) api.DashboardRoomItem {
	id, idOK := parseUUID(room.ID)
	name := room.Name
	status := api.DashboardRoomItemStatus(room.Status)

	response := api.DashboardRoomItem{
		Name:   &name,
		Status: &status,
	}
	if idOK {
		response.Id = &id
	}

	return response
}

func toDashboardRecentJournalItem(journal appproperty.DashboardRecentJournal) api.DashboardRecentJournalItem {
	id, idOK := parseUUID(journal.ID)
	content := journal.Content
	createdAt := journal.CreatedAt
	itemType := journal.Type

	response := api.DashboardRecentJournalItem{
		Content:   &content,
		CreatedAt: &createdAt,
		Type:      &itemType,
	}
	if idOK {
		response.Id = &id
	}

	return response
}

func (s *APIServer) runJob(c *gin.Context, jobKey appjobs.JobKey, windowKey string) {
	if windowKey == "" {
		c.Error(apperr.ErrValidationWindowKeyRequired)
		return
	}

	result, err := s.jobTriggerService.Execute(c.Request.Context(), jobKey, windowKey, requestctx.GetRequestID(c))
	if err != nil {
		if errors.Is(err, appjobs.ErrInvalidWindowKey) {
			c.Error(apperr.ErrBadRequest.WithCause(err).WithDetails(map[string]interface{}{"field": "window_key"}))
			return
		}
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	requestID := requestctx.GetRequestID(c)
	response := api.SchedulerJobTriggerResponse{
		JobKey:      result.JobKey,
		RequestedAt: result.RequestedAt,
		Status:      api.SchedulerJobTriggerResponseStatus(result.Status),
		WindowKey:   result.WindowKey,
	}
	if requestID != "" {
		response.RequestId = &requestID
	}
	if result.Message != "" {
		response.Message = &result.Message
	}
	response.RetryCount = &result.RetryCount
	durationMs := int(result.DurationMs)
	response.DurationMs = &durationMs
	if result.Summary != nil {
		note := result.Summary.Note
		response.Summary = &api.SchedulerJobExecutionSummary{
			FailedCount:    &result.Summary.FailedCount,
			ProcessedCount: &result.Summary.ProcessedCount,
			SkippedCount:   &result.Summary.SkippedCount,
			Note:           &note,
		}
	}

	requestctx.SetJobMeta(c, requestctx.JobMeta{
		JobKey:     result.JobKey,
		WindowKey:  result.WindowKey,
		Status:     result.Status,
		RetryCount: result.RetryCount,
		DurationMs: result.DurationMs,
	})

	c.JSON(http.StatusAccepted, response)
}

func toPropertyResponse(property *dbpropertyquery.Property) api.PropertyResponse {
	id, ok := parseUUID(property.ID)
	ownerID, ownerOK := parseUUID(property.OwnerID)
	name := property.Name
	address := property.Address
	createdAt := property.CreatedAt
	updatedAt := property.UpdatedAt
	version := property.Version

	response := api.PropertyResponse{
		Address:          &address,
		ContactPhone:     property.ContactPhone,
		CreatedAt:        &createdAt,
		Name:             &name,
		Notes:            property.Notes,
		OccupancySummary: toDBPropertyOccupancySummaryResponse(property.Occupancy),
		Facilities:       property.Facilities,
		Subtitle:         property.Subtitle,
		UpdatedAt:        &updatedAt,
		Version:          &version,
	}
	if property.ContactEmail != nil {
		if parsed, err := mail.ParseAddress(*property.ContactEmail); err == nil {
			email := openapi_types.Email(parsed.Address)
			response.ContactEmail = &email
		}
	}
	if property.DefaultElectricityBillingCadence != "" {
		cadence := api.PropertyResponseDefaultElectricityBillingCadence(property.DefaultElectricityBillingCadence)
		response.DefaultElectricityBillingCadence = &cadence
	}
	if property.ElectricityUnitPrice != nil {
		electricityUnitPrice := *property.ElectricityUnitPrice
		response.ElectricityUnitPrice = &electricityUnitPrice
	}
	if ok {
		response.Id = &id
	}
	if ownerOK {
		response.OwnerId = &ownerID
	}

	return response
}

func toRoomResponse(room *dbpropertyquery.Room) api.RoomResponse {
	id, ok := parseUUID(room.ID)
	propertyID, propertyOK := parseUUID(room.PropertyID)
	name := room.Name
	status := api.RoomResponseStatus(room.Status)
	createdAt := room.CreatedAt
	updatedAt := room.UpdatedAt

	response := api.RoomResponse{
		DefaultRentAmount: room.DefaultRentAmount,
		Facilities:        room.Facilities,
		Floor:             room.Floor,
		Name:              &name,
		Notes:             room.Notes,
		RoomType:          room.RoomType,
		Size:              room.Size,
		Status:            &status,
		UpdatedAt:         &updatedAt,
		Zone:              room.Zone,
		CreatedAt:         &createdAt,
	}
	if ok {
		response.Id = &id
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}

	return response
}

func toCreatedPropertyResponse(property *appproperty.Property) api.PropertyResponse {
	queryShape := &dbpropertyquery.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Subtitle:                         property.Subtitle,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		ContactPhone:                     property.ContactPhone,
		ContactEmail:                     property.ContactEmail,
		Notes:                            property.Notes,
		Facilities:                       property.Facilities,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}

	return toPropertyResponse(queryShape)
}

func toCreatedRoomResponse(room *appproperty.Room) api.RoomResponse {
	queryShape := &dbpropertyquery.Room{
		ID:                room.ID,
		PropertyID:        room.PropertyID,
		Name:              room.Name,
		Status:            room.Status,
		Size:              room.Size,
		Floor:             room.Floor,
		RoomType:          room.RoomType,
		Facilities:        room.Facilities,
		DefaultRentAmount: room.DefaultRentAmount,
		Notes:             room.Notes,
		Zone:              room.Zone,
		CreatedAt:         room.CreatedAt,
		UpdatedAt:         room.UpdatedAt,
	}

	return toRoomResponse(queryShape)
}

func toTenantResponse(tenant *dbtenantquery.Tenant) api.TenantResponse {
	id, ok := parseUUID(tenant.ID)
	name := tenant.Name
	status := api.TenantResponseStatus(tenant.Status)
	createdAt := tenant.CreatedAt
	updatedAt := tenant.UpdatedAt
	version := tenant.Version

	response := api.TenantResponse{
		Address:    tenant.Address,
		Contacts:   tenantMapsToContacts(tenant.Contacts),
		CreatedAt:  &createdAt,
		Name:       &name,
		NationalId: tenant.NationalID,
		Occupation: tenant.Occupation,
		Phone:      tenant.Phone,
		Status:     &status,
		UpdatedAt:  &updatedAt,
		Version:    &version,
	}
	if tenant.BirthDate != nil {
		birthDate := openapi_types.Date{Time: *tenant.BirthDate}
		response.BirthDate = &birthDate
	}
	if tenant.Email != nil {
		email := openapi_types.Email(*tenant.Email)
		response.Email = &email
	}
	if ok {
		response.Id = &id
	}

	return response
}

func toCreatedTenantResponse(tenant *apptenant.Tenant) api.TenantResponse {
	queryShape := &dbtenantquery.Tenant{
		ID:         tenant.ID,
		Name:       tenant.Name,
		Email:      tenant.Email,
		Phone:      tenant.Phone,
		Contacts:   tenant.Contacts,
		BirthDate:  tenant.BirthDate,
		NationalID: tenant.NationalID,
		Address:    tenant.Address,
		Occupation: tenant.Occupation,
		Status:     tenant.Status,
		CreatedAt:  tenant.CreatedAt,
		UpdatedAt:  tenant.UpdatedAt,
		Version:    tenant.Version,
	}

	return toTenantResponse(queryShape)
}

func toTenantLeaseResponse(lease *dbtenantquery.Lease) api.LeaseResponse {
	id, ok := parseUUID(lease.ID)
	tenantID, tenantOK := parseUUID(lease.TenantID)
	propertyID, propertyOK := parseUUID(lease.PropertyID)
	roomID, roomOK := parseUUID(lease.RoomID)
	startDate := openapi_types.Date{Time: lease.StartDate}
	endDate := openapi_types.Date{Time: lease.EndDate}
	status := api.LeaseResponseStatus(lease.Status)
	depositStatus := api.LeaseResponseDepositStatus(lease.DepositStatus)
	rentCadence := api.LeaseResponseRentBillingCadence(lease.RentBillingCadence)
	cadence := api.LeaseResponseElectricityBillingCadence(lease.ElectricityBillingCadence)
	rentAmount := lease.RentAmount
	depositAmount := lease.DepositAmount
	createdAt := lease.CreatedAt
	updatedAt := lease.UpdatedAt
	version := lease.Version

	response := api.LeaseResponse{
		CreatedAt:                 &createdAt,
		DepositAmount:             &depositAmount,
		DepositDeductionAmount:    lease.DepositDeductionAmount,
		DepositDeductionReason:    lease.DepositDeductionReason,
		DepositRefundAmount:       lease.DepositRefundAmount,
		DepositStatus:             &depositStatus,
		ElectricityBillingCadence: &cadence,
		EndDate:                   &endDate,
		RentBillingCadence:        &rentCadence,
		RentAmount:                &rentAmount,
		StartDate:                 &startDate,
		Status:                    &status,
		Notes:                     lease.Notes,
		SettlementDetail:          lease.SettlementDetail,
		TerminationReason:         lease.TerminationReason,
		UpdatedAt:                 &updatedAt,
		Version:                   &version,
	}
	response.StartingMeterReading = lease.StartingMeterReading
	if ok {
		response.Id = &id
	}
	if tenantOK {
		response.TenantId = &tenantID
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}

	return response
}

func toLeaseResponse(lease *dbleasequery.Lease) api.LeaseResponse {
	tenantLease := &dbtenantquery.Lease{
		ID:                        lease.ID,
		TenantID:                  lease.TenantID,
		PropertyID:                lease.PropertyID,
		RoomID:                    lease.RoomID,
		RentAmount:                lease.RentAmount,
		StartDate:                 lease.StartDate,
		EndDate:                   lease.EndDate,
		RentBillingCadence:        lease.RentBillingCadence,
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
		StartingMeterReading:      lease.StartingMeterReading,
		Status:                    lease.Status,
		DepositAmount:             lease.DepositAmount,
		DepositRefundAmount:       lease.DepositRefundAmount,
		DepositDeductionAmount:    lease.DepositDeductionAmount,
		DepositStatus:             lease.DepositStatus,
		DepositDeductionReason:    lease.DepositDeductionReason,
		Notes:                     lease.Notes,
		TerminationReason:         lease.TerminationReason,
		SettlementDetail:          lease.SettlementDetail,
		CreatedAt:                 lease.CreatedAt,
		UpdatedAt:                 lease.UpdatedAt,
		Version:                   lease.Version,
	}

	response := toTenantLeaseResponse(tenantLease)
	response.PropertyLabel = &lease.PropertyLabel
	response.RoomLabel = &lease.RoomLabel
	response.TenantLabel = &lease.TenantLabel
	return response
}

func toLeaseCheckoutReviewResponse(review *dbleasequery.CheckoutReview) api.LeaseCheckoutReviewResponse {
	leaseID, leaseOK := parseUUID(review.LeaseID)
	propertyID, propertyOK := parseUUID(review.PropertyID)
	roomID, roomOK := parseUUID(review.RoomID)
	tenantID, tenantOK := parseUUID(review.TenantID)
	startDate := openapi_types.Date{Time: review.StartDate}
	endDate := openapi_types.Date{Time: review.EndDate}
	leaseStatus := api.LeaseCheckoutReviewResponseLeaseStatus(review.LeaseStatus)
	depositStatus := api.LeaseCheckoutReviewResponseDepositStatus(review.DepositStatus)
	exportAvailable := review.ExportAvailable

	response := api.LeaseCheckoutReviewResponse{
		CheckoutFinalizedAt:    review.CheckoutFinalizedAt,
		DepositDeductionAmount: review.DepositDeductionAmount,
		DepositRefundAmount:    review.DepositRefundAmount,
		DepositStatus:          &depositStatus,
		EndDate:                &endDate,
		ExportAvailable:        &exportAvailable,
		LeaseStatus:            &leaseStatus,
		PropertyLabel:          &review.PropertyLabel,
		RoomLabel:              &review.RoomLabel,
		StartDate:              &startDate,
		TenantLabel:            &review.TenantLabel,
		TerminationReason:      review.TerminationReason,
	}
	if leaseOK {
		response.LeaseId = &leaseID
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if tenantOK {
		response.TenantId = &tenantID
	}
	if review.ForceTerminationID != nil {
		forceTerminationID, ok := parseUUID(*review.ForceTerminationID)
		if ok {
			response.ForceTerminationId = &forceTerminationID
		}
	}
	if review.ForceTerminationStatus != nil {
		status := api.LeaseCheckoutReviewResponseForceTerminationStatus(*review.ForceTerminationStatus)
		response.ForceTerminationStatus = &status
	}
	if review.ForceTerminationReason != nil {
		response.ForceTerminationReason = review.ForceTerminationReason
	}
	if review.ForceTerminationHandling != nil {
		handling := api.LeaseCheckoutReviewResponseForceTerminationDepositHandling(*review.ForceTerminationHandling)
		response.ForceTerminationDepositHandling = &handling
	}

	return response
}

func toCreatedLeaseResponse(lease *applease.Lease) api.LeaseResponse {
	return toLeaseResponse(&dbleasequery.Lease{
		ID:                        lease.ID,
		TenantID:                  lease.TenantID,
		PropertyID:                lease.PropertyID,
		RoomID:                    lease.RoomID,
		RentAmount:                lease.RentAmount,
		StartDate:                 lease.StartDate,
		EndDate:                   lease.EndDate,
		RentBillingCadence:        lease.RentBillingCadence,
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
		StartingMeterReading:      lease.StartingMeterReading,
		Status:                    lease.Status,
		DepositAmount:             lease.DepositAmount,
		DepositRefundAmount:       lease.DepositRefundAmount,
		DepositDeductionAmount:    lease.DepositDeductionAmount,
		DepositStatus:             lease.DepositStatus,
		DepositDeductionReason:    lease.DepositDeductionReason,
		Notes:                     lease.Notes,
		TerminationReason:         lease.TerminationReason,
		SettlementDetail:          lease.SettlementDetail,
		CreatedAt:                 lease.CreatedAt,
		UpdatedAt:                 lease.UpdatedAt,
		Version:                   lease.Version,
	})
}

func toForceTerminationResponse(forceTermination *applease.ForceTermination) api.ForceTerminationResponse {
	id, idOK := parseUUID(forceTermination.ID)
	leaseID, leaseOK := parseUUID(forceTermination.LeaseID)
	propertyID, propertyOK := parseUUID(forceTermination.PropertyID)
	roomID, roomOK := parseUUID(forceTermination.RoomID)
	tenantID, tenantOK := parseUUID(forceTermination.TenantID)
	initiatedBy, initiatedByOK := parseUUID(forceTermination.InitiatedBy)
	status := api.ForceTerminationResponseStatus(forceTermination.Status)
	reason := forceTermination.Reason
	createdAt := forceTermination.CreatedAt
	updatedAt := forceTermination.UpdatedAt
	depositHandling := api.ForceTerminationResponseDepositHandling(forceTermination.DepositHandling)

	bills := make([]struct {
		BillId      *openapi_types.UUID                      `json:"bill_id,omitempty"`
		PeriodEnd   *openapi_types.Date                      `json:"period_end,omitempty"`
		PeriodLabel *string                                  `json:"period_label,omitempty"`
		PeriodStart *openapi_types.Date                      `json:"period_start,omitempty"`
		Status      *api.ForceTerminationResponseBillsStatus `json:"status,omitempty"`
		Type        *string                                  `json:"type,omitempty"`
	}, 0, len(forceTermination.Bills))
	for _, bill := range forceTermination.Bills {
		billID, billIDOK := parseUUID(bill.BillID)
		billStatus := api.ForceTerminationResponseBillsStatus(bill.Status)
		billType := bill.Type
		periodStart := openapi_types.Date{Time: bill.PeriodStart}
		periodEnd := openapi_types.Date{Time: bill.PeriodEnd}
		item := struct {
			BillId      *openapi_types.UUID                      `json:"bill_id,omitempty"`
			PeriodEnd   *openapi_types.Date                      `json:"period_end,omitempty"`
			PeriodLabel *string                                  `json:"period_label,omitempty"`
			PeriodStart *openapi_types.Date                      `json:"period_start,omitempty"`
			Status      *api.ForceTerminationResponseBillsStatus `json:"status,omitempty"`
			Type        *string                                  `json:"type,omitempty"`
		}{
			PeriodEnd:   &periodEnd,
			PeriodLabel: &bill.PeriodLabel,
			PeriodStart: &periodStart,
			Status:      &billStatus,
			Type:        &billType,
		}
		if billIDOK {
			item.BillId = &billID
		}
		bills = append(bills, item)
	}

	response := api.ForceTerminationResponse{
		Bills:            &bills,
		CreatedAt:        &createdAt,
		DepositHandling:  &depositHandling,
		InitiatedByLabel: &forceTermination.InitiatedByLabel,
		PropertyLabel:    &forceTermination.PropertyLabel,
		Reason:           &reason,
		RoomLabel:        &forceTermination.RoomLabel,
		Status:           &status,
		TenantLabel:      &forceTermination.TenantLabel,
		UpdatedAt:        &updatedAt,
	}
	if idOK {
		response.Id = &id
	}
	if leaseOK {
		response.LeaseId = &leaseID
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if tenantOK {
		response.TenantId = &tenantID
	}
	if initiatedByOK {
		response.InitiatedBy = &initiatedBy
	}

	return response
}

func toCheckoutSettlementInput(principal requestctx.Principal, leaseID string, request api.CheckoutSettlementInput, previewToken string) applease.CheckoutSettlementInput {
	return applease.CheckoutSettlementInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             leaseID,
		CheckoutDate:        request.CheckoutDate.Time,
		Reason:              request.Reason,
		FinalMeterReading:   request.FinalMeterReading,
		CleaningFee:         intValuePtr(request.CleaningFee),
		KeyCardLossFee:      intValuePtr(request.KeyCardLossFee),
		OtherFee:            intValuePtr(request.OtherFee),
		OtherFeeReason:      request.OtherFeeReason,
		Notes:               request.Notes,
		PreviewToken:        previewToken,
	}
}

func toCheckoutSettlementResponse(settlement *applease.CheckoutSettlement) (api.CheckoutSettlementResponse, error) {
	leaseID, err := uuid.Parse(settlement.LeaseID)
	if err != nil {
		return api.CheckoutSettlementResponse{}, apperr.ErrInternalServerError.WithCause(err)
	}
	propertyID, err := uuid.Parse(settlement.PropertyID)
	if err != nil {
		return api.CheckoutSettlementResponse{}, apperr.ErrInternalServerError.WithCause(err)
	}
	tenantID, err := uuid.Parse(settlement.TenantID)
	if err != nil {
		return api.CheckoutSettlementResponse{}, apperr.ErrInternalServerError.WithCause(err)
	}
	roomID, err := uuid.Parse(settlement.RoomID)
	if err != nil {
		return api.CheckoutSettlementResponse{}, apperr.ErrInternalServerError.WithCause(err)
	}

	lines := make([]api.CheckoutSettlementLine, 0, len(settlement.Lines))
	for _, line := range settlement.Lines {
		var sourceRef *map[string]interface{}
		if line.SourceRef != nil {
			sourceRef = &line.SourceRef
		}
		lines = append(lines, api.CheckoutSettlementLine{
			Amount:      line.Amount,
			Description: line.Description,
			Direction:   api.CheckoutSettlementLineDirection(line.Direction),
			Kind:        api.CheckoutSettlementLineKind(line.Kind),
			Label:       line.Label,
			SourceRef:   sourceRef,
		})
	}

	blockers := make([]api.CheckoutSettlementBlocker, 0, len(settlement.Blockers))
	for _, blocker := range settlement.Blockers {
		blockers = append(blockers, api.CheckoutSettlementBlocker{
			Code:     api.CheckoutSettlementBlockerCode(blocker.Code),
			Message:  blocker.Message,
			SourceId: blocker.SourceID,
		})
	}

	warnings := make([]api.CheckoutSettlementWarning, 0, len(settlement.Warnings))
	for _, warning := range settlement.Warnings {
		warnings = append(warnings, api.CheckoutSettlementWarning{
			Code:    api.CheckoutSettlementWarningCode(warning.Code),
			Message: warning.Message,
		})
	}

	return api.CheckoutSettlementResponse{
		Blockers:          blockers,
		CheckoutDate:      openapi_types.Date{Time: settlement.CheckoutDate},
		DepositAmount:     settlement.DepositAmount,
		ExportAvailable:   settlement.ExportAvailable,
		FinalMeterReading: settlement.FinalMeterReading,
		FinalizedAt:       settlement.FinalizedAt,
		LeaseId:           leaseID,
		Lines:             lines,
		NetAmount:         settlement.NetAmount,
		NetDirection:      api.CheckoutSettlementResponseNetDirection(settlement.NetDirection),
		Notes:             settlement.Notes,
		PreviewToken:      settlement.PreviewToken,
		PropertyId:        propertyID,
		PropertyLabel:     settlement.PropertyLabel,
		Reason:            settlement.Reason,
		RoomId:            roomID,
		RoomLabel:         settlement.RoomLabel,
		TenantId:          tenantID,
		TenantLabel:       settlement.TenantLabel,
		TotalCharge:       settlement.TotalCharge,
		TotalRefund:       settlement.TotalRefund,
		Warnings:          warnings,
	}, nil
}

func intValuePtr(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func toLeaseReplaceResponse(result *applease.ReplaceLeaseResult) api.LeaseReplaceResponse {
	effectiveStart := openapi_types.Date{Time: result.EffectiveStartDate}
	depositHandling := api.LeaseReplaceResponseReplacementDepositHandling(result.DepositHandling)
	changedFields := append([]string(nil), result.ChangedFields...)
	reason := result.Reason
	oldLease := toCreatedLeaseResponse(result.OldLease)
	newLease := toCreatedLeaseResponse(result.NewLease)

	return api.LeaseReplaceResponse{
		OldLease: &oldLease,
		NewLease: &newLease,
		Replacement: &struct {
			ChangedFields      *[]string                                           `json:"changed_fields,omitempty"`
			DepositHandling    *api.LeaseReplaceResponseReplacementDepositHandling `json:"deposit_handling,omitempty"`
			EffectiveStartDate *openapi_types.Date                                 `json:"effective_start_date,omitempty"`
			Reason             *string                                             `json:"reason,omitempty"`
		}{
			ChangedFields:      &changedFields,
			DepositHandling:    &depositHandling,
			EffectiveStartDate: &effectiveStart,
			Reason:             &reason,
		},
	}
}

func toPropertyRepairRequestResponse(repairRequest *appproperty.RepairRequest) api.RepairRequestResponse {
	if repairRequest == nil {
		return api.RepairRequestResponse{}
	}

	return toRepairRequestResponseFields(
		repairRequest.ID,
		repairRequest.PropertyID,
		repairRequest.RoomID,
		repairRequest.SubmittedBy,
		repairRequest.AssignedTo,
		repairRequest.Title,
		repairRequest.Description,
		repairRequest.Status,
		repairRequest.SubmittedAt,
		repairRequest.AssignedAt,
		repairRequest.CompletedAt,
		repairRequest.CreatedAt,
		repairRequest.UpdatedAt,
	)
}

func toApplicationRepairRequestResponse(repairRequest *apprepair.RepairRequest) api.RepairRequestResponse {
	if repairRequest == nil {
		return api.RepairRequestResponse{}
	}

	response := toRepairRequestResponseFields(
		repairRequest.ID,
		repairRequest.PropertyID,
		repairRequest.RoomID,
		repairRequest.SubmittedBy,
		repairRequest.AssignedTo,
		repairRequest.Title,
		repairRequest.Description,
		repairRequest.Status,
		repairRequest.SubmittedAt,
		repairRequest.AssignedAt,
		repairRequest.CompletedAt,
		repairRequest.CreatedAt,
		repairRequest.UpdatedAt,
	)
	response.PropertyLabel = &repairRequest.PropertyLabel
	response.RoomLabel = &repairRequest.RoomLabel
	response.SubmittedByLabel = &repairRequest.SubmittedByLabel
	response.AssignedToLabel = repairRequest.AssignedToLabel
	return response
}

func toRepairRequestResponseFields(
	idValue string,
	propertyIDValue string,
	roomIDValue string,
	submittedByValue string,
	assignedToValue *string,
	titleValue string,
	descriptionValue string,
	statusValue string,
	submittedAtValue time.Time,
	assignedAtValue *time.Time,
	completedAtValue *time.Time,
	createdAtValue time.Time,
	updatedAtValue time.Time,
) api.RepairRequestResponse {
	id, ok := parseUUID(idValue)
	propertyID, propertyOK := parseUUID(propertyIDValue)
	roomID, roomOK := parseUUID(roomIDValue)
	submittedBy, submittedByOK := parseUUID(submittedByValue)
	title := titleValue
	description := descriptionValue
	status := api.RepairRequestResponseStatus(statusValue)
	submittedAt := submittedAtValue
	createdAt := createdAtValue
	updatedAt := updatedAtValue

	response := api.RepairRequestResponse{
		AssignedAt:  assignedAtValue,
		CompletedAt: completedAtValue,
		CreatedAt:   &createdAt,
		Description: &description,
		Status:      &status,
		SubmittedAt: &submittedAt,
		Title:       &title,
		UpdatedAt:   &updatedAt,
	}
	if ok {
		response.Id = &id
	}
	if propertyOK {
		response.PropertyId = &propertyID
	}
	if roomOK {
		response.RoomId = &roomID
	}
	if submittedByOK {
		response.SubmittedBy = &submittedBy
	}
	if assignedToValue != nil {
		assignedTo, assignedToOK := parseUUID(*assignedToValue)
		if assignedToOK {
			response.AssignedTo = &assignedTo
		}
	}

	return response
}

func isSupportedRoomStatus(status string) bool {
	switch status {
	case "vacant", "occupied", "maintenance":
		return true
	default:
		return false
	}
}
