package devseed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	DemoCommand = "frontend-demo"

	reportYear  = 2026
	reportMonth = 5

	localAdminUID     = "local-admin"
	localOrganizerUID = "local-organizer"
	localStaffUID     = "local-staff"
	localOwnerUID     = "local-owner"

	localAdminEmail     = "local-admin@example.com"
	localOrganizerEmail = "local-organizer@example.com"
	localStaffEmail     = "local-staff@example.com"
	localOwnerEmail     = "local-owner@example.com"

	propertyID        = "15700000-0000-4000-8000-000000000001"
	propertyAccountID = "15700000-0000-4000-8000-000000000201"

	room101ID = "15700000-0000-4000-8000-000000000101"
	room102ID = "15700000-0000-4000-8000-000000000102"
	room103ID = "15700000-0000-4000-8000-000000000103"
	room201ID = "15700000-0000-4000-8000-000000000104"

	tenantAID = "15700000-0000-4000-8000-000000000301"
	tenantBID = "15700000-0000-4000-8000-000000000302"
	tenantCID = "15700000-0000-4000-8000-000000000303"

	activeLeaseRentID     = "15700000-0000-4000-8000-000000000401"
	activeLeaseElectricID = "15700000-0000-4000-8000-000000000402"
	checkoutLeaseID       = "15700000-0000-4000-8000-000000000403"

	paidRentBillID        = "15700000-0000-4000-8000-000000000501"
	paidElectricBillID    = "15700000-0000-4000-8000-000000000502"
	overdueRentBillID     = "15700000-0000-4000-8000-000000000503"
	pendingElectricBillID = "15700000-0000-4000-8000-000000000504"
	checkoutRentBillID    = "15700000-0000-4000-8000-000000000505"

	journalLogID      = "15700000-0000-4000-8000-000000000601"
	repairSubmittedID = "15700000-0000-4000-8000-000000000701"
	repairAssignedID  = "15700000-0000-4000-8000-000000000702"
	repairCompletedID = "15700000-0000-4000-8000-000000000703"
	reportSnapshotID  = "15700000-0000-4000-8000-000000000801"
	openingSnapshotID = "15700000-0000-4000-8000-000000000802"
)

var taipeiLocation = mustLoadLocation("Asia/Taipei")

type Options struct {
	AppEnv      string
	DatabaseURL string
	Now         time.Time
}

type Result struct {
	Command               string
	PropertyID            string
	ReportYear            int
	ReportMonth           int
	ReportMode            string
	PaidRentBillID        string
	PaidElectricBillID    string
	CheckoutLeaseID       string
	SeededLocalAdminEmail string
}

