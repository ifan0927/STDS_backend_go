package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"stds_backend/internal/http/requestctx"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbresourceownership "stds_backend/internal/platform/database/resourceownership"
	"stds_backend/internal/shared/apperr"
)

// PropertyIDResolver extracts the property ID that should be authorized for
// the current request.
type PropertyIDResolver func(*gin.Context) (string, error)

// PropertyIDsResolver extracts property IDs that should be authorized for the
// current request.
type PropertyIDsResolver func(*gin.Context) ([]string, error)

// PropertyLookup resolves a resource id to its owning property id.
type PropertyLookup func(context.Context, string) (string, error)

// PropertyIDsLookup resolves a resource id to its associated property ids.
type PropertyIDsLookup func(context.Context, string) ([]string, error)

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

// RequireAnyPropertyAccess rejects requests when none of the resolved
// properties is assigned to or owned by the authenticated principal.
func RequireAnyPropertyAccess(resolvePropertyIDs PropertyIDsResolver, propertyRepo dbproperties.Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := requestctx.GetPrincipal(c)
		if !ok {
			c.Error(apperr.ErrUnauthorized)
			c.Abort()
			return
		}

		propertyIDs, err := resolvePropertyIDs(c)
		if err != nil {
			c.Error(mapPropertyResolverError(err))
			c.Abort()
			return
		}

		if principal.Role == "admin" {
			c.Next()
			return
		}

		if principal.Role == "owner" {
			for _, propertyID := range propertyIDs {
				ownerID, err := propertyRepo.FindOwnerIDByPropertyID(c.Request.Context(), propertyID)
				if err != nil {
					if errors.Is(err, dbproperties.ErrNotFound) {
						continue
					}
					c.Error(mapPropertyLookupError(err))
					c.Abort()
					return
				}
				if ownerID == principal.UserID {
					requestctx.SetPropertyID(c, propertyID)
					c.Next()
					return
				}
			}

			c.Error(apperr.ErrForbidden)
			c.Abort()
			return
		}

		for _, propertyID := range propertyIDs {
			for _, assignedPropertyID := range principal.AssignedPropertyIDs {
				if assignedPropertyID == propertyID {
					requestctx.SetPropertyID(c, propertyID)
					c.Next()
					return
				}
			}
		}

		c.Error(apperr.ErrForbidden)
		c.Abort()
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

		propertyID, err := resolvePropertyID(c)
		if err != nil {
			c.Error(mapPropertyResolverError(err))
			c.Abort()
			return
		}

		if propertyID == "" {
			c.Next()
			return
		}

		requestctx.SetPropertyID(c, propertyID)

		if principal.Role == "admin" {
			c.Next()
			return
		}

		if principal.Role == "owner" {
			ownerID, err := propertyRepo.FindOwnerIDByPropertyID(c.Request.Context(), propertyID)
			if err != nil {
				c.Error(mapPropertyLookupError(err))
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

		if _, err := propertyRepo.FindOwnerIDByPropertyID(c.Request.Context(), propertyID); err != nil {
			c.Error(mapPropertyLookupError(err))
			c.Abort()
			return
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
		propertyID, err := resolveUUIDParam(c, param)
		if err != nil {
			return "", err
		}

		return propertyID, nil
	}
}

// QueryPropertyID returns a resolver that reads an optional property ID from
// the given query parameter.
func QueryPropertyID(param string) PropertyIDResolver {
	return func(c *gin.Context) (string, error) {
		if param == "" {
			return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{
				"parameter": "property_id",
			})
		}

		value := strings.TrimSpace(c.Query(param))
		if value == "" {
			return "", nil
		}

		if _, err := uuid.Parse(value); err != nil {
			return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{
				"parameter": param,
			}).WithCause(err)
		}

		return value, nil
	}
}

// ResourcePropertyID returns a resolver that first extracts a route parameter
// and then maps it back to a property id through a repository lookup.
func ResourcePropertyID(param string, lookup PropertyLookup) PropertyIDResolver {
	return ResourcePropertyIDWithNotFound(param, lookup, nil)
}

// ResourcePropertyIDWithNotFound returns a resolver that first extracts a
// route parameter and then maps it back to a property id through a repository
// lookup, translating lookup not-found results into the provided application
// error when configured.
func ResourcePropertyIDWithNotFound(param string, lookup PropertyLookup, notFoundErr *apperr.Error) PropertyIDResolver {
	return func(c *gin.Context) (string, error) {
		if param == "" {
			return "", errors.New("resource param is required")
		}
		if lookup == nil {
			return "", errors.New("property lookup is required")
		}

		resourceID := c.Param(param)
		if _, err := resolveUUIDParam(c, param); err != nil {
			return "", err
		}

		propertyID, err := lookup(c.Request.Context(), resourceID)
		if err != nil {
			if errors.Is(err, dbresourceownership.ErrNotFound) && notFoundErr != nil {
				return "", notFoundErr.WithCause(err)
			}

			return "", err
		}

		return propertyID, nil
	}
}

// ResourcePropertyIDsWithNotFound returns a resolver that maps a route resource
// id to all associated property ids, translating lookup not-found results into
// the provided application error when configured.
func ResourcePropertyIDsWithNotFound(param string, lookup PropertyIDsLookup, notFoundErr *apperr.Error) PropertyIDsResolver {
	return func(c *gin.Context) ([]string, error) {
		if param == "" {
			return nil, errors.New("resource param is required")
		}
		if lookup == nil {
			return nil, errors.New("property lookup is required")
		}

		resourceID := c.Param(param)
		if _, err := resolveUUIDParam(c, param); err != nil {
			return nil, err
		}

		propertyIDs, err := lookup(c.Request.Context(), resourceID)
		if err != nil {
			if errors.Is(err, dbresourceownership.ErrNotFound) && notFoundErr != nil {
				return nil, notFoundErr.WithCause(err)
			}

			return nil, err
		}

		return propertyIDs, nil
	}
}

func resolveUUIDParam(c *gin.Context, param string) (string, error) {
	if param == "" {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"parameter": "id",
		})
	}

	value := c.Param(param)
	if value == "" {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"parameter": param,
		})
	}

	if _, err := uuid.Parse(value); err != nil {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"parameter": param,
		}).WithCause(err)
	}

	return value, nil
}

func mapPropertyResolverError(err error) error {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr
	}

	return apperr.ErrInternalServerError.WithCause(err)
}

func mapPropertyLookupError(err error) error {
	switch {
	case errors.Is(err, dbproperties.ErrNotFound):
		return apperr.ErrPropertyNotFound.WithCause(err)
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
