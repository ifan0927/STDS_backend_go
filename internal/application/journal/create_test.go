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

func TestCreateServicePublishesJournalExpenseRecordedWhenExpensePresent(t *testing.T) {
	amount := 3500
	description := "Pipe repair"
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
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
	}
	runner, publisher, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewCreateService(repo, runner)

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
}

func TestCreateServiceDoesNotPublishExpenseEventWithoutExpense(t *testing.T) {
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
	runner, publisher, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewCreateService(repo, runner)

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
}

func TestCreateServiceRejectsEmptyContent(t *testing.T) {
	service := NewCreateService(&journalRepositoryStub{}, nil)

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
	service := NewCreateService(&journalRepositoryStub{}, nil)

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
	service := NewCreateService(repo, runner)

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
	service := NewCreateService(&journalRepositoryStub{}, runner)

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
	service := NewListService(&journalRepositoryStub{})

	t.Run("invalid property id", func(t *testing.T) {
		value := "not-a-uuid"
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole:  "admin",
			PropertyID: &value,
			Limit:      20,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole: "admin",
			Limit:     0,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
		}
	})

	t.Run("invalid offset", func(t *testing.T) {
		_, err := service.Execute(context.Background(), ListInput{
			ActorRole: "admin",
			Limit:     20,
			Offset:    -1,
		})
		if !errors.Is(err, apperr.ErrBadRequest) {
			t.Fatalf("Execute() error = %v, want bad request", err)
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
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		},
	}
	runner, _, cleanup := newJournalTxRunner(t)
	defer cleanup()
	service := NewUpdateService(repo, runner)
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
	service := NewUpdateService(repo, runner)
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
}

func (s *journalRepositoryStub) List(context.Context, ListQuery) ([]JournalLog, error) {
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
