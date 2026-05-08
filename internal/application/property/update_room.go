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
	ID                string
	Name              *string
	Size              *float64
	ClearSize         bool
	Floor             *string
	ClearFloor        bool
	RoomType          *string
	ClearRoomType     bool
	Facilities        *map[string]interface{}
	ClearFacilities   bool
	DefaultRentAmount *int
	ClearDefaultRent  bool
	Notes             *string
	ClearNotes        bool
	Zone              *string
	ClearZone         bool
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
	if input.Name == nil && input.Size == nil && !input.ClearSize && input.Floor == nil && !input.ClearFloor && input.RoomType == nil && !input.ClearRoomType && input.Facilities == nil && !input.ClearFacilities && input.DefaultRentAmount == nil && !input.ClearDefaultRent && input.Notes == nil && !input.ClearNotes && input.Zone == nil && !input.ClearZone {
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
			ID:                current.ID,
			PropertyID:        current.PropertyID,
			Name:              current.Name,
			Status:            current.Status,
			Size:              current.Size,
			Floor:             current.Floor,
			RoomType:          current.RoomType,
			Facilities:        current.Facilities,
			DefaultRentAmount: current.DefaultRentAmount,
			Notes:             current.Notes,
			Zone:              current.Zone,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		if err := aggregate.UpdateRoom(domainproperty.RoomUpdateInput{
			Name:              input.Name,
			Size:              input.Size,
			ClearSize:         input.ClearSize,
			Floor:             input.Floor,
			ClearFloor:        input.ClearFloor,
			RoomType:          input.RoomType,
			ClearRoomType:     input.ClearRoomType,
			Facilities:        input.Facilities,
			ClearFacilities:   input.ClearFacilities,
			DefaultRentAmount: input.DefaultRentAmount,
			ClearDefaultRent:  input.ClearDefaultRent,
			Notes:             input.Notes,
			ClearNotes:        input.ClearNotes,
			Zone:              input.Zone,
			ClearZone:         input.ClearZone,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.RoomState()
		room, err := s.propertyRepo.UpdateRoom(ctx, tx, UpdateRoomParams{
			ID:                state.ID,
			Name:              state.Name,
			Status:            nil,
			Size:              state.Size,
			Floor:             state.Floor,
			RoomType:          state.RoomType,
			Facilities:        state.Facilities,
			DefaultRentAmount: state.DefaultRentAmount,
			Notes:             state.Notes,
			Zone:              state.Zone,
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
