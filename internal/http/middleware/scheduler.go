package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/shared/apperr"
)

const schedulerKeyHeader = "X-Scheduler-Key"

// RequireSchedulerKey authenticates job trigger endpoints called by an external
// scheduler using a shared header value.
func RequireSchedulerKey(sharedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(sharedKey) == "" {
			c.Error(apperr.ErrInternalServerError.WithDetails(map[string]interface{}{
				"component": "scheduler_auth",
			}))
			c.Abort()
			return
		}

		provided := strings.TrimSpace(c.GetHeader(schedulerKeyHeader))
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(sharedKey)) != 1 {
			c.Error(apperr.ErrUnauthorized.WithDetails(map[string]interface{}{
				"auth_scheme": "scheduler_key",
			}))
			c.Abort()
			return
		}

		c.Next()
	}
}
