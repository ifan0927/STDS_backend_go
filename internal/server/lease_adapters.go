package server

import (
	"context"
	"database/sql"
	"time"

	applease "stds_backend/internal/application/lease"
	dbleases "stds_backend/internal/platform/database/leases"
)

type leaseRepositoryAdapter struct {
	repo dbleases.CommandRepository
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

func (a leaseRepositoryAdapter) CreateLease(ctx context.Context, tx *sql.Tx, params applease.CreateLeaseParams) (*applease.Lease, error) {
	lease, err := a.repo.CreateLease(ctx, tx, dbleases.CreateLeaseParams{
		TenantID:                  params.TenantID,
		RoomID:                    params.RoomID,
		PropertyID:                params.PropertyID,
		RentAmount:                params.RentAmount,
		StartDate:                 params.StartDate,
		EndDate:                   params.EndDate,
		ElectricityBillingCadence: params.ElectricityBillingCadence,
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
		TerminationReason: params.TerminationReason,
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

func (a leaseRepositoryAdapter) ActivateTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	return a.repo.ActivateTenant(ctx, tx, tenantID)
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
		ElectricityBillingCadence: lease.ElectricityBillingCadence,
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
