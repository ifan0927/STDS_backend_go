package leasequery

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListAccessibleAdminDoesNotApplyPropertyScope(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*)::int
FROM leases l
WHERE l.deleted_at IS NULL
`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	mock.ExpectQuery(`(?s)SELECT.*property_label.*tenant_label.*room_label.*FROM leases l.*WHERE l\.deleted_at IS NULL\s+ORDER BY l\.created_at DESC LIMIT \$1 OFFSET \$2`).
		WithArgs(10, 20).
		WillReturnRows(leaseQueryRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 12000, startDate, endDate,
			"monthly", "monthly", "active", 24000, nil, nil, "held", nil, nil, nil, nil, now, now, 1,
		))

	result, err := repo.ListAccessible(context.Background(), "admin", []string{"property-ignored"}, ListParams{Limit: 10, Offset: 20})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if result.Total != 3 {
		t.Fatalf("expected total 3, got %d", result.Total)
	}
	leases := result.Items
	if len(leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(leases))
	}
	if leases[0].PropertyID != "property-1" {
		t.Fatalf("PropertyID = %q, want property-1", leases[0].PropertyID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListAccessibleOrganizerAndStaffApplyAssignedPropertyScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
	}{
		{name: "organizer", role: "organizer"},
		{name: "staff", role: "staff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, repo := newLeaseQueryRepoTest(t)
			defer closeLeaseQueryDB(t, db)

			mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM leases l\s+WHERE l\.deleted_at IS NULL\s+AND l\.property_id IN \(\$1, \$2\)`).
				WithArgs("property-1", "property-2").
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

			mock.ExpectQuery(`(?s)WHERE l\.deleted_at IS NULL\s+AND l\.property_id IN \(\$1, \$2\)\s+ORDER BY l\.created_at DESC LIMIT \$3 OFFSET \$4`).
				WithArgs("property-1", "property-2", 25, 50).
				WillReturnRows(leaseQueryRows())

			result, err := repo.ListAccessible(context.Background(), tc.role, []string{"property-1", "property-2"}, ListParams{Limit: 25, Offset: 50})
			if err != nil {
				t.Fatalf("ListAccessible: %v", err)
			}
			if result.Total != 0 {
				t.Fatalf("expected total 0, got %d", result.Total)
			}
			if len(result.Items) != 0 {
				t.Fatalf("expected no leases, got %d", len(result.Items))
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestListAccessibleEmptyAssignedAndUnknownRoleReturnEmptyWithoutQuery(t *testing.T) {
	for _, tc := range []struct {
		name                string
		role                string
		assignedPropertyIDs []string
	}{
		{name: "organizer empty assigned", role: "organizer"},
		{name: "staff empty assigned", role: "staff"},
		{name: "unknown role", role: "viewer", assignedPropertyIDs: []string{"property-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, repo := newLeaseQueryRepoTest(t)
			defer closeLeaseQueryDB(t, db)

			result, err := repo.ListAccessible(context.Background(), tc.role, tc.assignedPropertyIDs, ListParams{Limit: 10})
			if err != nil {
				t.Fatalf("ListAccessible: %v", err)
			}
			if result.Total != 0 {
				t.Fatalf("expected total 0, got %d", result.Total)
			}
			if len(result.Items) != 0 {
				t.Fatalf("expected empty leases, got %d", len(result.Items))
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestListAccessibleAppliesFiltersAndPagination(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	propertyID := "property-filter"
	roomID := "room-filter"
	tenantID := "tenant-filter"

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM leases l\s+WHERE l\.deleted_at IS NULL\s+AND l\.property_id IN \(\$1\).*l\.property_id = \$2.*l\.room_id = \$3.*l\.tenant_id = \$4.*l\.status = \$5`).
		WithArgs("property-assigned", propertyID, roomID, tenantID, "active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(12))

	mock.ExpectQuery(`(?s)l\.property_id IN \(\$1\).*l\.property_id = \$2.*l\.room_id = \$3.*l\.tenant_id = \$4.*l\.status = \$5.*LIMIT \$6 OFFSET \$7`).
		WithArgs("property-assigned", propertyID, roomID, tenantID, "active", 30, 60).
		WillReturnRows(leaseQueryRows())

	result, err := repo.ListAccessible(context.Background(), "staff", []string{"property-assigned"}, ListParams{
		PropertyID: &propertyID,
		RoomID:     &roomID,
		TenantID:   &tenantID,
		Status:     "active",
		Limit:      30,
		Offset:     60,
	})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if result.Total != 12 {
		t.Fatalf("expected total 12, got %d", result.Total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListAccessibleScansNullableFieldsAndSettlementDetail(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM leases l\s+WHERE l\.deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(`(?s)WHERE l\.deleted_at IS NULL\s+ORDER BY l\.created_at DESC LIMIT \$1 OFFSET \$2`).
		WithArgs(10, 0).
		WillReturnRows(leaseQueryRows().
			AddRow(
				"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 12000, startDate, endDate,
				"monthly", "monthly", "terminated", 24000, 18000, 6000, "refunded", "cleaning",
				"tenant note", "early termination", `{"refund_method":"bank","deduction":6000}`, now, now, 2,
			).
			AddRow(
				"lease-2", "tenant-2", "property-2", "room-2", "Property 2", "Tenant 2", "Room 2", 15000, startDate, endDate,
				"monthly", "bi_monthly", "active", 30000, nil, nil, "held", nil, nil, nil, nil, now, now, 1,
			))

	result, err := repo.ListAccessible(context.Background(), "admin", nil, ListParams{Limit: 10})
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected total 2, got %d", result.Total)
	}
	leases := result.Items
	if len(leases) != 2 {
		t.Fatalf("expected 2 leases, got %d", len(leases))
	}

	if leases[0].DepositRefundAmount == nil || *leases[0].DepositRefundAmount != 18000 {
		t.Fatalf("DepositRefundAmount = %v, want 18000", leases[0].DepositRefundAmount)
	}
	if leases[0].DepositDeductionAmount == nil || *leases[0].DepositDeductionAmount != 6000 {
		t.Fatalf("DepositDeductionAmount = %v, want 6000", leases[0].DepositDeductionAmount)
	}
	if leases[0].DepositDeductionReason == nil || *leases[0].DepositDeductionReason != "cleaning" {
		t.Fatalf("DepositDeductionReason = %v, want cleaning", leases[0].DepositDeductionReason)
	}
	if leases[0].Notes == nil || *leases[0].Notes != "tenant note" {
		t.Fatalf("Notes = %v, want tenant note", leases[0].Notes)
	}
	if leases[0].TerminationReason == nil || *leases[0].TerminationReason != "early termination" {
		t.Fatalf("TerminationReason = %v, want early termination", leases[0].TerminationReason)
	}
	if leases[0].SettlementDetail == nil || (*leases[0].SettlementDetail)["refund_method"] != "bank" || (*leases[0].SettlementDetail)["deduction"] != float64(6000) {
		t.Fatalf("SettlementDetail = %#v, want decoded detail", leases[0].SettlementDetail)
	}

	if leases[1].DepositRefundAmount != nil {
		t.Fatalf("DepositRefundAmount = %v, want nil", leases[1].DepositRefundAmount)
	}
	if leases[1].DepositDeductionAmount != nil {
		t.Fatalf("DepositDeductionAmount = %v, want nil", leases[1].DepositDeductionAmount)
	}
	if leases[1].DepositDeductionReason != nil {
		t.Fatalf("DepositDeductionReason = %v, want nil", leases[1].DepositDeductionReason)
	}
	if leases[1].Notes != nil {
		t.Fatalf("Notes = %v, want nil", leases[1].Notes)
	}
	if leases[1].TerminationReason != nil {
		t.Fatalf("TerminationReason = %v, want nil", leases[1].TerminationReason)
	}
	if leases[1].SettlementDetail != nil {
		t.Fatalf("SettlementDetail = %#v, want nil", leases[1].SettlementDetail)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestListCheckoutReviewsAccessibleReturnsLabelsAndExportAvailability(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	propertyID := "property-1"
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM leases l\s+WHERE l\.deleted_at IS NULL\s+AND l\.status IN \('terminated', 'expired', 'force_terminated'\)\s+AND l\.property_id IN \(\$1\)\s+AND l\.property_id = \$2\s+AND l\.status = \$3`).
		WithArgs("property-1", propertyID, "terminated").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT.*property_label.*room_label.*tenant_label.*checkout_finalized_at.*export_available.*FROM leases l.*LEFT JOIN force_terminations ft.*l\.property_id IN \(\$1\).*l\.property_id = \$2.*l\.status = \$3.*LIMIT \$4 OFFSET \$5`).
		WithArgs("property-1", propertyID, "terminated", 20, 0).
		WillReturnRows(checkoutReviewRows().AddRow(
			"lease-1", "property-1", "room-1", "tenant-1", "Property 1", "Room 1", "Tenant 1",
			startDate, endDate, "terminated", "settled", 33000, 3000, "checkout done", now, true,
			nil, nil, nil, nil,
		))

	result, err := repo.ListCheckoutReviewsAccessible(context.Background(), "staff", []string{"property-1"}, CheckoutReviewListParams{
		PropertyID: &propertyID,
		Status:     "terminated",
		Limit:      20,
	})
	if err != nil {
		t.Fatalf("ListCheckoutReviewsAccessible: %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	review := result.Items[0]
	if review.RoomLabel != "Room 1" || review.TenantLabel != "Tenant 1" || !review.ExportAvailable {
		t.Fatalf("unexpected labels/export flag: %+v", review)
	}
	if review.CheckoutFinalizedAt == nil || !review.CheckoutFinalizedAt.Equal(now) {
		t.Fatalf("CheckoutFinalizedAt = %v, want %v", review.CheckoutFinalizedAt, now)
	}
}

func TestFindByIDAccessibleAdminSuccess(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT.*property_label.*tenant_label.*room_label.*FROM leases l.*WHERE l\.id = \$1\s+AND l\.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("lease-1").
		WillReturnRows(leaseQueryRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 12000, startDate, endDate,
			"monthly", "monthly", "active", 24000, nil, nil, "held", nil, nil, nil, nil, now, now, 1,
		))

	lease, err := repo.FindByIDAccessible(context.Background(), "lease-1", "admin", []string{"property-ignored"})
	if err != nil {
		t.Fatalf("FindByIDAccessible: %v", err)
	}
	if lease.ID != "lease-1" {
		t.Fatalf("ID = %q, want lease-1", lease.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDAccessibleOrganizerAndStaffApplyAssignedPropertyScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		role string
	}{
		{name: "organizer", role: "organizer"},
		{name: "staff", role: "staff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, repo := newLeaseQueryRepoTest(t)
			defer closeLeaseQueryDB(t, db)

			now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
			startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

			mock.ExpectQuery(`(?s)WHERE l\.id = \$1\s+AND l\.deleted_at IS NULL\s+AND l\.property_id IN \(\$2, \$3\)\s+LIMIT 1`).
				WithArgs("lease-1", "property-1", "property-2").
				WillReturnRows(leaseQueryRows().AddRow(
					"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 12000, startDate, endDate,
					"monthly", "monthly", "active", 24000, nil, nil, "held", nil, nil, nil, nil, now, now, 1,
				))

			lease, err := repo.FindByIDAccessible(context.Background(), "lease-1", tc.role, []string{"property-1", "property-2"})
			if err != nil {
				t.Fatalf("FindByIDAccessible: %v", err)
			}
			if lease.PropertyID != "property-1" {
				t.Fatalf("PropertyID = %q, want property-1", lease.PropertyID)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestFindByIDAccessibleEmptyAssignedAndUnknownRoleReturnNotFoundWithoutQuery(t *testing.T) {
	for _, tc := range []struct {
		name                string
		role                string
		assignedPropertyIDs []string
	}{
		{name: "organizer empty assigned", role: "organizer"},
		{name: "staff empty assigned", role: "staff"},
		{name: "unknown role", role: "viewer", assignedPropertyIDs: []string{"property-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, repo := newLeaseQueryRepoTest(t)
			defer closeLeaseQueryDB(t, db)

			lease, err := repo.FindByIDAccessible(context.Background(), "lease-1", tc.role, tc.assignedPropertyIDs)
			if lease != nil {
				t.Fatalf("expected nil lease, got %#v", lease)
			}
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("expected ErrNotFound, got %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestFindByIDAccessibleMapsNoRowsToErrNotFound(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	mock.ExpectQuery(`(?s)WHERE l\.id = \$1\s+AND l\.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("lease-missing").
		WillReturnRows(leaseQueryRows())

	lease, err := repo.FindByIDAccessible(context.Background(), "lease-missing", "admin", nil)
	if lease != nil {
		t.Fatalf("expected nil lease, got %#v", lease)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDAccessibleInvalidSettlementDetailReturnsError(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)WHERE l\.id = \$1\s+AND l\.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("lease-1").
		WillReturnRows(leaseQueryRows().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 12000, startDate, endDate,
			"monthly", "monthly", "terminated", 24000, nil, nil, "held", nil, nil, nil, `{"broken"`, now, now, 1,
		))

	lease, err := repo.FindByIDAccessible(context.Background(), "lease-1", "admin", nil)
	if lease != nil {
		t.Fatalf("expected nil lease, got %#v", lease)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("expected decode error, got ErrNotFound")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindByIDAccessibleScansRentBillingCadence(t *testing.T) {
	db, mock, repo := newLeaseQueryRepoTest(t)
	defer closeLeaseQueryDB(t, db)

	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT.*property_label.*tenant_label.*room_label.*FROM leases l.*WHERE l\.id = \$1\s+AND l\.deleted_at IS NULL\s+LIMIT 1`).
		WithArgs("lease-1").
		WillReturnRows(leaseQueryRowsWithRentCadence().AddRow(
			"lease-1", "tenant-1", "property-1", "room-1", "Property 1", "Tenant 1", "Room 1", 54000, startDate, endDate,
			"quarterly", "monthly", "active", 24000, nil, nil, "held", nil, nil, nil, nil, now, now, 1,
		))

	lease, err := repo.FindByIDAccessible(context.Background(), "lease-1", "admin", nil)
	if err != nil {
		t.Fatalf("FindByIDAccessible: %v", err)
	}
	if lease.RentBillingCadence != "quarterly" {
		t.Fatalf("RentBillingCadence = %q, want quarterly", lease.RentBillingCadence)
	}
	if lease.ElectricityBillingCadence != "monthly" {
		t.Fatalf("ElectricityBillingCadence = %q, want monthly", lease.ElectricityBillingCadence)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func newLeaseQueryRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeLeaseQueryDB(t *testing.T, db *sql.DB) {
	t.Helper()

	_ = db.Close()
}

func leaseQueryRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"tenant_id",
		"property_id",
		"room_id",
		"property_label",
		"tenant_label",
		"room_label",
		"rent_amount",
		"start_date",
		"end_date",
		"rent_billing_cadence",
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

func leaseQueryRowsWithRentCadence() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"tenant_id",
		"property_id",
		"room_id",
		"property_label",
		"tenant_label",
		"room_label",
		"rent_amount",
		"start_date",
		"end_date",
		"rent_billing_cadence",
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

func checkoutReviewRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"lease_id",
		"property_id",
		"room_id",
		"tenant_id",
		"property_label",
		"room_label",
		"tenant_label",
		"start_date",
		"end_date",
		"lease_status",
		"deposit_status",
		"deposit_refund_amount",
		"deposit_deduction_amount",
		"termination_reason",
		"checkout_finalized_at",
		"export_available",
		"force_termination_id",
		"force_termination_status",
		"force_termination_reason",
		"force_termination_deposit_handling",
	})
}
