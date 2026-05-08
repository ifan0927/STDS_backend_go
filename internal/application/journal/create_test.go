package journal

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const (
	testJournalID  = "60000000-0000-0000-0000-000000000001"
	testPropertyID = "10000000-0000-0000-0000-000000000001"
	testRoomID     = "20000000-0000-0000-0000-000000000001"
	testActorID    = "00000000-0000-0000-0000-000000000002"
)

func TestCreateServiceCreatesAccountingEntryAndPublishesEventWhenExpensePresent(t *testing.T) {
	amount := 3500
	description := "Pipe repair"
	createdAt := time.Date(2026, 4, 30, 16, 30, 0, 0, time.UTC)
	repo := &journalRepositoryStub{
		propertyExists: true,
		room:           &Room{ID: testRoomID, PropertyID: testPropertyID},
		created: &JournalLog{
			ID:                 testJournalID,
			PropertyID:         testPropertyID,
			RoomID:             stringPtr(testRoomID),
			AuthorID:           testActorID,
			Content:            "Bathroom repair",
			ExpenseAmount:      &amount,
			ExpenseDescription: &description,
			CreatedAt:          createdAt,
			UpdatedAt:          createdAt,
		},
	}
	accountingRepo := &expenseAccountingRepositoryStub{}
	runner, publisher, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewCreateService(repo, accountingRepo, runner)

	created, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "organizer",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		RoomID:              stringPtr(testRoomID),
		Content:             " Bathroom repair ",
		ExpenseAmount:       &amount,
		ExpenseDescription:  &description,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if created.ID != testJournalID {
		t.Fatalf("created.ID = %s, want %s", created.ID, testJournalID)
	}
	if accountingRepo.createCalls != 1 {
		t.Fatalf("CreateExpenseAccountingEntry calls = %d, want 1", accountingRepo.createCalls)
	}
	if accountingRepo.entry.PropertyID != testPropertyID || accountingRepo.entry.Category != accountingCategoryJournalExpense || accountingRepo.entry.AccountingTitleCode != accountingTitleCodeJournalExpense || accountingRepo.entry.Amount != amount {
		t.Fatalf("accounting entry = %+v", accountingRepo.entry)
	}
	if accountingRepo.entry.Description == nil || *accountingRepo.entry.Description != description {
		t.Fatalf("accounting description = %v, want %s", accountingRepo.entry.Description, description)
	}
	if accountingRepo.entry.Year != 2026 || accountingRepo.entry.Month != 5 {
		t.Fatalf("accounting Year/Month = %d/%d, want 2026/5", accountingRepo.entry.Year, accountingRepo.entry.Month)
	}
	if accountingRepo.entry.SourceRef["type"] != "JournalExpenseRecorded" || accountingRepo.entry.SourceRef["journal_log_id"] != testJournalID {
		t.Fatalf("accounting SourceRef = %#v", accountingRepo.entry.SourceRef)
	}
	wantSourceDate := createdAt.In(journalAccountingLocation)
	if accountingRepo.entry.SourceDate == nil || !accountingRepo.entry.SourceDate.Equal(wantSourceDate) {
		t.Fatalf("accounting SourceDate = %v, want %s", accountingRepo.entry.SourceDate, wantSourceDate)
	}
	if accountingRepo.entry.DisplayNote == nil || *accountingRepo.entry.DisplayNote != description {
		t.Fatalf("accounting DisplayNote = %v, want %s", accountingRepo.entry.DisplayNote, description)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.JournalExpenseRecorded)
	if !ok {
		t.Fatalf("event type = %T, want JournalExpenseRecorded", publisher.events[0])
	}
	if event.JournalLogID != testJournalID || event.PropertyID != testPropertyID || event.Amount != amount {
		t.Fatalf("event = %+v", event)
	}
	if !event.OccurredAt.Equal(createdAt.UTC()) {
		t.Fatalf("event.OccurredAt = %s, want %s", event.OccurredAt, createdAt.UTC())
	}
}

func TestCreateServiceDoesNotCreateAccountingEntryOrPublishExpenseEventWithoutExpense(t *testing.T) {
	repo := &journalRepositoryStub{
		propertyExists: true,
		created: &JournalLog{
			ID:         testJournalID,
			PropertyID: testPropertyID,
			AuthorID:   testActorID,
			Content:    "Inspection",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}
	accountingRepo := &expenseAccountingRepositoryStub{}
	runner, publisher, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewCreateService(repo, accountingRepo, runner)

	if _, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Content:             "Inspection",
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events = %d, want 0", len(publisher.events))
	}
	if accountingRepo.createCalls != 0 {
		t.Fatalf("CreateExpenseAccountingEntry calls = %d, want 0", accountingRepo.createCalls)
	}
}

