package brand

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// FAQItem is the application-facing brand FAQ item.
type FAQItem struct {
	ID        string
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int
}

// ListFAQItemsInput contains brand FAQ list filters.
type ListFAQItemsInput struct {
	IncludeInactive bool
}

// CreateFAQItemInput contains writable fields for creating a brand FAQ item.
type CreateFAQItemInput struct {
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
}

// UpdateFAQItemInput contains writable fields for updating a brand FAQ item.
type UpdateFAQItemInput struct {
	ID        string
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
	Version   int
}

// DeactivateFAQItemInput contains fields for deactivating a brand FAQ item.
type DeactivateFAQItemInput struct {
	ID      string
	Version int
}

// CreateFAQItemParams contains normalized fields for creating a brand FAQ item.
type CreateFAQItemParams struct {
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
}

// UpdateFAQItemParams contains normalized fields for updating a brand FAQ item.
type UpdateFAQItemParams struct {
	ID        string
	Question  string
	Answer    string
	SortOrder int
	IsActive  bool
	Version   int
}

// FAQRepository defines persistence needed by brand FAQ application services.
type FAQRepository interface {
	List(ctx context.Context, includeInactive bool) ([]FAQItem, error)
	FindForUpdate(ctx context.Context, tx *sql.Tx, id string) (*FAQItem, error)
	Create(ctx context.Context, tx *sql.Tx, params CreateFAQItemParams) (*FAQItem, error)
	Update(ctx context.Context, tx *sql.Tx, params UpdateFAQItemParams) (*FAQItem, error)
	Deactivate(ctx context.Context, tx *sql.Tx, id string, version int) (*FAQItem, error)
}

// FAQService owns brand FAQ read and write use cases.
type FAQService struct {
	repo     FAQRepository
	txRunner *txrunner.Runner
}

// NewFAQService returns a brand FAQ service.
func NewFAQService(repo FAQRepository, txRunner *txrunner.Runner) *FAQService {
	return &FAQService{repo: repo, txRunner: txRunner}
}

// ListFAQItems returns brand FAQ items ordered by sort_order.
func (s *FAQService) ListFAQItems(ctx context.Context, input ListFAQItemsInput) ([]FAQItem, error) {
	items, err := s.repo.List(ctx, input.IncludeInactive)
	if err != nil {
		return nil, mapError(err)
	}

	return items, nil
}

// CreateFAQItem creates a brand FAQ item.
func (s *FAQService) CreateFAQItem(ctx context.Context, input CreateFAQItemInput) (*FAQItem, error) {
	params, err := normalizeCreateFAQItemInput(input)
	if err != nil {
		return nil, err
	}

	var saved *FAQItem
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		item, err := s.repo.Create(ctx, tx, params)
		if err != nil {
			return mapError(err)
		}
		saved = item
		return nil
	})
	if err != nil {
		return nil, err
	}

	return saved, nil
}

// UpdateFAQItem updates a brand FAQ item with optimistic locking.
func (s *FAQService) UpdateFAQItem(ctx context.Context, input UpdateFAQItemInput) (*FAQItem, error) {
	params, err := normalizeUpdateFAQItemInput(input)
	if err != nil {
		return nil, err
	}

	var saved *FAQItem
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if _, err := s.repo.FindForUpdate(ctx, tx, params.ID); err != nil {
			return mapError(err)
		}

		item, err := s.repo.Update(ctx, tx, params)
		if err != nil {
			if errors.Is(err, ErrBrandFAQItemNotFound) {
				return apperr.ErrConcurrentUpdateConflict
			}
			return mapError(err)
		}
		saved = item
		return nil
	})
	if err != nil {
		return nil, err
	}

	return saved, nil
}

// DeactivateFAQItem marks a brand FAQ item inactive with optimistic locking.
func (s *FAQService) DeactivateFAQItem(ctx context.Context, input DeactivateFAQItemInput) (*FAQItem, error) {
	if strings.TrimSpace(input.ID) == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	var saved *FAQItem
	err := s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		if _, err := s.repo.FindForUpdate(ctx, tx, input.ID); err != nil {
			return mapError(err)
		}

		item, err := s.repo.Deactivate(ctx, tx, input.ID, input.Version)
		if err != nil {
			if errors.Is(err, ErrBrandFAQItemNotFound) {
				return apperr.ErrConcurrentUpdateConflict
			}
			return mapError(err)
		}
		saved = item
		return nil
	})
	if err != nil {
		return nil, err
	}

	return saved, nil
}

func normalizeCreateFAQItemInput(input CreateFAQItemInput) (CreateFAQItemParams, error) {
	question, answer, err := normalizeFAQText(input.Question, input.Answer)
	if err != nil {
		return CreateFAQItemParams{}, err
	}

	return CreateFAQItemParams{
		Question:  question,
		Answer:    answer,
		SortOrder: input.SortOrder,
		IsActive:  input.IsActive,
	}, nil
}

func normalizeUpdateFAQItemInput(input UpdateFAQItemInput) (UpdateFAQItemParams, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return UpdateFAQItemParams{}, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "id"})
	}

	question, answer, err := normalizeFAQText(input.Question, input.Answer)
	if err != nil {
		return UpdateFAQItemParams{}, err
	}

	return UpdateFAQItemParams{
		ID:        id,
		Question:  question,
		Answer:    answer,
		SortOrder: input.SortOrder,
		IsActive:  input.IsActive,
		Version:   input.Version,
	}, nil
}

func normalizeFAQText(questionValue string, answerValue string) (string, string, error) {
	question := strings.TrimSpace(questionValue)
	if question == "" {
		return "", "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "question"})
	}

	answer := strings.TrimSpace(answerValue)
	if answer == "" {
		return "", "", apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "answer"})
	}

	return question, answer, nil
}
