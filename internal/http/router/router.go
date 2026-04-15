package router

import (
	"github.com/gin-gonic/gin"

	"stds_backend/internal/config"
	"stds_backend/internal/http/handler"
)

func New(appCfg config.AppConfig) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())

	healthHandler := handler.NewHealthHandler(appCfg)
	docsHandler := handler.NewDocsHandler()

	engine.GET("/healthz", healthHandler.Live)
	engine.GET("/openapi.yaml", docsHandler.OpenAPI)
	engine.GET("/scalar", docsHandler.Scalar)

	api := engine.Group("/api/v1")
	{
		api.GET("/healthz", healthHandler.Live)
	}

	return engine
}