func TestCreateServiceRollsBackWhenAccountingEntryFails(t *testing.T) {
	amount := 3500
	repo := &journalRepositoryStub{
		propertyExists: true,
		created: &JournalLog{
			ID:            testJournalID,
			PropertyID:    testPropertyID,
			AuthorID:      testActorID,
			Content:       "Bathroom repair",
			ExpenseAmount: &amount,
			CreatedAt:     time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC),
		},
	}
	accountingRepo := &expenseAccountingRepositoryStub{err: errors.New("insert accounting entry")}
	runner, publisher, cleanup := newJournalTxRunnerExpectRollback(t)
	defer cleanup()
	service := NewCreateService(repo, accountingRepo, runner)

	_, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Content:             "Bathroom repair",
		ExpenseAmount:       &amount,
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if accountingRepo.createCalls != 1 {
		t.Fatalf("CreateExpenseAccountingEntry calls = %d, want 1", accountingRepo.createCalls)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events = %d, want 0", len(publisher.events))
	}
}

func TestCreateServiceRejectsEmptyContent(t *testing.T) {
	service := NewCreateService(&journalRepositoryStub{}, nil, nil)

	_, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "admin",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Content:             " ",
	})
	if !errors.Is(err, ErrValidationJournalContentRequired) {
		t.Fatalf("Execute() error = %v, want ErrValidationJournalContentRequired", err)
	}
}

func TestCreateServiceRejectsUnassignedProperty(t *testing.T) {
	service := NewCreateService(&journalRepositoryStub{}, nil, nil)

	_, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "staff",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000099"},
		PropertyID:          testPropertyID,
		Content:             "Inspection",
	})
	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("Execute() error = %v, want forbidden", err)
	}
}

func TestCreateServiceRejectsRoomFromDifferentProperty(t *testing.T) {
	repo := &journalRepositoryStub{
		propertyExists: true,
		room:           &Room{ID: testRoomID, PropertyID: "10000000-0000-0000-0000-000000000099"},
	}
	runner, _, cleanup := newJournalTxRunnerExpectRollback(t)
	defer cleanup()
	service := NewCreateService(repo, &expenseAccountingRepositoryStub{}, runner)

	_, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "admin",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		RoomID:              stringPtr(testRoomID),
		Content:             "Inspection",
	})
	if !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("Execute() error = %v, want bad request", err)
	}
}

func TestCreateServiceMapsMissingPropertyToNotFound(t *testing.T) {
	runner, _, cleanup := newJournalTxRunnerExpectRollback(t)
	defer cleanup()
	service := NewCreateService(&journalRepositoryStub{}, &expenseAccountingRepositoryStub{}, runner)

	_, err := service.Execute(context.Background(), CreateInput{
		ActorRole:           "admin",
		ActorUserID:         testActorID,
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Content:             "Inspection",
	})
	if !errors.Is(err, apperr.ErrPropertyNotFound) {
		t.Fatalf("Execute() error = %v, want property not found", err)
	}
}

func TestListServiceRejectsInvalidInputBeforeRepository(t *testing.T) {
	t.Run("invalid property id", func(t *testing.T) {
		repo := &journalRepositoryStub{}
		service := NewListService(repo)
		value := "not-a-uuid"
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole:  "admin",
			PropertyID: &value,
			Limit:      20,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
		}
		if repo.listCalls != 0 {
			t.Fatalf("List calls = %d, want 0", repo.listCalls)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		repo := &journalRepositoryStub{}
		service := NewListService(repo)
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole: "admin",
			Limit:     0,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
		}
		if repo.listCalls != 0 {
			t.Fatalf("List calls = %d, want 0", repo.listCalls)
		}
	})

	t.Run("invalid offset", func(t *testing.T) {
		repo := &journalRepositoryStub{}
		service := NewListService(repo)
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole: "admin",
			Limit:     20,
			Offset:    -1,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
		}
		if repo.listCalls != 0 {
			t.Fatalf("List calls = %d, want 0", repo.listCalls)
		}
	})
}

