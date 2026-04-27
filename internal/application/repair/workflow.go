package repair

import (
	"context"
	"database/sql"
	"strings"
	"time"

	domainevents "stds_backend/internal/domain/events"
	domainrepair "stds_backend/internal/domain/repair"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// AssignInput is the command payload for repair assignment.
type AssignInput struct {
	ActorRole   string
	ActorUserID string
	ID          string
	AssignedTo  string
}

// CancelInput is the command payload for repair cancellation.
type CancelInput struct {
	ID     string
	Reason *string
}

// WorkflowService applies repair status transitions.
type WorkflowService struct {
	repo     Repository
	txRunner TransactionRunner
	now      func() time.Time
}

// NewWorkflowService returns a WorkflowService.
func NewWorkflowService(repo Repository, txRunner TransactionRunner) *WorkflowService {
	return &WorkflowService{repo: repo, txRunner: txRunner, now: time.Now}
}

// Assign assigns a submitted repair request.
func (s *WorkflowService) Assign(ctx context.Context, input AssignInput) (*RepairRequest, error) {
	actorRole, err := normalizeWriteRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	actorUserID, err := normalizeRequiredUUID(input.ActorUserID, "actor_user_id", apperr.ErrUnauthorized)
	if err != nil {
		return nil, err
	}
	id, err := normalizeRequiredUUID(input.ID, "id", ErrRepairRequestNotFound)
	if err != nil {
		return nil, err
	}
	assignedTo, err := normalizeRequiredUUID(input.AssignedTo, "assigned_to", apperr.ErrUserNotFound)
	if err != nil {
		return nil, err
	}
	if actorRole == "staff" && assignedTo != actorUserID {
		return nil, apperr.ErrForbidden
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	var updated *RepairRequest
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		assignee, err := s.repo.FindUserByID(ctx, tx, assignedTo)
		if err != nil {
			return mapRepositoryError(err)
		}
		if assignee.Role != "organizer" && assignee.Role != "staff" {
			return apperr.ErrForbidden
		}

		current, err := s.repo.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return mapRepositoryError(err)
		}
		if !containsAssignedProperty(assignee.AssignedPropertyIDs, current.PropertyID) {
			return apperr.ErrForbidden.WithDetails(map[string]interface{}{"property_id": current.PropertyID})
		}
		aggregate, err := domainrepair.Rehydrate(toDomainState(current))
		if err != nil {
			return mapDomainError(err)
		}
		assignedAt := s.now().UTC()
		if err := aggregate.Assign(assignedTo, assignedAt); err != nil {
			return mapDomainError(err)
		}
		state := aggregate.State()

		repairRequest, err := s.repo.Assign(ctx, tx, AssignParams{
			ID:         id,
			AssignedTo: *state.AssignedTo,
			AssignedAt: *state.AssignedAt,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		updated = repairRequest
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

// Progress moves an assigned repair request to in_progress.
func (s *WorkflowService) Progress(ctx context.Context, id string) (*RepairRequest, error) {
	return s.transition(ctx, id, func(aggregate *domainrepair.Aggregate, recorder *txrunner.EventRecorder) error {
		return aggregate.Progress()
	}, func(ctx context.Context, tx *sql.Tx, state domainrepair.State) (*RepairRequest, error) {
		return s.repo.Progress(ctx, tx, state.ID)
	})
}

// Complete marks an in-progress repair request completed and emits RepairCompleted.
func (s *WorkflowService) Complete(ctx context.Context, id string) (*RepairRequest, error) {
	completedAt := s.now().UTC()
	return s.transition(ctx, id, func(aggregate *domainrepair.Aggregate, recorder *txrunner.EventRecorder) error {
		return aggregate.Complete(completedAt)
	}, func(ctx context.Context, tx *sql.Tx, state domainrepair.State) (*RepairRequest, error) {
		return s.repo.Complete(ctx, tx, CompleteParams{ID: state.ID, CompletedAt: completedAt})
	}, func(updated *RepairRequest, recorder *txrunner.EventRecorder) {
		recorder.Record(domainevents.RepairCompleted{
			RepairRequestID: updated.ID,
			RoomID:          updated.RoomID,
			PropertyID:      updated.PropertyID,
			OccurredAt:      completedAt,
		})
	})
}

// Cancel marks an active repair request cancelled and emits RepairCancelled.
func (s *WorkflowService) Cancel(ctx context.Context, input CancelInput) (*RepairRequest, error) {
	reason := trimmedString(input.Reason)
	cancelledAt := s.now().UTC()
	return s.transition(ctx, input.ID, func(aggregate *domainrepair.Aggregate, recorder *txrunner.EventRecorder) error {
		return aggregate.Cancel(reason)
	}, func(ctx context.Context, tx *sql.Tx, state domainrepair.State) (*RepairRequest, error) {
		return s.repo.Cancel(ctx, tx, CancelParams{ID: state.ID, CancelReason: state.CancelReason})
	}, func(updated *RepairRequest, recorder *txrunner.EventRecorder) {
		recorder.Record(domainevents.RepairCancelled{
			RepairRequestID: updated.ID,
			RoomID:          updated.RoomID,
			PropertyID:      updated.PropertyID,
			OccurredAt:      cancelledAt,
		})
	})
}

func (s *WorkflowService) transition(
	ctx context.Context,
	rawID string,
	mutate func(*domainrepair.Aggregate, *txrunner.EventRecorder) error,
	persist func(context.Context, *sql.Tx, domainrepair.State) (*RepairRequest, error),
	afterPersist ...func(*RepairRequest, *txrunner.EventRecorder),
) (*RepairRequest, error) {
	id, err := normalizeRequiredUUID(rawID, "id", ErrRepairRequestNotFound)
	if err != nil {
		return nil, err
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	var updated *RepairRequest
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return mapRepositoryError(err)
		}
		aggregate, err := domainrepair.Rehydrate(toDomainState(current))
		if err != nil {
			return mapDomainError(err)
		}
		if err := mutate(aggregate, recorder); err != nil {
			return mapDomainError(err)
		}
		state := aggregate.State()
		state.ID = id

		repairRequest, err := persist(ctx, tx, state)
		if err != nil {
			return mapRepositoryError(err)
		}
		for _, fn := range afterPersist {
			fn(repairRequest, recorder)
		}

		updated = repairRequest
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func toDomainState(repairRequest *RepairRequest) domainrepair.State {
	if repairRequest == nil {
		return domainrepair.State{}
	}

	return domainrepair.State{
		ID:           repairRequest.ID,
		PropertyID:   repairRequest.PropertyID,
		RoomID:       repairRequest.RoomID,
		SubmittedBy:  repairRequest.SubmittedBy,
		AssignedTo:   repairRequest.AssignedTo,
		Title:        repairRequest.Title,
		Description:  repairRequest.Description,
		Status:       repairRequest.Status,
		SubmittedAt:  repairRequest.SubmittedAt,
		AssignedAt:   repairRequest.AssignedAt,
		CompletedAt:  repairRequest.CompletedAt,
		CancelReason: repairRequest.CancelReason,
	}
}

func trimmedString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
