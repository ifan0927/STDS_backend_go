package router

import (
	"database/sql"
	"log/slog"

	"github.com/gin-gonic/gin"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	applease "stds_backend/internal/application/lease"
	appproperty "stds_backend/internal/application/property"
	apptenant "stds_backend/internal/application/tenant"
	"stds_backend/internal/config"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/middleware"
	dbleasequery "stds_backend/internal/platform/database/leasequery"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	dbtenantquery "stds_backend/internal/platform/database/tenantquery"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

type authStrategy string

const (
	authStrategyFirebase          authStrategy = "firebase"
	authStrategyFirebaseTokenOnly authStrategy = "firebase_token_only"
	authStrategyScheduler         authStrategy = "scheduler"
)

// AuthorizationRepositories groups repository-backed resolvers used by
// authorization middleware.
type AuthorizationRepositories struct {
	Properties        dbproperties.Repository
	ResourceOwnership dbresourceownership.Repository
}

// New builds the application's HTTP router with shared middleware, public
// endpoints, and authenticated API routes.
func New(
	appCfg config.AppConfig,
	logger *slog.Logger,
	db *sql.DB,
	authenticator platformfirebase.Authenticator,
	userRepo users.Repository,
	authzRepos AuthorizationRepositories,
	createUserService *appiam.CreateUserService,
	sendPasswordResetService *appiam.SendUserPasswordResetService,
	syncAuthService *appiam.SyncAuthService,
	updateCurrentUserService *appiam.UpdateCurrentUserService,
	updateUserService *appiam.UpdateUserService,
	assignUserPropertiesService *appiam.AssignUserPropertiesService,
	jobTriggerService *appjobs.TriggerService,
	propertyQueryRepo dbpropertyquery.Repository,
	leaseQueryRepo dbleasequery.Repository,
	tenantQueryRepo dbtenantquery.Repository,
	createPropertyService *appproperty.CreatePropertyService,
	updatePropertyService *appproperty.UpdatePropertyService,
	deletePropertyService *appproperty.DeletePropertyService,
	createRoomService *appproperty.CreateRoomService,
	updateRoomService *appproperty.UpdateRoomService,
	deleteRoomService *appproperty.DeleteRoomService,
	setRoomMaintenanceService *appproperty.SetRoomMaintenanceService,
	createTenantService *apptenant.CreateTenantService,
	updateTenantService *apptenant.UpdateTenantService,
	createLeaseService *applease.CreateLeaseService,
) *gin.Engine {
	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Recovery(logger),
		middleware.Logging(logger),
		middleware.ErrorHandler(logger),
	)

	healthHandler := handler.NewHealthHandler(appCfg, db)
	docsHandler := handler.NewDocsHandler()

	engine.GET("/healthz", healthHandler.Live)
	engine.GET("/openapi.yaml", docsHandler.OpenAPI)
	engine.GET("/scalar", docsHandler.Scalar)

	api.RegisterHandlersWithOptions(engine, handler.NewAPIServer(userRepo, createUserService, sendPasswordResetService, syncAuthService, updateCurrentUserService, updateUserService, assignUserPropertiesService, jobTriggerService, propertyQueryRepo, leaseQueryRepo, tenantQueryRepo, createPropertyService, updatePropertyService, deletePropertyService, createRoomService, updateRoomService, deleteRoomService, setRoomMaintenanceService, createTenantService, updateTenantService, createLeaseService), api.GinServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []api.MiddlewareFunc{
			protectedAPIMiddleware(appCfg, authenticator, userRepo, authzRepos),
		},
		ErrorHandler: func(c *gin.Context, err error, statusCode int) {
			middleware.WriteError(c, middleware.NewHTTPStatusError(statusCode, err))
		},
	})

	return engine
}

type routePolicy struct {
	method           string
	path             string
	authStrategy     authStrategy
	allowedRoles     []string
	propertyResolver middleware.PropertyIDResolver
}

type compiledRoutePolicy struct {
	authenticate          gin.HandlerFunc
	requireRoles          gin.HandlerFunc
	requirePropertyAccess gin.HandlerFunc
}

func protectedAPIMiddleware(appCfg config.AppConfig, authenticator platformfirebase.Authenticator, userRepo users.Repository, authzRepos AuthorizationRepositories) api.MiddlewareFunc {
	policies := compileRoutePolicies(appCfg, authenticator, userRepo, authzRepos, routePolicies(authzRepos))

	return func(c *gin.Context) {
		policy, ok := policies[routePolicyKey(c.Request.Method, c.FullPath())]
		if !ok {
			c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{
				"method": c.Request.Method,
				"path":   c.FullPath(),
			}))
			c.Abort()
			return
		}

		policy.authenticate(c)
		if c.IsAborted() {
			return
		}

		if policy.requireRoles != nil {
			policy.requireRoles(c)
			if c.IsAborted() {
				return
			}
		}

		if policy.requirePropertyAccess != nil {
			policy.requirePropertyAccess(c)
		}
	}
}