func Run(ctx context.Context, db *sql.DB, options Options) (*Result, error) {
	if err := validateSafety(options); err != nil {
		return nil, err
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin dev seed transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	adminID, ownerID, staffID, err := upsertLocalUsers(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := cleanupDemoRows(ctx, tx); err != nil {
		return nil, err
	}
	if err := seedDemoRows(ctx, tx, adminID, ownerID, staffID, now); err != nil {
		return nil, err
	}
	if err := assignDemoProperty(ctx, tx, propertyID); err != nil {
		return nil, err
	}
	if err := verifyDemoRows(ctx, tx, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit dev seed transaction: %w", err)
	}
	tx = nil

	reportMode := "snapshot"
	if isReportCurrent(now) {
		reportMode = "live"
	}
	return &Result{
		Command:               DemoCommand,
		PropertyID:            propertyID,
		ReportYear:            reportYear,
		ReportMonth:           reportMonth,
		ReportMode:            reportMode,
		PaidRentBillID:        paidRentBillID,
		PaidElectricBillID:    paidElectricBillID,
		CheckoutLeaseID:       checkoutLeaseID,
		SeededLocalAdminEmail: localAdminEmail,
	}, nil
}

func validateSafety(options Options) error {
	env := strings.ToLower(strings.TrimSpace(options.AppEnv))
	switch env {
	case "local", "dev", "development", "e2e", "test":
	default:
		return fmt.Errorf("refusing to run dev seed with APP_ENV=%q", options.AppEnv)
	}

	dbName, err := databaseName(options.DatabaseURL)
	if err != nil {
		return err
	}
	name := strings.ToLower(dbName)
	if strings.Contains(name, "prod") || strings.Contains(name, "production") {
		return fmt.Errorf("refusing to run dev seed against production-like database %q", dbName)
	}
	for _, allowed := range []string{"dev", "local", "e2e", "test", "stds_backend"} {
		if strings.Contains(name, allowed) {
			return nil
		}
	}
	return fmt.Errorf("refusing to run dev seed against database %q: name must contain dev, local, e2e, test, or stds_backend", dbName)
}

func databaseName(databaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(databaseURL))
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	name := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if name == "" {
		return "", errors.New("parse DATABASE_URL: database name is required")
	}
	return name, nil
}

func upsertLocalUsers(ctx context.Context, tx *sql.Tx) (adminID string, ownerID string, staffID string, err error) {
	adminID, err = upsertLocalUser(ctx, tx, localAdminUID, localAdminEmail, "Local Admin", "admin")
	if err != nil {
		return "", "", "", err
	}
	if _, err := upsertLocalUser(ctx, tx, localOrganizerUID, localOrganizerEmail, "Local Organizer", "organizer"); err != nil {
		return "", "", "", err
	}
	staffID, err = upsertLocalUser(ctx, tx, localStaffUID, localStaffEmail, "Local Staff", "staff")
	if err != nil {
		return "", "", "", err
	}
	ownerID, err = upsertLocalUser(ctx, tx, localOwnerUID, localOwnerEmail, "Local Owner", "owner")
	if err != nil {
		return "", "", "", err
	}
	return adminID, ownerID, staffID, nil
}

func upsertLocalUser(ctx context.Context, tx *sql.Tx, firebaseUID string, email string, name string, role string) (string, error) {
	const query = `
INSERT INTO users (
	firebase_uid,
	email,
	name,
	role,
	permission_overrides,
	assigned_property_ids
) VALUES (
	$1,
	$2,
	$3,
	$4,
	'[]'::jsonb,
	'[]'::jsonb
)
ON CONFLICT (firebase_uid) WHERE deleted_at IS NULL
DO UPDATE SET
	email = EXCLUDED.email,
	name = EXCLUDED.name,
	role = EXCLUDED.role,
	updated_at = now(),
	version = users.version + 1
RETURNING id
`
	var id string
	if err := tx.QueryRowContext(ctx, query, firebaseUID, email, name, role).Scan(&id); err != nil {
		return "", fmt.Errorf("upsert local user %s: %w", email, err)
	}
	return id, nil
}

func cleanupDemoRows(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`DELETE FROM repair_requests WHERE id IN ($1, $2, $3)`,
		`DELETE FROM journal_logs WHERE id = $1`,
		`DELETE FROM monthly_snapshot_entries WHERE snapshot_id IN ($1, $2)`,
		`DELETE FROM monthly_snapshots WHERE id IN ($1, $2) OR property_id = $3`,
		`DELETE FROM accounting_entries WHERE property_account_id = $1`,
		`DELETE FROM bills WHERE id IN ($1, $2, $3, $4, $5)`,
		`DELETE FROM leases WHERE id IN ($1, $2, $3)`,
		`DELETE FROM tenants WHERE id IN ($1, $2, $3)`,
		`DELETE FROM rooms WHERE id IN ($1, $2, $3, $4)`,
		`DELETE FROM property_accounts WHERE property_id = $1 OR id = $2`,
		`DELETE FROM properties WHERE id = $1`,
	}
	args := [][]any{
		{repairSubmittedID, repairAssignedID, repairCompletedID},
		{journalLogID},
		{reportSnapshotID, openingSnapshotID},
		{reportSnapshotID, openingSnapshotID, propertyID},
		{propertyAccountID},
		{paidRentBillID, paidElectricBillID, overdueRentBillID, pendingElectricBillID, checkoutRentBillID},
		{activeLeaseRentID, activeLeaseElectricID, checkoutLeaseID},
		{tenantAID, tenantBID, tenantCID},
		{room101ID, room102ID, room103ID, room201ID},
		{propertyID, propertyAccountID},
		{propertyID},
	}
	for i, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement, args[i]...); err != nil {
			return fmt.Errorf("cleanup demo rows: %w", err)
		}
	}
	return nil
}

func seedDemoRows(ctx context.Context, tx *sql.Tx, adminID string, ownerID string, staffID string, now time.Time) error {
	if err := seedPropertyAndRooms(ctx, tx, ownerID); err != nil {
		return err
	}
	if err := seedTenants(ctx, tx); err != nil {
		return err
	}
	if err := seedLeasesAndBills(ctx, tx); err != nil {
		return err
	}
	if err := seedJournalAndRepairs(ctx, tx, adminID, staffID); err != nil {
		return err
	}
	if err := seedAccounting(ctx, tx, now); err != nil {
		return err
	}
	return nil
}

