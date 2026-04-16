package middleware

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
)

// Logging writes a structured access log entry after each HTTP request
// completes.
func Logging(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		principal, _ := requestctx.GetPrincipal(c)
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		jobMeta, _ := requestctx.GetJobMeta(c)

		logger.Info("http request",
			slog.String("request_id", requestctx.GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
			slog.String("user_id", principal.UserID),
			slog.String("firebase_uid", principal.FirebaseUID),
			slog.String("role", principal.Role),
			slog.String("property_id", loggedPropertyID(c, path)),
			slog.String("error_code", requestctx.GetErrorCode(c)),
			slog.String("job_key", jobMeta.JobKey),
			slog.String("job_window_key", jobMeta.WindowKey),
			slog.String("job_status", jobMeta.Status),
			slog.Int("job_retry_count", jobMeta.RetryCount),
			slog.Int64("job_duration_ms", jobMeta.DurationMs),
		)
	}
}

func loggedPropertyID(c *gin.Context, path string) string {
	if propertyID := requestctx.GetPropertyID(c); propertyID != "" {
		return propertyID
	}

	if strings.HasSuffix(path, "/properties/:id") || path == "/properties/:id" {
		return c.Param("id")
	}

	return ""
}
