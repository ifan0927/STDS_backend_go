package middleware

import (
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
	"stds_backend/internal/shared/apperr"
)

// Recovery captures panics, logs diagnostic context, and returns a standard
// internal server error response.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error("panic recovered",
			slog.String("request_id", requestctx.GetRequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.FullPath()),
			slog.Any("panic", recovered),
			slog.String("stack", string(debug.Stack())),
		)

		appErr := apperr.ErrInternalServerError.WithDetails(map[string]interface{}{
			"request_id": requestctx.GetRequestID(c),
		}).WithCause(fmt.Errorf("panic: %v", recovered))
		WriteError(c, appErr)
	})
}
