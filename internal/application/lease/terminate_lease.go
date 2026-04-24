package lease

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	domainevents "stds_backend/internal/domain/events"
	domainlease "stds_backend/internal/domain/lease"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// TerminateLeaseInput is the command payload for normal lease termination.
type TerminateLeaseInput struct {
	ActorRole           string
	AssignedPropertyIDs []string
	LeaseID             string
	RefundAmount        *int
	DeductionAmount     *int
	DeductionReason     *string
}

// TerminateLeaseService normally terminates a lease after all bills are settled.
type TerminateLeaseService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewTerminateLeaseService returns a TerminateLeaseService.
func NewTerminateLeaseService(repo Repository, txRunner *txrunner.Runner) *TerminateLeaseService {
	return &TerminateLeaseService{repo: repo, txRunner: txRunner}
}

// Execute enforces BR-04, settles the deposit, terminates the lease, and records LeaseTerminated.
func (s *TerminateLeaseService) Execute(ctx context.Context, input TerminateLeaseInput) (*Lease, error) {
	leaseID, err := normalizeLeaseID(input.LeaseID)
	if err != nil {
		return nil, err
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer", "staff":
	default:
		return nil, apperr.ErrForbidden
	}

	refundAmount := intValue(input.RefundAmount)
	deductionAmount := intValue(input.DeductionAmount)

	var terminated *Lease
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindLeaseByIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if (actorRole == "organizer" || actorRole == "staff") && !containsAssignedProperty(input.AssignedPropertyIDs, current.PropertyID) {
			return apperr.ErrForbidden
		}

		bills, err := s.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if unpaidBillIDs := unpaidBillIDs(bills); len(unpaidBillIDs) > 0 {
			return errLeaseHasUnpaidBills.WithDetails(map[string]interface{}{"unpaid_bill_ids": unpaidBillIDs})
		}

		aggregate, err := domainlease.Rehydrate(domainlease.State{
			ID:                        current.ID,
			TenantID:                  current.TenantID,
			RoomID:                    current.RoomID,
			PropertyID:                current.PropertyID,
			RentAmount:                current.RentAmount,
			StartDate:                 current.StartDate,
			EndDate:                   current.EndDate,
			ElectricityBillingCadence: current.ElectricityBillingCadence,
			Status:                    current.Status,
			DepositAmount:             current.DepositAmount,
			DepositRefundAmount:       current.DepositRefundAmount,
			DepositDeductionAmount:    current.DepositDeductionAmount,
			DepositStatus:             current.DepositStatus,
			DepositDeductionReason:    current.DepositDeductionReason,
			Version:                   current.Version,
		})
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.SettleDeposit(refundAmount, deductionAmount, input.DeductionReason); err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.Terminate(); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		settled, err := s.repo.SettleDeposit(ctx, tx, SettleDepositParams{
			LeaseID:                leaseID,
			RefundAmount:           *state.DepositRefundAmount,
			DeductionAmount:        *state.DepositDeductionAmount,
			DepositDeductionReason: state.DepositDeductionReason,
		})
		if err != nil {
			return mapLeaseLookupError(err)
		}

		terminatedLease, err := s.repo.TerminateLease(ctx, tx, TerminateLeaseParams{
			LeaseID:           leaseID,
			EndDate:           normalizeDate(time.Now().UTC()),
			TerminationReason: "normal termination",
		})
		if err != nil {
			return mapLeaseLookupError(err)
		}

		occurredAt := time.Now().UTC()
		if refundAmount > 0 {
			recorder.Record(domainevents.DepositRefunded{
				LeaseID:    settled.ID,
				PropertyID: settled.PropertyID,
				Amount:     refundAmount,
				OccurredAt: occurredAt,
			})
		}
		if deductionAmount > 0 {
			recorder.Record(domainevents.DepositDeducted{
				LeaseID:    settled.ID,
				PropertyID: settled.PropertyID,
				Amount:     deductionAmount,
				Reason:     depositReasonValue(input.DeductionReason),
				OccurredAt: occurredAt,
			})
		}
		recorder.Record(domainevents.LeaseTerminated{
			LeaseID:       terminatedLease.ID,
			RoomID:        terminatedLease.RoomID,
			PropertyID:    terminatedLease.PropertyID,
			TenantID:      terminatedLease.TenantID,
			Forced:        false,
			IsReplacement: false,
			OccurredAt:    occurredAt,
		})

		terminated = terminatedLease
		return nil
	})
	if err != nil {
		return nil, err
	}

	return terminated, nil
}

// ForceTerminateLeaseInput is the command payload for forced lease termination.
type ForceTerminateLeaseInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	LeaseID             string
	Reason              string
	DepositHandling     string
}

// ForceTerminateLeaseService force-terminates a lease and tracks bill write-off progress.
type ForceTerminateLeaseService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewForceTerminateLeaseService returns a ForceTerminateLeaseService.
func NewForceTerminateLeaseService(repo Repository, txRunner *txrunner.Runner) *ForceTerminateLeaseService {
	return &ForceTerminateLeaseService{repo: repo, txRunner: txRunner}
}

