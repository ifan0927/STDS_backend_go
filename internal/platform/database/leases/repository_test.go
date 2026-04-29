package leases

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindTenantByIDReturnsNotFoundWhenNoRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, status
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1`)).
		WithArgs("tenant-1").
		WillReturnError(sql.ErrNoRows)

	tenant, err := repo.FindTenantByID(context.Background(), tx, "tenant-1")
	if !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected ErrTenantNotFound, got tenant=%+v err=%v", tenant, err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindRoomByIDForUpdateLocksRoomAndScansDefaultCadence(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
	r.id,
	r.property_id,
	r.status,
	p.default_electricity_billing_cadence
FROM rooms r
JOIN properties p ON p.id = r.property_id
WHERE r.id = $1
  AND r.deleted_at IS NULL
  AND p.deleted_at IS NULL
FOR UPDATE`)).
		WithArgs("room-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "property_id", "status", "default_electricity_billing_cadence",
		}).AddRow("room-1", "property-1", "vacant", "monthly"))

	room, err := repo.FindRoomByIDForUpdate(context.Background(), tx, "room-1")
	if err != nil {
		t.Fatalf("FindRoomByIDForUpdate: %v", err)
	}
	if room.ID != "room-1" || room.PropertyID != "property-1" || room.Status != "vacant" || room.DefaultElectricityBillingCadence != "monthly" {
		t.Fatalf("unexpected room: %+v", room)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindLeaseByIDForUpdateScansNullableFieldsAndSettlementDetail(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version
FROM leases
WHERE id = $1
  AND deleted_at IS NULL
FOR UPDATE`)).
		WithArgs("lease-1").
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 25000, startDate, endDate, "monthly",
			"terminated", 50000, 30000, 20000, "settled", "cleaning", "renewal candidate",
			"tenant requested move-out", `{"deductions":[{"type":"cleaning","amount":20000}],"refund":30000}`,
			now, now, 7,
		))

	lease, err := repo.FindLeaseByIDForUpdate(context.Background(), tx, "lease-1")
	if err != nil {
		t.Fatalf("FindLeaseByIDForUpdate: %v", err)
	}
	if lease.DepositRefundAmount == nil || *lease.DepositRefundAmount != 30000 {
		t.Fatalf("unexpected deposit refund amount: %+v", lease.DepositRefundAmount)
	}
	if lease.DepositDeductionAmount == nil || *lease.DepositDeductionAmount != 20000 {
		t.Fatalf("unexpected deposit deduction amount: %+v", lease.DepositDeductionAmount)
	}
	if lease.DepositDeductionReason == nil || *lease.DepositDeductionReason != "cleaning" {
		t.Fatalf("unexpected deposit deduction reason: %+v", lease.DepositDeductionReason)
	}
	if lease.Notes == nil || *lease.Notes != "renewal candidate" {
		t.Fatalf("unexpected notes: %+v", lease.Notes)
	}
	if lease.TerminationReason == nil || *lease.TerminationReason != "tenant requested move-out" {
		t.Fatalf("unexpected termination reason: %+v", lease.TerminationReason)
	}
	if lease.SettlementDetail == nil || (*lease.SettlementDetail)["refund"] != float64(30000) {
		t.Fatalf("unexpected settlement detail: %+v", lease.SettlementDetail)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateLeasePersistsHeldDepositStatusAndNotes(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)
	notes := "prefers email"

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO leases (
	tenant_id,
	room_id,
	property_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	deposit_amount,
	deposit_status,
	notes
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'held', $9)
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version`)).
		WithArgs("tenant-1", "room-1", "property-1", 25000, startDate, endDate, "monthly", 50000, &notes).
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 25000, startDate, endDate, "monthly",
			"active", 50000, nil, nil, "held", nil, notes, nil, nil, now, now, 1,
		))

	lease, err := repo.CreateLease(context.Background(), tx, CreateLeaseParams{
		TenantID:                  "tenant-1",
		RoomID:                    "room-1",
		PropertyID:                "property-1",
		RentAmount:                25000,
		StartDate:                 startDate,
		EndDate:                   endDate,
		ElectricityBillingCadence: "monthly",
		DepositAmount:             50000,
		Notes:                     &notes,
	})
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	if lease.DepositStatus != "held" {
		t.Fatalf("expected held deposit status, got %q", lease.DepositStatus)
	}
	if lease.Notes == nil || *lease.Notes != notes {
		t.Fatalf("unexpected notes: %+v", lease.Notes)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestLeaseReturningCommandsMapNoRowsToNotFound(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, *SQLRepository, *sql.Tx) (*Lease, error)
	}{
		{
			name: "TerminateLease",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx) (*Lease, error) {
				endDate := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
				return repo.TerminateLease(ctx, tx, TerminateLeaseParams{
					LeaseID:           "lease-1",
					EndDate:           endDate,
					TerminationReason: "tenant requested",
				})
			},
		},
		{
			name: "ForceTerminateLease",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx) (*Lease, error) {
				return repo.ForceTerminateLease(ctx, tx, ForceTerminateLeaseParams{
					LeaseID:           "lease-1",
					TerminationReason: "breach",
					DepositStatus:     "forfeited",
				})
			},
		},
		{
			name: "UpdateLeaseConditions",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx) (*Lease, error) {
				return repo.UpdateLeaseConditions(ctx, tx, UpdateLeaseParams{
					LeaseID:    "lease-1",
					RentAmount: 28000,
				})
			},
		},
		{
			name: "SettleDeposit",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx) (*Lease, error) {
				reason := "cleaning"
				return repo.SettleDeposit(ctx, tx, SettleDepositParams{
					LeaseID:                "lease-1",
					RefundAmount:           30000,
					DeductionAmount:        20000,
					DepositDeductionReason: &reason,
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, repo := newLeaseRepoTest(t)
			defer closeLeaseDB(t, db)
			tx := beginLeaseTx(t, db, mock)

			mock.ExpectQuery("UPDATE leases").
				WillReturnError(sql.ErrNoRows)

			lease, err := tt.run(context.Background(), repo, tx)
			if !errors.Is(err, ErrLeaseNotFound) {
				t.Fatalf("expected ErrLeaseNotFound, got lease=%+v err=%v", lease, err)
			}

			mock.ExpectRollback()
			if err := tx.Rollback(); err != nil {
				t.Fatalf("Rollback: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestTerminateLeaseUpdatesFieldsAndScansReturnedLease(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE leases
SET status = 'terminated',
	end_date = $2,
	termination_reason = $3,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version`)).
		WithArgs("lease-1", endDate, "tenant requested").
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 25000, startDate, endDate, "monthly",
			"terminated", 50000, nil, nil, "held", nil, nil, "tenant requested", nil, now, now, 2,
		))

	lease, err := repo.TerminateLease(context.Background(), tx, TerminateLeaseParams{
		LeaseID:           "lease-1",
		EndDate:           endDate,
		TerminationReason: "tenant requested",
	})
	if err != nil {
		t.Fatalf("TerminateLease: %v", err)
	}
	if lease.Status != "terminated" || lease.TerminationReason == nil || *lease.TerminationReason != "tenant requested" {
		t.Fatalf("unexpected lease: %+v", lease)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestForceTerminateLeaseUpdatesReasonAndDepositStatus(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE leases
SET status = 'force_terminated',
	termination_reason = $2,
	deposit_status = $3,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version`)).
		WithArgs("lease-1", "breach", "forfeited").
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 25000, startDate, endDate, "monthly",
			"force_terminated", 50000, nil, nil, "forfeited", nil, nil, "breach", nil, now, now, 3,
		))

	lease, err := repo.ForceTerminateLease(context.Background(), tx, ForceTerminateLeaseParams{
		LeaseID:           "lease-1",
		TerminationReason: "breach",
		DepositStatus:     "forfeited",
	})
	if err != nil {
		t.Fatalf("ForceTerminateLease: %v", err)
	}
	if lease.Status != "force_terminated" || lease.DepositStatus != "forfeited" {
		t.Fatalf("unexpected lease: %+v", lease)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateLeaseConditionsUpdatesRentAmount(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE leases
SET rent_amount = $2,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version`)).
		WithArgs("lease-1", 28000).
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 28000, startDate, endDate, "monthly",
			"active", 50000, nil, nil, "held", nil, nil, nil, nil, now, now, 4,
		))

	lease, err := repo.UpdateLeaseConditions(context.Background(), tx, UpdateLeaseParams{
		LeaseID:    "lease-1",
		RentAmount: 28000,
	})
	if err != nil {
		t.Fatalf("UpdateLeaseConditions: %v", err)
	}
	if lease.RentAmount != 28000 || lease.Version != 4 {
		t.Fatalf("unexpected lease: %+v", lease)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSettleDepositUpdatesSettlementFields(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2027, 4, 30, 0, 0, 0, 0, time.UTC)
	reason := "cleaning"

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE leases
SET deposit_status = 'settled',
	deposit_refund_amount = $2,
	deposit_deduction_amount = $3,
	deposit_deduction_reason = $4,
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND deleted_at IS NULL
RETURNING
	id,
	tenant_id,
	property_id,
	room_id,
	rent_amount,
	start_date,
	end_date,
	electricity_billing_cadence,
	status,
	deposit_amount,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_status,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail::text,
	created_at,
	updated_at,
	version`)).
		WithArgs("lease-1", 30000, 20000, reason).
		WillReturnRows(newLeaseRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", 25000, startDate, endDate, "monthly",
			"terminated", 50000, 30000, 20000, "settled", reason, nil, nil, nil, now, now, 5,
		))

	lease, err := repo.SettleDeposit(context.Background(), tx, SettleDepositParams{
		LeaseID:                "lease-1",
		RefundAmount:           30000,
		DeductionAmount:        20000,
		DepositDeductionReason: &reason,
	})
	if err != nil {
		t.Fatalf("SettleDeposit: %v", err)
	}
	if lease.DepositStatus != "settled" || lease.DepositRefundAmount == nil || *lease.DepositRefundAmount != 30000 {
		t.Fatalf("unexpected lease: %+v", lease)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListBillsByLeaseIDForUpdateLocksBills(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, type, status, period_start, period_end
FROM bills
WHERE lease_id = $1
  AND deleted_at IS NULL
ORDER BY period_start ASC, type ASC
FOR UPDATE`)).
		WithArgs("lease-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "status", "period_start", "period_end"}).
			AddRow("bill-1", "rent", "pending_payment", periodStart, periodEnd).
			AddRow("bill-2", "electricity", "pending_meter", periodStart, periodEnd))

	bills, err := repo.ListBillsByLeaseIDForUpdate(context.Background(), tx, "lease-1")
	if err != nil {
		t.Fatalf("ListBillsByLeaseIDForUpdate: %v", err)
	}
	if len(bills) != 2 || bills[0].ID != "bill-1" || bills[1].Type != "electricity" {
		t.Fatalf("unexpected bills: %+v", bills)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateBillsNoOpsWhenEmpty(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	if err := repo.CreateBills(context.Background(), tx, nil); err != nil {
		t.Fatalf("CreateBills: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestCreateBillsPreparesAndExecsMultipleRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	amount := 25000
	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	dueDate := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`)).
		ExpectExec().
		WithArgs("lease-1", "tenant-1", "room-1", "property-1", "rent", amount, periodStart, periodEnd, dueDate, "pending_payment").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO bills (
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	period_start,
	period_end,
	due_date,
	status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`)).
		WithArgs("lease-1", "tenant-1", "room-1", "property-1", "electricity", nil, periodStart, periodEnd, dueDate, "pending_meter").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.CreateBills(context.Background(), tx, []CreateBillParams{
		{
			LeaseID:     "lease-1",
			TenantID:    "tenant-1",
			RoomID:      "room-1",
			PropertyID:  "property-1",
			Type:        "rent",
			Amount:      &amount,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			DueDate:     dueDate,
			Status:      "pending_payment",
		},
		{
			LeaseID:     "lease-1",
			TenantID:    "tenant-1",
			RoomID:      "room-1",
			PropertyID:  "property-1",
			Type:        "electricity",
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			DueDate:     dueDate,
			Status:      "pending_meter",
		},
	})
	if err != nil {
		t.Fatalf("CreateBills: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestVoidRentBillsFromDueDateVoidsPendingRentBills(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	dueDate := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bills
SET status = 'voided',
	updated_at = now(),
	version = version + 1
WHERE lease_id = $1
  AND type = 'rent'
  AND due_date >= $2
  AND status IN ('pending_payment', 'pending_meter')
  AND deleted_at IS NULL`)).
		WithArgs("lease-1", dueDate).
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := repo.VoidRentBillsFromDueDate(context.Background(), tx, "lease-1", dueDate); err != nil {
		t.Fatalf("VoidRentBillsFromDueDate: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestVoidBillsOverlappingOrAfterVoidsPendingBillsByPeriodBoundary(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)
	boundary := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bills
SET status = 'voided',
	updated_at = now(),
	version = version + 1
WHERE lease_id = $1
  AND period_end >= $2
  AND status IN ('pending_payment', 'pending_meter')
  AND deleted_at IS NULL`)).
		WithArgs("lease-1", boundary).
		WillReturnResult(sqlmock.NewResult(0, 3))

	if err := repo.VoidBillsOverlappingOrAfter(context.Background(), tx, "lease-1", boundary); err != nil {
		t.Fatalf("VoidBillsOverlappingOrAfter: %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestRoomAndTenantStatusCommandsExecuteExpectedUpdates(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		arg  string
		run  func(context.Context, *SQLRepository, *sql.Tx, string) error
	}{
		{
			name: "MarkRoomOccupied",
			sql: `UPDATE rooms
SET status = 'occupied',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL`,
			arg: "room-1",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx, id string) error {
				return repo.MarkRoomOccupied(ctx, tx, id)
			},
		},
		{
			name: "MarkRoomVacant",
			sql: `UPDATE rooms
SET status = 'vacant',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL`,
			arg: "room-1",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx, id string) error {
				return repo.MarkRoomVacant(ctx, tx, id)
			},
		},
		{
			name: "ActivateTenant",
			sql: `UPDATE tenants
SET status = 'active',
	updated_at = now()
WHERE id = $1
  AND status = 'inactive'
  AND deleted_at IS NULL`,
			arg: "tenant-1",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx, id string) error {
				return repo.ActivateTenant(ctx, tx, id)
			},
		},
		{
			name: "DeactivateTenantIfNoActiveLeases",
			sql: `UPDATE tenants
SET status = 'inactive',
	updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL
  AND NOT EXISTS (
	SELECT 1
	FROM leases
	WHERE tenant_id = $1
	  AND status IN ('active', 'expired')
	  AND deleted_at IS NULL
  )`,
			arg: "tenant-1",
			run: func(ctx context.Context, repo *SQLRepository, tx *sql.Tx, id string) error {
				return repo.DeactivateTenantIfNoActiveLeases(ctx, tx, id)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, repo := newLeaseRepoTest(t)
			defer closeLeaseDB(t, db)
			tx := beginLeaseTx(t, db, mock)

			mock.ExpectExec(regexp.QuoteMeta(tt.sql)).
				WithArgs(tt.arg).
				WillReturnResult(sqlmock.NewResult(0, 1))

			if err := tt.run(context.Background(), repo, tx, tt.arg); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}

			mock.ExpectRollback()
			if err := tx.Rollback(); err != nil {
				t.Fatalf("Rollback: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestMarkLeaseExpiredReturnsNotFoundWhenNoRowsAffected(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE leases
SET status = 'expired',
	updated_at = now(),
	version = version + 1
WHERE id = $1
  AND version = $2
  AND status = 'active'
  AND deleted_at IS NULL`)).
		WithArgs("lease-1", 4).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.MarkLeaseExpired(context.Background(), tx, "lease-1", 4)
	if !errors.Is(err, ErrLeaseNotFound) {
		t.Fatalf("expected ErrLeaseNotFound, got %v", err)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListPendingForceTerminationBillIDsLocksPendingRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT bill_id
FROM force_termination_bills
WHERE force_termination_id = $1
  AND status = 'pending'
ORDER BY created_at ASC, bill_id ASC
FOR UPDATE`)).
		WithArgs("force-termination-1").
		WillReturnRows(sqlmock.NewRows([]string{"bill_id"}).AddRow("bill-1").AddRow("bill-2"))

	billIDs, err := repo.ListPendingForceTerminationBillIDs(context.Background(), tx, "force-termination-1")
	if err != nil {
		t.Fatalf("ListPendingForceTerminationBillIDs: %v", err)
	}
	if len(billIDs) != 2 || billIDs[0] != "bill-1" || billIDs[1] != "bill-2" {
		t.Fatalf("unexpected bill ids: %+v", billIDs)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestWriteOffBillsWithCountReturnsAffectedRows(t *testing.T) {
	db, mock, repo := newLeaseRepoTest(t)
	defer closeLeaseDB(t, db)
	tx := beginLeaseTx(t, db, mock)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE bills
SET status = 'written_off',
	written_off_reason = $1,
	updated_at = now(),
	version = version + 1
WHERE id IN ($2, $3)
  AND status NOT IN ('paid', 'voided')
  AND deleted_at IS NULL`)).
		WithArgs("legacy cleanup", "bill-1", "bill-2").
		WillReturnResult(sqlmock.NewResult(0, 1))

	affected, err := repo.WriteOffBillsWithCount(context.Background(), tx, []string{"bill-1", "bill-2"}, "legacy cleanup")
	if err != nil {
		t.Fatalf("WriteOffBillsWithCount: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 affected row, got %d", affected)
	}

	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func newLeaseRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeLeaseDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func beginLeaseTx(t *testing.T, db *sql.DB, mock sqlmock.Sqlmock) *sql.Tx {
	t.Helper()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return tx
}

func newLeaseRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"tenant_id",
		"property_id",
		"room_id",
		"rent_amount",
		"start_date",
		"end_date",
		"electricity_billing_cadence",
		"status",
		"deposit_amount",
		"deposit_refund_amount",
		"deposit_deduction_amount",
		"deposit_status",
		"deposit_deduction_reason",
		"notes",
		"termination_reason",
		"settlement_detail",
		"created_at",
		"updated_at",
		"version",
	})
}
