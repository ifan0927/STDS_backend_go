package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appproperty "stds_backend/internal/application/property"
	domainusers "stds_backend/internal/domain/users"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/queryparams"
	"stds_backend/internal/http/requestctx"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
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
	createPropertySvc *appproperty.CreatePropertyService
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
	createPropertySvc *appproperty.CreatePropertyService,
) *APIServer {
	return &APIServer{
		userRepo:          userRepo,
		createUserService: createUserService,
		sendPasswordReset: sendPasswordReset,
		syncAuthService:   syncAuthService,
		updateCurrentUser: updateCurrentUser,
		updateUser:        updateUser,
		assignProperties:  assignProperties,
		jobTriggerService: jobTriggerService,
		propertyQueryRepo: propertyQueryRepo,
		createPropertySvc: createPropertySvc,
	}
}

func writeNotImplemented(c *gin.Context) {
	message := "Not implemented yet."
	c.AbortWithStatusJSON(http.StatusNotImplemented, api.ErrorResponse{
		Message: &message,
	})
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
func (s *APIServer) ListBills(c *gin.Context, params api.ListBillsParams) { writeNotImplemented(c) }

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
func (s *APIServer) GetBill(c *gin.Context, id string) { writeNotImplemented(c) }

// SubmitBillMeter handles meter submission for a bill.
func (s *APIServer) SubmitBillMeter(c *gin.Context, id string) { writeNotImplemented(c) }

// RecordBillPayment handles payment recording for a bill.
func (s *APIServer) RecordBillPayment(c *gin.Context, id string) { writeNotImplemented(c) }

// GetForceTermination handles force-termination detail retrieval.
func (s *APIServer) GetForceTermination(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
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
	writeNotImplemented(c)
}

// CreateJournalLog handles journal log creation.
func (s *APIServer) CreateJournalLog(c *gin.Context) { writeNotImplemented(c) }

// ListJournalLogAttachments handles journal log attachment listing.
func (s *APIServer) ListJournalLogAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateJournalLogAttachment handles journal log attachment registration.
func (s *APIServer) CreateJournalLogAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// DeleteJournalLog handles journal log deletion.
func (s *APIServer) DeleteJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

// GetJournalLog handles journal log detail retrieval.
func (s *APIServer) GetJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

// UpdateJournalLog handles journal log updates.
func (s *APIServer) UpdateJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

// ListLeases handles the lease listing endpoint.
func (s *APIServer) ListLeases(c *gin.Context, params api.ListLeasesParams) { writeNotImplemented(c) }

// CreateLease handles lease creation.
func (s *APIServer) CreateLease(c *gin.Context) { writeNotImplemented(c) }

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
func (s *APIServer) GetLease(c *gin.Context, id string) { writeNotImplemented(c) }

// UpdateLease handles lease updates.
func (s *APIServer) UpdateLease(c *gin.Context, id string) { writeNotImplemented(c) }

// UpdateLeaseDeposit handles lease deposit updates.
func (s *APIServer) UpdateLeaseDeposit(c *gin.Context, id string) { writeNotImplemented(c) }

// ReplaceLease handles lease replacement.
func (s *APIServer) ReplaceLease(c *gin.Context, id string) { writeNotImplemented(c) }

// ForceTerminateLease handles forced lease termination.
func (s *APIServer) ForceTerminateLease(c *gin.Context, id string) { writeNotImplemented(c) }

// TerminateLease handles lease termination.
func (s *APIServer) TerminateLease(c *gin.Context, id string) { writeNotImplemented(c) }

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
func (s *APIServer) DeleteProperty(c *gin.Context, id string) { writeNotImplemented(c) }

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
func (s *APIServer) UpdateProperty(c *gin.Context, id string) { writeNotImplemented(c) }

// GetPropertyDashboard handles property dashboard retrieval.
func (s *APIServer) GetPropertyDashboard(c *gin.Context, id string) { writeNotImplemented(c) }

// GetPropertyFinancialReportSummary handles financial report summary retrieval
// for a property.
func (s *APIServer) GetPropertyFinancialReportSummary(c *gin.Context, id string, params api.GetPropertyFinancialReportSummaryParams) {
	writeNotImplemented(c)
}

// GetPropertyFinancialReport handles financial report retrieval for a property
// month.
func (s *APIServer) GetPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	writeNotImplemented(c)
}

