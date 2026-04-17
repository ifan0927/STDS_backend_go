package router

import (
	"database/sql"
	"log/slog"

	"github.com/gin-gonic/gin"

	appiam "stds_backend/internal/application/iam"
	appjobs "stds_backend/internal/application/jobs"
	appproperty "stds_backend/internal/application/property"
	"stds_backend/internal/config"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/middleware"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
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
	syncAuthService *appiam.SyncAuthService,
	jobTriggerService *appjobs.TriggerService,
	propertyQueryRepo dbpropertyquery.Repository,
	createPropertyService *appproperty.CreatePropertyService,
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

	api.RegisterHandlersWithOptions(engine, handler.NewAPIServer(userRepo, createUserService, syncAuthService, jobTriggerService, propertyQueryRepo, createPropertyService), api.GinServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []api.MiddlewareFunc{
			protectedAPIMiddleware(appCfg, authenticator, userRepo, authzRepos),
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
		{method: "DELETE", path: "/api/v1/attachments/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByAttachmentID)},

		{method: "GET", path: "/api/v1/bills", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/bills/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByBillID)},
		{method: "POST", path: "/api/v1/bills/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByBillID)},
		{method: "GET", path: "/api/v1/bills/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByBillID)},
		{method: "POST", path: "/api/v1/bills/:id/meter", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByBillID)},
		{method: "POST", path: "/api/v1/bills/:id/payment", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByBillID)},

		{method: "GET", path: "/api/v1/force-terminations/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByForceTerminationID)},
		{method: "POST", path: "/api/v1/internal/jobs/force-terminations/compensate", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/leases/expire", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/leases/expiring-soon", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/monthly-snapshots/run", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/overdue-bills/reminders", authStrategy: authStrategyScheduler},
		{method: "POST", path: "/api/v1/internal/jobs/overdue-bills/scan", authStrategy: authStrategyScheduler},

		{method: "GET", path: "/api/v1/journal-logs", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/journal-logs", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/journal-logs/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByJournalLogID)},
		{method: "POST", path: "/api/v1/journal-logs/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByJournalLogID)},
		{method: "DELETE", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByJournalLogID)},
		{method: "GET", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByJournalLogID)},
		{method: "PATCH", path: "/api/v1/journal-logs/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByJournalLogID)},

		{method: "GET", path: "/api/v1/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/leases/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "POST", path: "/api/v1/leases/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "DELETE", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "GET", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "PATCH", path: "/api/v1/leases/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "PATCH", path: "/api/v1/leases/:id/deposit", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "POST", path: "/api/v1/leases/:id/force-terminate", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},
		{method: "POST", path: "/api/v1/leases/:id/terminate", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByLeaseID)},

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
		{method: "GET", path: "/api/v1/repair-requests/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "POST", path: "/api/v1/repair-requests/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "DELETE", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "GET", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "PATCH", path: "/api/v1/repair-requests/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "POST", path: "/api/v1/repair-requests/:id/assign", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "POST", path: "/api/v1/repair-requests/:id/cancel", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "POST", path: "/api/v1/repair-requests/:id/complete", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},
		{method: "POST", path: "/api/v1/repair-requests/:id/progress", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRepairRequestID)},

		{method: "GET", path: "/api/v1/rooms/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "POST", path: "/api/v1/rooms/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "DELETE", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "GET", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "PATCH", path: "/api/v1/rooms/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "POST", path: "/api/v1/rooms/:id/maintenance", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},
		{method: "GET", path: "/api/v1/rooms/:id/meter-history", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}, propertyResolver: middleware.ResourcePropertyID("id", ownership.FindPropertyIDByRoomID)},

		{method: "GET", path: "/api/v1/tenants", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/tenants", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/tenants/:id/attachments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/tenants/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id/leases", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/users", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/users", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/users/me", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/users/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/users/:id", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "POST", path: "/api/v1/users/:id/property-assignments", authStrategy: authStrategyFirebase, allowedRoles: []string{"admin"}},
	}
}
