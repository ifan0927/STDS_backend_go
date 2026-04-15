package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"stds_backend/internal/http/api"
)

var _ api.ServerInterface = (*APIServer)(nil)

type APIServer struct{}

func NewAPIServer() *APIServer {
	return &APIServer{}
}

func writeNotImplemented(c *gin.Context) {
	message := "Not implemented yet."
	c.AbortWithStatusJSON(http.StatusNotImplemented, api.ErrorResponse{
		Message: &message,
	})
}

func (s *APIServer) SyncAuth(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) ListBills(c *gin.Context, params api.ListBillsParams) { writeNotImplemented(c) }

func (s *APIServer) GetBill(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) SubmitBillMeter(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) RecordBillPayment(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetForceTermination(c *gin.Context, id openapi_types.UUID) {
	writeNotImplemented(c)
}

func (s *APIServer) ListJournalLogs(c *gin.Context, params api.ListJournalLogsParams) {
	writeNotImplemented(c)
}

func (s *APIServer) CreateJournalLog(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) DeleteJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateJournalLog(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListLeases(c *gin.Context, params api.ListLeasesParams) { writeNotImplemented(c) }

func (s *APIServer) CreateLease(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) DeleteLease(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetLease(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateLease(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateLeaseDeposit(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ForceTerminateLease(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) TerminateLease(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListProperties(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) CreateProperty(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) DeleteProperty(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetProperty(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateProperty(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetPropertyDashboard(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetPropertyFinancialReportSummary(c *gin.Context, id string, params api.GetPropertyFinancialReportSummaryParams) {
	writeNotImplemented(c)
}

func (s *APIServer) GetPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	writeNotImplemented(c)
}

func (s *APIServer) SendPropertyFinancialReport(c *gin.Context, id string, year int, month int) {
	writeNotImplemented(c)
}

func (s *APIServer) ListPropertyMeterHistory(c *gin.Context, id string, params api.ListPropertyMeterHistoryParams) {
	writeNotImplemented(c)
}

func (s *APIServer) ListPropertyPendingMeters(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListPropertyRooms(c *gin.Context, id string, params api.ListPropertyRoomsParams) {
	writeNotImplemented(c)
}

func (s *APIServer) CreatePropertyRoom(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListRepairRequests(c *gin.Context, params api.ListRepairRequestsParams) {
	writeNotImplemented(c)
}

func (s *APIServer) CreateRepairRequest(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) DeleteRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) AssignRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) CancelRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) CompleteRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ProgressRepairRequest(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) DeleteRoom(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) GetRoom(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateRoom(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) CreateRoomMaintenance(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListRoomMeterHistory(c *gin.Context, id string, params api.ListRoomMeterHistoryParams) {
	writeNotImplemented(c)
}

func (s *APIServer) ListTenants(c *gin.Context, params api.ListTenantsParams) { writeNotImplemented(c) }

func (s *APIServer) CreateTenant(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) GetTenant(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateTenant(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) ListTenantLeases(c *gin.Context, id string, params api.ListTenantLeasesParams) {
	writeNotImplemented(c)
}

func (s *APIServer) ListUsers(c *gin.Context, params api.ListUsersParams) { writeNotImplemented(c) }

func (s *APIServer) CreateUser(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) GetCurrentUser(c *gin.Context) { writeNotImplemented(c) }

func (s *APIServer) GetUser(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) UpdateUser(c *gin.Context, id string) { writeNotImplemented(c) }

func (s *APIServer) AssignUserProperties(c *gin.Context, id string) { writeNotImplemented(c) }
