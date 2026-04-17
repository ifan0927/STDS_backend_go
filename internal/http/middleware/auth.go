package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
	"stds_backend/internal/platform/database/users"
	platformfirebase "stds_backend/internal/platform/firebase"
	"stds_backend/internal/shared/apperr"
)

// Auth authenticates the Firebase bearer token and stores the resolved user
// principal in the request context.
func Auth(authenticator platformfirebase.Authenticator, userRepo users.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}

		claims, err := authenticator.VerifyIDToken(c.Request.Context(), token)
		if err != nil {
			c.Error(apperr.ErrInvalidFirebaseToken.WithCause(err))
			c.Abort()
			return
		}

		user, err := userRepo.FindByFirebaseUID(c.Request.Context(), claims.UID)
		if err != nil {
			if err == users.ErrNotFound {
				c.Error(apperr.ErrUnauthorized.WithDetails(map[string]interface{}{
					"firebase_uid": claims.UID,
				}))
				c.Abort()
				return
			}
			c.Error(apperr.ErrInternalServerError.WithCause(err))
			c.Abort()
			return
		}

		requestctx.SetPrincipal(c, requestctx.Principal{
			UserID:              user.ID,
			FirebaseUID:         user.FirebaseUID,
			Role:                user.Role,
			AssignedPropertyIDs: user.AssignedPropertyIDs,
		})
		requestctx.SetFirebaseUID(c, claims.UID)

		c.Next()
	}
}

// FirebaseTokenOnly authenticates the Firebase bearer token and stores the
// resolved Firebase UID in the request context without requiring a DB user.
func FirebaseTokenOnly(authenticator platformfirebase.Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := bearerToken(c.GetHeader("Authorization"))
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}

		claims, err := authenticator.VerifyIDToken(c.Request.Context(), token)
		if err != nil {
			c.Error(apperr.ErrInvalidFirebaseToken.WithCause(err))
			c.Abort()
			return
		}

		requestctx.SetFirebaseUID(c, claims.UID)
		c.Next()
	}
}

func bearerToken(header string) (string, error) {
	if header == "" {
		return "", apperr.ErrUnauthorized
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", apperr.ErrUnauthorized
	}

	return strings.TrimSpace(parts[1]), nil
}
