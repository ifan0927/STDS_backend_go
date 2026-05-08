package billing

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainbilling "stds_backend/internal/domain/billing"
	domainevents "stds_backend/internal/domain/events"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

const (
	testBillID     = "30000000-0000-0000-0000-000000000001"
	testLeaseID    = "40000000-0000-0000-0000-000000000001"
	testTenantID   = "50000000-0000-0000-0000-000000000001"
	testRoomID     = "20000000-0000-0000-0000-000000000001"
	testPropertyID = "10000000-0000-0000-0000-000000000001"
)

func TestGetBillServicePassesActorScopeToRepository(t *testing.T) {
	repo := &billingRepositoryStub{bill: billForCommand(domainbilling.TypeRent, domainbilling.StatusPendingPayment, intPtr(12000))}
	service := NewGetBillService(repo)

	bill, err := service.Execute(context.Background(), GetBillInput{
		ActorRole:           "staff",
		ActorUserID:         "user-1",
		AssignedPropertyIDs: []string{testPropertyID},
		BillID:              testBillID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if bill == nil || bill.ID != testBillID {
		t.Fatalf("unexpected bill: %+v", bill)
	}
	if repo.getBillQuery == nil {
		t.Fatal("expected FindBillByID query")
	}
	if repo.getBillQuery.ActorRole != "staff" || repo.getBillQuery.ActorUserID != "user-1" || repo.getBillQuery.BillID != testBillID {
		t.Fatalf("unexpected query: %+v", repo.getBillQuery)
	}
	if len(repo.getBillQuery.AssignedPropertyIDs) != 1 || repo.getBillQuery.AssignedPropertyIDs[0] != testPropertyID {
		t.Fatalf("unexpected assigned properties: %+v", repo.getBillQuery.AssignedPropertyIDs)
	}
}

func TestListBillsServiceRejectsInvalidPagination(t *testing.T) {
	tests := []struct {
		name  string
		input ListBillsInput
		field string
	}{
		{
			name:  "zero limit",
			input: ListBillsInput{ActorRole: "staff", Limit: 0},
			field: "limit",
		},
		{
			name:  "limit too large",
			input: ListBillsInput{ActorRole: "staff", Limit: 101},
			field: "limit",
		},
		{
			name:  "negative offset",
			input: ListBillsInput{ActorRole: "staff", Limit: 10, Offset: -1},
			field: "offset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &billingRepositoryStub{}
			service := NewListBillsService(repo)

			_, err := service.Execute(context.Background(), tt.input)
			assertAppErrorCode(t, err, apperr.CodeBadRequest)
			assertAppErrorDetails(t, err, map[string]any{"field": tt.field})
			if repo.listBillsQuery != nil {
				t.Fatalf("unexpected ListBills call: %+v", repo.listBillsQuery)
			}
		})
	}
}

func TestListBillsServiceForwardsValidatedPagination(t *testing.T) {
	repo := &billingRepositoryStub{}
	service := NewListBillsService(repo)

	_, err := service.Execute(context.Background(), ListBillsInput{
		ActorRole: "staff",
		Limit:     25,
		Offset:    50,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.listBillsQuery == nil {
		t.Fatal("expected ListBills query")
	}
	if repo.listBillsQuery.Limit != 25 || repo.listBillsQuery.Offset != 50 {
		t.Fatalf("pagination = limit %d offset %d, want 25/50", repo.listBillsQuery.Limit, repo.listBillsQuery.Offset)
	}
}

func TestRecordMeterServiceRecordsMeterAndPublishesEventAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	recordedAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	repo := &billingRepositoryStub{
		billForUpdate:        billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingMeter, nil),
		previousReading:      1250,
		electricityUnitPrice: floatPtr(4.5),
		updatedMeterBill:     billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingPayment, intPtr(585)),
	}
	publisher := &recordingPublisher{}
	service := NewRecordMeterService(repo, dbtxrunner.New(db, publisher))

	bill, err := service.Execute(context.Background(), RecordMeterInput{
		ActorRole:      "staff",
		BillID:         testBillID,
		CurrentReading: 1380,
		RecordedAt:     &recordedAt,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if bill == nil || bill.Status != domainbilling.StatusPendingPayment || bill.Amount == nil || *bill.Amount != 585 {
		t.Fatalf("unexpected bill: %+v", bill)
	}
	if repo.previousLookupRoomID != testRoomID || !repo.previousLookupBeforePeriodStart.Equal(repo.billForUpdate.PeriodStart) {
		t.Fatalf("previous lookup = room %q before %v, want room %q before %v", repo.previousLookupRoomID, repo.previousLookupBeforePeriodStart, testRoomID, repo.billForUpdate.PeriodStart)
	}
	if repo.previousLookupTx == nil {
		t.Fatal("expected previous reading lookup to receive transaction")
	}
	if repo.unitPriceLookupTx == nil {
		t.Fatal("expected unit price lookup to receive transaction")
	}
	if repo.updateMeterParams == nil {
		t.Fatal("expected UpdateBillMeter call")
	}
	if repo.updateMeterParams.PreviousReading != 1250 || repo.updateMeterParams.CurrentReading != 1380 || repo.updateMeterParams.Amount != 585 {
		t.Fatalf("unexpected meter update params: %+v", repo.updateMeterParams)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.MeterRecorded)
	if !ok {
		t.Fatalf("expected MeterRecorded event, got %T", publisher.events[0])
	}
	if event.BillID != testBillID || event.PreviousReading != 1250 || event.CurrentReading != 1380 || event.Amount != 585 {
		t.Fatalf("unexpected event: %+v", event)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestRecordMeterServiceRejectsBusinessRulesAndRollsBackWithoutEvent(t *testing.T) {
	tests := []struct {
		name           string
		bill           *Bill
		previous       int
		current        int
		expectedCode   string
		expectedDetail map[string]any
	}{
		{
			name:         "lower than previous",
			bill:         billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingMeter, nil),
			previous:     1380,
			current:      1250,
			expectedCode: CodeMeterReadingLessThanPrevious,
			expectedDetail: map[string]any{
				"previous_reading":  1380,
				"submitted_reading": 1250,
			},
		},
		{
			name:         "non electricity bill",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusPendingPayment, intPtr(12000)),
			previous:     0,
			current:      1250,
			expectedCode: CodeBillNotElectricityType,
		},
		{
			name:         "electricity bill not pending meter",
			bill:         billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingPayment, nil),
			previous:     0,
			current:      1250,
			expectedCode: CodeBillStatusNotRecordable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectBegin()
			mock.ExpectRollback()

			repo := &billingRepositoryStub{
				billForUpdate:        tt.bill,
				previousReading:      tt.previous,
				electricityUnitPrice: floatPtr(4.5),
				updatedMeterBill:     billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingPayment, intPtr(585)),
			}
			publisher := &recordingPublisher{}
			service := NewRecordMeterService(repo, dbtxrunner.New(db, publisher))

			_, err = service.Execute(context.Background(), RecordMeterInput{
				ActorRole:      "staff",
				BillID:         testBillID,
				CurrentReading: tt.current,
			})
			assertAppErrorCode(t, err, tt.expectedCode)
			if tt.expectedDetail != nil {
				assertAppErrorDetails(t, err, tt.expectedDetail)
			}
			if repo.updateMeterParams != nil {
				t.Fatalf("unexpected UpdateBillMeter call: %+v", repo.updateMeterParams)
			}
			if len(publisher.events) != 0 {
				t.Fatalf("expected no events, got %d", len(publisher.events))
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestRecordMeterServiceMapsMissingUnitPriceDependency(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	repo := &billingRepositoryStub{
		billForUpdate:    billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingMeter, nil),
		previousReading:  1250,
		unitPriceErr:     ErrPropertyElectricityUnitPriceNotFound,
		updatedMeterBill: billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingPayment, intPtr(585)),
	}
	publisher := &recordingPublisher{}
	service := NewRecordMeterService(repo, dbtxrunner.New(db, publisher))

	_, err = service.Execute(context.Background(), RecordMeterInput{
		ActorRole:      "staff",
		BillID:         testBillID,
		CurrentReading: 1380,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
	assertAppErrorDetails(t, err, map[string]any{"dependency": "electricity_unit_price"})
	if repo.updateMeterParams != nil {
		t.Fatalf("unexpected UpdateBillMeter call: %+v", repo.updateMeterParams)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("expected no events, got %d", len(publisher.events))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestRecordPaymentServiceRecordsPaymentAndPublishesEventAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	paidAt := time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)
	repo := &billingRepositoryStub{
		billForUpdate:      billForCommand(domainbilling.TypeRent, domainbilling.StatusPendingPayment, intPtr(12000)),
		updatedPaymentBill: billForCommand(domainbilling.TypeRent, domainbilling.StatusPaid, intPtr(12000)),
	}
	accountingRepo := &accountingRepositoryStub{}
	publisher := &recordingPublisher{}
	service := NewRecordPaymentService(repo, accountingRepo, dbtxrunner.New(db, publisher))

	bill, err := service.Execute(context.Background(), RecordPaymentInput{
		ActorRole:     "organizer",
		BillID:        testBillID,
		PaidAmount:    12000,
		PaymentMethod: "transfer",
		PaidAt:        &paidAt,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if bill == nil || bill.Status != domainbilling.StatusPaid {
		t.Fatalf("unexpected bill: %+v", bill)
	}
	if repo.updatePaymentParams == nil || repo.updatePaymentParams.PaidAmount != 12000 || repo.updatePaymentParams.PaymentMethod != "transfer" {
		t.Fatalf("unexpected payment update params: %+v", repo.updatePaymentParams)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(publisher.events))
	}
	event, ok := publisher.events[0].(domainevents.BillPaid)
	if !ok {
		t.Fatalf("expected BillPaid event, got %T", publisher.events[0])
	}
	if event.BillID != testBillID || event.Amount != 12000 || event.PaidAmount != 12000 || event.PaymentMethod != "transfer" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if accountingRepo.entry == nil {
		t.Fatal("expected accounting entry")
	}
	if accountingRepo.entry.PropertyID != testPropertyID || accountingRepo.entry.Category != AccountingCategoryRentPayment || accountingRepo.entry.AccountingTitleCode != AccountingTitleCodeRentPayment || accountingRepo.entry.Amount != 12000 {
		t.Fatalf("unexpected accounting entry: %+v", accountingRepo.entry)
	}
	if accountingRepo.entry.SourceRef["type"] != "BillPaid" || accountingRepo.entry.SourceRef["bill_id"] != testBillID {
		t.Fatalf("unexpected source ref: %+v", accountingRepo.entry.SourceRef)
	}
	if accountingRepo.entry.SourceDate == nil {
		t.Fatal("expected accounting source date")
	}
	if accountingRepo.entry.RoomLabel != nil || accountingRepo.entry.TenantLabel != nil {
		t.Fatalf("unexpected unavailable labels: room=%v tenant=%v", accountingRepo.entry.RoomLabel, accountingRepo.entry.TenantLabel)
	}
	if accountingRepo.entry.PeriodLabel == nil || *accountingRepo.entry.PeriodLabel != "2026-03-15 至 2026-04-14" {
		t.Fatalf("period label = %v, want 2026-03-15 至 2026-04-14", accountingRepo.entry.PeriodLabel)
	}
	if accountingRepo.entry.DisplayNote == nil || *accountingRepo.entry.DisplayNote != "租金：2026-03-15 至 2026-04-14" {
		t.Fatalf("display note = %v, want rent period note", accountingRepo.entry.DisplayNote)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestRecordPaymentServiceAllowsOverdueBill(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	repo := &billingRepositoryStub{
		billForUpdate:      billForCommand(domainbilling.TypeRent, domainbilling.StatusOverdue, intPtr(12000)),
		updatedPaymentBill: billForCommand(domainbilling.TypeRent, domainbilling.StatusPaid, intPtr(12000)),
	}
	service := NewRecordPaymentService(repo, &accountingRepositoryStub{}, dbtxrunner.New(db, nil))

	_, err = service.Execute(context.Background(), RecordPaymentInput{
		ActorRole:     "staff",
		BillID:        testBillID,
		PaidAmount:    12000,
		PaymentMethod: "cash",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestAccountingSourceDateUsesTaiwanReportDate(t *testing.T) {
	sourceDate := accountingSourceDate(time.Date(2026, 4, 30, 16, 30, 0, 0, time.UTC))
	if sourceDate.Year() != 2026 || sourceDate.Month() != time.May || sourceDate.Day() != 1 {
		t.Fatalf("source date = %s, want 2026-05-01 in Taiwan", sourceDate)
	}
	if sourceDate.Location().String() != taiwanReportLocation.String() {
		t.Fatalf("source date location = %s, want %s", sourceDate.Location(), taiwanReportLocation)
	}
}

func TestRecordPaymentServiceRejectsBusinessRulesAndRollsBackWithoutEvent(t *testing.T) {
	tests := []struct {
		name           string
		bill           *Bill
		paidAmount     int
		expectedCode   string
		expectedDetail map[string]any
	}{
		{
			name:         "already paid",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusPaid, intPtr(12000)),
			paidAmount:   12000,
			expectedCode: CodeBillAlreadyPaid,
		},
		{
			name:         "pending meter",
			bill:         billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingMeter, nil),
			paidAmount:   12000,
			expectedCode: CodeBillStatusNotPayable,
		},
		{
			name:         "voided",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusVoided, intPtr(12000)),
			paidAmount:   12000,
			expectedCode: CodeBillStatusNotPayable,
		},
		{
			name:         "written off",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusWrittenOff, intPtr(12000)),
			paidAmount:   12000,
			expectedCode: CodeBillStatusNotPayable,
		},
		{
			name:         "missing amount",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusPendingPayment, nil),
			paidAmount:   12000,
			expectedCode: CodeBillStatusNotPayable,
		},
		{
			name:         "amount mismatch",
			bill:         billForCommand(domainbilling.TypeRent, domainbilling.StatusPendingPayment, intPtr(12000)),
			paidAmount:   11000,
			expectedCode: CodeBillPaidAmountMismatch,
			expectedDetail: map[string]any{
				"expected_amount":  12000,
				"submitted_amount": 11000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectBegin()
			mock.ExpectRollback()

			repo := &billingRepositoryStub{
				billForUpdate:      tt.bill,
				updatedPaymentBill: billForCommand(domainbilling.TypeRent, domainbilling.StatusPaid, intPtr(12000)),
			}
			publisher := &recordingPublisher{}
			accountingRepo := &accountingRepositoryStub{}
			service := NewRecordPaymentService(repo, accountingRepo, dbtxrunner.New(db, publisher))

			_, err = service.Execute(context.Background(), RecordPaymentInput{
				ActorRole:     "staff",
				BillID:        testBillID,
				PaidAmount:    tt.paidAmount,
				PaymentMethod: "cash",
			})
			assertAppErrorCode(t, err, tt.expectedCode)
			if tt.expectedDetail != nil {
				assertAppErrorDetails(t, err, tt.expectedDetail)
			}
			if repo.updatePaymentParams != nil {
				t.Fatalf("unexpected UpdateBillPayment call: %+v", repo.updatePaymentParams)
			}
			if len(publisher.events) != 0 {
				t.Fatalf("expected no events, got %d", len(publisher.events))
			}
			if accountingRepo.entry != nil {
				t.Fatalf("unexpected accounting entry: %+v", accountingRepo.entry)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestRecordPaymentServiceRollsBackWhenAccountingEntryFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	paidAt := time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC)
	repo := &billingRepositoryStub{
		billForUpdate:      billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPendingPayment, intPtr(585)),
		updatedPaymentBill: billForCommand(domainbilling.TypeElectricity, domainbilling.StatusPaid, intPtr(585)),
	}
	accountingRepo := &accountingRepositoryStub{err: errors.New("insert accounting entry")}
	publisher := &recordingPublisher{}
	service := NewRecordPaymentService(repo, accountingRepo, dbtxrunner.New(db, publisher))

	_, err = service.Execute(context.Background(), RecordPaymentInput{
		ActorRole:     "staff",
		BillID:        testBillID,
		PaidAmount:    585,
		PaymentMethod: "cash",
		PaidAt:        &paidAt,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
	if repo.updatePaymentParams == nil {
		t.Fatal("expected bill payment update before accounting failure")
	}
	if accountingRepo.entry == nil {
		t.Fatal("expected attempted accounting entry")
	}
	if accountingRepo.entry.Category != AccountingCategoryElectricityPayment || accountingRepo.entry.AccountingTitleCode != AccountingTitleCodeElectricityPayment || accountingRepo.entry.Amount != 585 {
		t.Fatalf("unexpected accounting entry: %+v", accountingRepo.entry)
	}
	if accountingRepo.entry.DisplayNote == nil || *accountingRepo.entry.DisplayNote != "電費：2026-03-15 至 2026-04-14" {
		t.Fatalf("display note = %v, want electricity period note", accountingRepo.entry.DisplayNote)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("expected no committed events, got %d", len(publisher.events))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

type billingRepositoryStub struct {
	bills                           []Bill
	listBillsQuery                  *ListBillsQuery
	bill                            *Bill
	getBillQuery                    *GetBillQuery
	billForUpdate                   *Bill
	billErr                         error
	previousReading                 int
	previousErr                     error
	previousLookupTx                *sql.Tx
	previousLookupRoomID            string
	previousLookupBeforePeriodStart time.Time
	electricityUnitPrice            *float64
	unitPriceLookupTx               *sql.Tx
	unitPriceErr                    error
	updatedMeterBill                *Bill
	updateMeterParams               *UpdateMeterParams
	updateMeterErr                  error
	updatedPaymentBill              *Bill
	updatePaymentParams             *UpdatePaymentParams
	updatePaymentErr                error
}

func (s *billingRepositoryStub) ListBills(_ context.Context, query ListBillsQuery) ([]Bill, error) {
	s.listBillsQuery = &query
	return s.bills, nil
}

func (s *billingRepositoryStub) FindBillByID(_ context.Context, query GetBillQuery) (*Bill, error) {
	s.getBillQuery = &query
	if s.billErr != nil {
		return nil, s.billErr
	}
	return s.bill, nil
}

func (s *billingRepositoryStub) FindBillByIDForUpdate(_ context.Context, _ *sql.Tx, _ string) (*Bill, error) {
	if s.billErr != nil {
		return nil, s.billErr
	}
	return s.billForUpdate, nil
}

func (s *billingRepositoryStub) FindPreviousElectricityReading(_ context.Context, tx *sql.Tx, roomID string, beforePeriodStart time.Time) (int, error) {
	s.previousLookupTx = tx
	s.previousLookupRoomID = roomID
	s.previousLookupBeforePeriodStart = beforePeriodStart
	if s.previousErr != nil {
		return 0, s.previousErr
	}
	return s.previousReading, nil
}

func (s *billingRepositoryStub) FindPropertyElectricityUnitPrice(_ context.Context, tx *sql.Tx, _ string) (*float64, error) {
	s.unitPriceLookupTx = tx
	if s.unitPriceErr != nil {
		return nil, s.unitPriceErr
	}
	return s.electricityUnitPrice, nil
}

func (s *billingRepositoryStub) UpdateBillMeter(_ context.Context, _ *sql.Tx, params UpdateMeterParams) (*Bill, error) {
	s.updateMeterParams = &params
	if s.updateMeterErr != nil {
		return nil, s.updateMeterErr
	}
	return s.updatedMeterBill, nil
}

func (s *billingRepositoryStub) UpdateBillPayment(_ context.Context, _ *sql.Tx, params UpdatePaymentParams) (*Bill, error) {
	s.updatePaymentParams = &params
	if s.updatePaymentErr != nil {
		return nil, s.updatePaymentErr
	}
	return s.updatedPaymentBill, nil
}

type accountingRepositoryStub struct {
	entry *AccountingEntryParams
	err   error
}

func (s *accountingRepositoryStub) CreateAccountingEntry(_ context.Context, _ *sql.Tx, params AccountingEntryParams) error {
	s.entry = &params
	if s.err != nil {
		return s.err
	}
	return nil
}

type recordingPublisher struct {
	events []any
}

func (p *recordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}

func billForCommand(billType string, status string, amount *int) *Bill {
	return &Bill{
		ID:          testBillID,
		LeaseID:     testLeaseID,
		TenantID:    testTenantID,
		RoomID:      testRoomID,
		PropertyID:  testPropertyID,
		Type:        billType,
		Amount:      amount,
		PeriodStart: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC),
		DueDate:     time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
		Status:      status,
		Version:     1,
	}
}

func assertAppErrorCode(t *testing.T, err error, expectedCode string) {
	t.Helper()

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected apperr.Error, got %T: %v", err, err)
	}
	if appErr.Code != expectedCode {
		t.Fatalf("error code = %q, want %q", appErr.Code, expectedCode)
	}
}

func assertAppErrorDetails(t *testing.T, err error, expected map[string]any) {
	t.Helper()

	var appErr *apperr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected apperr.Error, got %T: %v", err, err)
	}
	details, ok := appErr.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected details map, got %#v", appErr.Details)
	}
	for key, expectedValue := range expected {
		if details[key] != expectedValue {
			t.Fatalf("details[%s] = %#v, want %#v", key, details[key], expectedValue)
		}
	}
}

func floatPtr(value float64) *float64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}
