package billing

import (
	"context"
	"errors"
	"strings"
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

func TestListPropertyMeterHistoryReturnsGridRowsAndForwardsScope(t *testing.T) {
	repo := &reportRepositoryStub{
		propertyMeterHistoryRows: []PropertyMeterHistoryRow{{
			BillID:          "bill-1",
			PropertyID:      testPropertyID,
			RoomID:          testRoomID,
			RoomLabel:       "101",
			TenantID:        testTenantID,
			TenantLabel:     "王小明",
			LeaseID:         testLeaseID,
			PreviousReading: 1120,
			CurrentReading:  1250,
			Usage:           130,
			UnitPrice:       5,
			Status:          "paid",
		}},
	}
	service := NewListPropertyMeterHistoryService(repo)
	year := 2026

	rows, err := service.Execute(context.Background(), ListPropertyMeterHistoryInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Year:                &year,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rows) != 1 || rows[0].RoomLabel != "101" || rows[0].Usage != 130 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if repo.propertyMeterHistoryQuery == nil || repo.propertyMeterHistoryQuery.ActorRole != "staff" || repo.propertyMeterHistoryQuery.PropertyID != testPropertyID {
		t.Fatalf("unexpected query: %+v", repo.propertyMeterHistoryQuery)
	}
	if repo.propertyMeterHistoryQuery.Year == nil || *repo.propertyMeterHistoryQuery.Year != 2026 {
		t.Fatalf("unexpected year query: %+v", repo.propertyMeterHistoryQuery)
	}
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

func TestTenantRosterTemplateUsesA4PageLayout(t *testing.T) {
	longTenantName := "很長很長的租客姓名需要在欄位內自動換行"
	longPhone := "091234567809123456780912345678"
	longNotes := "這是一段很長的備註內容，需要留在表格欄位內換行，不能把 A4 預覽頁面撐寬。"
	view := newTenantRosterView(time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), []TenantRosterRow{
		{
			PropertyName: "Demo Property",
			RoomName:     "A-1001-很長的房號",
			LeaseID:      stringPtr("lease-1"),
			TenantName:   &longTenantName,
			TenantPhone:  &longPhone,
			ReportNotes:  &longNotes,
		},
	})
	renderer := MustNewTenantRosterRenderer()

	html, err := renderer.Render("tenant_roster.html", view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rendered := string(html)
	for _, want := range []string{
		`<main class="page">`,
		`width: 210mm;`,
		`min-height: 297mm;`,
		`table-layout: fixed;`,
		`overflow-wrap: anywhere;`,
		`size: A4 portrait;`,
		longTenantName,
		longPhone,
		longNotes,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered tenant roster HTML missing %q", want)
		}
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

func TestExportBillReceiptTreatsPaidElectricityMissingAmountAsInternalError(t *testing.T) {
	previous := 1280
	current := 1452
	unitPrice := 5.0
	repo := &reportRepositoryStub{
		billReceipt: &BillReceipt{
			BillID:               testBillID,
			BillType:             "electricity",
			BillStatus:           "paid",
			MeterPreviousReading: &previous,
			MeterCurrentReading:  &current,
			MeterUnitPrice:       &unitPrice,
		},
	}
	service := NewExportBillReceiptService(repo, &recordingReportRenderer{html: []byte("<html></html>")})

	_, err := service.Execute(context.Background(), ExportBillReceiptInput{
		ActorRole: "staff",
		BillID:    testBillID,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
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

func TestExportMonthlyCashflowUsesCurrentMonthLiveRowsAndRunningBalance(t *testing.T) {
	description := "101 2026-05 rent"
	sourceDate := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	displayNote := "101 王小明 2026-05 租金"
	repo := &reportRepositoryStub{
		liveCashflow: &MonthlyCashflow{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        5,
			Rows: []MonthlyCashflowEntry{
				{
					Category:            "rent_payment",
					AccountingTitleCode: stringPtr("4603"),
					AccountingTitleName: stringPtr("租金收入"),
					Description:         &description,
					SourceDate:          &sourceDate,
					DisplayNote:         &displayNote,
					Amount:              18000,
					SourceRef:           []byte(`{"bill_id":"bill-1"}`),
					CreatedAt:           time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
				},
				{
					Category:            "journal_expense",
					AccountingTitleCode: stringPtr("6681"),
					AccountingTitleName: stringPtr("其他支出"),
					Amount:              -2500,
					SourceRef:           []byte(`{"journal_log_id":"journal-1"}`),
					CreatedAt:           time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC),
				},
				{
					Category:            "rent_refund",
					AccountingTitleCode: stringPtr("4604"),
					AccountingTitleName: stringPtr("租金退回(減項)"),
					Amount:              -3000,
					SourceRef:           []byte(`{"type":"RentRefunded","lease_id":"lease-1","reason":"提前退租租金退回"}`),
					CreatedAt:           time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
				},
			},
		},
		openingBalance: 1000,
	}
	renderer := &recordingReportRenderer{html: []byte("<html>cashflow</html>")}
	service := NewExportMonthlyCashflowService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	document, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Year:                2026,
		Month:               5,
		Format:              "html",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.liveCashflowQuery == nil || repo.snapshotCashflowQuery != nil {
		t.Fatalf("unexpected source queries: live=%+v snapshot=%+v", repo.liveCashflowQuery, repo.snapshotCashflowQuery)
	}
	if repo.openingBalanceQuery == nil || repo.openingBalanceQuery.PropertyID != testPropertyID {
		t.Fatalf("unexpected opening balance query: %+v", repo.openingBalanceQuery)
	}
	if renderer.name != "monthly_cashflow.html" {
		t.Fatalf("renderer template = %q", renderer.name)
	}
	view, ok := renderer.data.(monthlyCashflowView)
	if !ok {
		t.Fatalf("renderer data = %T, want monthlyCashflowView", renderer.data)
	}
	if view.Title != "Demo Property收支表 (2026/05)" || view.PeriodLabel != "2026-05" {
		t.Fatalf("unexpected view metadata: %+v", view)
	}
	if view.OpeningBalanceLabel != "NT$ 1,000" || view.MonthlyIncomeTotalLabel != "NT$ 18,000" || view.MonthlyExpenseTotalLabel != "NT$ 5,500" || view.EndingBalanceLabel != "NT$ 13,500" {
		t.Fatalf("unexpected totals: %+v", view)
	}
	if len(view.Rows) != 3 || view.Rows[0].BalanceLabel != "NT$ 19,000" || view.Rows[1].BalanceLabel != "NT$ 16,500" || view.Rows[2].BalanceLabel != "NT$ 13,500" {
		t.Fatalf("unexpected rows: %+v", view.Rows)
	}
	if view.Rows[0].DateLabel != "05/02" || view.Rows[0].SubjectLabel != "租金收入" || view.Rows[0].Note != "101 王小明 2026-05 租金" {
		t.Fatalf("unexpected first row: %+v", view.Rows[0])
	}
	if view.Rows[1].SubjectLabel != "其他支出" {
		t.Fatalf("second row subject = %q, want title label", view.Rows[1].SubjectLabel)
	}
	if view.Rows[1].Note != "" {
		t.Fatalf("second row note = %q, want empty note without raw source_ref JSON", view.Rows[1].Note)
	}
	if view.Rows[2].SubjectLabel != "租金退回(減項)" || view.Rows[2].ExpenseLabel != "NT$ 3,000" || view.Rows[2].Note != "提前退租租金退回" {
		t.Fatalf("rent refund row = %+v", view.Rows[2])
	}
	if document.Filename != "monthly-cashflow-demo-property-2026-05.html" {
		t.Fatalf("filename = %q", document.Filename)
	}
}

func TestExportMonthlyCashflowFallsBackToCategorySubjectForOldRows(t *testing.T) {
	repo := &reportRepositoryStub{
		liveCashflow: &MonthlyCashflow{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        5,
			Rows: []MonthlyCashflowEntry{
				{
					Category:  "deposit_refund",
					Amount:    -6000,
					CreatedAt: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>cashflow</html>")}
	service := NewExportMonthlyCashflowService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
		ActorRole:  "staff",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      5,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	view, ok := renderer.data.(monthlyCashflowView)
	if !ok {
		t.Fatalf("renderer data = %T, want monthlyCashflowView", renderer.data)
	}
	if len(view.Rows) != 1 || view.Rows[0].SubjectLabel != "押金退還" {
		t.Fatalf("unexpected fallback row: %+v", view.Rows)
	}
	if view.Rows[0].DateLabel != "05/06" {
		t.Fatalf("fallback date = %q, want created_at date", view.Rows[0].DateLabel)
	}
}

func TestExportMonthlyCashflowFallsBackFromDisplayNoteToDescriptionThenSourceRefReason(t *testing.T) {
	description := "journal description"
	repo := &reportRepositoryStub{
		liveCashflow: &MonthlyCashflow{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        5,
			Rows: []MonthlyCashflowEntry{
				{
					Category:    "journal_expense",
					Description: &description,
					Amount:      -1000,
					CreatedAt:   time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
				},
				{
					Category:  "deposit_refund",
					Amount:    -2000,
					SourceRef: []byte(`{"reason":"押金退還原因"}`),
					CreatedAt: time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>cashflow</html>")}
	service := NewExportMonthlyCashflowService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
		ActorRole:  "staff",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      5,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	view, ok := renderer.data.(monthlyCashflowView)
	if !ok {
		t.Fatalf("renderer data = %T, want monthlyCashflowView", renderer.data)
	}
	if len(view.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(view.Rows))
	}
	if view.Rows[0].Note != "journal description" {
		t.Fatalf("first row note = %q, want description fallback", view.Rows[0].Note)
	}
	if view.Rows[1].Note != "押金退還原因" {
		t.Fatalf("second row note = %q, want source_ref.reason fallback", view.Rows[1].Note)
	}
}

func TestFinancialReportEntrySourceFromRefNormalizesKnownSourceRefs(t *testing.T) {
	tests := []struct {
		name       string
		category   string
		sourceRef  []byte
		wantType   string
		wantID     string
		wantDetail string
	}{
		{
			name:       "bill payment",
			category:   AccountingCategoryRentPayment,
			sourceRef:  []byte(`{"type":"BillPaid","bill_id":"10000000-0000-0000-0000-000000000011"}`),
			wantType:   "bill",
			wantID:     "10000000-0000-0000-0000-000000000011",
			wantDetail: "payment",
		},
		{
			name:       "journal expense",
			category:   "journal_expense",
			sourceRef:  []byte(`{"type":"JournalExpenseRecorded","journal_log_id":"10000000-0000-0000-0000-000000000012"}`),
			wantType:   "journal_log",
			wantID:     "10000000-0000-0000-0000-000000000012",
			wantDetail: "journal_expense",
		},
		{
			name:       "deposit refund",
			category:   "deposit_refund",
			sourceRef:  []byte(`{"type":"DepositRefunded","lease_id":"10000000-0000-0000-0000-000000000013"}`),
			wantType:   "lease",
			wantID:     "10000000-0000-0000-0000-000000000013",
			wantDetail: "deposit_refund",
		},
		{
			name:       "rent refund",
			category:   "rent_refund",
			sourceRef:  []byte(`{"type":"RentRefunded","lease_id":"10000000-0000-0000-0000-000000000015"}`),
			wantType:   "lease",
			wantID:     "10000000-0000-0000-0000-000000000015",
			wantDetail: "rent_refund",
		},
		{
			name:       "deposit deduction",
			category:   "deposit_deduction",
			sourceRef:  []byte(`{"type":"DepositDeducted","lease_id":"10000000-0000-0000-0000-000000000014"}`),
			wantType:   "lease",
			wantID:     "10000000-0000-0000-0000-000000000014",
			wantDetail: "deposit_deduction",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := FinancialReportEntrySourceFromRef(tc.category, tc.sourceRef)
			if source == nil {
				t.Fatal("source = nil")
			}
			if source.Type != tc.wantType || source.ID != tc.wantID || source.Detail == nil || *source.Detail != tc.wantDetail {
				t.Fatalf("source = %+v, want type %q id %q detail %q", source, tc.wantType, tc.wantID, tc.wantDetail)
			}
		})
	}
}

func TestFinancialReportEntrySourceFromRefIgnoresUnknownOrMalformedSourceRefs(t *testing.T) {
	tests := [][]byte{
		nil,
		[]byte(`null`),
		[]byte(`{"type":"Unknown"}`),
		[]byte(`not json`),
	}

	for _, sourceRef := range tests {
		if source := FinancialReportEntrySourceFromRef("rent_payment", sourceRef); source != nil {
			t.Fatalf("source = %+v, want nil for %s", source, sourceRef)
		}
	}
}

func TestExportMonthlyCashflowUsesSnapshotRowsForHistoricalMonth(t *testing.T) {
	repo := &reportRepositoryStub{
		snapshotCashflow: &MonthlyCashflow{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        4,
			IsFinalized:  true,
		},
	}
	service := NewExportMonthlyCashflowService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
		ActorRole:  "owner",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      4,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.snapshotCashflowQuery == nil || repo.liveCashflowQuery != nil {
		t.Fatalf("unexpected source queries: live=%+v snapshot=%+v", repo.liveCashflowQuery, repo.snapshotCashflowQuery)
	}
}

func TestExportMonthlyCashflowRejectsUnsupportedFormatAndMapsErrors(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		service := NewExportMonthlyCashflowService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      5,
			Format:     "pdf",
		})
		assertAppErrorCode(t, err, apperr.CodeBadRequest)
		if repo.liveCashflowQuery != nil || repo.snapshotCashflowQuery != nil {
			t.Fatalf("unexpected query: live=%+v snapshot=%+v", repo.liveCashflowQuery, repo.snapshotCashflowQuery)
		}
	})

	t.Run("missing historical snapshot", func(t *testing.T) {
		repo := &reportRepositoryStub{snapshotCashflowErr: ErrFinancialReportNotFoundRepository}
		service := NewExportMonthlyCashflowService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      4,
		})
		assertAppErrorCode(t, err, CodeFinancialReportNotFound)
	})

	t.Run("renderer", func(t *testing.T) {
		repo := &reportRepositoryStub{
			liveCashflow: &MonthlyCashflow{PropertyID: testPropertyID, PropertyName: "Demo Property", Year: 2026, Month: 5},
		}
		service := NewExportMonthlyCashflowService(repo, &recordingReportRenderer{err: errors.New("render failed")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportMonthlyCashflowInput{
			ActorRole:  "staff",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      5,
		})
		assertAppErrorCode(t, err, apperr.CodeInternalServerError)
	})
}

func TestExportProfitLossCombinesCurrentPreviousAndDelta(t *testing.T) {
	repo := &reportRepositoryStub{
		liveProfitLoss: &ProfitLossPeriod{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        5,
			Rows: []ProfitLossSourceRow{
				{SubjectCode: "4603", SubjectName: "租金收入", Amount: 10000, Supported: true},
				{SubjectCode: "6681", SubjectName: "其他支出", Amount: -1200, Supported: true},
			},
		},
		snapshotProfitLoss: &ProfitLossPeriod{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        4,
			IsFinalized:  true,
			Rows: []ProfitLossSourceRow{
				{SubjectCode: "4603", SubjectName: "租金收入", Amount: 9000, Supported: true},
				{SubjectCode: "6681", SubjectName: "其他支出", Amount: -1500, Supported: true},
			},
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>profit loss</html>")}
	service := NewExportProfitLossService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	document, err := service.Execute(context.Background(), ExportProfitLossInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Year:                2026,
		Month:               5,
		Format:              "html",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.liveProfitLossQuery == nil || repo.snapshotProfitLossQuery == nil {
		t.Fatalf("expected live current and snapshot previous queries")
	}
	if repo.snapshotProfitLossQuery.Year != 2026 || repo.snapshotProfitLossQuery.Month != 4 {
		t.Fatalf("previous query = %+v, want 2026/4", repo.snapshotProfitLossQuery)
	}
	if document.Filename != "profit-loss-demo-property-2026-05.html" {
		t.Fatalf("filename = %q", document.Filename)
	}
	view, ok := renderer.data.(profitLossView)
	if !ok {
		t.Fatalf("renderer data = %T, want profitLossView", renderer.data)
	}
	if view.CurrentTotalLabel != "NT$ 8,800" || view.PreviousTotalLabel != "NT$ 7,500" || view.DeltaTotalLabel != "NT$ 1,300" {
		t.Fatalf("totals = %s/%s/%s", view.CurrentTotalLabel, view.PreviousTotalLabel, view.DeltaTotalLabel)
	}
	if len(view.Rows) < 2 || view.Rows[0].CurrentAmountLabel != "NT$ 10,000" || view.Rows[0].PreviousAmountLabel != "NT$ 9,000" || view.Rows[0].DeltaAmountLabel != "NT$ 1,000" {
		t.Fatalf("unexpected rows: %+v", view.Rows)
	}
}

func TestExportProfitLossJanuaryPreviousMonthRollover(t *testing.T) {
	repo := &reportRepositoryStub{
		liveProfitLoss:     profitLossPeriodForTest(2026, 1),
		snapshotProfitLoss: profitLossPeriodForTest(2025, 12),
	}
	service := NewExportProfitLossService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 1, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportProfitLossInput{
		ActorRole:  "admin",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      1,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.snapshotProfitLossQuery == nil || repo.snapshotProfitLossQuery.Year != 2025 || repo.snapshotProfitLossQuery.Month != 12 {
		t.Fatalf("previous query = %+v, want 2025/12", repo.snapshotProfitLossQuery)
	}
}

func TestExportProfitLossRejectsUnsupportedFormatAndMapsErrors(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		repo := &reportRepositoryStub{}
		service := NewExportProfitLossService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportProfitLossInput{
			ActorRole:  "admin",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      5,
			Format:     "pdf",
		})
		assertAppErrorCode(t, err, apperr.CodeBadRequest)
		if repo.liveProfitLossQuery != nil || repo.snapshotProfitLossQuery != nil {
			t.Fatalf("unexpected query: live=%+v snapshot=%+v", repo.liveProfitLossQuery, repo.snapshotProfitLossQuery)
		}
	})

	t.Run("missing historical snapshot", func(t *testing.T) {
		repo := &reportRepositoryStub{snapshotProfitLossErr: ErrFinancialReportNotFoundRepository}
		service := NewExportProfitLossService(repo, &recordingReportRenderer{html: []byte("<html></html>")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportProfitLossInput{
			ActorRole:  "admin",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      4,
		})
		assertAppErrorCode(t, err, CodeFinancialReportNotFound)
		if repo.liveProfitLossQuery != nil {
			t.Fatalf("unexpected live fallback: %+v", repo.liveProfitLossQuery)
		}
	})

	t.Run("renderer", func(t *testing.T) {
		repo := &reportRepositoryStub{
			liveProfitLoss:     profitLossPeriodForTest(2026, 5),
			snapshotProfitLoss: profitLossPeriodForTest(2026, 4),
		}
		service := NewExportProfitLossService(repo, &recordingReportRenderer{err: errors.New("render failed")}, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

		_, err := service.Execute(context.Background(), ExportProfitLossInput{
			ActorRole:  "admin",
			PropertyID: testPropertyID,
			Year:       2026,
			Month:      5,
		})
		assertAppErrorCode(t, err, apperr.CodeInternalServerError)
	})
}

func TestExportProfitLossPreservesUnsupportedRows(t *testing.T) {
	note := "未支援會計分類：legacy_misc"
	anotherNote := "未支援會計分類：legacy_fee"
	repo := &reportRepositoryStub{
		liveProfitLoss: &ProfitLossPeriod{
			PropertyID:   testPropertyID,
			PropertyName: "Demo Property",
			Year:         2026,
			Month:        5,
			Rows: []ProfitLossSourceRow{
				{SubjectCode: "unsupported", SubjectName: "未支援科目", Amount: 300, Supported: false, Note: &note},
				{SubjectCode: "unsupported", SubjectName: "未支援科目", Amount: 700, Supported: false, Note: &anotherNote},
			},
		},
		snapshotProfitLoss: profitLossPeriodForTest(2026, 4),
	}
	renderer := &recordingReportRenderer{html: []byte("<html></html>")}
	service := NewExportProfitLossService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportProfitLossInput{
		ActorRole:  "admin",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      5,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	view := renderer.data.(profitLossView)
	if view.CurrentTotalLabel != "NT$ 0" || view.PreviousTotalLabel != "NT$ 0" || view.DeltaTotalLabel != "NT$ 0" {
		t.Fatalf("totals include unsupported rows: %s/%s/%s", view.CurrentTotalLabel, view.PreviousTotalLabel, view.DeltaTotalLabel)
	}
	unsupportedRows := map[string]string{}
	for _, row := range view.Rows {
		if row.Unsupported && row.Note != "" {
			unsupportedRows[row.Note] = row.CurrentAmountLabel
		}
	}
	if unsupportedRows[note] != "NT$ 300" || unsupportedRows[anotherNote] != "NT$ 700" {
		t.Fatalf("unsupported rows collapsed or changed: %+v", unsupportedRows)
	}
}

func TestExportProfitLossAddsFixedUnsupportedLegacyZeroRows(t *testing.T) {
	repo := &reportRepositoryStub{
		liveProfitLoss:     profitLossPeriodForTest(2026, 5),
		snapshotProfitLoss: profitLossPeriodForTest(2026, 4),
	}
	renderer := &recordingReportRenderer{html: []byte("<html></html>")}
	service := NewExportProfitLossService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	_, err := service.Execute(context.Background(), ExportProfitLossInput{
		ActorRole:  "admin",
		PropertyID: testPropertyID,
		Year:       2026,
		Month:      5,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	view := renderer.data.(profitLossView)
	wantRows := map[string]string{
		"管理費收入": "未支援舊系統損益科目：管理費收入",
		"薪資支出":  "未支援舊系統損益科目：薪資支出",
		"用品支出":  "未支援舊系統損益科目：用品支出",
		"水電瓦斯費": "未支援舊系統損益科目：水電瓦斯費",
		"清潔費":   "未支援舊系統損益科目：清潔費",
		"修繕費":   "未支援舊系統損益科目：修繕費",
		"佣金支出":  "未支援舊系統損益科目：佣金支出",
		"稅捐":    "未支援舊系統損益科目：稅捐",
	}
	for _, row := range view.Rows {
		note, ok := wantRows[row.SubjectLabel]
		if !ok {
			continue
		}
		if !row.Unsupported || row.CurrentAmountLabel != "NT$ 0" || row.PreviousAmountLabel != "NT$ 0" || row.DeltaAmountLabel != "NT$ 0" || row.Note != note {
			t.Fatalf("fixed unsupported row %q = %+v", row.SubjectLabel, row)
		}
		delete(wantRows, row.SubjectLabel)
	}
	if len(wantRows) != 0 {
		t.Fatalf("missing fixed unsupported rows: %+v", wantRows)
	}
}

func TestExportOperationReportUsesCurrentMonthLiveRows(t *testing.T) {
	roomName := "101"
	repo := &reportRepositoryStub{
		liveOperationReport: &OperationReport{
			PropertyID:        testPropertyID,
			PropertyName:      "Demo Property",
			Year:              2026,
			Month:             5,
			PreviousBalance:   1000,
			MonthlyIncome:     12000,
			MonthlyExpense:    500,
			OwnerDistribution: 0,
			EndingBalance:     12500,
			PreviousRented:    8,
			NewRentals:        2,
			Terminations:      1,
			EndingRented:      9,
			ManagementLogRows: []OperationReportLogRow{
				{
					Date:     time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
					RoomName: &roomName,
					Summary:  "Repair sink",
				},
			},
		},
	}
	renderer := &recordingReportRenderer{html: []byte("<html>operation report</html>")}
	service := NewExportOperationReportService(repo, renderer, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	document, err := service.Execute(context.Background(), ExportOperationReportInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		Year:                2026,
		Month:               5,
		Format:              "html",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.liveOperationReportQuery == nil || repo.snapshotOperationReportQuery != nil {
		t.Fatalf("unexpected source queries: live=%+v snapshot=%+v", repo.liveOperationReportQuery, repo.snapshotOperationReportQuery)
	}
	if renderer.name != "operation_report.html" {
		t.Fatalf("renderer template = %q", renderer.name)
	}
	view, ok := renderer.data.(operationReportView)
	if !ok {
		t.Fatalf("renderer data = %T, want operationReportView", renderer.data)
	}
	if view.OwnerDistributionLabel != "NT$ 0" || view.EndingBalanceLabel != "NT$ 12,500" {
		t.Fatalf("unexpected financial labels: %+v", view)
	}
	if view.NewRentalsLabel != "2" || view.TerminationsLabel != "1" || len(view.ManagementLogRows) != 1 {
		t.Fatalf("unexpected occupancy/log view: %+v", view)
	}
	if document.Filename != "operation-report-demo-property-2026-05.html" {
		t.Fatalf("Filename = %q", document.Filename)
	}
}

type reportRepositoryStub struct {
	pendingMeterBills            []Bill
	pendingMeterQuery            *PendingMeterQuery
	propertyMeterHistoryRows     []PropertyMeterHistoryRow
	propertyMeterHistoryQuery    *MeterHistoryQuery
	roomMeterHistoryBills        []Bill
	roomMeterHistoryQuery        *MeterHistoryQuery
	summaries                    []FinancialReportSummary
	summaryQuery                 *FinancialReportSummaryQuery
	liveReport                   *FinancialReport
	liveReportQuery              *FinancialReportQuery
	liveReportErr                error
	snapshotReport               *FinancialReport
	snapshotReportQuery          *FinancialReportQuery
	snapshotReportErr            error
	tenantRosterRows             []TenantRosterRow
	tenantRosterQuery            *TenantRosterQuery
	billReceipt                  *BillReceipt
	billReceiptQuery             *BillReceiptQuery
	liveCashflow                 *MonthlyCashflow
	liveCashflowQuery            *MonthlyCashflowQuery
	liveCashflowErr              error
	snapshotCashflow             *MonthlyCashflow
	snapshotCashflowQuery        *MonthlyCashflowQuery
	snapshotCashflowErr          error
	openingBalance               int
	openingBalanceQuery          *MonthlyCashflowQuery
	liveProfitLoss               *ProfitLossPeriod
	liveProfitLossQuery          *ProfitLossQuery
	liveProfitLossErr            error
	snapshotProfitLoss           *ProfitLossPeriod
	snapshotProfitLossQuery      *ProfitLossQuery
	snapshotProfitLossErr        error
	liveOperationReport          *OperationReport
	liveOperationReportQuery     *OperationReportQuery
	liveOperationReportErr       error
	snapshotOperationReport      *OperationReport
	snapshotOperationReportQuery *OperationReportQuery
	snapshotOperationReportErr   error
	err                          error
}

func (s *reportRepositoryStub) ListPendingMeterBills(_ context.Context, query PendingMeterQuery) ([]Bill, error) {
	s.pendingMeterQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.pendingMeterBills, nil
}

func (s *reportRepositoryStub) ListPropertyMeterHistory(_ context.Context, query MeterHistoryQuery) ([]PropertyMeterHistoryRow, error) {
	s.propertyMeterHistoryQuery = &query
	if s.err != nil {
		return nil, s.err
	}
	return s.propertyMeterHistoryRows, nil
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

func (s *reportRepositoryStub) FindLiveMonthlyCashflow(_ context.Context, query MonthlyCashflowQuery) (*MonthlyCashflow, error) {
	s.liveCashflowQuery = &query
	if s.liveCashflowErr != nil {
		return nil, s.liveCashflowErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.liveCashflow, nil
}

func (s *reportRepositoryStub) FindSnapshotMonthlyCashflow(_ context.Context, query MonthlyCashflowQuery) (*MonthlyCashflow, error) {
	s.snapshotCashflowQuery = &query
	if s.snapshotCashflowErr != nil {
		return nil, s.snapshotCashflowErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshotCashflow, nil
}

func (s *reportRepositoryStub) CalculateMonthlyCashflowOpeningBalance(_ context.Context, query MonthlyCashflowQuery) (int, error) {
	s.openingBalanceQuery = &query
	if s.err != nil {
		return 0, s.err
	}
	return s.openingBalance, nil
}

func (s *reportRepositoryStub) FindLiveProfitLossPeriod(_ context.Context, query ProfitLossQuery) (*ProfitLossPeriod, error) {
	s.liveProfitLossQuery = &query
	if s.liveProfitLossErr != nil {
		return nil, s.liveProfitLossErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.liveProfitLoss, nil
}

func (s *reportRepositoryStub) FindSnapshotProfitLossPeriod(_ context.Context, query ProfitLossQuery) (*ProfitLossPeriod, error) {
	s.snapshotProfitLossQuery = &query
	if s.snapshotProfitLossErr != nil {
		return nil, s.snapshotProfitLossErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshotProfitLoss, nil
}

func (s *reportRepositoryStub) FindLiveOperationReport(_ context.Context, query OperationReportQuery) (*OperationReport, error) {
	s.liveOperationReportQuery = &query
	if s.liveOperationReportErr != nil {
		return nil, s.liveOperationReportErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.liveOperationReport, nil
}

func (s *reportRepositoryStub) FindSnapshotOperationReport(_ context.Context, query OperationReportQuery) (*OperationReport, error) {
	s.snapshotOperationReportQuery = &query
	if s.snapshotOperationReportErr != nil {
		return nil, s.snapshotOperationReportErr
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshotOperationReport, nil
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

func profitLossPeriodForTest(year int, month int) *ProfitLossPeriod {
	return &ProfitLossPeriod{
		PropertyID:   testPropertyID,
		PropertyName: "Demo Property",
		Year:         year,
		Month:        month,
		Rows:         []ProfitLossSourceRow{},
	}
}

func stringPtr(value string) *string {
	return &value
}

var _ ReportRepository = (*reportRepositoryStub)(nil)
var _ Clock = fixedClock{}
var _ domainevents.Publisher = (*recordingPublisher)(nil)