// Execute enforces BR-14, writes off unpaid bills, and records LeaseTerminated.
func (s *ForceTerminateLeaseService) Execute(ctx context.Context, input ForceTerminateLeaseInput) (*ForceTermination, error) {
	leaseID, err := normalizeLeaseID(input.LeaseID)
	if err != nil {
		return nil, err
	}
	actorUserID := strings.TrimSpace(input.ActorUserID)
	if actorUserID == "" || actorUserID == zeroUUID {
		return nil, apperr.ErrUnauthorized
	}
	if _, err := uuid.Parse(actorUserID); err != nil {
		return nil, apperr.ErrUnauthorized
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer":
	default:
		return nil, errForbiddenForceTermination
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, errForceTerminationReasonRequired
	}
	if strings.TrimSpace(input.DepositHandling) == "" {
		return nil, errForceTerminationDepositHandlingRequired
	}

	var forceTermination *ForceTermination
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		current, err := s.repo.FindLeaseByIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if actorRole == "organizer" && !containsAssignedProperty(input.AssignedPropertyIDs, current.PropertyID) {
			return apperr.ErrForbidden
		}
		aggregate, err := domainlease.Rehydrate(domainlease.State{
			ID:                        current.ID,
			TenantID:                  current.TenantID,
			RoomID:                    current.RoomID,
			PropertyID:                current.PropertyID,
			RentAmount:                current.RentAmount,
			StartDate:                 current.StartDate,
			EndDate:                   current.EndDate,
			ElectricityBillingCadence: current.ElectricityBillingCadence,
			Status:                    current.Status,
			DepositAmount:             current.DepositAmount,
			DepositRefundAmount:       current.DepositRefundAmount,
			DepositDeductionAmount:    current.DepositDeductionAmount,
			DepositStatus:             current.DepositStatus,
			DepositDeductionReason:    current.DepositDeductionReason,
			Version:                   current.Version,
		})
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.ForceTerminate(reason, input.DepositHandling); err != nil {
			return mapDomainError(err)
		}
		state := aggregate.State()

		bills, err := s.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		billIDs := unpaidBillIDs(bills)

		created, err := s.repo.CreateForceTermination(ctx, tx, CreateForceTerminationParams{
			LeaseID:     leaseID,
			InitiatedBy: actorUserID,
			Reason:      reason,
		})
		if err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := s.repo.CreateForceTerminationBills(ctx, tx, created.ID, billIDs); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := s.repo.WriteOffBills(ctx, tx, billIDs, reason); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}
		if err := s.repo.MarkForceTerminationBillsDone(ctx, tx, created.ID, billIDs); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		terminatedLease, err := s.repo.ForceTerminateLease(ctx, tx, ForceTerminateLeaseParams{
			LeaseID:           leaseID,
			TerminationReason: reason,
			DepositStatus:     state.DepositStatus,
		})
		if err != nil {
			return mapLeaseLookupError(err)
		}
		if err := s.repo.CompleteForceTermination(ctx, tx, created.ID); err != nil {
			return apperr.ErrInternalServerError.WithCause(err)
		}

		detail, err := s.repo.FindForceTerminationByID(ctx, tx, created.ID)
		if err != nil {
			return mapForceTerminationLookupError(err)
		}

		recorder.Record(domainevents.LeaseTerminated{
			LeaseID:       terminatedLease.ID,
			RoomID:        terminatedLease.RoomID,
			PropertyID:    terminatedLease.PropertyID,
			TenantID:      terminatedLease.TenantID,
			Forced:        true,
			IsReplacement: false,
			OccurredAt:    time.Now().UTC(),
		})

		forceTermination = detail
		return nil
	})
	if err != nil {
		return nil, err
	}

	return forceTermination, nil
}

// GetForceTerminationInput is the query payload for force-termination detail.
type GetForceTerminationInput struct {
	ActorRole          string
	ForceTerminationID string
}

// GetForceTerminationService returns force-termination progress detail.
type GetForceTerminationService struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewGetForceTerminationService returns a GetForceTerminationService.
func NewGetForceTerminationService(repo Repository, txRunner *txrunner.Runner) *GetForceTerminationService {
	return &GetForceTerminationService{repo: repo, txRunner: txRunner}
}

// Execute loads a single force-termination progress record.
func (s *GetForceTerminationService) Execute(ctx context.Context, input GetForceTerminationInput) (*ForceTermination, error) {
	forceTerminationID := strings.TrimSpace(input.ForceTerminationID)
	if forceTerminationID == "" || forceTerminationID == zeroUUID {
		return nil, apperr.ErrForceTerminationNotFound
	}
	if _, err := uuid.Parse(forceTerminationID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	actorRole := strings.ToLower(strings.TrimSpace(input.ActorRole))
	switch actorRole {
	case "admin", "organizer":
	default:
		return nil, apperr.ErrForbidden
	}

	var detail *ForceTermination
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		record, err := s.repo.FindForceTerminationByID(ctx, tx, forceTerminationID)
		if err != nil {
			return mapForceTerminationLookupError(err)
		}
		detail = record
		return nil
	})
	if err != nil {
		return nil, err
	}

	return detail, nil
}

func normalizeLeaseID(value string) (string, error) {
	leaseID := strings.TrimSpace(value)
	if leaseID == "" || leaseID == zeroUUID {
		return "", apperr.ErrLeaseNotFound
	}
	if _, err := uuid.Parse(leaseID); err != nil {
		return "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "lease_id"})
	}

	return leaseID, nil
}

func unpaidBillIDs(bills []Bill) []string {
	ids := make([]string, 0)
	for _, bill := range bills {
		switch bill.Status {
		case billStatusPaid, billStatusVoided, billStatusWrittenOff:
			continue
		default:
			ids = append(ids, bill.ID)
		}
	}

	return ids
}

func mapLeaseLookupError(err error) error {
	switch {
	case errors.Is(err, ErrLeaseNotFound):
		return apperr.ErrLeaseNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}

func mapForceTerminationLookupError(err error) error {
	switch {
	case errors.Is(err, ErrForceTerminationNotFound):
		return apperr.ErrForceTerminationNotFound
	default:
		return apperr.ErrInternalServerError.WithCause(err)
	}
}
