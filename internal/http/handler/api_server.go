package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

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
	forceTerminateSvc *applease.ForceTerminateLeaseService
	getForceTermSvc   *applease.GetForceTerminationService
	billing           BillingServices
	journal           JournalServices
	repair            RepairServices
}

// LeaseCommandServices groups optional lease command services beyond creation.
type LeaseCommandServices struct {
	UpdateLease         *applease.UpdateLeaseService
	UpdateDeposit       *applease.UpdateDepositService
	ReplaceLease        *applease.ReplaceLeaseService
	TerminateLease      *applease.TerminateLeaseService
	ForceTerminateLease *applease.ForceTerminateLeaseService
	GetForceTermination *applease.GetForceTerminationService
	Billing             BillingServices
}

// BillingServices groups the billing application entry points used by the
// transport layer.
type BillingServices struct {
	Query            BillingQueryService
	Meter            BillingMeterService
	Payment          BillingPaymentService
	PropertyMeters   BillingPropertyMeterService
	RoomMeters       BillingRoomMeterService
	FinancialReports BillingFinancialReportService
}

// JournalServices groups journal log application services used by the transport layer.
type JournalServices struct {
	List   *appjournal.ListService
	Get    *appjournal.GetService
	Create *appjournal.CreateService
	Update *appjournal.UpdateService
	Delete *appjournal.DeleteService
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
	List(ctx context.Context, query apprepair.ListQuery) ([]apprepair.RepairRequest, error)
	FindByID(ctx context.Context, id string) (*apprepair.RepairRequest, error)
}

type BillingQueryService interface {
	ListBills(ctx context.Context, input BillingListInput) ([]BillingBill, error)
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
	ListPropertyMeterHistory(ctx context.Context, input BillingPropertyMeterHistoryInput) ([]BillingBill, error)
}

type BillingRoomMeterService interface {
	ListRoomMeterHistory(ctx context.Context, input BillingRoomMeterHistoryInput) ([]BillingBill, error)
}

type BillingFinancialReportService interface {
	ListFinancialReportSummaries(ctx context.Context, input BillingFinancialReportSummaryInput) ([]BillingFinancialReportSummary, error)
	GetFinancialReport(ctx context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error)
	SendFinancialReport(ctx context.Context, input BillingFinancialReportInput) (*BillingFinancialReport, error)
}

type BillingListInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          *string
	LeaseID             *string
	TenantID            *string
	Status              string
	Month               *string
	Limit               int
	Offset              int
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

type BillingBill struct {
	ID                   string
	LeaseID              string
	TenantID             string
	RoomID               string
	PropertyID           string
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
	Category    string
	Description *string
	Amount      int
}

