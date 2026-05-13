package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	applease "stds_backend/internal/application/lease"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbleases "stds_backend/internal/platform/database/leases"
)

type leaseRepositoryAdapter struct {
	repo dbleases.CommandRepository
}

type leaseDepositAccountingAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a leaseDepositAccountingAdapter) CreateDepositAccountingEntry(ctx context.Context, tx *sql.Tx, params applease.DepositAccountingEntryParams) error {
	account, err := a.repo.FindPropertyAccountByPropertyID(ctx, tx, params.PropertyID)
	if err != nil {
		if errors.Is(err, dbbilling.ErrNotFound) {
			return applease.ErrPropertyAccountNotFound
		}

		return err
	}

	return a.repo.InsertAccountingEntry(ctx, tx, dbbilling.CreateAccountingEntryParams{
		PropertyAccountID:   account.ID,
		Category:            params.Category,
		AccountingTitleCode: params.AccountingTitleCode,
		Amount:              params.Amount,
		Description:         params.Description,
		SourceRef:           params.SourceRef,
		Year:                params.Year,
		Month:               params.Month,
		SourceDate:          params.SourceDate,
		TenantLabel:         params.TenantLabel,
		DisplayNote:         params.DisplayNote,
	})
}

func (a leaseRepositoryAdapter) FindTenantByID(ctx context.Context, tx *sql.Tx, tenantID string) (*applease.Tenant, error) {
	tenant, err := a.repo.FindTenantByID(ctx, tx, tenantID)
	if err != nil {
		switch err {
		case dbleases.ErrTenantNotFound:
			return nil, applease.ErrTenantNotFound
		default:
			return nil, err
		}
	}

	return &applease.Tenant{
		ID:     tenant.ID,
		Status: tenant.Status,
	}, nil
}

func (a leaseRepositoryAdapter) FindRoomByIDForUpdate(ctx context.Context, tx *sql.Tx, roomID string) (*applease.Room, error) {
	room, err := a.repo.FindRoomByIDForUpdate(ctx, tx, roomID)
	if err != nil {
		switch err {
		case dbleases.ErrRoomNotFound:
			return nil, applease.ErrRoomNotFound
		default:
			return nil, err
		}
	}

	return &applease.Room{
		ID:                               room.ID,
		PropertyID:                       room.PropertyID,
		Status:                           room.Status,
		DefaultElectricityBillingCadence: room.DefaultElectricityBillingCadence,
	}, nil
}

func (a leaseRepositoryAdapter) FindLeaseByIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*applease.Lease, error) {
	lease, err := a.repo.FindLeaseByIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) FindCheckoutSettlementContextForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) (*applease.CheckoutSettlementContext, error) {
	context, err := a.repo.FindCheckoutSettlementContextForUpdate(ctx, tx, leaseID)
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationCheckoutSettlementContext(context), nil
}

func (a leaseRepositoryAdapter) FindCheckoutSettlementContext(ctx context.Context, tx *sql.Tx, leaseID string) (*applease.CheckoutSettlementContext, error) {
	context, err := a.repo.FindCheckoutSettlementContext(ctx, tx, leaseID)
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationCheckoutSettlementContext(context), nil
}

