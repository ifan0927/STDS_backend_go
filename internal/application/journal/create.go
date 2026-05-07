package journal

import (
	"context"
	"database/sql"

	domainevents "stds_backend/internal/domain/events"
	domainjournal "stds_backend/internal/domain/journal"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const (
	accountingCategoryJournalExpense  = "journal_expense"
	accountingTitleCodeJournalExpense = "6681"
)

// CreateInput is the command payload for journal log creation.
type CreateInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
	PropertyID          string
	RoomID              *string
	Content             string
	ExpenseAmount       *int
	ExpenseDescription  *string
}

// CreateService creates journal logs.
type CreateService struct {
	repo           Repository
	accountingRepo ExpenseAccountingRepository
	txRunner       TransactionRunner
}

// NewCreateService returns a CreateService.
func NewCreateService(repo Repository, accountingRepo ExpenseAccountingRepository, txRunner TransactionRunner) *CreateService {
	return &CreateService{repo: repo, accountingRepo: accountingRepo, txRunner: txRunner}
}

// Execute creates a journal log.
func (s *CreateService) Execute(ctx context.Context, input CreateInput) (*JournalLog, error) {
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
	roomID, err := normalizeOptionalUUID(input.RoomID, "room_id")
	if err != nil {
		return nil, err
	}
	if err := requirePropertyAccess(actorRole, input.AssignedPropertyIDs, propertyID); err != nil {
		return nil, err
	}
	aggregate, err := domainjournal.New(domainjournal.State{
		PropertyID:         propertyID,
		RoomID:             roomID,
		AuthorID:           actorUserID,
		Content:            input.Content,
		ExpenseAmount:      input.ExpenseAmount,
		ExpenseDescription: input.ExpenseDescription,
	})
	if err != nil {
		return nil, mapDomainError(err)
	}
	state := aggregate.State()
	if s.txRunner == nil {
		return nil, apperr.ErrInternalServerError
	}
	if state.ExpenseAmount != nil && s.accountingRepo == nil {
		return nil, apperr.ErrInternalServerError.WithDetails(map[string]interface{}{"dependency": "journal_accounting"})
	}

	var created *JournalLog
	err = s.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, recorder *txrunner.EventRecorder) error {
		if err := s.repo.EnsurePropertyExists(ctx, tx, propertyID); err != nil {
			return mapRepositoryError(err)
		}
		if roomID != nil {
			room, err := s.repo.FindRoomByID(ctx, tx, *roomID)
			if err != nil {
				return mapRepositoryError(err)
			}
			if room.PropertyID != propertyID {
				return apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "room_id"})
			}
		}

		journalLog, err := s.repo.Create(ctx, tx, CreateParams{
			PropertyID:         state.PropertyID,
			RoomID:             state.RoomID,
			AuthorID:           state.AuthorID,
			Content:            state.Content,
			ExpenseAmount:      state.ExpenseAmount,
			ExpenseDescription: state.ExpenseDescription,
		})
		if err != nil {
			return mapRepositoryError(err)
		}

		if journalLog.ExpenseAmount != nil {
			year, month := journalAccountingPeriod(journalLog.CreatedAt)
			if err := s.accountingRepo.CreateExpenseAccountingEntry(ctx, tx, ExpenseAccountingEntryParams{
				PropertyID:          journalLog.PropertyID,
				Category:            accountingCategoryJournalExpense,
				AccountingTitleCode: accountingTitleCodeJournalExpense,
				Amount:              *journalLog.ExpenseAmount,
				Description:         journalLog.ExpenseDescription,
				SourceRef:           map[string]interface{}{"type": "JournalExpenseRecorded", "journal_log_id": journalLog.ID},
				Year:                year,
				Month:               month,
			}); err != nil {
				return mapAccountingRepositoryError(err)
			}

			occurredAt := journalLog.CreatedAt.UTC()
			recorder.Record(domainevents.JournalExpenseRecorded{
				JournalLogID: journalLog.ID,
				PropertyID:   journalLog.PropertyID,
				Amount:       *journalLog.ExpenseAmount,
				Description:  journalLog.ExpenseDescription,
				OccurredAt:   occurredAt,
			})
		}

		created = journalLog
		return nil
	})
	if err != nil {
		return nil, err
	}

	return created, nil
}
