package repair

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

func TestAssignRejectsStaffAssigningAnotherUser(t *testing.T) {
	service := NewWorkflowService(&workflowRepoStub{}, fakeTxRunner{})

	_, err := service.Assign(context.Background(), AssignInput{
		ActorRole:   "staff",
		ActorUserID: testStaffID,
		ID:          testRepairID,
		AssignedTo:  testOrganizerID,
	})

	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestAssignAllowsOrganizerAssigneeAndPersistsTransition(t *testing.T) {
	repo := &workflowRepoStub{
		user:          &User{ID: testOrganizerID, Role: "organizer", AssignedPropertyIDs: []string{testPropertyID}},
		repairRequest: submittedRepairRequest(),
	}
	service := NewWorkflowService(repo, fakeTxRunner{})
	service.now = func() time.Time { return testNow }

	repairRequest, err := service.Assign(context.Background(), AssignInput{
		ActorRole:   "organizer",
		ActorUserID: testOrganizerID,
		ID:          testRepairID,
		AssignedTo:  testOrganizerID,
	})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}

	if repo.assignParams.AssignedTo != testOrganizerID {
		t.Fatalf("expected assigned_to %s, got %s", testOrganizerID, repo.assignParams.AssignedTo)
	}
	if !repo.assignParams.AssignedAt.Equal(testNow) {
		t.Fatalf("expected assigned_at %s, got %s", testNow, repo.assignParams.AssignedAt)
	}
	if repairRequest.Status != "assigned" {
		t.Fatalf("expected assigned status, got %s", repairRequest.Status)
	}
}