func (a leaseRepositoryAdapter) CreateLease(ctx context.Context, tx *sql.Tx, params applease.CreateLeaseParams) (*applease.Lease, error) {
	lease, err := a.repo.CreateLease(ctx, tx, dbleases.CreateLeaseParams{
		TenantID:                  params.TenantID,
		RoomID:                    params.RoomID,
		PropertyID:                params.PropertyID,
		RentAmount:                params.RentAmount,
		StartDate:                 params.StartDate,
		EndDate:                   params.EndDate,
		RentBillingCadence:        params.RentBillingCadence,
		ElectricityBillingCadence: params.ElectricityBillingCadence,
		StartingMeterReading:      params.StartingMeterReading,
		DepositAmount:             params.DepositAmount,
		Notes:                     params.Notes,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) TerminateLease(ctx context.Context, tx *sql.Tx, params applease.TerminateLeaseParams) (*applease.Lease, error) {
	lease, err := a.repo.TerminateLease(ctx, tx, dbleases.TerminateLeaseParams{
		LeaseID:           params.LeaseID,
		EndDate:           params.EndDate,
		ActualMoveOutDate: params.ActualMoveOutDate,
		TerminationReason: params.TerminationReason,
		SettlementDetail:  params.SettlementDetail,
	})
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) ForceTerminateLease(ctx context.Context, tx *sql.Tx, params applease.ForceTerminateLeaseParams) (*applease.Lease, error) {
	lease, err := a.repo.ForceTerminateLease(ctx, tx, dbleases.ForceTerminateLeaseParams{
		LeaseID:           params.LeaseID,
		TerminationDate:   params.TerminationDate,
		ActualMoveOutDate: params.ActualMoveOutDate,
		TerminationReason: params.TerminationReason,
		DepositStatus:     params.DepositStatus,
	})
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) ListBillsByLeaseIDForUpdate(ctx context.Context, tx *sql.Tx, leaseID string) ([]applease.Bill, error) {
	bills, err := a.repo.ListBillsByLeaseIDForUpdate(ctx, tx, leaseID)
	if err != nil {
		return nil, err
	}

	items := make([]applease.Bill, 0, len(bills))
	for _, bill := range bills {
		items = append(items, applease.Bill{
			ID:          bill.ID,
			Type:        bill.Type,
			Status:      bill.Status,
			PeriodStart: bill.PeriodStart,
			PeriodEnd:   bill.PeriodEnd,
		})
	}

	return items, nil
}

func (a leaseRepositoryAdapter) CreateForceTermination(ctx context.Context, tx *sql.Tx, params applease.CreateForceTerminationParams) (*applease.ForceTermination, error) {
	forceTermination, err := a.repo.CreateForceTermination(ctx, tx, dbleases.CreateForceTerminationParams{
		LeaseID:         params.LeaseID,
		InitiatedBy:     params.InitiatedBy,
		Reason:          params.Reason,
		DepositHandling: params.DepositHandling,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationForceTermination(forceTermination), nil
}

func (a leaseRepositoryAdapter) CreateForceTerminationBills(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error {
	return a.repo.CreateForceTerminationBills(ctx, tx, forceTerminationID, billIDs)
}

func (a leaseRepositoryAdapter) WriteOffBills(ctx context.Context, tx *sql.Tx, billIDs []string, reason string) error {
	return a.repo.WriteOffBills(ctx, tx, billIDs, reason)
}

func (a leaseRepositoryAdapter) MarkForceTerminationBillsDone(ctx context.Context, tx *sql.Tx, forceTerminationID string, billIDs []string) error {
	return a.repo.MarkForceTerminationBillsDone(ctx, tx, forceTerminationID, billIDs)
}

func (a leaseRepositoryAdapter) CompleteForceTermination(ctx context.Context, tx *sql.Tx, forceTerminationID string) error {
	return a.repo.CompleteForceTermination(ctx, tx, forceTerminationID)
}

func (a leaseRepositoryAdapter) FindForceTerminationByID(ctx context.Context, tx *sql.Tx, forceTerminationID string) (*applease.ForceTermination, error) {
	forceTermination, err := a.repo.FindForceTerminationByID(ctx, tx, forceTerminationID)
	if err != nil {
		switch err {
		case dbleases.ErrForceTerminationNotFound:
			return nil, applease.ErrForceTerminationNotFound
		default:
			return nil, err
		}
	}

	return toApplicationForceTermination(forceTermination), nil
}

func (a leaseRepositoryAdapter) UpdateLeaseConditions(ctx context.Context, tx *sql.Tx, params applease.UpdateLeaseParams) (*applease.Lease, error) {
	lease, err := a.repo.UpdateLeaseConditions(ctx, tx, dbleases.UpdateLeaseParams{
		LeaseID:    params.LeaseID,
		RentAmount: params.RentAmount,
	})
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) SettleDeposit(ctx context.Context, tx *sql.Tx, params applease.SettleDepositParams) (*applease.Lease, error) {
	lease, err := a.repo.SettleDeposit(ctx, tx, dbleases.SettleDepositParams{
		LeaseID:                params.LeaseID,
		RefundAmount:           params.RefundAmount,
		DeductionAmount:        params.DeductionAmount,
		DepositDeductionReason: params.DepositDeductionReason,
	})
	if err != nil {
		switch err {
		case dbleases.ErrLeaseNotFound:
			return nil, applease.ErrLeaseNotFound
		default:
			return nil, err
		}
	}

	return toApplicationLease(lease), nil
}

func (a leaseRepositoryAdapter) HasLockedRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) (bool, error) {
	return a.repo.HasLockedRentBillsFromDueDate(ctx, tx, leaseID, dueDate)
}

func (a leaseRepositoryAdapter) VoidRentBillsFromDueDate(ctx context.Context, tx *sql.Tx, leaseID string, dueDate time.Time) error {
	return a.repo.VoidRentBillsFromDueDate(ctx, tx, leaseID, dueDate)
}

func (a leaseRepositoryAdapter) VoidBillsOverlappingOrAfter(ctx context.Context, tx *sql.Tx, leaseID string, boundary time.Time) error {
	return a.repo.VoidBillsOverlappingOrAfter(ctx, tx, leaseID, boundary)
}

func (a leaseRepositoryAdapter) CreateBills(ctx context.Context, tx *sql.Tx, params []applease.CreateBillParams) error {
	items := make([]dbleases.CreateBillParams, 0, len(params))
	for _, item := range params {
		items = append(items, dbleases.CreateBillParams{
			LeaseID:     item.LeaseID,
			TenantID:    item.TenantID,
			RoomID:      item.RoomID,
			PropertyID:  item.PropertyID,
			Type:        item.Type,
			Amount:      item.Amount,
			PeriodStart: item.PeriodStart,
			PeriodEnd:   item.PeriodEnd,
			DueDate:     item.DueDate,
			Status:      item.Status,
		})
	}

	return a.repo.CreateBills(ctx, tx, items)
}

func (a leaseRepositoryAdapter) MarkRoomOccupied(ctx context.Context, tx *sql.Tx, roomID string) error {
	return a.repo.MarkRoomOccupied(ctx, tx, roomID)
}

func (a leaseRepositoryAdapter) MarkRoomVacant(ctx context.Context, tx *sql.Tx, roomID string) error {
	return a.repo.MarkRoomVacant(ctx, tx, roomID)
}

func (a leaseRepositoryAdapter) ActivateTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	return a.repo.ActivateTenant(ctx, tx, tenantID)
}

func (a leaseRepositoryAdapter) DeactivateTenantIfNoActiveLeases(ctx context.Context, tx *sql.Tx, tenantID string) error {
	return a.repo.DeactivateTenantIfNoActiveLeases(ctx, tx, tenantID)
}

func toApplicationCheckoutSettlementContext(context *dbleases.CheckoutSettlementContext) *applease.CheckoutSettlementContext {
	if context == nil {
		return nil
	}
	return &applease.CheckoutSettlementContext{
		Lease:        *toApplicationLease(&context.Lease),
		PropertyName: context.PropertyName,
		RoomName:     context.RoomName,
		TenantName:   context.TenantName,
	}
}

func toApplicationForceTermination(forceTermination *dbleases.ForceTermination) *applease.ForceTermination {
	bills := make([]applease.ForceTerminationBill, 0, len(forceTermination.Bills))
	for _, bill := range forceTermination.Bills {
		bills = append(bills, applease.ForceTerminationBill{
			BillID:      bill.BillID,
			Status:      bill.Status,
			Type:        bill.Type,
			PeriodStart: bill.PeriodStart,
			PeriodEnd:   bill.PeriodEnd,
			PeriodLabel: bill.PeriodLabel,
		})
	}

	return &applease.ForceTermination{
		ID:               forceTermination.ID,
		LeaseID:          forceTermination.LeaseID,
		PropertyID:       forceTermination.PropertyID,
		RoomID:           forceTermination.RoomID,
		TenantID:         forceTermination.TenantID,
		PropertyLabel:    forceTermination.PropertyLabel,
		RoomLabel:        forceTermination.RoomLabel,
		TenantLabel:      forceTermination.TenantLabel,
		InitiatedByLabel: forceTermination.InitiatedByLabel,
		Status:           forceTermination.Status,
		InitiatedBy:      forceTermination.InitiatedBy,
		Reason:           forceTermination.Reason,
		DepositHandling:  forceTermination.DepositHandling,
		Bills:            bills,
		CreatedAt:        forceTermination.CreatedAt,
		UpdatedAt:        forceTermination.UpdatedAt,
	}
}

func toApplicationLease(lease *dbleases.Lease) *applease.Lease {
	return &applease.Lease{
		ID:                        lease.ID,
		TenantID:                  lease.TenantID,
		PropertyID:                lease.PropertyID,
		RoomID:                    lease.RoomID,
		RentAmount:                lease.RentAmount,
		StartDate:                 lease.StartDate,
		EndDate:                   lease.EndDate,
		ActualMoveOutDate:        lease.ActualMoveOutDate,
		RentBillingCadence:        lease.RentBillingCadence,
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
		StartingMeterReading:      lease.StartingMeterReading,
		Status:                    lease.Status,
		DepositAmount:             lease.DepositAmount,
		DepositRefundAmount:       lease.DepositRefundAmount,
		DepositDeductionAmount:    lease.DepositDeductionAmount,
		DepositStatus:             lease.DepositStatus,
		DepositDeductionReason:    lease.DepositDeductionReason,
		Notes:                     lease.Notes,
		TerminationReason:         lease.TerminationReason,
		SettlementDetail:          lease.SettlementDetail,
		CreatedAt:                 lease.CreatedAt,
		UpdatedAt:                 lease.UpdatedAt,
		Version:                   lease.Version,
	}
}
