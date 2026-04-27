package journal

import "context"

// GetInput is the query payload for journal log detail retrieval.
type GetInput struct {
	ID string
}

// GetService returns one journal log.
type GetService struct {
	repo Repository
}

// NewGetService returns a GetService.
func NewGetService(repo Repository) *GetService {
	return &GetService{repo: repo}
}

// Execute returns one active journal log.
func (s *GetService) Execute(ctx context.Context, input GetInput) (*JournalLog, error) {
	id, err := normalizeRequiredUUID(input.ID, "id", ErrJournalLogNotFound)
	if err != nil {
		return nil, err
	}

	item, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, mapRepositoryError(err)
	}

	return item, nil
}