func seedPropertyAndRooms(ctx context.Context, tx *sql.Tx, ownerID string) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO properties (
	id,
	name,
	subtitle,
	address,
	electricity_unit_price,
	default_electricity_billing_cadence,
	owner_id,
	contact_phone,
	contact_email,
	notes,
	facilities,
	created_at,
	updated_at
) VALUES (
	$1,
	'前端展示公寓',
	'Frontend Demo Property',
	'台北市中正區測試路 157 號',
	4.5,
	'monthly',
	$2,
	'02-2399-0157',
	'demo-property@example.com',
	'Opt-in deterministic frontend demo seed.',
	'{"elevator":true,"laundry":true,"parking":false}'::jsonb,
	'2026-05-01 09:00:00+08',
	'2026-05-01 09:00:00+08'
)
`, propertyID, ownerID); err != nil {
		return fmt.Errorf("seed demo property: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO property_accounts (id, property_id, created_at, updated_at)
VALUES ($1, $2, '2026-05-01 09:00:00+08', '2026-05-01 09:00:00+08')
`, propertyAccountID, propertyID); err != nil {
		return fmt.Errorf("seed demo property account: %w", err)
	}

	rooms := []struct {
		id     string
		name   string
		status string
		rent   int
		zone   string
	}{
		{room101ID, "101", "occupied", 18000, "A 棟"},
		{room102ID, "102", "occupied", 16500, "A 棟"},
		{room103ID, "103", "vacant", 17000, "A 棟"},
		{room201ID, "201", "vacant", 21000, "B 棟"},
	}
	for _, room := range rooms {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO rooms (
	id,
	property_id,
	name,
	status,
	size,
	floor,
	room_type,
	facilities,
	default_rent_amount,
	notes,
	zone,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3::varchar,
	$4,
	12.5,
	LEFT($3::text, 1),
	'套房',
	'{"bed":true,"desk":true,"private_bath":true}'::jsonb,
	$5,
	'Frontend demo room.',
	$6,
	'2026-05-01 09:05:00+08',
	'2026-05-01 09:05:00+08'
)
`, room.id, propertyID, room.name, room.status, room.rent, room.zone); err != nil {
			return fmt.Errorf("seed demo room %s: %w", room.name, err)
		}
	}
	return nil
}

func seedTenants(ctx context.Context, tx *sql.Tx) error {
	tenants := []struct {
		id    string
		name  string
		email string
		phone string
	}{
		{tenantAID, "林怡君", "demo-tenant-a@example.com", "0912-000-157"},
		{tenantBID, "陳柏翰", "demo-tenant-b@example.com", "0922-000-157"},
		{tenantCID, "王雅婷", "demo-tenant-c@example.com", "0932-000-157"},
	}
	for _, tenant := range tenants {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tenants (
	id,
	name,
	email,
	phone,
	contacts,
	status,
	birth_date,
	national_id,
	address,
	occupation,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	'[{"name":"緊急聯絡人","phone":"0999-000-157","relationship":"family"}]'::jsonb,
	'active',
	'1993-05-15',
	'DEMO157',
	'台北市測試區',
	'前端測試資料',
	'2026-05-01 09:10:00+08',
	'2026-05-01 09:10:00+08'
)
`, tenant.id, tenant.name, tenant.email, tenant.phone); err != nil {
			return fmt.Errorf("seed demo tenant %s: %w", tenant.name, err)
		}
	}
	return nil
}