// NewAPIServer returns an API server with only the currently implemented
// vertical slices wired in.
func NewAPIServer(
	userRepo users.Repository,
	createUserService *appiam.CreateUserService,
	sendPasswordReset *appiam.SendUserPasswordResetService,
	syncAuthService *appiam.SyncAuthService,
	updateCurrentUser *appiam.UpdateCurrentUserService,
	updateUser *appiam.UpdateUserService,
	assignProperties *appiam.AssignUserPropertiesService,
	jobTriggerService *appjobs.TriggerService,
	propertyQueryRepo dbpropertyquery.Repository,
	leaseQueryRepo dbleasequery.Repository,
	repairQueryRepo RepairQueryRepository,
	tenantQueryRepo dbtenantquery.Repository,
	createPropertySvc *appproperty.CreatePropertyService,
	updatePropertySvc *appproperty.UpdatePropertyService,
	deletePropertySvc *appproperty.DeletePropertyService,
	createRoomSvc *appproperty.CreateRoomService,
	updateRoomSvc *appproperty.UpdateRoomService,
	deleteRoomSvc *appproperty.DeleteRoomService,
	setMaintenanceSvc *appproperty.SetRoomMaintenanceService,
	createTenantSvc *apptenant.CreateTenantService,
	updateTenantSvc *apptenant.UpdateTenantService,
	createLeaseSvc *applease.CreateLeaseService,
	journalServices JournalServices,
	repairServices RepairServices,
	leaseCommands ...LeaseCommandServices,
) *APIServer {
	server := &APIServer{
		userRepo:          userRepo,
		createUserService: createUserService,
		sendPasswordReset: sendPasswordReset,
		syncAuthService:   syncAuthService,
		updateCurrentUser: updateCurrentUser,
		updateUser:        updateUser,
		assignProperties:  assignProperties,
		jobTriggerService: jobTriggerService,
		propertyQueryRepo: propertyQueryRepo,
		leaseQueryRepo:    leaseQueryRepo,
		repairQueryRepo:   repairQueryRepo,
		tenantQueryRepo:   tenantQueryRepo,
		createPropertySvc: createPropertySvc,
		updatePropertySvc: updatePropertySvc,
		deletePropertySvc: deletePropertySvc,
		createRoomSvc:     createRoomSvc,
		updateRoomSvc:     updateRoomSvc,
		deleteRoomSvc:     deleteRoomSvc,
		setMaintenanceSvc: setMaintenanceSvc,
		createTenantSvc:   createTenantSvc,
		updateTenantSvc:   updateTenantSvc,
		createLeaseSvc:    createLeaseSvc,
		journal:           journalServices,
		repair:            repairServices,
	}
	if len(leaseCommands) > 0 {
		server.updateLeaseSvc = leaseCommands[0].UpdateLease
		server.updateDepositSvc = leaseCommands[0].UpdateDeposit
		server.replaceLeaseSvc = leaseCommands[0].ReplaceLease
		server.terminateLeaseSvc = leaseCommands[0].TerminateLease
		server.forceTerminateSvc = leaseCommands[0].ForceTerminateLease
		server.getForceTermSvc = leaseCommands[0].GetForceTermination
		server.billing = leaseCommands[0].Billing
	}

	return server
}

func writeNotImplemented(c *gin.Context) {
	message := "Not implemented yet."
	c.AbortWithStatusJSON(http.StatusNotImplemented, api.ErrorResponse{
		Message: &message,
	})
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
	if s.billing.Query == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_query"}))
		return
	}

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

	var tenantID *string
	if params.TenantId != nil {
		value := params.TenantId.String()
		tenantID = &value
	}

	bills, err := s.billing.Query.ListBills(c.Request.Context(), BillingListInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          params.PropertyId,
		LeaseID:             params.LeaseId,
		TenantID:            tenantID,
		Status:              status,
		Month:               params.Month,
		Limit:               pagination.Limit,
		Offset:              pagination.Offset,
	})
	if err != nil {
		c.Error(err)
		return
	}

	items := make([]api.BillResponse, 0, len(bills))
	for i := range bills {
		items = append(items, toBillResponse(&bills[i]))
	}

	c.JSON(http.StatusOK, api.BillListResponse{Data: &items})
}

// CreateAttachmentUploadURL handles attachment upload URL creation.
func (s *APIServer) CreateAttachmentUploadURL(c *gin.Context) { writeNotImplemented(c) }

// DeleteAttachment handles attachment deletion.
func (s *APIServer) DeleteAttachment(c *gin.Context, id openapi_types.UUID) { writeNotImplemented(c) }

// ListBillAttachments handles bill attachment listing.
func (s *APIServer) ListBillAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateBillAttachment handles bill attachment registration.
func (s *APIServer) CreateBillAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// GetBill handles the bill detail endpoint.
func (s *APIServer) GetBill(c *gin.Context, id string) {
	if s.billing.Query == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_query"}))
		return
	}

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
	if s.billing.Meter == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_meter"}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	var request api.RecordMeterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	bill, err := s.billing.Meter.SubmitBillMeter(c.Request.Context(), BillingMeterInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		BillID:              id,
		CurrentReading:      request.CurrentReading,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toBillResponse(bill))
}

// RecordBillPayment handles payment recording for a bill.
func (s *APIServer) RecordBillPayment(c *gin.Context, id string) {
	if s.billing.Payment == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_payment"}))
		return
	}

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
	if s.getForceTermSvc == nil {
		writeNotImplemented(c)
		return
	}

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

