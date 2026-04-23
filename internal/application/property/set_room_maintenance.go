package property

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domainproperty "stds_backend/internal/domain/property"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// SetRoomMaintenanceInput is the command payload for entering maintenance.
type SetRoomMaintenanceInput struct {
	RoomID      string
	OperatorID  string
	Title       string
	Description string
}

// SetRoomMaintenanceResult returns the updated room and created repair request.
type SetRoomMaintenanceResult struct {
	Room          *Room
	RepairRequest *RepairRequest
}

// SetRoomMaintenanceService coordinates the composite maintenance entry flow.
type SetRoomMaintenanceService struct {
	propertyRepo Repository
	txRunner     *txrunner.Runner
	now          func() time.Time
}

// NewSetRoomMaintenanceService returns a SetRoomMaintenanceService.
func NewSetRoomMaintenanceService(propertyRepo Repository, txRunner *txrunner.Runner) *SetRoomMaintenanceService {
	return &SetRoomMaintenanceService{
		propertyRepo: propertyRepo,
		txRunner:     txRunner,
		now:          time.Now,
	}
}

// Execute creates a room-scoped repair request and moves the room into maintenance in one transaction.
func (s *SetRoomMaintenanceService) Execute(ctx context.Context, input SetRoomMaintenanceInput) (*SetRoomMaintenanceResult, error) {
	roomID := strings.TrimSpace(input.RoomID)
	operatorID := strings.TrimSpace(input.OperatorID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)

	if roomID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}
	if operatorID == "" {
		return nil, apperr.ErrUnauthorized
	}
	if title == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "title"})
	}
	if description == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "description"})
	}

	result := &SetRoomMaintenanceResult{}
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
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

		if err := aggregate.EnterMaintenance(); err != nil {
			return mapDomainError(err)
		}

		repairRequest, err := s.propertyRepo.CreateRepairRequest(ctx, tx, CreateRepairRequestParams{
			PropertyID:  current.PropertyID,
			RoomID:      current.ID,
			SubmittedBy: operatorID,
			Title:       title,
			Description: description,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		room, err := s.propertyRepo.UpdateRoom(ctx, tx, UpdateRoomParams{
			ID:     current.ID,
			Name:   aggregate.RoomState().Name,
			Status: ptrString(aggregate.RoomState().Status),
		})
		if err != nil {
			switch {
			case errors.Is(err, apperr.ErrRoomNotFound):
				return apperr.ErrRoomNotFound
			default:
				return apperr.ErrInternalServerError.WithCause(err)
			}
		}

		result.Room = room
		result.RepairRequest = repairRequest
		recorder.Record(domainevents.RoomSetToMaintenance{
			RoomID:     current.ID,
			PropertyID: current.PropertyID,
			OperatorID: operatorID,
			OccurredAt: s.now().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func ptrString(value string) *string {
	return &value
}
