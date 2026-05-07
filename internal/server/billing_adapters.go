package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	appbilling "stds_backend/internal/application/billing"
	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/http/handler"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/reporthtml"
)

func newBillingServices(db *sql.DB, txRunner *dbtxrunner.Runner, publisher domainevents.Publisher) handler.BillingServices {
	repo := dbbilling.NewRepository(db)
	billingRepo := billingRepositoryAdapter{repo: repo}
	reportRepo := billingReportRepositoryAdapter{repo: repo}
	accountingRepo := billingAccountingRepositoryAdapter{repo: repo}

	return handler.BillingServices{
		Query: billingQueryServiceAdapter{
			listBills: appbilling.NewListBillsService(billingRepo),
			getBill:   appbilling.NewGetBillService(billingRepo),
		},
		Meter:          billingMeterServiceAdapter{service: appbilling.NewRecordMeterService(billingRepo, txRunner)},
		Payment:        billingPaymentServiceAdapter{service: appbilling.NewRecordPaymentService(billingRepo, accountingRepo, txRunner)},
		PropertyMeters: billingPropertyMeterServiceAdapter{pendingMeters: appbilling.NewListPendingMeterService(reportRepo), meterHistory: appbilling.NewListPropertyMeterHistoryService(reportRepo)},
		RoomMeters:     billingRoomMeterServiceAdapter{meterHistory: appbilling.NewListRoomMeterHistoryService(reportRepo)},
		FinancialReports: billingFinancialReportServiceAdapter{
			summaries:    appbilling.NewListFinancialReportSummariesService(reportRepo),
			get:          appbilling.NewGetFinancialReportService(reportRepo, nil),
			send:         appbilling.NewSendFinancialReportService(reportRepo, publisher, nil),
			tenantRoster: appbilling.NewExportTenantRosterService(reportRepo, appbilling.MustNewTenantRosterRenderer(), nil),
			billReceipt:  appbilling.NewExportBillReceiptService(reportRepo, appbilling.MustNewBillReceiptRenderer()),
		},
	}
}

type billingQueryServiceAdapter struct {
	listBills *appbilling.ListBillsService
	getBill   *appbilling.GetBillService
}

