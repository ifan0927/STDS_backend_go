//go:build legacye2e

package legacye2e

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"stds_backend/internal/platform/database"
)

func TestLegacyE2EMigratedDataReadChecks(t *testing.T) {
	cfg, err := loadLegacyE2EConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ValidationReportPath != "" {
		if _, err := os.Stat(cfg.ValidationReportPath); err != nil {
			t.Fatalf("legacy validation report is required before legacy E2E sign-off: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	token, err := issueLegacyFirebaseEmulatorToken(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedLegacyE2EAdminUser(ctx, db, token.UID, cfg.TestEmail); err != nil {
		t.Fatal(err)
	}

	client := newLegacyAPIClient(cfg.BaseURL, token.IDToken)
	targets := discoverLegacyReadTargets(t, ctx, db)

	t.Run("health endpoint reaches migrated database", func(t *testing.T) {
		resp, body, err := client.getJSON(ctx, "/health")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusOK)

		var payload struct {
			OK bool `json:"ok"`
			DB bool `json:"db"`
		}
		decodeJSON(t, body, &payload)
		if !payload.OK || !payload.DB {
			t.Fatalf("expected healthy API and database, got ok=%t db=%t body=%s", payload.OK, payload.DB, string(body))
		}
	})

	t.Run("property read path returns mapped legacy property", func(t *testing.T) {
		var property struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			OwnerID string `json:"owner_id"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/properties/"+targets.PropertyID, &property)
		if property.ID != targets.PropertyID {
			t.Fatalf("expected property id %q, got %q", targets.PropertyID, property.ID)
		}
		if property.Name == "" {
			t.Fatalf("expected property name in response")
		}
		if property.OwnerID == "" {
			t.Fatalf("expected property owner_id in response")
		}
	})

	t.Run("room read path returns mapped legacy room relationship", func(t *testing.T) {
		var room struct {
			ID         string `json:"id"`
			PropertyID string `json:"property_id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/rooms/"+targets.RoomID, &room)
		if room.ID != targets.RoomID {
			t.Fatalf("expected room id %q, got %q", targets.RoomID, room.ID)
		}
		if room.PropertyID != targets.RoomPropertyID {
			t.Fatalf("expected room property_id %q, got %q", targets.RoomPropertyID, room.PropertyID)
		}
		if room.Name == "" || room.Status == "" {
			t.Fatalf("expected room name and status in response")
		}
	})

	t.Run("tenant read path returns mapped legacy tenant", func(t *testing.T) {
		var tenant struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Status   string `json:"status"`
			Contacts []any  `json:"contacts"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/tenants/"+targets.TenantID, &tenant)
		if tenant.ID != targets.TenantID {
			t.Fatalf("expected tenant id %q, got %q", targets.TenantID, tenant.ID)
		}
		if tenant.Name == "" || tenant.Status == "" {
			t.Fatalf("expected tenant name and status in response")
		}
		if tenant.Contacts == nil {
			t.Fatalf("expected tenant contacts array in response")
		}
	})

	t.Run("lease read path returns mapped legacy lease relationships", func(t *testing.T) {
		var lease struct {
			ID                        string `json:"id"`
			TenantID                  string `json:"tenant_id"`
			RoomID                    string `json:"room_id"`
			Status                    string `json:"status"`
			RentBillingCadence        string `json:"rent_billing_cadence"`
			ElectricityBillingCadence string `json:"electricity_billing_cadence"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/leases/"+targets.LeaseID, &lease)
		if lease.ID != targets.LeaseID {
			t.Fatalf("expected lease id %q, got %q", targets.LeaseID, lease.ID)
		}
		if lease.TenantID != targets.LeaseTenantID {
			t.Fatalf("expected lease tenant_id %q, got %q", targets.LeaseTenantID, lease.TenantID)
		}
		if lease.RoomID != targets.LeaseRoomID {
			t.Fatalf("expected lease room_id %q, got %q", targets.LeaseRoomID, lease.RoomID)
		}
		if lease.Status == "" || lease.RentBillingCadence == "" || lease.ElectricityBillingCadence == "" {
			t.Fatalf("expected lease status and cadence fields in response")
		}
	})

	t.Run("bill read path returns mapped legacy bill scope", func(t *testing.T) {
		var bill struct {
			ID         string `json:"id"`
			LeaseID    string `json:"lease_id"`
			TenantID   string `json:"tenant_id"`
			RoomID     string `json:"room_id"`
			PropertyID string `json:"property_id"`
			Type       string `json:"type"`
			Status     string `json:"status"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/bills/"+targets.BillID, &bill)
		if bill.ID != targets.BillID {
			t.Fatalf("expected bill id %q, got %q", targets.BillID, bill.ID)
		}
		if bill.LeaseID != targets.BillLeaseID || bill.TenantID != targets.BillTenantID || bill.RoomID != targets.BillRoomID || bill.PropertyID != targets.BillPropertyID {
			t.Fatalf("unexpected bill scope: %+v", bill)
		}
		if bill.Type == "" || bill.Status == "" {
			t.Fatalf("expected bill type and status in response")
		}
	})

	t.Run("financial report read path returns migrated snapshot when available", func(t *testing.T) {
		if targets.FinancialReport == nil {
			t.Skip("no finalized monthly snapshot was found for mapped legacy properties")
		}

		reportTarget := targets.FinancialReport
		var report struct {
			PropertyID  string `json:"property_id"`
			Year        int    `json:"year"`
			Month       int    `json:"month"`
			IsFinalized bool   `json:"is_finalized"`
			Entries     []any  `json:"entries"`
		}
		path := fmt.Sprintf("/api/v1/properties/%s/financial-report/%d/%d", reportTarget.PropertyID, reportTarget.Year, reportTarget.Month)
		getLegacyJSON(t, ctx, client, path, &report)
		if report.PropertyID != reportTarget.PropertyID || report.Year != reportTarget.Year || report.Month != reportTarget.Month {
			t.Fatalf("unexpected financial report identity: %+v", report)
		}
		if !report.IsFinalized {
			t.Fatalf("expected finalized migrated financial report")
		}
		if report.Entries == nil {
			t.Fatalf("expected financial report entries array in response")
		}
	})

	t.Run("journal read path returns mapped legacy journal log", func(t *testing.T) {
		var journal struct {
			ID         string `json:"id"`
			PropertyID string `json:"property_id"`
			AuthorID   string `json:"author_id"`
			Content    string `json:"content"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/journal-logs/"+targets.JournalLogID, &journal)
		if journal.ID != targets.JournalLogID {
			t.Fatalf("expected journal id %q, got %q", targets.JournalLogID, journal.ID)
		}
		if journal.PropertyID == "" || journal.AuthorID == "" || journal.Content == "" {
			t.Fatalf("expected journal scope and content in response")
		}
	})

	t.Run("repair read path returns mapped legacy repair request", func(t *testing.T) {
		var repair struct {
			ID          string `json:"id"`
			PropertyID  string `json:"property_id"`
			RoomID      string `json:"room_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Status      string `json:"status"`
		}
		getLegacyJSON(t, ctx, client, "/api/v1/repair-requests/"+targets.RepairRequestID, &repair)
		if repair.ID != targets.RepairRequestID {
			t.Fatalf("expected repair request id %q, got %q", targets.RepairRequestID, repair.ID)
		}
		if repair.PropertyID == "" || repair.RoomID == "" || repair.Title == "" || repair.Description == "" || repair.Status == "" {
			t.Fatalf("expected repair scope, text, and status in response")
		}
	})

	t.Run("attachment metadata read path returns mapped resource attachment when available", func(t *testing.T) {
		if targets.Attachment == nil {
			t.Skip("no attachment metadata was found for mapped legacy resources")
		}

		var list struct {
			Data []struct {
				ID       string `json:"id"`
				FileName string `json:"file_name"`
			} `json:"data"`
		}
		getLegacyJSON(t, ctx, client, targets.Attachment.ListPath, &list)
		for _, attachment := range list.Data {
			if attachment.ID == targets.Attachment.ID {
				if attachment.FileName == "" {
					t.Fatalf("expected attachment file_name in response")
				}
				return
			}
		}
		t.Fatalf("expected attachment %q in %s response", targets.Attachment.ID, targets.Attachment.ListPath)
	})
}

type legacyReadTargets struct {
	PropertyID      string
	RoomID          string
	RoomPropertyID  string
	TenantID        string
	LeaseID         string
	LeaseTenantID   string
	LeaseRoomID     string
	BillID          string
	BillLeaseID     string
	BillTenantID    string
	BillRoomID      string
	BillPropertyID  string
	JournalLogID    string
	RepairRequestID string
	FinancialReport *legacyFinancialReportTarget
	Attachment      *legacyAttachmentTarget
}

type legacyFinancialReportTarget struct {
	PropertyID string
	Year       int
	Month      int
}

type legacyAttachmentTarget struct {
	ID       string
	ListPath string
}

func discoverLegacyReadTargets(t *testing.T, ctx context.Context, db *sql.DB) legacyReadTargets {
	t.Helper()

	targets := legacyReadTargets{
		PropertyID:      requireLegacyString(t, ctx, db, "mapped legacy property", mappedLegacyPropertyQuery),
		TenantID:        requireLegacyString(t, ctx, db, "mapped legacy tenant", mappedLegacyTenantQuery),
		JournalLogID:    requireLegacyString(t, ctx, db, "mapped legacy journal log", mappedLegacyJournalLogQuery),
		RepairRequestID: requireLegacyString(t, ctx, db, "mapped legacy repair request", mappedLegacyRepairRequestQuery),
	}

	requireLegacyRow(t, ctx, db, "mapped legacy room", mappedLegacyRoomQuery, &targets.RoomID, &targets.RoomPropertyID)
	requireLegacyRow(t, ctx, db, "mapped legacy lease", mappedLegacyLeaseQuery, &targets.LeaseID, &targets.LeaseTenantID, &targets.LeaseRoomID)
	requireLegacyRow(t, ctx, db, "mapped legacy bill", mappedLegacyBillQuery, &targets.BillID, &targets.BillLeaseID, &targets.BillTenantID, &targets.BillRoomID, &targets.BillPropertyID)

	var report legacyFinancialReportTarget
	if err := queryOptionalLegacyRow(ctx, db, mappedLegacyFinancialReportQuery, &report.PropertyID, &report.Year, &report.Month); err != nil {
		t.Fatalf("discover mapped legacy financial report: %v", err)
	}
	if report.PropertyID != "" {
		targets.FinancialReport = &report
	}

	var attachment legacyAttachmentTarget
	if err := queryOptionalLegacyRow(ctx, db, mappedLegacyAttachmentQuery, &attachment.ID, &attachment.ListPath); err != nil {
		t.Fatalf("discover mapped legacy attachment metadata: %v", err)
	}
	if attachment.ID != "" {
		targets.Attachment = &attachment
	}

	return targets
}

func getLegacyJSON(t *testing.T, ctx context.Context, client legacyAPIClient, path string, target any) {
	t.Helper()

	resp, body, err := client.getJSON(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)
	decodeJSON(t, body, target)
}

func requireLegacyString(t *testing.T, ctx context.Context, db *sql.DB, name string, query string) string {
	t.Helper()

	var value string
	requireLegacyRow(t, ctx, db, name, query, &value)
	return value
}

func requireLegacyRow(t *testing.T, ctx context.Context, db *sql.DB, name string, query string, destinations ...any) {
	t.Helper()

	err := db.QueryRowContext(ctx, query).Scan(destinations...)
	if err == nil {
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing required %s; prepare the legacy E2E database with full legacy import and validation first", name)
	}
	t.Fatalf("discover %s: %v", name, err)
}

func queryOptionalLegacyRow(ctx context.Context, db *sql.DB, query string, destinations ...any) error {
	err := db.QueryRowContext(ctx, query).Scan(destinations...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

const mappedLegacyPropertyQuery = `
SELECT p.id::text
FROM legacy_property_mappings m
JOIN properties p ON p.id = m.property_id
WHERE p.deleted_at IS NULL
ORDER BY m.legacy_estate_id
LIMIT 1
`

const mappedLegacyRoomQuery = `
SELECT r.id::text, r.property_id::text
FROM legacy_room_mappings m
JOIN rooms r ON r.id = m.room_id
JOIN properties p ON p.id = r.property_id AND p.deleted_at IS NULL
WHERE r.deleted_at IS NULL
ORDER BY m.legacy_room_id
LIMIT 1
`

const mappedLegacyTenantQuery = `
SELECT t.id::text
FROM legacy_tenant_mappings m
JOIN tenants t ON t.id = m.tenant_id
WHERE t.deleted_at IS NULL
ORDER BY m.legacy_tenant_id
LIMIT 1
`

const mappedLegacyLeaseQuery = `
SELECT l.id::text, l.tenant_id::text, l.room_id::text
FROM legacy_lease_mappings m
JOIN leases l ON l.id = m.lease_id
JOIN tenants t ON t.id = l.tenant_id AND t.deleted_at IS NULL
JOIN rooms r ON r.id = l.room_id AND r.deleted_at IS NULL
JOIN properties p ON p.id = l.property_id AND p.deleted_at IS NULL
WHERE l.deleted_at IS NULL
ORDER BY m.legacy_rent_id
LIMIT 1
`

const mappedLegacyBillQuery = `
SELECT b.id::text, b.lease_id::text, b.tenant_id::text, b.room_id::text, b.property_id::text
FROM legacy_bill_mappings m
JOIN bills b ON b.id = m.bill_id
JOIN leases l ON l.id = b.lease_id AND l.deleted_at IS NULL
JOIN tenants t ON t.id = b.tenant_id AND t.deleted_at IS NULL
JOIN rooms r ON r.id = b.room_id AND r.deleted_at IS NULL
JOIN properties p ON p.id = b.property_id AND p.deleted_at IS NULL
WHERE b.deleted_at IS NULL
ORDER BY m.legacy_bill_key
LIMIT 1
`

const mappedLegacyFinancialReportQuery = `
SELECT ms.property_id::text, ms.year::int, ms.month::int
FROM monthly_snapshots ms
JOIN legacy_property_mappings m ON m.property_id = ms.property_id
JOIN properties p ON p.id = ms.property_id AND p.deleted_at IS NULL
ORDER BY ms.year DESC, ms.month DESC
LIMIT 1
`

const mappedLegacyJournalLogQuery = `
SELECT jl.id::text
FROM legacy_schedule_mappings m
JOIN journal_logs jl ON m.target_table = 'journal_logs' AND jl.id = m.target_id
JOIN properties p ON p.id = jl.property_id AND p.deleted_at IS NULL
WHERE jl.deleted_at IS NULL
ORDER BY m.legacy_schedule_id
LIMIT 1
`

const mappedLegacyRepairRequestQuery = `
SELECT rr.id::text
FROM legacy_schedule_mappings m
JOIN repair_requests rr ON m.target_table = 'repair_requests' AND rr.id = m.target_id
JOIN properties p ON p.id = rr.property_id AND p.deleted_at IS NULL
JOIN rooms r ON r.id = rr.room_id AND r.deleted_at IS NULL
WHERE rr.deleted_at IS NULL
ORDER BY m.legacy_schedule_id
LIMIT 1
`

const mappedLegacyAttachmentQuery = `
SELECT id, list_path
FROM (
	SELECT pa.id::text AS id, '/api/v1/properties/' || pa.property_id::text || '/attachments' AS list_path
	FROM property_attachments pa
	JOIN legacy_property_mappings m ON m.property_id = pa.property_id
	WHERE pa.deleted_at IS NULL

	UNION ALL

	SELECT ra.id::text AS id, '/api/v1/rooms/' || ra.room_id::text || '/attachments' AS list_path
	FROM room_attachments ra
	JOIN legacy_room_mappings m ON m.room_id = ra.room_id
	WHERE ra.deleted_at IS NULL

	UNION ALL

	SELECT ta.id::text AS id, '/api/v1/tenants/' || ta.tenant_id::text || '/attachments' AS list_path
	FROM tenant_attachments ta
	JOIN legacy_tenant_mappings m ON m.tenant_id = ta.tenant_id
	WHERE ta.deleted_at IS NULL

	UNION ALL

	SELECT la.id::text AS id, '/api/v1/leases/' || la.lease_id::text || '/attachments' AS list_path
	FROM lease_attachments la
	JOIN legacy_lease_mappings m ON m.lease_id = la.lease_id
	WHERE la.deleted_at IS NULL

	UNION ALL

	SELECT ba.id::text AS id, '/api/v1/bills/' || ba.bill_id::text || '/attachments' AS list_path
	FROM bill_attachments ba
	JOIN legacy_bill_mappings m ON m.bill_id = ba.bill_id
	WHERE ba.deleted_at IS NULL

	UNION ALL

	SELECT jla.id::text AS id, '/api/v1/journal-logs/' || jla.journal_log_id::text || '/attachments' AS list_path
	FROM journal_log_attachments jla
	JOIN legacy_schedule_mappings m ON m.target_table = 'journal_logs' AND m.target_id = jla.journal_log_id
	WHERE jla.deleted_at IS NULL

	UNION ALL

	SELECT rra.id::text AS id, '/api/v1/repair-requests/' || rra.repair_request_id::text || '/attachments' AS list_path
	FROM repair_request_attachments rra
	JOIN legacy_schedule_mappings m ON m.target_table = 'repair_requests' AND m.target_id = rra.repair_request_id
	WHERE rra.deleted_at IS NULL
) mapped_attachments
ORDER BY id
LIMIT 1
`