func TestAssignRejectsAssigneeOutsideRepairProperty(t *testing.T) {
	repo := &workflowRepoStub{
		user:          &User{ID: testOrganizerID, Role: "organizer", AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000099"}},
		repairRequest: submittedRepairRequest(),
	}
	service := NewWorkflowService(repo, fakeTxRunner{})

	_, err := service.Assign(context.Background(), AssignInput{
		ActorRole:   "organizer",
		ActorUserID: testOrganizerID,
		ID:          testRepairID,
		AssignedTo:  testOrganizerID,
	})

	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestAssignRejectsOwnerAssignee(t *testing.T) {
	repo := &workflowRepoStub{
		user:          &User{ID: testOwnerID, Role: "owner"},
		repairRequest: submittedRepairRequest(),
	}
	service := NewWorkflowService(repo, fakeTxRunner{})

	_, err := service.Assign(context.Background(), AssignInput{
		ActorRole:   "organizer",
		ActorUserID: testOrganizerID,
		ID:          testRepairID,
		AssignedTo:  testOwnerID,
	})

	if !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestProgressRejectsNonAssignedRepair(t *testing.T) {
	repo := &workflowRepoStub{repairRequest: submittedRepairRequest()}
	service := NewWorkflowService(repo, fakeTxRunner{})

	_, err := service.Progress(context.Background(), testRepairID)

	if !errors.Is(err, ErrInvalidStatusForProgress) {
		t.Fatalf("expected invalid progress status, got %v", err)
	}
}

func TestCancelPersistsReason(t *testing.T) {
	reason := "owner deferred"
	repo := &workflowRepoStub{repairRequest: submittedRepairRequest()}
	service := NewWorkflowService(repo, fakeTxRunner{})

	repairRequest, err := service.Cancel(context.Background(), CancelInput{
		ID:     testRepairID,
		Reason: &reason,
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if repo.cancelParams.CancelReason == nil || *repo.cancelParams.CancelReason != reason {
		t.Fatalf("expected cancel reason %q, got %#v", reason, repo.cancelParams.CancelReason)
	}
	if repairRequest.Status != "cancelled" {
		t.Fatalf("expected cancelled status, got %s", repairRequest.Status)
	}
}

func TestCancelCompletedRepairReturnsAlreadyCompleted(t *testing.T) {
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "completed"
	repo := &workflowRepoStub{repairRequest: repairRequest}
	service := NewWorkflowService(repo, fakeTxRunner{})

	_, err := service.Cancel(context.Background(), CancelInput{ID: testRepairID})

	if !errors.Is(err, ErrRepairAlreadyCompleted) {
		t.Fatalf("expected already completed, got %v", err)
	}
}

func TestCancelCancelledRepairReturnsInvalidStatus(t *testing.T) {
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "cancelled"
	repo := &workflowRepoStub{repairRequest: repairRequest}
	service := NewWorkflowService(repo, fakeTxRunner{})

	_, err := service.Cancel(context.Background(), CancelInput{ID: testRepairID})

	if !errors.Is(err, ErrInvalidStatusForCancel) {
		t.Fatalf("expected invalid cancel status, got %v", err)
	}
}

func TestCompletePublishesRepairCompletedAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repairRequest := submittedRepairRequest()
	repairRequest.Status = "in_progress"
	repo := &workflowRepoStub{repairRequest: repairRequest}
	publisher := &repairRecordingPublisher{}
	service := NewWorkflowService(repo, txrunner.New(db, publisher))
	service.now = func() time.Time { return testNow }

	_, err = service.Complete(context.Background(), testRepairID)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.RepairCompleted)
	if !ok {
		t.Fatalf("expected RepairCompleted, got %T", publisher.events[0])
	}
	if event.RepairRequestID != testRepairID || event.RoomID != testRoomID || event.PropertyID != testPropertyID {
		t.Fatalf("unexpected event: %+v", event)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestCancelPublishesRepairCancelledAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &workflowRepoStub{repairRequest: submittedRepairRequest()}
	publisher := &repairRecordingPublisher{}
	service := NewWorkflowService(repo, txrunner.New(db, publisher))
	service.now = func() time.Time { return testNow }

	_, err = service.Cancel(context.Background(), CancelInput{ID: testRepairID})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.RepairCancelled)
	if !ok {
		t.Fatalf("expected RepairCancelled, got %T", publisher.events[0])
	}
	if event.RepairRequestID != testRepairID || event.RoomID != testRoomID || event.PropertyID != testPropertyID {
		t.Fatalf("unexpected event: %+v", event)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

const (
	testRepairID    = "70000000-0000-0000-0000-000000000001"
	testPropertyID  = "10000000-0000-0000-0000-000000000001"
	testRoomID      = "20000000-0000-0000-0000-000000000001"
	testStaffID     = "00000000-0000-0000-0000-000000000002"
	testOrganizerID = "00000000-0000-0000-0000-000000000003"
	testOwnerID     = "00000000-0000-0000-0000-000000000004"
)

var testNow = time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)

type fakeTxRunner struct{}

func (fakeTxRunner) WithinTransaction(ctx context.Context, fn func(context.Context, *sql.Tx, *txrunner.EventRecorder) error) error {
	return fn(ctx, nil, &txrunner.EventRecorder{})
}

type repairRecordingPublisher struct {
	events []any
}

func (p *repairRecordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}

type workflowRepoStub struct {
	user          *User
	repairRequest *RepairRequest
	assignParams  AssignParams
	cancelParams  CancelParams
}

func (r *workflowRepoStub) List(context.Context, ListQuery) ([]RepairRequest, error) {
	return nil, nil
}

func (r *workflowRepoStub) FindByID(context.Context, string) (*RepairRequest, error) {
	return r.repairRequest, nil
}

func (r *workflowRepoStub) FindByIDForUpdate(context.Context, *sql.Tx, string) (*RepairRequest, error) {
	if r.repairRequest == nil {
		return nil, ErrRepairRequestNotFound
	}
	cloned := *r.repairRequest
	return &cloned, nil
}

func (r *workflowRepoStub) FindRoomByID(context.Context, *sql.Tx, string) (*Room, error) {
	return &Room{ID: testRoomID, PropertyID: testPropertyID}, nil
}

func (r *workflowRepoStub) FindUserByID(context.Context, *sql.Tx, string) (*User, error) {
	if r.user == nil {
		return nil, ErrUserNotFound
	}
	return r.user, nil
}

func (r *workflowRepoStub) Create(context.Context, *sql.Tx, CreateParams) (*RepairRequest, error) {
	return nil, nil
}

func (r *workflowRepoStub) Update(context.Context, *sql.Tx, UpdateParams) (*RepairRequest, error) {
	return nil, nil
}

func (r *workflowRepoStub) SoftDelete(context.Context, *sql.Tx, string) error {
	return nil
}

func (r *workflowRepoStub) Assign(_ context.Context, _ *sql.Tx, params AssignParams) (*RepairRequest, error) {
	r.assignParams = params
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "assigned"
	repairRequest.AssignedTo = &params.AssignedTo
	repairRequest.AssignedAt = &params.AssignedAt
	return repairRequest, nil
}

func (r *workflowRepoStub) Progress(context.Context, *sql.Tx, string) (*RepairRequest, error) {
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "in_progress"
	return repairRequest, nil
}

func (r *workflowRepoStub) Complete(context.Context, *sql.Tx, CompleteParams) (*RepairRequest, error) {
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "completed"
	return repairRequest, nil
}

func (r *workflowRepoStub) Cancel(_ context.Context, _ *sql.Tx, params CancelParams) (*RepairRequest, error) {
	r.cancelParams = params
	repairRequest := submittedRepairRequest()
	repairRequest.Status = "cancelled"
	repairRequest.CancelReason = params.CancelReason
	return repairRequest, nil
}

func submittedRepairRequest() *RepairRequest {
	return &RepairRequest{
		ID:          testRepairID,
		PropertyID:  testPropertyID,
		RoomID:      testRoomID,
		SubmittedBy: testStaffID,
		Title:       "Leak",
		Description: "Bathroom leak",
		Status:      "submitted",
		SubmittedAt: testNow,
		CreatedAt:   testNow,
		UpdatedAt:   testNow,
	}
}