func (a billingQueryServiceAdapter) ListBills(ctx context.Context, input handler.BillingListInput) ([]handler.BillingBill, error) {
	var status *string
	if input.Status != "" {
		status = &input.Status
	}

	bills, err := a.listBills.Execute(ctx, appbilling.ListBillsInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		LeaseID:             input.LeaseID,
		TenantID:            input.TenantID,
		Status:              status,
		Month:               input.Month,
		Limit:               input.Limit,
		Offset:              input.Offset,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBills(bills), nil
}

func (a billingQueryServiceAdapter) GetBill(ctx context.Context, input handler.BillingGetInput) (*handler.BillingBill, error) {
	bill, err := a.getBill.Execute(ctx, appbilling.GetBillInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		BillID:              input.BillID,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBill(*bill), nil
}

type billingMeterServiceAdapter struct {
	service *appbilling.RecordMeterService
}

func (a billingMeterServiceAdapter) SubmitBillMeter(ctx context.Context, input handler.BillingMeterInput) (*handler.BillingBill, error) {
	bill, err := a.service.Execute(ctx, appbilling.RecordMeterInput{
		ActorRole:      input.ActorRole,
		BillID:         input.BillID,
		CurrentReading: input.CurrentReading,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBill(*bill), nil
}

type billingPaymentServiceAdapter struct {
	service *appbilling.RecordPaymentService
}

func (a billingPaymentServiceAdapter) RecordBillPayment(ctx context.Context, input handler.BillingPaymentInput) (*handler.BillingBill, error) {
	bill, err := a.service.Execute(ctx, appbilling.RecordPaymentInput{
		ActorRole:     input.ActorRole,
		BillID:        input.BillID,
		PaidAmount:    input.PaidAmount,
		PaymentMethod: input.PaymentMethod,
		PaidAt:        input.PaidAt,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBill(*bill), nil
}

type billingPropertyMeterServiceAdapter struct {
	pendingMeters *appbilling.ListPendingMeterService
	meterHistory  *appbilling.ListPropertyMeterHistoryService
}

func (a billingPropertyMeterServiceAdapter) ListPropertyPendingMeters(ctx context.Context, input handler.BillingPropertyMetersInput) ([]handler.BillingBill, error) {
	bills, err := a.pendingMeters.Execute(ctx, appbilling.ListPendingMeterInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBills(bills), nil
}

func (a billingPropertyMeterServiceAdapter) ListPropertyMeterHistory(ctx context.Context, input handler.BillingPropertyMeterHistoryInput) ([]handler.BillingBill, error) {
	bills, err := a.meterHistory.Execute(ctx, appbilling.ListPropertyMeterHistoryInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		Year:                input.Year,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBills(bills), nil
}

type billingRoomMeterServiceAdapter struct {
	meterHistory *appbilling.ListRoomMeterHistoryService
}

func (a billingRoomMeterServiceAdapter) ListRoomMeterHistory(ctx context.Context, input handler.BillingRoomMeterHistoryInput) ([]handler.BillingBill, error) {
	bills, err := a.meterHistory.Execute(ctx, appbilling.ListRoomMeterHistoryInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		RoomID:              input.RoomID,
		Year:                input.Year,
		Month:               input.Month,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerBills(bills), nil
}

type billingFinancialReportServiceAdapter struct {
	summaries    *appbilling.ListFinancialReportSummariesService
	get          *appbilling.GetFinancialReportService
	send         *appbilling.SendFinancialReportService
	tenantRoster *appbilling.ExportTenantRosterService
	billReceipt  *appbilling.ExportBillReceiptService
}

func (a billingFinancialReportServiceAdapter) ListFinancialReportSummaries(ctx context.Context, input handler.BillingFinancialReportSummaryInput) ([]handler.BillingFinancialReportSummary, error) {
	summaries, err := a.summaries.Execute(ctx, appbilling.ListFinancialReportSummariesInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		Year:                input.Year,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerFinancialReportSummaries(summaries), nil
}

func (a billingFinancialReportServiceAdapter) GetFinancialReport(ctx context.Context, input handler.BillingFinancialReportInput) (*handler.BillingFinancialReport, error) {
	report, err := a.get.Execute(ctx, appbilling.GetFinancialReportInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		Year:                input.Year,
		Month:               input.Month,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerFinancialReport(*report), nil
}

func (a billingFinancialReportServiceAdapter) SendFinancialReport(ctx context.Context, input handler.BillingFinancialReportInput) (*handler.BillingFinancialReport, error) {
	report, err := a.send.Execute(ctx, appbilling.SendFinancialReportInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		Year:                input.Year,
		Month:               input.Month,
	})
	if err != nil {
		return nil, err
	}

	return toHandlerFinancialReport(*report), nil
}

func (a billingFinancialReportServiceAdapter) ExportTenantRoster(ctx context.Context, input handler.BillingTenantRosterInput) (*reporthtml.Document, error) {
	return a.tenantRoster.Execute(ctx, appbilling.ExportTenantRosterInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		PropertyID:          input.PropertyID,
		AsOf:                input.AsOf,
		IncludeVacant:       input.IncludeVacant,
		Format:              input.Format,
	})
}

func (a billingFinancialReportServiceAdapter) ExportBillReceipt(ctx context.Context, input handler.BillingReceiptInput) (*reporthtml.Document, error) {
	return a.billReceipt.Execute(ctx, appbilling.ExportBillReceiptInput{
		ActorRole:           input.ActorRole,
		ActorUserID:         input.ActorUserID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
		BillID:              input.BillID,
		Format:              input.Format,
	})
}

type billingRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a billingRepositoryAdapter) ListBills(ctx context.Context, query appbilling.ListBillsQuery) ([]appbilling.Bill, error) {
	var month *string
	if query.Month != nil {
		value := query.Month.Format("2006-01")
		month = &value
	}

	bills, err := a.repo.ListAccessible(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, dbbilling.BillFilter{
		PropertyID: query.PropertyID,
		LeaseID:    query.LeaseID,
		TenantID:   query.TenantID,
		Status:     query.Status,
		Month:      month,
		Limit:      query.Limit,
		Offset:     query.Offset,
	})
	if err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return toAppBills(bills), nil
}

func (a billingRepositoryAdapter) FindBillByID(ctx context.Context, query appbilling.GetBillQuery) (*appbilling.Bill, error) {
	bill, err := a.repo.FindByIDAccessible(ctx, query.BillID, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	})
	if err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return toAppBill(*bill), nil
}

func (a billingRepositoryAdapter) FindBillByIDForUpdate(ctx context.Context, tx *sql.Tx, billID string) (*appbilling.Bill, error) {
	bill, err := a.repo.GetBillForUpdate(ctx, tx, billID)
	if err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return toAppBill(*bill), nil
}

func (a billingRepositoryAdapter) FindPreviousElectricityReading(ctx context.Context, tx *sql.Tx, roomID string, beforePeriodStart time.Time) (int, error) {
	reading, err := a.repo.FindPreviousMeterReadingForUpdate(ctx, tx, roomID, beforePeriodStart)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return 0, nil
		}
		return 0, mapBillingRepositoryError(err)
	}

	return reading, nil
}

func (a billingRepositoryAdapter) FindPropertyElectricityUnitPrice(ctx context.Context, tx *sql.Tx, propertyID string) (*float64, error) {
	unitPrice, err := a.repo.FindPropertyElectricityUnitPriceForUpdate(ctx, tx, propertyID)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return nil, appbilling.ErrPropertyElectricityUnitPriceNotFound
		}
		return nil, mapBillingRepositoryError(err)
	}

	return unitPrice, nil
}

func (a billingRepositoryAdapter) UpdateBillMeter(ctx context.Context, tx *sql.Tx, params appbilling.UpdateMeterParams) (*appbilling.Bill, error) {
	if err := a.repo.UpdateMeter(ctx, tx, dbbilling.UpdateMeterParams{
		BillID:               params.BillID,
		Version:              params.ExpectedVersion,
		MeterPreviousReading: params.PreviousReading,
		MeterCurrentReading:  params.CurrentReading,
		MeterUnitPrice:       params.UnitPrice,
		MeterRecordedAt:      params.MeterRecordedAt,
		Amount:               params.Amount,
	}); err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return a.FindBillByIDForUpdate(ctx, tx, params.BillID)
}

func (a billingRepositoryAdapter) UpdateBillPayment(ctx context.Context, tx *sql.Tx, params appbilling.UpdatePaymentParams) (*appbilling.Bill, error) {
	if err := a.repo.UpdatePayment(ctx, tx, dbbilling.UpdatePaymentParams{
		BillID:        params.BillID,
		Version:       params.ExpectedVersion,
		PaymentMethod: params.PaymentMethod,
		PaidAmount:    params.PaidAmount,
		PaidAt:        params.PaidAt,
	}); err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return a.FindBillByIDForUpdate(ctx, tx, params.BillID)
}

type billingReportRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a billingReportRepositoryAdapter) ListPendingMeterBills(ctx context.Context, query appbilling.PendingMeterQuery) ([]appbilling.Bill, error) {
	bills, err := a.repo.ListPropertyPendingMeters(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppBills(bills), nil
}

func (a billingReportRepositoryAdapter) ListPropertyMeterHistory(ctx context.Context, query appbilling.MeterHistoryQuery) ([]appbilling.Bill, error) {
	bills, err := a.repo.ListPropertyMeterHistory(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID, query.Year)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppBills(bills), nil
}

func (a billingReportRepositoryAdapter) ListRoomMeterHistory(ctx context.Context, query appbilling.MeterHistoryQuery) ([]appbilling.Bill, error) {
	bills, err := a.repo.ListRoomMeterHistory(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.RoomID, query.Year, query.Month)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppBills(bills), nil
}

func (a billingReportRepositoryAdapter) ListFinancialReportSummaries(ctx context.Context, query appbilling.FinancialReportSummaryQuery) ([]appbilling.FinancialReportSummary, error) {
	summaries, err := a.repo.ListFinancialReportSummaries(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID, query.Year, query.CurrentYear, query.CurrentMonth)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppFinancialReportSummaries(summaries), nil
}

func (a billingReportRepositoryAdapter) FindLiveFinancialReport(ctx context.Context, query appbilling.FinancialReportQuery) (*appbilling.FinancialReport, error) {
	report, err := a.repo.GetFinancialReport(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID, query.Year, query.Month, true)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppFinancialReport(*report), nil
}

func (a billingReportRepositoryAdapter) FindSnapshotFinancialReport(ctx context.Context, query appbilling.FinancialReportQuery) (*appbilling.FinancialReport, error) {
	report, err := a.repo.GetFinancialReport(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID, query.Year, query.Month, false)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppFinancialReport(*report), nil
}

func (a billingReportRepositoryAdapter) ListTenantRosterRows(ctx context.Context, query appbilling.TenantRosterQuery) ([]appbilling.TenantRosterRow, error) {
	rows, err := a.repo.ListTenantRosterRows(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.PropertyID, query.AsOf, query.IncludeVacant)
	if err != nil {
		return nil, mapBillingReportRepositoryError(err)
	}

	return toAppTenantRosterRows(rows), nil
}

func (a billingReportRepositoryAdapter) FindBillReceipt(ctx context.Context, query appbilling.BillReceiptQuery) (*appbilling.BillReceipt, error) {
	receipt, err := a.repo.FindBillReceipt(ctx, dbbilling.Scope{
		Role:                query.ActorRole,
		UserID:              query.ActorUserID,
		AssignedPropertyIDs: query.AssignedPropertyIDs,
	}, query.BillID)
	if err != nil {
		return nil, mapBillingRepositoryError(err)
	}

	return toAppBillReceipt(receipt), nil
}

type billingAccountingRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a billingAccountingRepositoryAdapter) CreateAccountingEntry(ctx context.Context, tx *sql.Tx, params appbilling.AccountingEntryParams) error {
	account, err := a.repo.FindPropertyAccountByPropertyID(ctx, tx, params.PropertyID)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return appbilling.ErrPropertyAccountNotFound
		}

		return err
	}

	return a.repo.InsertAccountingEntry(ctx, tx, dbbilling.CreateAccountingEntryParams{
		PropertyAccountID: account.ID,
		Category:          params.Category,
		Amount:            params.Amount,
		Description:       params.Description,
		SourceRef:         params.SourceRef,
		Year:              params.Year,
		Month:             params.Month,
	})
}

func mapBillingRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dbbilling.ErrNotFound):
		return appbilling.ErrBillNotFound
	case errors.Is(err, dbbilling.ErrConcurrentUpdate):
		return appbilling.ErrConcurrentUpdateConflict
	default:
		return err
	}
}

func mapBillingReportRepositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, dbbilling.ErrNotFound):
		return appbilling.ErrFinancialReportNotFoundRepository
	default:
		return err
	}
}

func toAppBills(bills []dbbilling.Bill) []appbilling.Bill {
	result := make([]appbilling.Bill, 0, len(bills))
	for i := range bills {
		result = append(result, *toAppBill(bills[i]))
	}

	return result
}

func toAppBill(bill dbbilling.Bill) *appbilling.Bill {
	return &appbilling.Bill{
		ID:                   bill.ID,
		LeaseID:              bill.LeaseID,
		TenantID:             bill.TenantID,
		RoomID:               bill.RoomID,
		PropertyID:           bill.PropertyID,
		Type:                 bill.Type,
		Amount:               bill.Amount,
		PeriodStart:          bill.PeriodStart,
		PeriodEnd:            bill.PeriodEnd,
		DueDate:              bill.DueDate,
		Status:               bill.Status,
		PaymentMethod:        bill.PaymentMethod,
		PaidAt:               bill.PaidAt,
		PaidAmount:           bill.PaidAmount,
		MeterPreviousReading: bill.MeterPreviousReading,
		MeterCurrentReading:  bill.MeterCurrentReading,
		MeterUnitPrice:       bill.MeterUnitPrice,
		MeterRecordedAt:      bill.MeterRecordedAt,
		WrittenOffReason:     bill.WrittenOffReason,
		OverdueNoticeCount:   bill.OverdueNoticeCount,
		CreatedAt:            bill.CreatedAt,
		UpdatedAt:            bill.UpdatedAt,
		Version:              bill.Version,
	}
}