func TestUpdateServiceUpdatesMutableFields(t *testing.T) {
	amount := 4200
	description := "Fixture replacement"
	repo := &journalRepositoryStub{
		current: &JournalLog{
			ID:                 testJournalID,
			PropertyID:         testPropertyID,
			AuthorID:           testActorID,
			Content:            "Original",
			ExpenseDescription: stringPtr("Old"),
			CreatedAt:          time.Date(2026, 4, 30, 16, 30, 0, 0, time.UTC),
			UpdatedAt:          time.Now(),
		},
	}
	runner, _, cleanup := newJournalTxRunner(t)
	defer cleanup()
	accountingRepo := &expenseAccountingRepositoryStub{}
	service := NewUpdateService(repo, accountingRepo, runner)
	content := " Updated journal "

	updated, err := service.Execute(context.Background(), UpdateInput{
		ID:                 testJournalID,
		Content:            &content,
		ExpenseAmount:      &amount,
		ExpenseDescription: &description,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if updated.Content != "Updated journal" {
		t.Fatalf("Content = %q, want trimmed content", updated.Content)
	}
	if updated.ExpenseAmount == nil || *updated.ExpenseAmount != amount {
		t.Fatalf("ExpenseAmount = %v, want %d", updated.ExpenseAmount, amount)
	}
	if updated.ExpenseDescription == nil || *updated.ExpenseDescription != description {
		t.Fatalf("ExpenseDescription = %v, want %s", updated.ExpenseDescription, description)
	}
	if accountingRepo.syncCalls != 1 {
		t.Fatalf("SyncExpenseAccountingEntry calls = %d, want 1", accountingRepo.syncCalls)
	}
	if accountingRepo.syncedJournalLogID != testJournalID {
		t.Fatalf("syncedJournalLogID = %s, want %s", accountingRepo.syncedJournalLogID, testJournalID)
	}
	if accountingRepo.syncedEntry == nil || accountingRepo.syncedEntry.Amount != amount {
		t.Fatalf("syncedEntry = %+v, want amount %d", accountingRepo.syncedEntry, amount)
	}
	if accountingRepo.syncedEntry.AccountingTitleCode != accountingTitleCodeJournalExpense {
		t.Fatalf("syncedEntry.AccountingTitleCode = %q, want %q", accountingRepo.syncedEntry.AccountingTitleCode, accountingTitleCodeJournalExpense)
	}
	if accountingRepo.syncedEntry.Year != 2026 || accountingRepo.syncedEntry.Month != 5 {
		t.Fatalf("syncedEntry Year/Month = %d/%d, want 2026/5", accountingRepo.syncedEntry.Year, accountingRepo.syncedEntry.Month)
	}
	wantSourceDate := repo.current.CreatedAt.In(journalAccountingLocation)
	if accountingRepo.syncedEntry.SourceDate == nil || !accountingRepo.syncedEntry.SourceDate.Equal(wantSourceDate) {
		t.Fatalf("syncedEntry SourceDate = %v, want %s", accountingRepo.syncedEntry.SourceDate, wantSourceDate)
	}
	if accountingRepo.syncedEntry.DisplayNote == nil || *accountingRepo.syncedEntry.DisplayNote != description {
		t.Fatalf("syncedEntry DisplayNote = %v, want %s", accountingRepo.syncedEntry.DisplayNote, description)
	}
}

func TestUpdateServiceRejectsEmptyContent(t *testing.T) {
	repo := &journalRepositoryStub{
		current: &JournalLog{
			ID:         testJournalID,
			PropertyID: testPropertyID,
			AuthorID:   testActorID,
			Content:    "Original",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}
	runner, _, cleanup := newJournalTxRunnerExpectRollback(t)
	defer cleanup()
	service := NewUpdateService(repo, &expenseAccountingRepositoryStub{}, runner)
	content := " "

	_, err := service.Execute(context.Background(), UpdateInput{
		ID:      testJournalID,
		Content: &content,
	})
	if !errors.Is(err, ErrValidationJournalContentRequired) {
		t.Fatalf("Execute() error = %v, want ErrValidationJournalContentRequired", err)
	}
}

func TestDeleteServiceSoftDeletesJournalLog(t *testing.T) {
	repo := &journalRepositoryStub{}
	runner, _, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewDeleteService(repo, runner)

	if err := service.Execute(context.Background(), DeleteInput{ID: testJournalID}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if repo.deletedID != testJournalID {
		t.Fatalf("deletedID = %s, want %s", repo.deletedID, testJournalID)
	}
}

type journalPublisherStub struct {
	events []any
}

func (s *journalPublisherStub) Publish(_ context.Context, event any) error {
	s.events = append(s.events, event)
	return nil
}

type expenseAccountingRepositoryStub struct {
	entry              ExpenseAccountingEntryParams
	syncedEntry        *ExpenseAccountingEntryParams
	syncedJournalLogID string
	err                error
	createCalls        int
	syncCalls          int
}

func (s *expenseAccountingRepositoryStub) CreateExpenseAccountingEntry(_ context.Context, _ *sql.Tx, params ExpenseAccountingEntryParams) error {
	s.createCalls++
	s.entry = params
	return s.err
}

func (s *expenseAccountingRepositoryStub) SyncExpenseAccountingEntry(_ context.Context, _ *sql.Tx, journalLogID string, params *ExpenseAccountingEntryParams) error {
	s.syncCalls++
	s.syncedJournalLogID = journalLogID
	s.syncedEntry = params
	return s.err
}

func newJournalTxRunner(t *testing.T) (*txrunner.Runner, *journalPublisherStub, func()) {
	t.Helper()

	return newJournalTxRunnerWithExpectation(t, true)
}

func newJournalTxRunnerExpectRollback(t *testing.T) (*txrunner.Runner, *journalPublisherStub, func()) {
	t.Helper()

	return newJournalTxRunnerWithExpectation(t, false)
}

func newJournalTxRunnerWithExpectation(t *testing.T, commit bool) (*txrunner.Runner, *journalPublisherStub, func()) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	mock.ExpectBegin()
	if commit {
		mock.ExpectCommit()
	} else {
		mock.ExpectRollback()
	}
	publisher := &journalPublisherStub{}
	cleanup := func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
		_ = db.Close()
	}

	return txrunner.New(db, publisher), publisher, cleanup
}

type journalRepositoryStub struct {
	propertyExists bool
	room           *Room
	created        *JournalLog
	current        *JournalLog
	deletedID      string
	listCalls      int
}

func (s *journalRepositoryStub) List(context.Context, ListQuery) ([]JournalLog, error) {
	s.listCalls++
	return nil, nil
}

func (s *journalRepositoryStub) FindByID(context.Context, string) (*JournalLog, error) {
	return nil, nil
}

func (s *journalRepositoryStub) FindByIDForUpdate(context.Context, *sql.Tx, string) (*JournalLog, error) {
	if s.current == nil {
		return nil, ErrJournalLogNotFound
	}

	return s.current, nil
}

func (s *journalRepositoryStub) EnsurePropertyExists(context.Context, *sql.Tx, string) error {
	if !s.propertyExists {
		return ErrPropertyNotFound
	}

	return nil
}

func (s *journalRepositoryStub) FindRoomByID(context.Context, *sql.Tx, string) (*Room, error) {
	if s.room == nil {
		return nil, ErrRoomNotFound
	}

	return s.room, nil
}

func (s *journalRepositoryStub) Create(_ context.Context, _ *sql.Tx, params CreateParams) (*JournalLog, error) {
	if s.created != nil {
		return s.created, nil
	}

	return &JournalLog{
		ID:                 testJournalID,
		PropertyID:         params.PropertyID,
		RoomID:             params.RoomID,
		AuthorID:           params.AuthorID,
		Content:            params.Content,
		ExpenseAmount:      params.ExpenseAmount,
		ExpenseDescription: params.ExpenseDescription,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}, nil
}

func (s *journalRepositoryStub) Update(_ context.Context, _ *sql.Tx, params UpdateParams) (*JournalLog, error) {
	if s.current == nil {
		return nil, ErrJournalLogNotFound
	}

	return &JournalLog{
		ID:                 params.ID,
		PropertyID:         s.current.PropertyID,
		RoomID:             s.current.RoomID,
		AuthorID:           s.current.AuthorID,
		Content:            params.Content,
		ExpenseAmount:      params.ExpenseAmount,
		ExpenseDescription: params.ExpenseDescription,
		CreatedAt:          s.current.CreatedAt,
		UpdatedAt:          time.Now(),
	}, nil
}

func (s *journalRepositoryStub) SoftDelete(_ context.Context, _ *sql.Tx, id string) error {
	s.deletedID = id
	return nil
}

func stringPtr(value string) *string {
	return &value
}
