package property

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	domainproperty "stds_backend/internal/domain/property"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// CreateRoomInput is the command payload for creating a room.
type CreateRoomInput struct {
	PropertyID string
	Name       string
}

// CreateRoomService creates rooms within a property.
type CreateRoomService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
}

// NewCreateRoomService returns a CreateRoomService.
func NewCreateRoomService(propertyRepo Repository, txRunner *txrunner.Runner) *CreateRoomService {
	return &CreateRoomService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
	}
}

// Execute validates input and persists a new room.
func (s *CreateRoomService) Execute(ctx context.Context, input CreateRoomInput) (*Room, error) {
	propertyID := strings.TrimSpace(input.PropertyID)
	if propertyID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "property_id"})
	}

	aggregate, err := domainproperty.NewRoom(domainproperty.RoomState{
		PropertyID: propertyID,
		Name:       input.Name,
		Status:     domainproperty.RoomStatusVacant,
	})
	if err != nil {
		return nil, mapDomainError(err)
	}

	var created *Room
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if _, err := s.propertyRepo.FindByID(ctx, tx, propertyID); err != nil {
			switch {
			case errors.Is(err, ErrPropertyNotFound):
				return apperr.ErrPropertyNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		room, err := s.propertyRepo.CreateRoom(ctx, tx, CreateRoomParams{
			PropertyID: propertyID,
			Name:       aggregate.RoomState().Name,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		created = room
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}
