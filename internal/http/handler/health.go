package handler

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/config"
)

// HealthHandler serves health check responses for the API process.
type HealthHandler struct {
	app config.AppConfig
	db  *sql.DB
}

// NewHealthHandler returns a handler that reports application and database
// readiness details.
func NewHealthHandler(app config.AppConfig, db *sql.DB) *HealthHandler {
	return &HealthHandler{app: app, db: db}
}

// Live responds with a liveness payload that includes basic database reachability.
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
