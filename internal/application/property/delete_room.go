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

// DeleteRoomInput is the command payload for deleting a room.
type DeleteRoomInput struct {
	ID string
}

// DeleteRoomService deletes rooms while enforcing BR-08.
type DeleteRoomService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
}

// NewDeleteRoomService returns a DeleteRoomService.
func NewDeleteRoomService(propertyRepo Repository, txRunner *txrunner.Runner) *DeleteRoomService {
	return &DeleteRoomService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
	}
}

// Execute loads the room, checks BR-08, and soft-deletes it.
func (s *DeleteRoomService) Execute(ctx context.Context, input DeleteRoomInput) error {
	roomID := strings.TrimSpace(input.ID)
	if roomID == "" {
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	return s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
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

		if err := aggregate.EnsureRoomDeletable(); err != nil {
			return mapDomainError(err)
		}

		if err := s.propertyRepo.SoftDeleteRoom(ctx, tx, current.ID); err != nil {
			switch {
			case errors.Is(err, apperr.ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		return nil
	})
}
