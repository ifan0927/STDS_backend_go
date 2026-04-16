package iam

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// ClaimsWriter updates Firebase custom claims for a user.
type ClaimsWriter interface {
	SetCustomClaims(ctx context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error
}

// CustomClaimsService writes Firebase custom claims in a consistent shape.
type CustomClaimsService struct {
	writer ClaimsWriter
}

// NewCustomClaimsService returns a CustomClaimsService.
func NewCustomClaimsService(writer ClaimsWriter) *CustomClaimsService {
	return &CustomClaimsService{writer: writer}
}

// Sync writes normalized role and assigned property claims.
func (s *CustomClaimsService) Sync(ctx context.Context, firebaseUID string, role string, assignedPropertyIDs []string) error {
	if s.writer == nil {
		return fmt.Errorf("claims writer is not configured")
	}

	normalized := normalizeAssignedPropertyIDs(assignedPropertyIDs)
	return s.writer.SetCustomClaims(ctx, strings.TrimSpace(firebaseUID), strings.TrimSpace(role), normalized)
}

func normalizeAssignedPropertyIDs(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