// SendPropertyFinancialReport handles sending a property's financial report.
func (s *APIServer) SendPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	writeNotImplemented(c)
}

// ListPropertyMeterHistory handles property meter history retrieval.
func (s *APIServer) ListPropertyMeterHistory(c *gin.Context, id string, params api.ListPropertyMeterHistoryParams) {
	writeNotImplemented(c)
}

// ListPropertyPendingMeters handles pending meter retrieval for a property.
func (s *APIServer) ListPropertyPendingMeters(c *gin.Context, id string) { writeNotImplemented(c) }

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
func (s *APIServer) CreatePropertyRoom(c *gin.Context, id string) { writeNotImplemented(c) }

// ListRepairRequests handles the repair request listing endpoint.
func (s *APIServer) ListRepairRequests(c *gin.Context, params api.ListRepairRequestsParams) {
	writeNotImplemented(c)
}

// CreateRepairRequest handles repair request creation.
func (s *APIServer) CreateRepairRequest(c *gin.Context) { writeNotImplemented(c) }

// ListRepairRequestAttachments handles repair request attachment listing.
func (s *APIServer) ListRepairRequestAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateRepairRequestAttachment handles repair request attachment registration.
func (s *APIServer) CreateRepairRequestAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// DeleteRepairRequest handles repair request deletion.
func (s *APIServer) DeleteRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// GetRepairRequest handles repair request detail retrieval.
func (s *APIServer) GetRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// UpdateRepairRequest handles repair request updates.
func (s *APIServer) UpdateRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// AssignRepairRequest handles repair request assignment.
func (s *APIServer) AssignRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// CancelRepairRequest handles repair request cancellation.
func (s *APIServer) CancelRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// CompleteRepairRequest handles repair request completion.
func (s *APIServer) CompleteRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// ProgressRepairRequest handles repair request progress updates.
func (s *APIServer) ProgressRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

// DeleteRoom handles room deletion.
func (s *APIServer) DeleteRoom(c *gin.Context, id string) { writeNotImplemented(c) }

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
func (s *APIServer) UpdateRoom(c *gin.Context, id string) { writeNotImplemented(c) }

// CreateRoomMaintenance handles setting room maintenance state.
func (s *APIServer) CreateRoomMaintenance(c *gin.Context, id string) { writeNotImplemented(c) }

// ListRoomMeterHistory handles room meter history retrieval.
func (s *APIServer) ListRoomMeterHistory(c *gin.Context, id string, params api.ListRoomMeterHistoryParams) {
	writeNotImplemented(c)
}

// ListTenants handles the tenant listing endpoint.
func (s *APIServer) ListTenants(c *gin.Context, params api.ListTenantsParams) { writeNotImplemented(c) }

// CreateTenant handles tenant creation.
func (s *APIServer) CreateTenant(c *gin.Context) { writeNotImplemented(c) }

// ListTenantAttachments handles tenant attachment listing.
func (s *APIServer) ListTenantAttachments(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// CreateTenantAttachment handles tenant attachment registration.
func (s *APIServer) CreateTenantAttachment(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

// GetTenant handles tenant detail retrieval.
func (s *APIServer) GetTenant(c *gin.Context, id string) { writeNotImplemented(c) }

// UpdateTenant handles tenant updates.
func (s *APIServer) UpdateTenant(c *gin.Context, id string) { writeNotImplemented(c) }

// ListTenantLeases handles lease listing for a tenant.
func (s *APIServer) ListTenantLeases(c *gin.Context, id string, params api.ListTenantLeasesParams) {
	writeNotImplemented(c)
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

func isSupportedRoomStatus(status string) bool {
	switch status {
	case "vacant", "occupied", "maintenance":
		return true
	default:
		return false
	}
}
