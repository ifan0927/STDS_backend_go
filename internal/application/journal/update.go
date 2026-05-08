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
	ID                       string
	Content                  *string
	ExpenseAmount            *int
	ExpenseDescription       *string
	ExpenseAccountingTitleID *string
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
	if input.Content == nil && input.ExpenseAmount == nil && input.ExpenseDescription == nil && input.ExpenseAccountingTitleID == nil {
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
		var expenseTitle *AccountingTitle
		if journalExpenseAmountPresent(state.ExpenseAmount) {
			expenseTitle, err = (&CreateService{repo: s.repo}).resolveExpenseAccountingTitle(ctx, tx, input.ExpenseAccountingTitleID, current)
			if err != nil {
				return err
			}
		} else if input.ExpenseAccountingTitleID != nil {
			return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "expense_accounting_title_id"})
		}
		accountingChanged := journalExpenseAccountingChanges(current, state.ExpenseAmount, state.ExpenseDescription, expenseTitle)
		if accountingChanged {
			if s.accountingRepo == nil {
				return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting"})
			}
			year, month := journalAccountingPeriod(current.CreatedAt)
			exists, err := s.accountingRepo.JournalExpenseSnapshotExists(ctx, tx, current.PropertyID, year, month)
			if err != nil {
				return mapAccountingRepositoryError(err)
			}
			if exists {
				return ErrJournalExpenseSnapshotFinalized
			}
		}
		journalLog, err := s.repo.Update(ctx, tx, UpdateParams{
			ID:                         id,
			Content:                    state.Content,
			ExpenseAmount:              state.ExpenseAmount,
			ExpenseDescription:         state.ExpenseDescription,
			ExpenseAccountingTitleID:   accountingTitleID(expenseTitle),
			ExpenseAccountingTitleCode: accountingTitleCode(expenseTitle),
			ExpenseAccountingTitleName: accountingTitleName(expenseTitle),
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		if accountingChanged {
			if s.accountingRepo == nil {
				return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting"})
			}
			var entry *ExpenseAccountingEntryParams
			if journalLog.ExpenseAmount != nil {
				if expenseTitle == nil {
					return apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting_title"})
				}
				year, month := journalAccountingPeriod(journalLog.CreatedAt)
				sourceDate := journalLog.CreatedAt.In(journalAccountingLocation)
				entry = &ExpenseAccountingEntryParams{
					PropertyID:          journalLog.PropertyID,
					Category:            accountingCategoryJournalExpense,
					AccountingTitleCode: expenseTitle.Code,
					Amount:              *journalLog.ExpenseAmount,
					Description:         journalLog.ExpenseDescription,
					SourceRef:           map[string]interface{}{"type": "JournalExpenseRecorded", "journal_log_id": journalLog.ID},
					Year:                year,
					Month:               month,
					SourceDate:          &sourceDate,
					DisplayNote:         journalLog.ExpenseDescription,
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

func journalExpenseAccountingChanges(current *JournalLog, nextAmount *int, nextDescription *string, nextTitle *AccountingTitle) bool {
	if current == nil {
		return nextAmount != nil
	}
	if !journalIntPtrEqual(current.ExpenseAmount, nextAmount) {
		return true
	}
	if !journalStringPtrEqual(current.ExpenseDescription, nextDescription) {
		return true
	}
	if current.ExpenseAmount == nil && nextAmount == nil {
		return false
	}
	currentTitleID := ""
	if current.ExpenseAccountingTitleID != nil {
		currentTitleID = *current.ExpenseAccountingTitleID
	}
	nextTitleID := ""
	if nextTitle != nil {
		nextTitleID = nextTitle.ID
	}
	return currentTitleID != nextTitleID
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

func journalIntPtrEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func journalStringPtrEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
