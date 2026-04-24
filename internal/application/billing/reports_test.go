package billing

import (
	"context"
	"testing"
	"time"

	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/shared/apperr"
)

func TestReportServicesValidateRoles(t *testing.T) {
	ctx := context.Background()

	t.Run("owner cannot read pending meters", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		service := NewListPendingMeterService(repo)

		_, err := service.Execute(ctx, ListPendingMeterInput{
			ActorRole:  "owner",
			PropertyID: testPropertyID,
		})
		assertAppErrorCode(t, err, apperr.CodeForbidden)
		if repo.pendingMeterQuery != nil {
			t.Fatalf("unexpected pending meter query: %+v", repo.pendingMeterQuery)
		}
	})

	t.Run("owner cannot read property meter history", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		service := NewListPropertyMeterHistoryService(repo)

		_, err := service.Execute(ctx, ListPropertyMeterHistoryInput{
			ActorRole:  "owner",
			PropertyID: testPropertyID,
		})
		assertAppErrorCode(t, err, apperr.CodeForbidden)
		if repo.propertyMeterHistoryQuery != nil {
			t.Fatalf("unexpected property meter history query: %+v", repo.propertyMeterHistoryQuery)
		}
	})

	t.Run("owner cannot read room meter history", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		service := NewListRoomMeterHistoryService(repo)

		_, err := service.Execute(ctx, ListRoomMeterHistoryInput{
			ActorRole: "owner",
			RoomID:    testRoomID,
		})
		assertAppErrorCode(t, err, apperr.CodeForbidden)
		if repo.roomMeterHistoryQuery != nil {
			t.Fatalf("unexpected room meter history query: %+v", repo.roomMeterHistoryQuery)
		}
	})

	t.Run("owner can read financial report", func(t *testing.T) {
		repo := &reportRepositoryStub{
			snapshotReport: reportForTest(2026, 3, true),
		}
		service := NewGetFinancialReportService(repo, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})

		report, err := service.Execute(ctx, GetFinancialReportInput{
			ActorRole:  "owner",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      3,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if report == nil || report.PropertyID != testPropertyID {
			t.Fatalf("unexpected report: %+v", report)
		}
		if repo.snapshotReportQuery == nil || repo.snapshotReportQuery.ActorRole != "owner" {
			t.Fatalf("unexpected snapshot query: %+v", repo.snapshotReportQuery)
		}
	})

	t.Run("owner cannot send financial report", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		publisher := &recordingPublisher{}
		service := NewSendFinancialReportService(repo, publisher, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})

		_, err := service.Execute(ctx, SendFinancialReportInput{
			ActorRole:  "owner",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      3,
		})
		assertAppErrorCode(t, err, apperr.CodeForbidden)
		if len(publisher.events) != 0 {
			t.Fatalf("expected no events, got %d", len(publisher.events))
		}
		if repo.snapshotReportQuery != nil || repo.liveReportQuery != nil {
			t.Fatalf("unexpected report query: live=%+v snapshot=%+v", repo.liveReportQuery, repo.snapshotReportQuery)
		}
	})
}

func TestFinancialReportDetailMissingMapsToNotFound(t *testing.T) {
	t.Run("snapshot missing", func(t *testing.T) {
		repo := &reportRepositoryStub{snapshotReportErr: ErrFinancialReportNotFoundRepository}
		service := NewGetFinancialReportService(repo, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), GetFinancialReportInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      3,
		})
		assertAppErrorCode(t, err, CodeFinancialReportNotFound)
	})

	t.Run("live missing", func(t *testing.T) {
		repo := &reportRepositoryStub{liveReportErr: ErrFinancialReportNotFoundRepository}
		service := NewGetFinancialReportService(repo, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), GetFinancialReportInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      4,
		})
		assertAppErrorCode(t, err, CodeFinancialReportNotFound)
	})
}

func TestFinancialReportDetailUsesCurrentMonthLivePath(t *testing.T) {
	repo := &reportRepositoryStub{
		liveReport:     reportForTest(2026, 4, false),
		snapshotReport: reportForTest(2026, 3, true),
	}
	service := NewGetFinancialReportService(repo, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})

	current, err := service.Execute(context.Background(), GetFinancialReportInput{
		ActorRole:  "staff",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      4,
	})
	if err != nil {
		t.Fatalf("current Execute: %v", err)
	}
	if current == nil || current.IsFinalized {
		t.Fatalf("unexpected current report: %+v", current)
	}
	if repo.liveReportQuery == nil || repo.liveReportQuery.Year != 2026 || repo.liveReportQuery.Month != 4 {
		t.Fatalf("expected live query for current month, got %+v", repo.liveReportQuery)
	}

	historical, err := service.Execute(context.Background(), GetFinancialReportInput{
		ActorRole:  "staff",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      3,
	})
	if err != nil {
		t.Fatalf("historical Execute: %v", err)
	}
	if historical == nil || !historical.IsFinalized {
		t.Fatalf("unexpected historical report: %+v", historical)
	}
	if repo.snapshotReportQuery == nil || repo.snapshotReportQuery.Year != 2026 || repo.snapshotReportQuery.Month != 3 {
		t.Fatalf("expected snapshot query for historical month, got %+v", repo.snapshotReportQuery)
	}
}

