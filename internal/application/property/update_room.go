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

// UpdateRoomInput is the command payload for updating a room.
type UpdateRoomInput struct {
	ID   string
	Name *string
}

// UpdateRoomService updates room fields while enforcing mutation rules.
type UpdateRoomService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
}

// NewUpdateRoomService returns an UpdateRoomService.
func NewUpdateRoomService(propertyRepo Repository, txRunner *txrunner.Runner) *UpdateRoomService {
	return &UpdateRoomService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
	}
}

// Execute validates the update payload and persists the resulting room state.
func (s *UpdateRoomService) Execute(ctx context.Context, input UpdateRoomInput) (*Room, error) {
	roomID := strings.TrimSpace(input.ID)
	if roomID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}
	if input.Name == nil {
		return nil, apperr.ErrBadRequest
	}

	var updated *Room
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.propertyRepo.FindRoomByID(ctx, tx, roomID)
		if err != nil {
			switch {
			case errors.Is(err, apperr.ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		aggregate, err := domainproperty.RehydrateRoom(domainproperty.RoomState{
			ID:         current.ID,
			PropertyID: current.PropertyID,
			Name:       current.Name,
			Status:     current.Status,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := aggregate.UpdateRoom(domainproperty.RoomUpdateInput{Name: input.Name}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.RoomState()
		room, err := s.propertyRepo.UpdateRoom(ctx, tx, UpdateRoomParams{
			ID:     state.ID,
			Name:   state.Name,
			Status: nil,
		})
		if err != nil {
			switch {
			case errors.Is(err, apperr.ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		updated = room
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}
