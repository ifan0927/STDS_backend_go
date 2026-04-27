package repair

import (
	"context"
	"database/sql"
	"strings"

	domainrepair "stds_backend/internal/domain/repair"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// CreateInput is the command payload for repair request creation.
type CreateInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	RoomID              string
	Title               string
	Description         string
}

// CreateService creates repair requests.
type CreateService struct {
	repo     Repository
	txRunner TransactionRunner
}

// NewCreateService returns a CreateService.
func NewCreateService(repo Repository, txRunner TransactionRunner) *CreateService {
	return &CreateService{repo: repo, txRunner: txRunner}
}

// Execute creates a submitted repair request.
func (s *CreateService) Execute(ctx context.Context, input CreateInput) (*RepairRequest, error) {
	actorRole, err := normalizeWriteRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	actorUserID, err := normalizeRequiredUUID(input.ActorUserID, "actor_user_id", apperr.ErrUnauthorized)
	if err != nil {
		return nil, err
	}
	propertyID, err := normalizeRequiredUUID(input.PropertyID, "property_id", apperr.ErrPropertyNotFound)
	if err != nil {
		return nil, err
	}
	roomID, err := normalizeRequiredUUID(input.RoomID, "room_id", apperr.ErrRoomNotFound)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	if _, err := domainrepair.New(domainrepair.State{
		PropertyID:  propertyID,
		RoomID:      roomID,
		SubmittedBy: actorUserID,
		Title:       title,
		Description: description,
	}); err != nil {
		return nil, mapDomainError(err)
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	var created *RepairRequest
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		room, err := s.repo.FindRoomByID(ctx, tx, roomID)
		if err != nil {
			return mapRepositoryError(err)
		}
		if room.PropertyID != propertyID {
			return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "room_id"})
		}
		if err := requirePropertyAccess(actorRole, input.AssignedPropertyIDs, propertyID); err != nil {
			return err
		}

		repairRequest, err := s.repo.Create(ctx, tx, CreateParams{
			PropertyID:  propertyID,
			RoomID:      roomID,
			SubmittedBy: actorUserID,
			Title:       title,
			Description: description,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		created = repairRequest
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}