func toAppFinancialReportSummaries(summaries []dbbilling.FinancialReportSummary) []appbilling.FinancialReportSummary {
	result := make([]appbilling.FinancialReportSummary, 0, len(summaries))
	for i := range summaries {
		result = append(result, appbilling.FinancialReportSummary{
			Year:         summaries[i].Year,
			Month:        summaries[i].Month,
			TotalIncome:  summaries[i].TotalIncome,
			TotalExpense: summaries[i].TotalExpense,
			Net:          summaries[i].Net,
		})
	}

	return result
}

func toAppFinancialReport(report dbbilling.FinancialReport) *appbilling.FinancialReport {
	entries := make([]appbilling.FinancialReportEntry, 0, len(report.Entries))
	for i := range report.Entries {
		entries = append(entries, appbilling.FinancialReportEntry{
			Category:    report.Entries[i].Category,
			Description: report.Entries[i].Description,
			Amount:      report.Entries[i].Amount,
		})
	}

	return &appbilling.FinancialReport{
		PropertyID:   report.PropertyID,
		Year:         report.Year,
		Month:        report.Month,
		TotalIncome:  report.TotalIncome,
		TotalExpense: report.TotalExpense,
		Net:          report.Net,
		IsFinalized:  report.IsFinalized,
		Entries:      entries,
	}
}

