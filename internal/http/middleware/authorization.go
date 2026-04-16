package middleware

import (
	"context"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"

	"stds_backend/internal/http/requestctx"
	dbproperties "stds_backend/internal/platform/database/properties"
	"stds_backend/internal/shared/apperr"
)

// PropertyIDResolver extracts the property ID that should be authorized for
// the current request.
type PropertyIDResolver func(*gin.Context) (string, error)

// PropertyLookup resolves a resource id to its owning property id.
type PropertyLookup func(context.Context, string) (string, error)

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
func RequirePropertyAccess(resolvePropertyID PropertyIDResolver, propertyRepo dbproperties.Repository) gin.HandlerFunc {
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

		requestctx.SetPropertyID(c, propertyID)

		if principal.Role == "owner" {
			ownerID, err := propertyRepo.FindOwnerIDByPropertyID(c.Request.Context(), propertyID)
			if err != nil {
				c.Error(apperr.ErrForbidden.WithCause(err))
				c.Abort()
				return
			}

			if ownerID == principal.UserID {
				c.Next()
				return
			}

			c.Error(apperr.ErrForbidden.WithDetails(map[string]interface{}{
				"property_id": propertyID,
			}))
			c.Abort()
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

// ResourcePropertyID returns a resolver that first extracts a route parameter
// and then maps it back to a property id through a repository lookup.
func ResourcePropertyID(param string, lookup PropertyLookup) PropertyIDResolver {
	return func(c *gin.Context) (string, error) {
		if param == "" {
			return "", errors.New("resource param is required")
		}
		if lookup == nil {
			return "", errors.New("property lookup is required")
		}

		resourceID := c.Param(param)
		if resourceID == "" {
			return "", fmt.Errorf("resource param %q is empty", param)
		}

		return lookup(c.Request.Context(), resourceID)
	}
}