func seedLeasesAndBills(ctx context.Context, tx *sql.Tx) error {
	leases := []struct {
		id        string
		tenantID  string
		roomID    string
		rent      int
		startDate string
		endDate   string
		status    string
		deposit   int
		refund    *int
		deduction *int
		reason    *string
		detail    any
		updatedAt string
	}{
		{activeLeaseRentID, tenantAID, room101ID, 18000, "2026-05-01", "2027-04-30", "active", 36000, nil, nil, nil, nil, "2026-05-01 09:20:00+08"},
		{activeLeaseElectricID, tenantBID, room102ID, 16500, "2026-04-01", "2027-03-31", "active", 33000, nil, nil, nil, nil, "2026-05-01 09:20:00+08"},
		{checkoutLeaseID, tenantCID, room201ID, 21000, "2025-09-01", "2026-05-20", "terminated", 42000, intPtr(39000), intPtr(3000), stringPtr("清潔費"), checkoutSettlementDetail(), "2026-05-20 15:00:00+08"},
	}
	for _, lease := range leases {
		detailJSON, err := json.Marshal(lease.detail)
		if err != nil {
			return fmt.Errorf("marshal checkout detail: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO leases (
	id,
	tenant_id,
	room_id,
	property_id,
	rent_amount,
	start_date,
	end_date,
	status,
	deposit_amount,
	deposit_status,
	deposit_refund_amount,
	deposit_deduction_amount,
	deposit_deduction_reason,
	notes,
	termination_reason,
	settlement_detail,
	rent_billing_cadence,
	electricity_billing_cadence,
	starting_meter_reading,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8::varchar,
	$9,
	CASE WHEN $8::text = 'terminated' THEN 'settled' ELSE 'held' END,
	$10,
	$11,
	$12,
	'Frontend demo lease.',
	CASE WHEN $8::text = 'terminated' THEN 'Demo checkout settlement' ELSE NULL END,
	$13::jsonb,
	'monthly',
	'monthly',
	50,
	'2026-05-01 09:20:00+08',
	$14
)
`, lease.id, lease.tenantID, lease.roomID, propertyID, lease.rent, lease.startDate, lease.endDate, lease.status, lease.deposit, lease.refund, lease.deduction, lease.reason, string(detailJSON), lease.updatedAt); err != nil {
			return fmt.Errorf("seed demo lease %s: %w", lease.id, err)
		}
	}

	bills := []struct {
		id          string
		leaseID     string
		tenantID    string
		roomID      string
		billType    string
		amount      *int
		dueDate     string
		status      string
		paidAmount  *int
		paidAt      *string
		prevReading *int
		curReading  *int
		unitPrice   *float64
		recordedAt  *string
		periodStart string
		periodEnd   string
	}{
		{paidRentBillID, activeLeaseRentID, tenantAID, room101ID, "rent", intPtr(18000), "2026-05-05", "paid", intPtr(18000), stringPtr("2026-05-05 10:00:00+08"), nil, nil, nil, nil, "2026-05-01", "2026-05-31"},
		{paidElectricBillID, activeLeaseElectricID, tenantBID, room102ID, "electricity", intPtr(630), "2026-05-10", "paid", intPtr(630), stringPtr("2026-05-10 12:00:00+08"), intPtr(50), intPtr(190), floatPtr(4.5), stringPtr("2026-05-09 18:00:00+08"), "2026-05-01", "2026-05-31"},
		{overdueRentBillID, activeLeaseElectricID, tenantBID, room102ID, "rent", intPtr(16500), "2026-04-05", "overdue", nil, nil, nil, nil, nil, nil, "2026-04-01", "2026-04-30"},
		{pendingElectricBillID, activeLeaseElectricID, tenantBID, room102ID, "electricity", nil, "2026-06-10", "pending_meter", nil, nil, nil, nil, nil, nil, "2026-06-01", "2026-06-30"},
		{checkoutRentBillID, checkoutLeaseID, tenantCID, room201ID, "rent", intPtr(21000), "2026-05-05", "paid", intPtr(21000), stringPtr("2026-05-15 10:30:00+08"), nil, nil, nil, nil, "2026-05-01", "2026-05-31"},
	}
	for _, bill := range bills {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bills (
	id,
	lease_id,
	tenant_id,
	room_id,
	property_id,
	type,
	amount,
	due_date,
	status,
	payment_method,
	paid_at,
	paid_amount,
	meter_previous_reading,
	meter_current_reading,
	meter_unit_price,
	meter_recorded_at,
	overdue_notice_count,
	source_ref,
	period_start,
	period_end,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9::varchar,
	CASE WHEN $9::text = 'paid' THEN 'transfer' ELSE NULL END,
	$10,
	$11,
	$12,
	$13,
	$14,
	$15,
	CASE WHEN $9::text = 'overdue' THEN 1 ELSE 0 END,
	'{"seed":"frontend-demo"}'::jsonb,
	$16,
	$17,
	'2026-05-01 09:25:00+08',
	'2026-05-01 09:25:00+08'
)
`, bill.id, bill.leaseID, bill.tenantID, bill.roomID, propertyID, bill.billType, bill.amount, bill.dueDate, bill.status, bill.paidAt, bill.paidAmount, bill.prevReading, bill.curReading, bill.unitPrice, bill.recordedAt, bill.periodStart, bill.periodEnd); err != nil {
			return fmt.Errorf("seed demo bill %s: %w", bill.id, err)
		}
	}
	return nil
}

