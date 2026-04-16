package middleware

import (
	"errors"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/api"
	"stds_backend/internal/http/requestctx"
	"stds_backend/internal/shared/apperr"
)

// ErrorHandler converts request-scoped errors into the standardized API error
// response format.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}

		appErr := toAppError(c.Errors.Last().Err)
		writeError(c, appErr)
	}
}

func toAppError(err error) *apperr.Error {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr
	}

	return apperr.ErrInternalServerError.WithCause(err)
}

func writeError(c *gin.Context, appErr *apperr.Error) {
	requestctx.SetErrorCode(c, appErr.Code)

	details := normalizeDetails(appErr.Details)
	if appErr.HTTPStatus >= 500 {
		details["request_id"] = requestctx.GetRequestID(c)
	}

	errorCode := appErr.Code
	message := appErr.Message

	c.AbortWithStatusJSON(appErr.HTTPStatus, api.ErrorResponse{
		ErrorCode: &errorCode,
		Message:   &message,
		Details:   &details,
	})
}

func normalizeDetails(details any) map[string]interface{} {
	if details == nil {
		return map[string]interface{}{}
	}

	mapped, ok := details.(map[string]interface{})
	if ok {
		return mapped
	}

	return map[string]interface{}{
		"value": details,
	}
}