func toHandlerBills(bills []appbilling.Bill) []handler.BillingBill {
	result := make([]handler.BillingBill, 0, len(bills))
	for i := range bills {
		result = append(result, *toHandlerBill(bills[i]))
	}

	return result
}

func toHandlerBill(bill appbilling.Bill) *handler.BillingBill {
	return &handler.BillingBill{
		ID:                   bill.ID,
		LeaseID:              bill.LeaseID,
		TenantID:             bill.TenantID,
		RoomID:               bill.RoomID,
		PropertyID:           bill.PropertyID,
		Type:                 bill.Type,
		Amount:               bill.Amount,
		PeriodStart:          bill.PeriodStart,
		PeriodEnd:            bill.PeriodEnd,
		DueDate:              bill.DueDate,
		Status:               bill.Status,
		PaymentMethod:        bill.PaymentMethod,
		PaidAt:               bill.PaidAt,
		PaidAmount:           bill.PaidAmount,
		MeterPreviousReading: bill.MeterPreviousReading,
		MeterCurrentReading:  bill.MeterCurrentReading,
		MeterUnitPrice:       bill.MeterUnitPrice,
		MeterRecordedAt:      bill.MeterRecordedAt,
		WrittenOffReason:     bill.WrittenOffReason,
		OverdueNoticeCount:   bill.OverdueNoticeCount,
		CreatedAt:            bill.CreatedAt,
		UpdatedAt:            bill.UpdatedAt,
		Version:              bill.Version,
	}
}

func toHandlerFinancialReportSummaries(summaries []appbilling.FinancialReportSummary) []handler.BillingFinancialReportSummary {
	result := make([]handler.BillingFinancialReportSummary, 0, len(summaries))
	for i := range summaries {
		result = append(result, handler.BillingFinancialReportSummary{
			Year:         summaries[i].Year,
			Month:        summaries[i].Month,
			TotalIncome:  summaries[i].TotalIncome,
			TotalExpense: summaries[i].TotalExpense,
			Net:          summaries[i].Net,
		})
	}

	return result
}

func toHandlerFinancialReport(report appbilling.FinancialReport) *handler.BillingFinancialReport {
	entries := make([]handler.BillingFinancialReportEntry, 0, len(report.Entries))
	for i := range report.Entries {
		entries = append(entries, handler.BillingFinancialReportEntry{
			Category:    report.Entries[i].Category,
			Description: report.Entries[i].Description,
			Amount:      report.Entries[i].Amount,
		})
	}

	return &handler.BillingFinancialReport{
		PropertyID:   report.PropertyID,
		Year:         report.Year,
		Month:        report.Month,
		TotalIncome:  report.TotalIncome,
		TotalExpense: report.TotalExpense,
		Net:          report.Net,
		IsFinalized:  report.IsFinalized,
		Entries:      entries,
	}
}

func toAppTenantRosterRows(rows []dbbilling.TenantRosterRow) []appbilling.TenantRosterRow {
	result := make([]appbilling.TenantRosterRow, 0, len(rows))
	for i := range rows {
		result = append(result, appbilling.TenantRosterRow{
			PropertyID:         rows[i].PropertyID,
			PropertyName:       rows[i].PropertyName,
			RoomID:             rows[i].RoomID,
			RoomName:           rows[i].RoomName,
			RoomStatus:         rows[i].RoomStatus,
			LeaseID:            rows[i].LeaseID,
			TenantName:         rows[i].TenantName,
			TenantPhone:        rows[i].TenantPhone,
			NextRentDueDate:    rows[i].NextRentDueDate,
			RentBillingCadence: rows[i].RentBillingCadence,
			RentAmount:         rows[i].RentAmount,
		})
	}
	return result
}

func toAppBillReceipt(receipt *dbbilling.BillReceipt) *appbilling.BillReceipt {
	if receipt == nil {
		return nil
	}
	return &appbilling.BillReceipt{
		BillID:               receipt.BillID,
		BillType:             receipt.BillType,
		BillStatus:           receipt.BillStatus,
		Amount:               receipt.Amount,
		PaidAmount:           receipt.PaidAmount,
		PeriodStart:          receipt.PeriodStart,
		PeriodEnd:            receipt.PeriodEnd,
		MeterPreviousReading: receipt.MeterPreviousReading,
		MeterCurrentReading:  receipt.MeterCurrentReading,
		MeterUnitPrice:       receipt.MeterUnitPrice,
		PropertyName:         receipt.PropertyName,
		RoomName:             receipt.RoomName,
		TenantName:           receipt.TenantName,
	}
}