func TestFinancialReportSummaryReturnsEmptySlice(t *testing.T) {
	repo := &reportRepositoryStub{summaries: []FinancialReportSummary{}}
	service := NewListFinancialReportSummariesService(repo)

	summaries, err := service.Execute(context.Background(), ListFinancialReportSummariesInput{
		ActorRole:  "owner",
		PropertyID: testPropertyID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if summaries == nil || len(summaries) != 0 {
		t.Fatalf("expected empty summary slice, got %+v", summaries)
	}
}

func TestFinancialReportSummaryPreservesActorScope(t *testing.T) {
	repo := &reportRepositoryStub{summaries: []FinancialReportSummary{}}
	service := NewListFinancialReportSummariesService(repo, fixedClock{now: time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)})
	year := 2026

	_, err := service.Execute(context.Background(), ListFinancialReportSummariesInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Year:                &year,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.summaryQuery == nil {
		t.Fatal("expected summary query")
	}
	if repo.summaryQuery.ActorRole != "staff" || repo.summaryQuery.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor scope: %+v", repo.summaryQuery)
	}
	if len(repo.summaryQuery.AssignedPropertyIDs) != 1 || repo.summaryQuery.AssignedPropertyIDs[0] != testPropertyID {
		t.Fatalf("unexpected assigned property scope: %+v", repo.summaryQuery.AssignedPropertyIDs)
	}
	if repo.summaryQuery.Year == nil || *repo.summaryQuery.Year != 2026 {
		t.Fatalf("unexpected year: %+v", repo.summaryQuery.Year)
	}
}

func TestSendFinancialReportEmitsEventWhenReportExists(t *testing.T) {
	occurredAt := time.Date(2026, 4, 24, 10, 30, 0, 0, time.UTC)
	repo := &reportRepositoryStub{snapshotReport: reportForTest(2026, 3, true)}
	publisher := &recordingPublisher{}
	service := NewSendFinancialReportService(repo, publisher, fixedClock{now: occurredAt})

	report, err := service.Execute(context.Background(), SendFinancialReportInput{
		ActorRole:  "organizer",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      3,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report == nil || report.Year != 2026 || report.Month != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.FinancialReportSendRequested)
	if !ok {
		t.Fatalf("expected FinancialReportSendRequested, got %T", publisher.events[0])
	}
	if event.PropertyID != testPropertyID || event.Year != 2026 || event.Month != 3 || !event.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected event: %+v", event)
	}
}

type reportRepositoryStub struct {
	pendingMeterBills         []Bill
	pendingMeterQuery         *PendingMeterQuery
	propertyMeterHistoryBills []Bill
	propertyMeterHistoryQuery *MeterHistoryQuery
	roomMeterHistoryBills     []Bill
	roomMeterHistoryQuery     *MeterHistoryQuery
	summaries                 []FinancialReportSummary
	summaryQuery              *FinancialReportSummaryQuery
	liveReport                *FinancialReport
	liveReportQuery           *FinancialReportQuery
	liveReportErr             error
	snapshotReport            *FinancialReport
	snapshotReportQuery       *FinancialReportQuery
	snapshotReportErr         error
	err                       error
}

func (s *reportRepositoryStub) ListPendingMeterBills(_ context.Context, query PendingMeterQuery) ([]Bill, error) {
	s.pendingMeterQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.pendingMeterBills, nil
}

func (s *reportRepositoryStub) ListPropertyMeterHistory(_ context.Context, query MeterHistoryQuery) ([]Bill, error) {
	s.propertyMeterHistoryQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.propertyMeterHistoryBills, nil
}

func (s *reportRepositoryStub) ListRoomMeterHistory(_ context.Context, query MeterHistoryQuery) ([]Bill, error) {
	s.roomMeterHistoryQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.roomMeterHistoryBills, nil
}

func (s *reportRepositoryStub) ListFinancialReportSummaries(_ context.Context, query FinancialReportSummaryQuery) ([]FinancialReportSummary, error) {
	s.summaryQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.summaries, nil
}

func (s *reportRepositoryStub) FindLiveFinancialReport(_ context.Context, query FinancialReportQuery) (*FinancialReport, error) {
	s.liveReportQuery = &query
	if s.liveReportErr != nil {
		return nil, s.liveReportErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.liveReport, nil
}

func (s *reportRepositoryStub) FindSnapshotFinancialReport(_ context.Context, query FinancialReportQuery) (*FinancialReport, error) {
	s.snapshotReportQuery = &query
	if s.snapshotReportErr != nil {
		return nil, s.snapshotReportErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshotReport, nil
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func reportForTest(year int, month int, finalized bool) *FinancialReport {
	return &FinancialReport{
		PropertyID:   testPropertyID,
		Year:         year,
		Month:        month,
		TotalIncome:  12000,
		TotalExpense: 500,
		Net:          11500,
		IsFinalized:  finalized,
		Entries: []FinancialReportEntry{
			{
				Category:    AccountingCategoryRentPayment,
				Description: stringPtr("rent"),
				Amount:      12000,
			},
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

var _ ReportRepository = (*reportRepositoryStub)(nil)
var _ Clock = fixedClock{}
var _ domainevents.Publisher = (*recordingPublisher)(nil)
