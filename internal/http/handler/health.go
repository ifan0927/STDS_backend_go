package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/config"
)

type HealthHandler struct {
	app config.AppConfig
}

func NewHealthHandler(app config.AppConfig) *HealthHandler {
	return &HealthHandler{app: app}
}

func (h *HealthHandler) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name": h.app.Name,
		"env":  h.app.Env,
		"ok":   true,
	})
}
