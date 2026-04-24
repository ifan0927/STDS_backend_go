package billing

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

// GetBillInput is the use-case input for bill detail retrieval.
type GetBillInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	BillID              string
}

// GetBillService retrieves one bill read model.
type GetBillService struct {
	repo Repository
}

// NewGetBillService returns a GetBillService.
func NewGetBillService(repo Repository) *GetBillService {
	return &GetBillService{repo: repo}
}

// Execute validates the bill id and loads the bill.
func (s *GetBillService) Execute(ctx context.Context, input GetBillInput) (*Bill, error) {
	actorRole, err := normalizeReadRole(input.ActorRole)
	if err != nil {
		return nil, err
	}
	billID := strings.TrimSpace(input.BillID)
	if billID == "" {
		return nil, apperr.ErrBillNotFound
	}
	if _, err := uuid.Parse(billID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	bill, err := s.repo.FindBillByID(ctx, GetBillQuery{
		ActorRole:           actorRole,
		ActorUserID:         strings.TrimSpace(input.ActorUserID),
		AssignedPropertyIDs: cloneStrings(input.AssignedPropertyIDs),
		BillID:              billID,
	})
	if err != nil {
		return nil, mapRepositoryError(err)
	}

	return bill, nil
}
