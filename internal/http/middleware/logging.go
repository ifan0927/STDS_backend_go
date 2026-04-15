package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
)

func Logging(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		principal, _ := requestctx.GetPrincipal(c)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		propertyID := c.Param("id")

		logger.Info("http request",
			slog.String("request_id", requestctx.GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("user_id", principal.UserID),
			slog.String("firebase_uid", principal.FirebaseUID),
			slog.String("role", principal.Role),
			slog.String("property_id", propertyID),
			slog.String("error_code", requestctx.GetErrorCode(c)),
		)
	}
}