// ListJournalLogs handles the journal log listing endpoint.
func (s *APIServer) ListJournalLogs(c *gin.Context, params api.ListJournalLogsParams) {
	if s.journal.List == nil {
		writeNotImplemented(c)
		return
	}
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

	items, err := s.journal.List.Execute(c.Request.Context(), appjournal.ListInput{
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

	responses := make([]api.JournalLogResponse, 0, len(items))
	for i := range items {
		responses = append(responses, toJournalLogResponse(&items[i]))
	}
	c.JSON(http.StatusOK, api.JournalLogListResponse{Data: &responses})
}

// CreateJournalLog handles journal log creation.
func (s *APIServer) CreateJournalLog(c *gin.Context) {
	if s.journal.Create == nil {
		writeNotImplemented(c)
		return
	}
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
	journalLog, err := s.journal.Create.Execute(c.Request.Context(), appjournal.CreateInput{
		ActorRole:           principal.Role,
		ActorUserID:         principal.UserID,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		PropertyID:          request.PropertyId.String(),
		RoomID:              roomID,
		Content:             request.Content,
		ExpenseAmount:       request.ExpenseAmount,
		ExpenseDescription:  request.ExpenseDescription,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toJournalLogResponse(journalLog))
}

// ListJournalLogAttachments handles journal log attachment listing.
func (s *APIServer) ListJournalLogAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateJournalLogAttachment handles journal log attachment registration.
func (s *APIServer) CreateJournalLogAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// DeleteJournalLog handles journal log deletion.
func (s *APIServer) DeleteJournalLog(c *gin.Context, id string) {
	if s.journal.Delete == nil {
		writeNotImplemented(c)
		return
	}
	if err := s.journal.Delete.Execute(c.Request.Context(), appjournal.DeleteInput{ID: id}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetJournalLog handles journal log detail retrieval.
func (s *APIServer) GetJournalLog(c *gin.Context, id string) {
	if s.journal.Get == nil {
		writeNotImplemented(c)
		return
	}
	journalLog, err := s.journal.Get.Execute(c.Request.Context(), appjournal.GetInput{ID: id})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toJournalLogResponse(journalLog))
}

// UpdateJournalLog handles journal log updates.
func (s *APIServer) UpdateJournalLog(c *gin.Context, id string) {
	if s.journal.Update == nil {
		writeNotImplemented(c)
		return
	}
	var request api.UpdateJournalLogRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	journalLog, err := s.journal.Update.Execute(c.Request.Context(), appjournal.UpdateInput{
		ID:                 id,
		Content:            request.Content,
		ExpenseAmount:      request.ExpenseAmount,
		ExpenseDescription: request.ExpenseDescription,
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

	leases, err := s.leaseQueryRepo.ListAccessible(c.Request.Context(), principal.Role, principal.AssignedPropertyIDs, dbleasequery.ListParams{
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

	items := make([]api.LeaseResponse, 0, len(leases))
	for _, lease := range leases {
		items = append(items, toLeaseResponse(&lease))
	}

	c.JSON(http.StatusOK, api.LeaseListResponse{Data: &items})
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

	lease, err := s.createLeaseSvc.Execute(c.Request.Context(), applease.CreateLeaseInput{
		ActorRole:                 principal.Role,
		AssignedPropertyIDs:       principal.AssignedPropertyIDs,
		TenantID:                  request.TenantId.String(),
		RoomID:                    request.RoomId.String(),
		RentAmount:                request.RentAmount,
		StartDate:                 request.StartDate.Time,
		EndDate:                   request.EndDate.Time,
		DepositAmount:             request.DepositAmount,
		ElectricityBillingCadence: cadence,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedLeaseResponse(lease))
}

// ListLeaseAttachments handles lease attachment listing.
func (s *APIServer) ListLeaseAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateLeaseAttachment handles lease attachment registration.
func (s *APIServer) CreateLeaseAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// DeleteLease handles lease deletion.
func (s *APIServer) DeleteLease(c *gin.Context, id string) { writeNotImplemented(c) }

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
	if s.updateLeaseSvc == nil {
		writeNotImplemented(c)
		return
	}

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

	var endDate *time.Time
	if request.EndDate != nil {
		value := request.EndDate.Time
		endDate = &value
	}

	lease, err := s.updateLeaseSvc.Execute(c.Request.Context(), applease.UpdateLeaseInput{
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		RentAmount:          request.RentAmount,
		EndDate:             endDate,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedLeaseResponse(lease))
}

// UpdateLeaseDeposit handles lease deposit updates.
func (s *APIServer) UpdateLeaseDeposit(c *gin.Context, id string) {
	if s.updateDepositSvc == nil {
		writeNotImplemented(c)
		return
	}

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
	if s.replaceLeaseSvc == nil {
		writeNotImplemented(c)
		return
	}

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
		ActorRole:           principal.Role,
		AssignedPropertyIDs: principal.AssignedPropertyIDs,
		LeaseID:             id,
		Reason:              string(request.Reason),
		EffectiveStartDate:  request.EffectiveStartDate.Time,
		DepositHandling:     string(request.DepositHandling),
		NewEndDate:          request.NewLease.EndDate.Time,
		NewRentAmount:       request.NewLease.RentAmount,
		NewCadence:          string(request.NewLease.ElectricityBillingCadence),
		NewNotes:            notes,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toLeaseReplaceResponse(result))
}

// ForceTerminateLease handles forced lease termination.
func (s *APIServer) ForceTerminateLease(c *gin.Context, id string) {
	if s.forceTerminateSvc == nil {
		writeNotImplemented(c)
		return
	}

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
	if s.terminateLeaseSvc == nil {
		writeNotImplemented(c)
		return
	}

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

// CreateProperty handles property creation.
func (s *APIServer) CreateProperty(c *gin.Context) {
	var request api.CreatePropertyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	property, err := s.createPropertySvc.Execute(c.Request.Context(), appproperty.CreatePropertyInput{
		Name:                             request.Name,
		Address:                          request.Address,
		ElectricityUnitPrice:             request.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: string(request.DefaultElectricityBillingCadence),
		OwnerID:                          request.OwnerId.String(),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedPropertyResponse(property))
}

// ListPropertyAttachments handles property attachment listing.
func (s *APIServer) ListPropertyAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreatePropertyAttachment handles property attachment registration.
func (s *APIServer) CreatePropertyAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
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
	if err := c.ShouldBindJSON(&request); err != nil {
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
		Address:                          request.Address,
		ElectricityUnitPrice:             request.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: cadence,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toCreatedPropertyResponse(property))
}

// GetPropertyDashboard handles property dashboard retrieval.
func (s *APIServer) GetPropertyDashboard(c *gin.Context, id string) { writeNotImplemented(c) }

// GetPropertyFinancialReportSummary handles financial report summary retrieval
// for a property.
func (s *APIServer) GetPropertyFinancialReportSummary(c *gin.Context, id string, params api.GetPropertyFinancialReportSummaryParams) {
	if s.billing.FinancialReports == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_financial_reports"}))
		return
	}

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
	if s.billing.FinancialReports == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_financial_reports"}))
		return
	}
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
	if s.billing.FinancialReports == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_financial_reports"}))
		return
	}
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

// ListPropertyMeterHistory handles property meter history retrieval.
func (s *APIServer) ListPropertyMeterHistory(c *gin.Context, id string, params api.ListPropertyMeterHistoryParams) {
	if s.billing.PropertyMeters == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_property_meters"}))
		return
	}

	principal, ok := requestctx.GetPrincipal(c)
	if !ok {
		c.Error(apperr.ErrUnauthorized)
		return
	}

	bills, err := s.billing.PropertyMeters.ListPropertyMeterHistory(c.Request.Context(), BillingPropertyMeterHistoryInput{
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

	c.JSON(http.StatusOK, toBillListResponse(bills))
}

// ListPropertyPendingMeters handles pending meter retrieval for a property.
func (s *APIServer) ListPropertyPendingMeters(c *gin.Context, id string) {
	if s.billing.PropertyMeters == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_property_meters"}))
		return
	}

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

	rooms, err := s.propertyQueryRepo.ListRoomsByProperty(c.Request.Context(), id, status, pagination.Limit, pagination.Offset)
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	if len(rooms) == 0 {
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

	items := make([]api.RoomResponse, 0, len(rooms))
	for i := range rooms {
		items = append(items, toRoomResponse(&rooms[i]))
	}

	c.JSON(http.StatusOK, api.RoomListResponse{Data: &items})
}

// CreatePropertyRoom handles room creation within a property.
func (s *APIServer) CreatePropertyRoom(c *gin.Context, id string) {
	var request api.CreateRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	room, err := s.createRoomSvc.Execute(c.Request.Context(), appproperty.CreateRoomInput{
		PropertyID: id,
		Name:       request.Name,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedRoomResponse(room))
}

// ListRepairRequests handles the repair request listing endpoint.
func (s *APIServer) ListRepairRequests(c *gin.Context, params api.ListRepairRequestsParams) {
	if s.repairQueryRepo == nil {
		writeNotImplemented(c)
		return
	}
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

	items, err := s.repairQueryRepo.List(c.Request.Context(), apprepair.ListQuery{
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

	responses := make([]api.RepairRequestResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, toApplicationRepairRequestResponse(&item))
	}
	c.JSON(http.StatusOK, api.RepairRequestListResponse{Data: &responses})
}

// CreateRepairRequest handles repair request creation.
func (s *APIServer) CreateRepairRequest(c *gin.Context) {
	if s.repair.Create == nil {
		writeNotImplemented(c)
		return
	}
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
	writeNotImplemented(c)
}

// CreateRepairRequestAttachment handles repair request attachment registration.
func (s *APIServer) CreateRepairRequestAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// DeleteRepairRequest handles repair request deletion.
func (s *APIServer) DeleteRepairRequest(c *gin.Context, id string) {
	if s.repair.Delete == nil {
		writeNotImplemented(c)
		return
	}
	if err := s.repair.Delete.Execute(c.Request.Context(), apprepair.DeleteInput{ID: id}); err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// GetRepairRequest handles repair request detail retrieval.
func (s *APIServer) GetRepairRequest(c *gin.Context, id string) {
	if s.repairQueryRepo == nil {
		writeNotImplemented(c)
		return
	}
	repairRequest, err := s.repairQueryRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.Error(mapRepairQueryError(err))
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// UpdateRepairRequest handles repair request updates.
func (s *APIServer) UpdateRepairRequest(c *gin.Context, id string) {
	if s.repair.Update == nil {
		writeNotImplemented(c)
		return
	}
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
	if s.repair.Workflow == nil {
		writeNotImplemented(c)
		return
	}
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
	if s.repair.Workflow == nil {
		writeNotImplemented(c)
		return
	}
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
	if s.repair.Workflow == nil {
		writeNotImplemented(c)
		return
	}
	repairRequest, err := s.repair.Workflow.Complete(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, toApplicationRepairRequestResponse(repairRequest))
}

// ProgressRepairRequest handles repair request progress updates.
func (s *APIServer) ProgressRepairRequest(c *gin.Context, id string) {
	if s.repair.Workflow == nil {
		writeNotImplemented(c)
		return
	}
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
	writeNotImplemented(c)
}

// CreateRoomAttachment handles room attachment registration.
func (s *APIServer) CreateRoomAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// UpdateRoom handles room updates.
func (s *APIServer) UpdateRoom(c *gin.Context, id string) {
	var request api.UpdateRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	room, err := s.updateRoomSvc.Execute(c.Request.Context(), appproperty.UpdateRoomInput{
		ID:   id,
		Name: request.Name,
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
	if s.billing.RoomMeters == nil {
		c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "billing_room_meters"}))
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

	tenants, err := s.tenantQueryRepo.ListAccessible(c.Request.Context(), principal.Role, principal.AssignedPropertyIDs, params.PropertyId, status, pagination.Limit, pagination.Offset)
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	items := make([]api.TenantResponse, 0, len(tenants))
	for _, tenant := range tenants {
		items = append(items, toTenantResponse(&tenant))
	}

	c.JSON(http.StatusOK, api.TenantListResponse{Data: &items})
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
		Name:     request.Name,
		Email:    string(request.Email),
		Phone:    phone,
		Contacts: request.Contacts,
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, toCreatedTenantResponse(tenant))
}

// ListTenantAttachments handles tenant attachment listing.
func (s *APIServer) ListTenantAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateTenantAttachment handles tenant attachment registration.
func (s *APIServer) CreateTenantAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
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
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(apperr.ErrBadRequest.WithCause(err))
		return
	}

	var email *string
	if request.Email != nil {
		value := string(*request.Email)
		email = &value
	}

	tenant, err := s.updateTenantSvc.Execute(c.Request.Context(), apptenant.UpdateTenantInput{
		ID:       id,
		Name:     request.Name,
		Email:    email,
		Phone:    request.Phone,
		Contacts: request.Contacts,
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

	items, err := s.userRepo.List(c.Request.Context(), users.ListParams{
		Role:   role,
		Limit:  pagination.Limit,
		Offset: pagination.Offset,
	})
	if err != nil {
		c.Error(apperr.ErrInternalServerError.WithCause(err))
		return
	}

	responseItems := make([]api.UserResponse, 0, len(items))
	for _, user := range items {
		responseItems = append(responseItems, toUserResponse(&user))
	}

	c.JSON(http.StatusOK, api.UserListResponse{Data: &responseItems})
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
		PeriodStart:          &periodStart,
		Status:               &status,
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
		Content:            &content,
		CreatedAt:          &createdAt,
		ExpenseAmount:      journalLog.ExpenseAmount,
		ExpenseDescription: journalLog.ExpenseDescription,
		UpdatedAt:          &updatedAt,
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

	return api.FinancialReportEntryItem{
		Amount:      &amount,
		Category:    &category,
		Description: entry.Description,
	}
}

func (s *APIServer) runJob(c *gin.Context, jobKey appjobs.JobKey, windowKey string) {
	if windowKey == "" {
		c.Error(apperr.ErrValidationWindowKeyRequired)
		return
	}

	result, err := s.jobTriggerService.Execute(c.Request.Context(), jobKey, windowKey, requestctx.GetRequestID(c))
	if err != nil {
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
		Address:   &address,
		CreatedAt: &createdAt,
		Name:      &name,
		UpdatedAt: &updatedAt,
		Version:   &version,
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
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}

	return toPropertyResponse(queryShape)
}

func toCreatedRoomResponse(room *appproperty.Room) api.RoomResponse {
	queryShape := &dbpropertyquery.Room{
		ID:         room.ID,
		PropertyID: room.PropertyID,
		Name:       room.Name,
		Status:     room.Status,
		CreatedAt:  room.CreatedAt,
		UpdatedAt:  room.UpdatedAt,
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
		Contacts:   &tenant.Contacts,
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
		RentAmount:                &rentAmount,
		StartDate:                 &startDate,
		Status:                    &status,
		Notes:                     lease.Notes,
		SettlementDetail:          lease.SettlementDetail,
		TerminationReason:         lease.TerminationReason,
		UpdatedAt:                 &updatedAt,
		Version:                   &version,
	}
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
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
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

	return toTenantLeaseResponse(tenantLease)
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
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
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
	initiatedBy, initiatedByOK := parseUUID(forceTermination.InitiatedBy)
	status := api.ForceTerminationResponseStatus(forceTermination.Status)
	reason := forceTermination.Reason
	createdAt := forceTermination.CreatedAt
	updatedAt := forceTermination.UpdatedAt
	depositHandling := api.ForceTerminationResponseDepositHandling(forceTermination.DepositHandling)

	bills := make([]struct {
		BillId *openapi_types.UUID                      `json:"bill_id,omitempty"`
		Status *api.ForceTerminationResponseBillsStatus `json:"status,omitempty"`
	}, 0, len(forceTermination.Bills))
	for _, bill := range forceTermination.Bills {
		billID, billIDOK := parseUUID(bill.BillID)
		billStatus := api.ForceTerminationResponseBillsStatus(bill.Status)
		item := struct {
			BillId *openapi_types.UUID                      `json:"bill_id,omitempty"`
			Status *api.ForceTerminationResponseBillsStatus `json:"status,omitempty"`
		}{
			Status: &billStatus,
		}
		if billIDOK {
			item.BillId = &billID
		}
		bills = append(bills, item)
	}

	response := api.ForceTerminationResponse{
		Bills:           &bills,
		CreatedAt:       &createdAt,
		DepositHandling: &depositHandling,
		Reason:          &reason,
		Status:          &status,
		UpdatedAt:       &updatedAt,
	}
	if idOK {
		response.Id = &id
	}
	if leaseOK {
		response.LeaseId = &leaseID
	}
	if initiatedByOK {
		response.InitiatedBy = &initiatedBy
	}

	return response
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
