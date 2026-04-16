package middleware

import (
	"errors"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
	"stds_backend/internal/shared/apperr"
)

// PropertyIDResolver extracts the property ID that should be authorized for
// the current request.
type PropertyIDResolver func(*gin.Context) (string, error)

// RequireRoles rejects requests whose authenticated principal does not match
// one of the allowed roles.
func RequireRoles(allowedRoles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, role := range allowedRoles {
		allowed[role] = struct{}{}
	}

	return func(c *gin.Context) {
		principal, ok := requestctx.GetPrincipal(c)
		if !ok {
			c.Error(apperr.ErrUnauthorized)
			c.Abort()
			return
		}

		if _, ok := allowed[principal.Role]; !ok {
			c.Error(apperr.ErrForbidden.WithDetails(map[string]interface{}{
				"required_roles": allowedRoles,
			}))
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePropertyAccess rejects requests for properties that the authenticated
// principal is not allowed to access.
func RequirePropertyAccess(resolvePropertyID PropertyIDResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := requestctx.GetPrincipal(c)
		if !ok {
			c.Error(apperr.ErrUnauthorized)
			c.Abort()
			return
		}

		if principal.Role == "admin" {
			c.Next()
			return
		}

		propertyID, err := resolvePropertyID(c)
		if err != nil {
			c.Error(apperr.ErrForbidden.WithCause(err))
			c.Abort()
			return
		}

		if propertyID == "" {
			c.Next()
			return
		}

		for _, assignedPropertyID := range principal.AssignedPropertyIDs {
			if assignedPropertyID == propertyID {
				c.Next()
				return
			}
		}

		c.Error(apperr.ErrForbidden.WithDetails(map[string]interface{}{
			"property_id": propertyID,
		}))
		c.Abort()
	}
}

// ParamPropertyID returns a resolver that reads the property ID from the given
// route parameter.
func ParamPropertyID(param string) PropertyIDResolver {
	return func(c *gin.Context) (string, error) {
		if param == "" {
			return "", errors.New("property param is required")
		}

		return c.Param(param), nil
	}
}
