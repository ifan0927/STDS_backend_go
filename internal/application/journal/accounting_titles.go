package journal

import (
	"context"
)

// ListExpenseAccountingTitlesService lists active expense accounting titles for journal expenses.
type ListExpenseAccountingTitlesService struct {
	repo Repository
}

// NewListExpenseAccountingTitlesService returns a ListExpenseAccountingTitlesService.
func NewListExpenseAccountingTitlesService(repo Repository) *ListExpenseAccountingTitlesService {
	return &ListExpenseAccountingTitlesService{repo: repo}
}

// Execute returns active expense accounting title options.
func (s *ListExpenseAccountingTitlesService) Execute(ctx context.Context) ([]AccountingTitle, error) {
	if s.repo == nil {
		return nil, nil
	}

	titles, err := s.repo.ListExpenseAccountingTitles(ctx)
	if err != nil {
		return nil, mapRepositoryError(err)
	}

	return titles, nil
}