func checkoutSettlementDetail() map[string]any {
	finalMeter := 190
	finalizedAt := "2026-05-20T15:00:00+08:00"
	return map[string]any{
		"lease_id":            checkoutLeaseID,
		"property_id":         propertyID,
		"tenant_id":           tenantCID,
		"room_id":             room201ID,
		"property_label":      "前端展示公寓",
		"tenant_label":        "王雅婷",
		"room_label":          "201",
		"checkout_date":       "2026-05-20T00:00:00+08:00",
		"reason":              "前端 demo 退租結算",
		"final_meter_reading": finalMeter,
		"notes":               "Frontend demo checkout settlement.",
		"deposit_amount":      42000,
		"total_refund":        42000,
		"total_charge":        3000,
		"net_amount":          39000,
		"net_direction":       "refund",
		"export_available":    true,
		"finalized_at":        finalizedAt,
		"blockers":            []any{},
		"warnings":            []map[string]any{{"code": "final_meter_snapshot_only", "message": "退租電表讀數已保存於結算快照；本次不由前端計算電費。"}},
		"lines": []map[string]any{
			{"kind": "deposit_refund", "label": "押金退還", "direction": "refund", "amount": 42000},
			{"kind": "cleaning_fee", "label": "清潔費", "direction": "charge", "amount": 3000},
		},
	}
}

func seedJournalAndRepairs(ctx context.Context, tx *sql.Tx, adminID string, staffID string) error {
	var titleID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM accounting_titles WHERE code = '6681'`).Scan(&titleID); err != nil {
		return fmt.Errorf("find journal expense accounting title: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO journal_logs (
	id,
	property_id,
	room_id,
	author_id,
	content,
	expense_amount,
	expense_description,
	expense_accounting_title_id,
	expense_accounting_title_code,
	expense_accounting_title_name,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	'更換 102 排水管與公共區域清潔',
	2500,
	'水電修繕',
	$5,
	'6681',
	'其他支出',
	'2026-05-12 14:00:00+08',
	'2026-05-12 14:00:00+08'
)
`, journalLogID, propertyID, room102ID, adminID, titleID); err != nil {
		return fmt.Errorf("seed demo journal log: %w", err)
	}

	repairs := []struct {
		id          string
		roomID      string
		assignedTo  *string
		title       string
		status      string
		submittedAt string
		assignedAt  *string
		completedAt *string
	}{
		{repairSubmittedID, room101ID, nil, "101 冷氣濾網需清潔", "submitted", "2026-05-03 11:00:00+08", nil, nil},
		{repairAssignedID, room102ID, &staffID, "102 浴室排水較慢", "assigned", "2026-05-08 10:00:00+08", stringPtr("2026-05-08 13:00:00+08"), nil},
		{repairCompletedID, room201ID, &staffID, "201 退租後牆面補漆", "completed", "2026-05-18 09:00:00+08", stringPtr("2026-05-18 10:00:00+08"), stringPtr("2026-05-19 16:00:00+08")},
	}
	for _, repair := range repairs {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO repair_requests (
	id,
	property_id,
	room_id,
	submitted_by,
	assigned_to,
	title,
	description,
	status,
	submitted_at,
	assigned_at,
	completed_at,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	'Frontend demo repair request.',
	$7::varchar,
	$8::timestamptz,
	$9::timestamptz,
	$10::timestamptz,
	$8::timestamptz,
	COALESCE($10::timestamptz, COALESCE($9::timestamptz, $8::timestamptz))
)
`, repair.id, propertyID, repair.roomID, adminID, repair.assignedTo, repair.title, repair.status, repair.submittedAt, repair.assignedAt, repair.completedAt); err != nil {
			return fmt.Errorf("seed demo repair %s: %w", repair.id, err)
		}
	}
	return nil
}

func seedAccounting(ctx context.Context, tx *sql.Tx, now time.Time) error {
	titleIDs, err := accountingTitleIDs(ctx, tx)
	if err != nil {
		return err
	}
	entries := demoAccountingEntries(titleIDs)
	for _, entry := range entries {
		if err := insertAccountingEntry(ctx, tx, entry); err != nil {
			return err
		}
	}
	if err := seedOpeningSnapshot(ctx, tx, titleIDs); err != nil {
		return err
	}
	if !isReportCurrent(now) {
		if err := seedReportSnapshot(ctx, tx, entries); err != nil {
			return err
		}
	}
	return nil
}

func accountingTitleIDs(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT code, id FROM accounting_titles WHERE code IN ('4601', '4602', '4603', '4605', '6681')`)
	if err != nil {
		return nil, fmt.Errorf("list accounting title ids: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var code string
		var id string
		if err := rows.Scan(&code, &id); err != nil {
			return nil, fmt.Errorf("scan accounting title id: %w", err)
		}
		result[code] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accounting title ids: %w", err)
	}
	for _, code := range []string{"4601", "4602", "4603", "4605", "6681"} {
		if result[code] == "" {
			return nil, fmt.Errorf("accounting title %s is missing; run migrations first", code)
		}
	}
	return result, nil
}

