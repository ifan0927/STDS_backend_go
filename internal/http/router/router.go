package router

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/config"
	"stds_backend/internal/http/api"
	"stds_backend/internal/http/handler"
	"stds_backend/internal/http/middleware"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
)

func New(appCfg config.AppConfig, logger *slog.Logger, db *sql.DB, authenticator platformfirebase.Authenticator, userRepo users.Repository) *gin.Engine {
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

	api.RegisterHandlersWithOptions(engine, handler.NewAPIServer(), api.GinServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []api.MiddlewareFunc{
			protectedPropertyDetailMiddleware(authenticator, userRepo),
		},
	})

	return engine
}

func protectedPropertyDetailMiddleware(authenticator platformfirebase.Authenticator, userRepo users.Repository) api.MiddlewareFunc {
	auth := middleware.Auth(authenticator, userRepo)
	requireRoles := middleware.RequireRoles("admin", "organizer", "staff", "owner")
	requirePropertyAccess := middleware.RequirePropertyAccess(middleware.ParamPropertyID("id"))

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet || c.FullPath() != "/api/v1/properties/:id" {
			return
		}

		auth(c)
		if c.IsAborted() {
			return
		}

		requireRoles(c)
		if c.IsAborted() {
			return
		}

		requirePropertyAccess(c)
	}
}
