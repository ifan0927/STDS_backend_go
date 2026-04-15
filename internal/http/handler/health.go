package handler

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/config"
)

type HealthHandler struct {
	app config.AppConfig
	db  *sql.DB
}

func NewHealthHandler(app config.AppConfig, db *sql.DB) *HealthHandler {
	return &HealthHandler{app: app, db: db}
}

func (h *HealthHandler) Live(c *gin.Context) {
	dbOK := false
	if h.db != nil && h.db.PingContext(c.Request.Context()) == nil {
		dbOK = true
	}

	c.JSON(http.StatusOK, gin.H{
		"name": h.app.Name,
		"env":  h.app.Env,
		"ok":   true,
		"db":   dbOK,
	})
}
