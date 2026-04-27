package journal

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

const maxListJournalLogsLimit = 100

// ListInput is the query payload for journal log listing.
type ListInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	PropertyID          *string
	RoomID              *string
	DateFrom            *time.Time
	DateTo              *time.Time
	Limit               int
	Offset              int
}

// ListService lists journal logs.
type ListService struct {
	repo Repository
}

// NewListService returns a ListService.
func NewListService(repo Repository) *ListService {
	return &ListService{repo: repo}
}

// Execute returns active journal logs visible to the actor.
func (s *ListService) Execute(ctx context.Context, input ListInput) ([]JournalLog, error) {
	role := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch role {
	case "admin", "organizer", "staff":
	default:
		return nil, apperr.ErrForbidden
	}
	propertyID, err := normalizeOptionalFilterUUID(input.PropertyID, "property_id")
	if err != nil {
		return nil, err
	}
	roomID, err := normalizeOptionalFilterUUID(input.RoomID, "room_id")
	if err != nil {
		return nil, err
	}
	if input.Limit < 1 || input.Limit > maxListJournalLogsLimit {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "limit",
			"reason": "must be between 1 and 100",
		})
	}
	if input.Offset < 0 {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field":  "offset",
			"reason": "must be greater than or equal to 0",
		})
	}

	items, err := s.repo.List(ctx, ListQuery{
		ActorRole:           role,
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		PropertyID:          propertyID,
		RoomID:              roomID,
		DateFrom:            input.DateFrom,
		DateTo:              input.DateTo,
		Limit:               input.Limit,
		Offset:              input.Offset,
	})
	if err != nil {
		return nil, mapRepositoryError(err)
	}

	return items, nil
}

func normalizeOptionalFilterUUID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": field})
	}

	return &trimmed, nil
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
