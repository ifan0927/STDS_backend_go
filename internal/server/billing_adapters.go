package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	appbilling "stds_backend/internal/application/billing"
	"stds_backend/internal/http/handler"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
)

func newBillingServices(db *sql.DB, txRunner *dbtxrunner.Runner) handler.BillingServices {
	repo := dbbilling.NewRepository(db)
	billingRepo := billingRepositoryAdapter{repo: repo}
	accountingRepo := billingAccountingRepositoryAdapter{repo: repo}

	return handler.BillingServices{
		Query: billingQueryServiceAdapter{
			listBills: appbilling.NewListBillsService(billingRepo),
			getBill:   appbilling.NewGetBillService(billingRepo),
		},
		Meter:   billingMeterServiceAdapter{service: appbilling.NewRecordMeterService(billingRepo, txRunner)},
		Payment: billingPaymentServiceAdapter{service: appbilling.NewRecordPaymentService(billingRepo, accountingRepo, txRunner)},
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