type accountingEntry struct {
	ID                  string
	Category            string
	AccountingTitleID   string
	AccountingTitleCode string
	AccountingTitleName string
	SourceDate          string
	RoomLabel           *string
	TenantLabel         *string
	PeriodLabel         *string
	DisplayNote         string
	Description         *string
	Amount              int
	SourceRef           string
	CreatedAt           string
}

func demoAccountingEntries(titleIDs map[string]string) []accountingEntry {
	return []accountingEntry{
		{
			ID:                  "15700000-0000-4000-8000-000000000901",
			Category:            "rent_payment",
			AccountingTitleID:   titleIDs["4603"],
			AccountingTitleCode: "4603",
			AccountingTitleName: "租金收入",
			SourceDate:          "2026-05-05",
			RoomLabel:           stringPtr("101"),
			TenantLabel:         stringPtr("林怡君"),
			PeriodLabel:         stringPtr("2026-05-01 至 2026-05-31"),
			DisplayNote:         "101 林怡君 2026-05 租金",
			Description:         stringPtr("rent"),
			Amount:              18000,
			SourceRef:           fmt.Sprintf(`{"type":"BillPaid","bill_id":"%s"}`, paidRentBillID),
			CreatedAt:           "2026-05-05 10:00:00+08",
		},
		{
			ID:                  "15700000-0000-4000-8000-000000000902",
			Category:            "electricity_payment",
			AccountingTitleID:   titleIDs["4605"],
			AccountingTitleCode: "4605",
			AccountingTitleName: "房客電費收入",
			SourceDate:          "2026-05-10",
			RoomLabel:           stringPtr("102"),
			TenantLabel:         stringPtr("陳柏翰"),
			PeriodLabel:         stringPtr("2026-05-01 至 2026-05-31"),
			DisplayNote:         "102 陳柏翰 2026-05 電費",
			Description:         stringPtr("electricity"),
			Amount:              630,
			SourceRef:           fmt.Sprintf(`{"type":"BillPaid","bill_id":"%s"}`, paidElectricBillID),
			CreatedAt:           "2026-05-10 12:00:00+08",
		},
		{
			ID:                  "15700000-0000-4000-8000-000000000903",
			Category:            "journal_expense",
			AccountingTitleID:   titleIDs["6681"],
			AccountingTitleCode: "6681",
			AccountingTitleName: "其他支出",
			SourceDate:          "2026-05-12",
			RoomLabel:           stringPtr("102"),
			TenantLabel:         stringPtr("陳柏翰"),
			PeriodLabel:         stringPtr("2026-05"),
			DisplayNote:         "水電修繕",
			Description:         stringPtr("水電修繕"),
			Amount:              2500,
			SourceRef:           fmt.Sprintf(`{"type":"JournalExpenseRecorded","journal_log_id":"%s"}`, journalLogID),
			CreatedAt:           "2026-05-12 14:00:00+08",
		},
		{
			ID:                  "15700000-0000-4000-8000-000000000904",
			Category:            "deposit_refund",
			AccountingTitleID:   titleIDs["4602"],
			AccountingTitleCode: "4602",
			AccountingTitleName: "押金退回(減項)",
			SourceDate:          "2026-05-20",
			RoomLabel:           stringPtr("201"),
			TenantLabel:         stringPtr("王雅婷"),
			PeriodLabel:         stringPtr("2026-05"),
			DisplayNote:         "201 王雅婷 退租押金退還",
			Description:         stringPtr("押金退還"),
			Amount:              39000,
			SourceRef:           fmt.Sprintf(`{"type":"DepositRefunded","lease_id":"%s"}`, checkoutLeaseID),
			CreatedAt:           "2026-05-20 15:00:00+08",
		},
		{
			ID:                  "15700000-0000-4000-8000-000000000905",
			Category:            "deposit_deduction",
			AccountingTitleID:   titleIDs["4601"],
			AccountingTitleCode: "4601",
			AccountingTitleName: "押金收入(暫收款)",
			SourceDate:          "2026-05-20",
			RoomLabel:           stringPtr("201"),
			TenantLabel:         stringPtr("王雅婷"),
			PeriodLabel:         stringPtr("2026-05"),
			DisplayNote:         "201 王雅婷 清潔費扣款",
			Description:         stringPtr("清潔費"),
			Amount:              3000,
			SourceRef:           fmt.Sprintf(`{"type":"DepositDeducted","lease_id":"%s"}`, checkoutLeaseID),
			CreatedAt:           "2026-05-20 15:00:01+08",
		},
	}
}

