package billing

import (
	"context"
	"errors"
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

func TestFinancialReportCurrentPeriodUsesTaiwanTimezone(t *testing.T) {
	now := time.Date(2026, 3, 31, 16, 30, 0, 0, time.UTC)

	t.Run("summary current period", func(t *testing.T) {
		repo := &reportRepositoryStub{summaries: []FinancialReportSummary{}}
		service := NewListFinancialReportSummariesService(repo, fixedClock{now: now})

		_, err := service.Execute(context.Background(), ListFinancialReportSummariesInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if repo.summaryQuery == nil {
			t.Fatal("expected summary query")
		}
		if repo.summaryQuery.CurrentYear != 2026 || repo.summaryQuery.CurrentMonth != 4 {
			t.Fatalf("unexpected current period: %+v", repo.summaryQuery)
		}
	})

	t.Run("detail current period", func(t *testing.T) {
		repo := &reportRepositoryStub{
			liveReport:     reportForTest(2026, 4, false),
			snapshotReport: reportForTest(2026, 4, true),
		}
		service := NewGetFinancialReportService(repo, fixedClock{now: now})

		report, err := service.Execute(context.Background(), GetFinancialReportInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      4,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if report == nil || report.IsFinalized {
			t.Fatalf("unexpected report: %+v", report)
		}
		if repo.liveReportQuery == nil || repo.snapshotReportQuery != nil {
			t.Fatalf("expected live query only, live=%+v snapshot=%+v", repo.liveReportQuery, repo.snapshotReportQuery)
		}
	})

	t.Run("send current period", func(t *testing.T) {
		repo := &reportRepositoryStub{
			liveReport:     reportForTest(2026, 4, false),
			snapshotReport: reportForTest(2026, 4, true),
		}
		publisher := &recordingPublisher{}
		service := NewSendFinancialReportService(repo, publisher, fixedClock{now: now})

		report, err := service.Execute(context.Background(), SendFinancialReportInput{
			ActorRole:  "organizer",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      4,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if report == nil || report.IsFinalized {
			t.Fatalf("unexpected report: %+v", report)
		}
		if repo.liveReportQuery == nil || repo.snapshotReportQuery != nil {
			t.Fatalf("expected live query only, live=%+v snapshot=%+v", repo.liveReportQuery, repo.snapshotReportQuery)
		}
	})
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

func TestSendFinancialReportUsesCurrentMonthLivePath(t *testing.T) {
	occurredAt := time.Date(2026, 4, 24, 10, 30, 0, 0, time.UTC)
	repo := &reportRepositoryStub{
		liveReport:     reportForTest(2026, 4, false),
		snapshotReport: reportForTest(2026, 4, true),
	}
	publisher := &recordingPublisher{}
	service := NewSendFinancialReportService(repo, publisher, fixedClock{now: occurredAt})

	report, err := service.Execute(context.Background(), SendFinancialReportInput{
		ActorRole:  "organizer",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      4,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report == nil || report.IsFinalized {
		t.Fatalf("unexpected report: %+v", report)
	}
	if repo.liveReportQuery == nil || repo.liveReportQuery.Year != 2026 || repo.liveReportQuery.Month != 4 {
		t.Fatalf("expected live query for current month, got %+v", repo.liveReportQuery)
	}
	if repo.snapshotReportQuery != nil {
		t.Fatalf("unexpected snapshot query: %+v", repo.snapshotReportQuery)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.FinancialReportSendRequested)
	if !ok {
		t.Fatalf("expected FinancialReportSendRequested, got %T", publisher.events[0])
	}
	if event.PropertyID != testPropertyID || event.Year != 2026 || event.Month != 4 || !event.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestSendFinancialReportRequiresPublisher(t *testing.T) {
	repo := &reportRepositoryStub{snapshotReport: reportForTest(2026, 3, true)}
	service := NewSendFinancialReportService(repo, nil, fixedClock{now: time.Date(2026, 4, 24, 10, 30, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), SendFinancialReportInput{
		ActorRole:  "organizer",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      3,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
}

func TestExportTenantRosterRendersHTMLDocument(t *testing.T) {
	asOf := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	dueDate := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	leaseID := "lease-1"
	tenantName := "Alice"
	tenantPhone := "0912-345-678"
	cadence := "quarterly"
	rentAmount := 36000
	repo := &reportRepositoryStub{
		tenantRosterRows: []TenantRosterRow{
			{
				PropertyID:         testPropertyID,
				PropertyName:       "Demo Property",
				RoomID:             testRoomID,
				RoomName:           "101",
				RoomStatus:         "occupied",
				LeaseID:            &leaseID,
				TenantName:         &tenantName,
				TenantPhone:        &tenantPhone,
				NextRentDueDate:    &dueDate,
				RentBillingCadence: &cadence,
				RentAmount:         &rentAmount,
			},
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>tenant roster</html>")}
	service := NewExportTenantRosterService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	document, err := service.Execute(context.Background(), ExportTenantRosterInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		AsOf:                &asOf,
		IncludeVacant:       true,
		Format:              "html",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.tenantRosterQuery == nil {
		t.Fatal("expected tenant roster query")
	}
	if repo.tenantRosterQuery.ActorRole != "staff" || repo.tenantRosterQuery.ActorUserID != "user-1" {
		t.Fatalf("unexpected actor scope: %+v", repo.tenantRosterQuery)
	}
	if !repo.tenantRosterQuery.IncludeVacant || !repo.tenantRosterQuery.AsOf.Equal(asOf) {
		t.Fatalf("unexpected roster query: %+v", repo.tenantRosterQuery)
	}
	if renderer.name != "tenant_roster.html" {
		t.Fatalf("renderer template = %q", renderer.name)
	}
	view, ok := renderer.data.(tenantRosterView)
	if !ok {
		t.Fatalf("renderer data = %T, want tenantRosterView", renderer.data)
	}
	if view.PropertyName != "Demo Property" || view.AsOfLabel != "2026-05-07" {
		t.Fatalf("unexpected view metadata: %+v", view)
	}
	if len(view.Rows) != 1 || view.Rows[0].NextRentDueDateLabel != "2026-05-10" || view.Rows[0].RentCadenceLabel != "季繳" {
		t.Fatalf("unexpected view rows: %+v", view.Rows)
	}
	if document.Filename != "tenant-roster-demo-property-2026-05-07.html" {
		t.Fatalf("filename = %q", document.Filename)
	}
	if string(document.HTML) != "<html>tenant roster</html>" {
		t.Fatalf("html = %q", document.HTML)
	}
}

func TestExportTenantRosterRejectsUnsupportedFormat(t *testing.T) {
	repo := &reportRepositoryStub{}
	renderer := &recordingReportRenderer{html: []byte("<html></html>")}
	service := NewExportTenantRosterService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportTenantRosterInput{
		ActorRole:  "staff",
		PropertyID: testPropertyID,
		Format:     "pdf",
	})
	assertAppErrorCode(t, err, apperr.CodeBadRequest)
	if repo.tenantRosterQuery != nil {
		t.Fatalf("unexpected tenant roster query: %+v", repo.tenantRosterQuery)
	}
}

func TestExportBillReceiptRendersRentHTMLDocument(t *testing.T) {
	amount := 18000
	repo := &reportRepositoryStub{
		billReceipt: &BillReceipt{
			BillID:       testBillID,
			BillType:     "rent",
			BillStatus:   "paid",
			Amount:       &amount,
			PeriodStart:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:    time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			PropertyName: "Demo Property",
			RoomName:     "101",
			TenantName:   "Alice",
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>rent receipt</html>")}
	service := NewExportBillReceiptService(repo, renderer)

	document, err := service.Execute(context.Background(), ExportBillReceiptInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		BillID:              testBillID,
		Format:              "html",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.billReceiptQuery == nil || repo.billReceiptQuery.BillID != testBillID {
		t.Fatalf("unexpected receipt query: %+v", repo.billReceiptQuery)
	}
	if renderer.name != "bill_receipt.html" {
		t.Fatalf("renderer template = %q", renderer.name)
	}
	view, ok := renderer.data.(billReceiptView)
	if !ok {
		t.Fatalf("renderer data = %T, want billReceiptView", renderer.data)
	}
	if !view.IsRent || view.IsElectricity || view.Title != "租金收據" {
		t.Fatalf("unexpected receipt view metadata: %+v", view)
	}
	if len(view.Copies) != 2 || view.Copies[0].CopyLabel != "客戶聯" || view.Copies[1].CopyLabel != "存根聯" {
		t.Fatalf("unexpected copies: %+v", view.Copies)
	}
	if view.Copies[0].PeriodLabel != "2026-05-01 至 2026-05-31" || view.Copies[0].AmountLabel != "NT$ 18,000" {
		t.Fatalf("unexpected copy data: %+v", view.Copies[0])
	}
	if view.Copies[0].PropertyName != "Demo Property" || view.Copies[0].RoomName != "101" || view.Copies[0].TenantName != "Alice" {
		t.Fatalf("unexpected render-time display labels: %+v", view.Copies[0])
	}
	if view.Copies[0].ReceiptDate != "" || view.Copies[0].Collector != "" {
		t.Fatalf("receipt date and collector should stay blank: %+v", view.Copies[0])
	}
	if document.Filename != "bill-receipt-rent-101-2026-05-01.html" {
		t.Fatalf("filename = %q", document.Filename)
	}
}

func TestExportBillReceiptRendersElectricityMeterFields(t *testing.T) {
	amount := 860
	previous := 1280
	current := 1452
	unitPrice := 5.0
	repo := &reportRepositoryStub{
		billReceipt: &BillReceipt{
			BillID:               testBillID,
			BillType:             "electricity",
			BillStatus:           "paid",
			Amount:               &amount,
			PeriodStart:          time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:            time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
			MeterPreviousReading: &previous,
			MeterCurrentReading:  &current,
			MeterUnitPrice:       &unitPrice,
			PropertyName:         "Demo Property",
			RoomName:             "101",
			TenantName:           "Alice",
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>electricity receipt</html>")}
	service := NewExportBillReceiptService(repo, renderer)

	_, err := service.Execute(context.Background(), ExportBillReceiptInput{
		ActorRole: "owner",
		BillID:    testBillID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	view := renderer.data.(billReceiptView)
	if !view.IsElectricity || view.Title != "電費收據" {
		t.Fatalf("unexpected receipt view metadata: %+v", view)
	}
	copy := view.Copies[0]
	if copy.PreviousReadingLabel != "1,280" || copy.CurrentReadingLabel != "1,452" || copy.UsageLabel != "172" {
		t.Fatalf("unexpected meter labels: %+v", copy)
	}
	if copy.UnitPriceLabel != "NT$ 5" || copy.AmountLabel != "NT$ 860" {
		t.Fatalf("unexpected amount labels: %+v", copy)
	}
}

func TestExportBillReceiptRejectsIneligibleStatusAndUnsupportedFormat(t *testing.T) {
	for _, status := range []string{"pending_meter", "pending_payment", "overdue", "voided", "written_off"} {
		t.Run(status, func(t *testing.T) {
			amount := 18000
			repo := &reportRepositoryStub{billReceipt: &BillReceipt{BillID: testBillID, BillType: "rent", BillStatus: status, Amount: &amount}}
			service := NewExportBillReceiptService(repo, &recordingReportRenderer{html: []byte("<html></html>")})

			_, err := service.Execute(context.Background(), ExportBillReceiptInput{
				ActorRole: "staff",
				BillID:    testBillID,
			})
			assertAppErrorCode(t, err, CodeBillReceiptNotExportable)
		})
	}

	repo := &reportRepositoryStub{}
	service := NewExportBillReceiptService(repo, &recordingReportRenderer{html: []byte("<html></html>")})
	_, err := service.Execute(context.Background(), ExportBillReceiptInput{
		ActorRole: "staff",
		BillID:    testBillID,
		Format:    "pdf",
	})
	assertAppErrorCode(t, err, apperr.CodeBadRequest)
	if repo.billReceiptQuery != nil {
		t.Fatalf("unexpected receipt query: %+v", repo.billReceiptQuery)
	}
}

func TestExportBillReceiptRendererFailureMapsInternalError(t *testing.T) {
	amount := 18000
	repo := &reportRepositoryStub{
		billReceipt: &BillReceipt{BillID: testBillID, BillType: "rent", BillStatus: "paid", Amount: &amount},
	}
	service := NewExportBillReceiptService(repo, &recordingReportRenderer{err: errors.New("render failed")})

	_, err := service.Execute(context.Background(), ExportBillReceiptInput{
		ActorRole: "staff",
		BillID:    testBillID,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
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
	tenantRosterRows          []TenantRosterRow
	tenantRosterQuery         *TenantRosterQuery
	billReceipt               *BillReceipt
	billReceiptQuery          *BillReceiptQuery
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

func (s *reportRepositoryStub) ListTenantRosterRows(_ context.Context, query TenantRosterQuery) ([]TenantRosterRow, error) {
	s.tenantRosterQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.tenantRosterRows, nil
}

func (s *reportRepositoryStub) FindBillReceipt(_ context.Context, query BillReceiptQuery) (*BillReceipt, error) {
	s.billReceiptQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.billReceipt, nil
}

type recordingReportRenderer struct {
	name string
	data any
	html []byte
	err  error
}

func (r *recordingReportRenderer) Render(name string, data any) ([]byte, error) {
	r.name = name
	r.data = data
	if r.err != nil {
		return nil, r.err
	}
	return r.html, nil
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
