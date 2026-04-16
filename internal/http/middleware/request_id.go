package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"stds_backend/internal/http/requestctx"
)

// RequestID ensures every request has an ID and exposes it through the request
// context and response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-Id")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Header("X-Request-Id", requestID)
		requestctx.SetRequestID(c, requestID)
		c.Next()
	}
}