func insertAccountingEntry(ctx context.Context, tx *sql.Tx, entry accountingEntry) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO accounting_entries (
	id,
	property_account_id,
	category,
	accounting_title_id,
	accounting_title_code,
	accounting_title_name,
	source_date,
	room_label,
	tenant_label,
	period_label,
	display_note,
	description,
	amount,
	source_ref,
	year,
	month,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10,
	$11,
	$12,
	$13,
	$14::jsonb,
	$15,
	$16,
	$17,
	$17
)
`, entry.ID, propertyAccountID, entry.Category, entry.AccountingTitleID, entry.AccountingTitleCode, entry.AccountingTitleName, entry.SourceDate, entry.RoomLabel, entry.TenantLabel, entry.PeriodLabel, entry.DisplayNote, entry.Description, entry.Amount, entry.SourceRef, reportYear, reportMonth, entry.CreatedAt); err != nil {
		return fmt.Errorf("seed accounting entry %s: %w", entry.ID, err)
	}
	return nil
}

func seedOpeningSnapshot(ctx context.Context, tx *sql.Tx, titleIDs map[string]string) error {
	if _, err := tx.ExecContext(ctx, `
INSERT INTO monthly_snapshots (
	id,
	property_id,
	year,
	month,
	total_income,
	total_expense,
	net,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	2026,
	4,
	15000,
	2000,
	13000,
	'2026-05-01 08:00:00+08',
	'2026-05-01 08:00:00+08'
)
`, openingSnapshotID, propertyID); err != nil {
		return fmt.Errorf("seed opening monthly snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO monthly_snapshot_entries (
	id,
	snapshot_id,
	category,
	accounting_title_id,
	accounting_title_code,
	accounting_title_name,
	source_date,
	room_label,
	tenant_label,
	period_label,
	display_note,
	description,
	amount,
	source_ref,
	created_at,
	updated_at
) VALUES (
	'15700000-0000-4000-8000-000000000911',
	$1,
	'rent_payment',
	$2,
	'4603',
	'租金收入',
	'2026-04-05',
	'102',
	'陳柏翰',
	'2026-04',
	'102 陳柏翰 2026-04 租金',
	'rent',
	15000,
	'{"seed":"frontend-demo","period":"opening"}'::jsonb,
	'2026-04-05 10:00:00+08',
	'2026-04-05 10:00:00+08'
)
`, openingSnapshotID, titleIDs["4603"]); err != nil {
		return fmt.Errorf("seed opening monthly snapshot entry: %w", err)
	}
	return nil
}

func seedReportSnapshot(ctx context.Context, tx *sql.Tx, entries []accountingEntry) error {
	totalIncome := 0
	totalExpense := 0
	for _, entry := range entries {
		switch entry.Category {
		case "rent_payment", "electricity_payment", "deposit_deduction":
			totalIncome += abs(entry.Amount)
		case "deposit_refund", "journal_expense":
			totalExpense += abs(entry.Amount)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO monthly_snapshots (
	id,
	property_id,
	year,
	month,
	total_income,
	total_expense,
	net,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	'2026-06-01 08:00:00+08',
	'2026-06-01 08:00:00+08'
)
`, reportSnapshotID, propertyID, reportYear, reportMonth, totalIncome, totalExpense, totalIncome-totalExpense); err != nil {
		return fmt.Errorf("seed report monthly snapshot: %w", err)
	}
	for i, entry := range entries {
		snapshotEntryID := fmt.Sprintf("15700000-0000-4000-8000-00000000092%d", i+1)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO monthly_snapshot_entries (
	id,
	snapshot_id,
	category,
	accounting_title_id,
	accounting_title_code,
	accounting_title_name,
	source_date,
	room_label,
	tenant_label,
	period_label,
	display_note,
	description,
	amount,
	source_ref,
	created_at,
	updated_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10,
	$11,
	$12,
	$13,
	$14::jsonb,
	$15,
	$15
)
`, snapshotEntryID, reportSnapshotID, entry.Category, entry.AccountingTitleID, entry.AccountingTitleCode, entry.AccountingTitleName, entry.SourceDate, entry.RoomLabel, entry.TenantLabel, entry.PeriodLabel, entry.DisplayNote, entry.Description, entry.Amount, entry.SourceRef, entry.CreatedAt); err != nil {
			return fmt.Errorf("seed report monthly snapshot entry %s: %w", snapshotEntryID, err)
		}
	}
	return nil
}