func compileRoutePolicies(appCfg config.AppConfig, authenticator platformfirebase.Authenticator, userRepo users.Repository, authzRepos AuthorizationRepositories, rawPolicies []routePolicy) map[string]compiledRoutePolicy {
	policies := make(map[string]compiledRoutePolicy, len(rawPolicies))
	for _, policy := range rawPolicies {
		compiled := compiledRoutePolicy{}
		switch policy.authStrategy {
		case authStrategyFirebaseTokenOnly:
			compiled.authenticate = middleware.FirebaseTokenOnly(authenticator)
		case authStrategyScheduler:
			compiled.authenticate = middleware.RequireSchedulerKey(appCfg.SchedulerKey)
		default:
			compiled.authenticate = middleware.Auth(authenticator, userRepo)
		}
		if len(policy.allowedRoles) > 0 {
			compiled.requireRoles = middleware.RequireRoles(policy.allowedRoles...)
		}
		if policy.propertyResolver != nil {
			compiled.requirePropertyAccess = middleware.RequirePropertyAccess(policy.propertyResolver, authzRepos.Properties)
		}

		policies[routePolicyKey(policy.method, policy.path)] = compiled
	}

	return policies
}

func routePolicyKey(method string, path string) string {
	return method + " " + path
}

func routePolicies(authzRepos AuthorizationRepositories) []routePolicy {
	ownership := authzRepos.ResourceOwnership

	return []routePolicy{
		{method: "POST", path: "/api/v1/auth/sync", authStrategy: authStrategyFirebaseTokenOnly},
		{method: "POST", path: "/api/v1/attachments/upload-url", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "DELETE", path: "/api/v1/attachments/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByAttachmentID, apperr.ErrAttachmentNotFound)},

		{method: "GET", path: "/api/v1/bills", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/bills/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByBillID, apperr.ErrBillNotFound)},
		{method: "POST", path: "/api/v1/bills/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByBillID, apperr.ErrBillNotFound)},
		{method: "GET", path: "/api/v1/bills/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByBillID, apperr.ErrBillNotFound)},
		{method: "POST", path: "/api/v1/bills/:id/meter", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByBillID, apperr.ErrBillNotFound)},
		{method: "POST", path: "/api/v1/bills/:id/payment", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByBillID, apperr.ErrBillNotFound)},

		{method: "GET", path: "/api/v1/force-terminations/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByForceTerminationID, apperr.ErrForceTerminationNotFound)},
		{method: "POST", path: "/api/v1/internal/jobs/force-terminations/compensate", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/leases/expire", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/leases/expiring-soon", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/monthly-snapshots/run", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/overdue-bills/reminders", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/overdue-bills/scan", authStrategy: authStrategyScheduler},

		{method: "GET", path: "/api/v1/journal-logs", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/journal-logs", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/journal-logs/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByJournalLogID, apperr.ErrJournalLogNotFound)},
		{method: "POST", path: "/api/v1/journal-logs/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByJournalLogID, apperr.ErrJournalLogNotFound)},
		{method: "DELETE", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByJournalLogID, apperr.ErrJournalLogNotFound)},
		{method: "GET", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByJournalLogID, apperr.ErrJournalLogNotFound)},
		{method: "PATCH", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByJournalLogID, apperr.ErrJournalLogNotFound)},

		{method: "GET", path: "/api/v1/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.QueryPropertyID("property_id")},
		{method: "POST", path: "/api/v1/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/leases/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "POST", path: "/api/v1/leases/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "DELETE", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "GET", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "PATCH", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "PATCH", path: "/api/v1/leases/:id/deposit", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "POST", path: "/api/v1/leases/:id/replace", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "POST", path: "/api/v1/leases/:id/force-terminate", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},
		{method: "POST", path: "/api/v1/leases/:id/terminate", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByLeaseID, apperr.ErrLeaseNotFound)},

		{method: "GET", path: "/api/v1/properties", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "POST", path: "/api/v1/properties", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/properties/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "POST", path: "/api/v1/properties/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "DELETE", path: "/api/v1/properties/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "PATCH", path: "/api/v1/properties/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/dashboard", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/financial-report", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/financial-report/:year/:month", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "POST", path: "/api/v1/properties/:id/financial-report/:year/:month/send", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/meter-history", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/pending-meter", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "GET", path: "/api/v1/properties/:id/rooms", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ParamPropertyID("id")},
		{method: "POST", path: "/api/v1/properties/:id/rooms", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ParamPropertyID("id")},

		{method: "GET", path: "/api/v1/repair-requests", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/repair-requests/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "POST", path: "/api/v1/repair-requests/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "DELETE", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "GET", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "PATCH", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "POST", path: "/api/v1/repair-requests/:id/assign", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "POST", path: "/api/v1/repair-requests/:id/cancel", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "POST", path: "/api/v1/repair-requests/:id/complete", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},
		{method: "POST", path: "/api/v1/repair-requests/:id/progress", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRepairRequestID, apperr.ErrRepairRequestNotFound)},

		{method: "GET", path: "/api/v1/rooms/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "POST", path: "/api/v1/rooms/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "DELETE", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "GET", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "PATCH", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "POST", path: "/api/v1/rooms/:id/maintenance", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},
		{method: "GET", path: "/api/v1/rooms/:id/meter-history", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByRoomID, apperr.ErrRoomNotFound)},

		{method: "GET", path: "/api/v1/tenants", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.QueryPropertyID("property_id")},
		{method: "POST", path: "/api/v1/tenants", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByTenantID, apperr.ErrTenantNotFound)},
		{method: "POST", path: "/api/v1/tenants/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyIDWithNotFound("id", ownership.FindPropertyIDByTenantID, apperr.ErrTenantNotFound)},
		{method: "GET", path: "/api/v1/tenants/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/tenants/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/users", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/users", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/users/me", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "PATCH", path: "/api/v1/users/me", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/users/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/users/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin"}},
		{method: "POST", path: "/api/v1/users/:id/property-assignments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin"}},
		{method: "POST", path: "/api/v1/users/:id/password-reset", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin"}},
	}
}
