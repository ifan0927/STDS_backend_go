package journal

import (
	"context"
	"database/sql"

	domainjournal "stds_backend/internal/domain/journal"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

// UpdateInput is the command payload for journal log updates.
type UpdateInput struct {
	ID                 string
	Content            *string
	ExpenseAmount      *int
	ExpenseDescription *string
}

// UpdateService updates journal logs.
type UpdateService struct {
	repo           Repository
	accountingRepo ExpenseAccountingRepository
	txRunner       TransactionRunner
}

// NewUpdateService returns an UpdateService.
func NewUpdateService(repo Repository, accountingRepo ExpenseAccountingRepository, txRunner TransactionRunner) *UpdateService {
	return &UpdateService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute updates mutable journal log fields.
func (s *UpdateService) Execute(ctx context.Context, input UpdateInput) (*JournalLog, error) {
	id, err := normalizeRequiredUUID(input.ID, "id", ErrJournalLogNotFound)
	if err != nil {
		return nil, err
	}
	if input.Content == nil && input.ExpenseAmount == nil && input.ExpenseDescription == nil {
		return nil, apperr.ErrBadRequest
	}
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}

	var updated *JournalLog
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		current, err := s.repo.FindByIDForUpdate(ctx, tx, id)
		if err != nil {
			return mapRepositoryError(err)
		}
		aggregate, err := domainjournal.Rehydrate(toDomainState(current))
		if err != nil {
			return mapDomainError(err)
		}
		if err := aggregate.Update(domainjournal.UpdateInput{
			Content:            input.Content,
			ExpenseAmount:      input.ExpenseAmount,
			ExpenseDescription: input.ExpenseDescription,
		}); err != nil {
			return mapDomainError(err)
		}

		state := aggregate.State()
		journalLog, err := s.repo.Update(ctx, tx, UpdateParams{
			ID:                 id,
			Content:            state.Content,
			ExpenseAmount:      state.ExpenseAmount,
			ExpenseDescription: state.ExpenseDescription,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		if current.ExpenseAmount != nil || journalLog.ExpenseAmount != nil {
			if s.accountingRepo == nil {
				return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting"})
			}
			var entry *ExpenseAccountingEntryParams
			if journalLog.ExpenseAmount != nil {
				year, month := journalAccountingPeriod(journalLog.CreatedAt)
				entry = &ExpenseAccountingEntryParams{
					PropertyID:  journalLog.PropertyID,
					Category:    accountingCategoryJournalExpense,
					Amount:      *journalLog.ExpenseAmount,
					Description: journalLog.ExpenseDescription,
					SourceRef:   map[string]interface{}{"type": "JournalExpenseRecorded", "journal_log_id": journalLog.ID},
					Year:        year,
					Month:       month,
				}
			}
			if err := s.accountingRepo.SyncExpenseAccountingEntry(ctx, tx, journalLog.ID, entry); err != nil {
				return mapAccountingRepositoryError(err)
			}
		}

		updated = journalLog
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func toDomainState(journalLog *JournalLog) domainjournal.State {
	if journalLog == nil {
		return domainjournal.State{}
	}

	return domainjournal.State{
		ID:                 journalLog.ID,
		PropertyID:         journalLog.PropertyID,
		RoomID:             journalLog.RoomID,
		AuthorID:           journalLog.AuthorID,
		Content:            journalLog.Content,
		ExpenseAmount:      journalLog.ExpenseAmount,
		ExpenseDescription: journalLog.ExpenseDescription,
	}
}