func assignDemoProperty(ctx context.Context, tx *sql.Tx, propertyID string) error {
	assigned, err := json.Marshal([]string{propertyID})
	if err != nil {
		return fmt.Errorf("marshal assigned property ids: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE users
SET assigned_property_ids = $1::jsonb,
    updated_at = now(),
    version = version + 1
WHERE firebase_uid IN ($2, $3, $4, $5)
  AND deleted_at IS NULL
`, string(assigned), localAdminUID, localOrganizerUID, localStaffUID, localOwnerUID); err != nil {
		return fmt.Errorf("assign demo property to local users: %w", err)
	}
	return nil
}

func verifyDemoRows(ctx context.Context, tx *sql.Tx, now time.Time) error {
	checks := []struct {
		name string
		want int
		sql  string
		args []any
	}{
		{
			name: "properties",
			want: 1,
			sql:  `SELECT count(*) FROM properties WHERE id = $1 AND deleted_at IS NULL`,
			args: []any{propertyID},
		},
		{
			name: "rooms",
			want: 4,
			sql:  `SELECT count(*) FROM rooms WHERE property_id = $1 AND deleted_at IS NULL`,
			args: []any{propertyID},
		},
		{
			name: "leases",
			want: 3,
			sql:  `SELECT count(*) FROM leases WHERE property_id = $1 AND deleted_at IS NULL`,
			args: []any{propertyID},
		},
		{
			name: "bills",
			want: 5,
			sql:  `SELECT count(*) FROM bills WHERE property_id = $1 AND deleted_at IS NULL`,
			args: []any{propertyID},
		},
		{
			name: "report accounting entries",
			want: 5,
			sql:  `SELECT count(*) FROM accounting_entries WHERE property_account_id = $1 AND year = $2 AND month = $3`,
			args: []any{propertyAccountID, reportYear, reportMonth},
		},
		{
			name: "repairs",
			want: 3,
			sql:  `SELECT count(*) FROM repair_requests WHERE property_id = $1 AND deleted_at IS NULL`,
			args: []any{propertyID},
		},
		{
			name: "assigned local users",
			want: 4,
			sql:  `SELECT count(*) FROM users WHERE firebase_uid IN ($1, $2, $3, $4) AND assigned_property_ids @> $5::jsonb AND deleted_at IS NULL`,
			args: []any{localAdminUID, localOrganizerUID, localStaffUID, localOwnerUID, `["` + propertyID + `"]`},
		},
	}
	for _, check := range checks {
		var got int
		if err := tx.QueryRowContext(ctx, check.sql, check.args...).Scan(&got); err != nil {
			return fmt.Errorf("verify dev seed %s: %w", check.name, err)
		}
		if got != check.want {
			return fmt.Errorf("verify dev seed %s: got %d, want %d", check.name, got, check.want)
		}
	}

	var checkoutSnapshots int
	if err := tx.QueryRowContext(ctx, `
SELECT count(*)
FROM leases
WHERE id = $1
  AND status = 'terminated'
  AND settlement_detail IS NOT NULL
  AND settlement_detail->>'export_available' = 'true'
`, checkoutLeaseID).Scan(&checkoutSnapshots); err != nil {
		return fmt.Errorf("verify dev seed checkout snapshot: %w", err)
	}
	if checkoutSnapshots != 1 {
		return fmt.Errorf("verify dev seed checkout snapshot: got %d, want 1", checkoutSnapshots)
	}

	var reportSnapshots int
	if err := tx.QueryRowContext(ctx, `
SELECT count(*)
FROM monthly_snapshots
WHERE property_id = $1
  AND year = $2
  AND month = $3
`, propertyID, reportYear, reportMonth).Scan(&reportSnapshots); err != nil {
		return fmt.Errorf("verify dev seed report snapshot mode: %w", err)
	}
	if isReportCurrent(now) && reportSnapshots != 0 {
		return fmt.Errorf("verify dev seed report snapshot mode: current report month should remain live")
	}
	if !isReportCurrent(now) && reportSnapshots != 1 {
		return fmt.Errorf("verify dev seed report snapshot mode: got %d historical snapshots, want 1", reportSnapshots)
	}

	return nil
}

func isReportCurrent(now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(taipeiLocation)
	return local.Year() == reportYear && int(local.Month()) == reportMonth
}

func mustLoadLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("Asia/Taipei", 8*60*60)
	}
	return location
}

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
