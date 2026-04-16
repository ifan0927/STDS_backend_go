package router

import (
	"database/sql"
	"log/slog"

	"github.com/gin-gonic/gin"

	appiam "stds_backend/internal/application/iam"
	"stds_backend/internal/config"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/middleware"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

// New builds the application's HTTP router with shared middleware, public
// endpoints, and authenticated API routes.
func New(appCfg config.AppConfig, logger *slog.Logger, db *sql.DB, authenticator platformfirebase.Authenticator, userRepo users.Repository, createUserService *appiam.CreateUserService) *gin.Engine {
	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Recovery(logger),
		middleware.Logging(logger),
		middleware.ErrorHandler(),
	)

	healthHandler := handler.NewHealthHandler(appCfg, db)
	docsHandler := handler.NewDocsHandler()

	engine.GET("/healthz", healthHandler.Live)
	engine.GET("/openapi.yaml", docsHandler.OpenAPI)
	engine.GET("/scalar", docsHandler.Scalar)

	engine.GET("/api/v1/healthz", healthHandler.Live)

	api.RegisterHandlersWithOptions(engine, handler.NewAPIServer(userRepo, createUserService), api.GinServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []api.MiddlewareFunc{
			protectedAPIMiddleware(authenticator, userRepo),
		},
	})

	return engine
}

type routePolicy struct {
	method        string
	path          string
	allowedRoles  []string
	propertyParam string
}

type compiledRoutePolicy struct {
	requireRoles          gin.HandlerFunc
	requirePropertyAccess gin.HandlerFunc
}

func protectedAPIMiddleware(authenticator platformfirebase.Authenticator, userRepo users.Repository) api.MiddlewareFunc {
	auth := middleware.Auth(authenticator, userRepo)
	policies := compileRoutePolicies(routePolicies())

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

		auth(c)
		if c.IsAborted() {
			return
		}

		policy.requireRoles(c)
		if c.IsAborted() {
			return
		}

		if policy.requirePropertyAccess != nil {
			policy.requirePropertyAccess(c)
		}
	}
}

func compileRoutePolicies(rawPolicies []routePolicy) map[string]compiledRoutePolicy {
	policies := make(map[string]compiledRoutePolicy, len(rawPolicies))
	for _, policy := range rawPolicies {
		compiled := compiledRoutePolicy{
			requireRoles: middleware.RequireRoles(policy.allowedRoles...),
		}
		if policy.propertyParam != "" {
			compiled.requirePropertyAccess = middleware.RequirePropertyAccess(middleware.ParamPropertyID(policy.propertyParam))
		}

		policies[routePolicyKey(policy.method, policy.path)] = compiled
	}

	return policies
}

func routePolicyKey(method string, path string) string {
	return method + " " + path
}

func routePolicies() []routePolicy {
	// Resource ownership checks that require resolving a property through an
	// indirect resource ID (for example room/lease/bill/journal/repair IDs) are
	// intentionally deferred until the corresponding repository-backed resolvers exist.
	return []routePolicy{
		{method: "POST", path: "/api/v1/auth/sync", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},

		{method: "GET", path: "/api/v1/bills", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/bills/:id", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "POST", path: "/api/v1/bills/:id/meter", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/bills/:id/payment", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/force-terminations/:id", allowedRoles: []string{"admin", "organizer"}},

		{method: "GET", path: "/api/v1/journal-logs", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/journal-logs", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "DELETE", path: "/api/v1/journal-logs/:id", allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/journal-logs/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/journal-logs/:id", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/leases", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/leases", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "DELETE", path: "/api/v1/leases/:id", allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/leases/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/leases/:id", allowedRoles: []string{"admin", "organizer"}},
		{method: "PATCH", path: "/api/v1/leases/:id/deposit", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/leases/:id/force-terminate", allowedRoles: []string{"admin", "organizer"}},
		{method: "POST", path: "/api/v1/leases/:id/terminate", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/properties", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "POST", path: "/api/v1/properties", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "DELETE", path: "/api/v1/properties/:id", allowedRoles: []string{"admin", "organizer"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id", allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyParam: "id"},
		{method: "PATCH", path: "/api/v1/properties/:id", allowedRoles: []string{"admin", "organizer", "staff"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/dashboard", allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/financial-report", allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/financial-report/:year/:month", allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyParam: "id"},
		{method: "POST", path: "/api/v1/properties/:id/financial-report/:year/:month/send", allowedRoles: []string{"admin", "organizer"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/meter-history", allowedRoles: []string{"admin", "organizer", "staff"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/pending-meter", allowedRoles: []string{"admin", "organizer", "staff"}, propertyParam: "id"},
		{method: "GET", path: "/api/v1/properties/:id/rooms", allowedRoles: []string{"admin", "organizer", "staff", "owner"}, propertyParam: "id"},
		{method: "POST", path: "/api/v1/properties/:id/rooms", allowedRoles: []string{"admin", "organizer", "staff"}, propertyParam: "id"},

		{method: "GET", path: "/api/v1/repair-requests", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "DELETE", path: "/api/v1/repair-requests/:id", allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/repair-requests/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/repair-requests/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests/:id/assign", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests/:id/cancel", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests/:id/complete", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/repair-requests/:id/progress", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "DELETE", path: "/api/v1/rooms/:id", allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/rooms/:id", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "PATCH", path: "/api/v1/rooms/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/rooms/:id/maintenance", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/rooms/:id/meter-history", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/tenants", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/tenants", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/tenants/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "GET", path: "/api/v1/tenants/:id/leases", allowedRoles: []string{"admin", "organizer", "staff"}},

		{method: "GET", path: "/api/v1/users", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "POST", path: "/api/v1/users", allowedRoles: []string{"admin", "organizer"}},
		{method: "GET", path: "/api/v1/users/me", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "GET", path: "/api/v1/users/:id", allowedRoles: []string{"admin", "organizer", "staff"}},
		{method: "PATCH", path: "/api/v1/users/:id", allowedRoles: []string{"admin", "organizer", "staff", "owner"}},
		{method: "POST", path: "/api/v1/users/:id/property-assignments", allowedRoles: []string{"admin"}},
	}
}
