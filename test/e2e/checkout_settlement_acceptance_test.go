//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2ELeaseCheckoutSettlementAcceptance(t *testing.T) {
	cfg, err := loadE2EConfig()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := resetAndMigrateDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adminToken, err := issueFirebaseEmulatorToken(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedAuthenticatedUser(ctx, db, adminToken.UID, cfg.TestEmail); err != nil {
		t.Fatal(err)
	}

	adminClient := newAPIClient(cfg.BaseURL, adminToken.IDToken)
	property := createProperty(t, ctx, adminClient, "E2E Checkout Property")
	room := createRoom(t, ctx, adminClient, property.ID, "E2E Checkout Room 101")
	tenant := terminationE2ECreateTenant(t, ctx, adminClient, cfg.TestEmail, "checkout")
	lease := billingFlowE2ECreateLease(t, ctx, adminClient, tenant.ID, room.ID, 2026, 6)
	if lease.PropertyID != property.ID {
		t.Fatalf("expected lease property_id %q, got %q", property.ID, lease.PropertyID)
	}

	checkoutE2ESettleBills(t, ctx, adminClient, lease.ID)

	preview := checkoutE2EPreview(t, ctx, adminClient, lease.ID)
	if preview.PreviewToken == nil || *preview.PreviewToken == "" {
		t.Fatalf("expected preview_token in response: %+v", preview)
	}
	if len(preview.Blockers) != 0 {
		t.Fatalf("expected no blockers, got %+v", preview.Blockers)
	}
	if preview.NetDirection != "refund" || preview.NetAmount != 33000 {
		t.Fatalf("unexpected preview net result: %+v", preview)
	}

	finalized := checkoutE2EFinalize(t, ctx, adminClient, lease.ID, *preview.PreviewToken)
	if finalized.PreviewToken != nil {
		t.Fatalf("finalized response should not include preview_token: %+v", finalized)
	}
	if !finalized.ExportAvailable {
		t.Fatalf("expected export_available true: %+v", finalized)
	}
	if finalized.FinalizedAt == nil {
		t.Fatalf("expected finalized_at: %+v", finalized)
	}

	readBack := terminationE2EGetLease(t, ctx, adminClient, lease.ID)
	if readBack.Status != "terminated" {
		t.Fatalf("expected lease status terminated, got %q", readBack.Status)
	}
	roomReadBack := getRoom(t, ctx, adminClient, room.ID)
	if roomReadBack.Status != "vacant" {
		t.Fatalf("expected room status vacant after checkout settlement, got %q", roomReadBack.Status)
	}

	resp, body, err := adminClient.getJSON(ctx, "/api/v1/leases/"+lease.ID+"/checkout-settlement/export?format=html")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	html := string(body)
	if !strings.Contains(html, "退租結算單") || !strings.Contains(html, "押金退還") || !strings.Contains(html, "清潔費") {
		t.Fatalf("expected checkout settlement HTML labels, got %s", html)
	}
}

type checkoutE2ESettlementResponse struct {
	LeaseID         string                      `json:"lease_id"`
	PreviewToken    *string                     `json:"preview_token"`
	Blockers        []checkoutE2EBlocker        `json:"blockers"`
	Lines           []checkoutE2ESettlementLine `json:"lines"`
	TotalRefund     int                         `json:"total_refund"`
	TotalCharge     int                         `json:"total_charge"`
	NetAmount       int                         `json:"net_amount"`
	NetDirection    string                      `json:"net_direction"`
	ExportAvailable bool                        `json:"export_available"`
	FinalizedAt     *string                     `json:"finalized_at"`
}

type checkoutE2EBlocker struct {
	Code     string  `json:"code"`
	Message  string  `json:"message"`
	SourceID *string `json:"source_id"`
}

type checkoutE2ESettlementLine struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
}

func checkoutE2ESettleBills(t *testing.T, ctx context.Context, client apiClient, leaseID string) {
	t.Helper()

	bills := billingFlowE2EListBills(t, ctx, client, leaseID)
	for _, bill := range bills {
		switch bill.Status {
		case "paid", "voided", "written_off":
			continue
		case "pending_meter":
			bill = billingFlowE2ERecordMeter(t, ctx, client, bill.ID, 120)
		}
		if bill.Status != "pending_payment" {
			t.Fatalf("bill %s status after meter handling = %q, want pending_payment", bill.ID, bill.Status)
		}
		if bill.Amount == nil {
			t.Fatalf("bill %s amount is nil", bill.ID)
		}
		billingFlowE2ERecordPayment(t, ctx, client, bill.ID, *bill.Amount, time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC))
	}
}

func checkoutE2EPreview(t *testing.T, ctx context.Context, client apiClient, leaseID string) checkoutE2ESettlementResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/leases/"+leaseID+"/checkout-settlement/preview", checkoutE2ERequest(""))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var result checkoutE2ESettlementResponse
	decodeJSON(t, body, &result)
	return result
}

func checkoutE2EFinalize(t *testing.T, ctx context.Context, client apiClient, leaseID string, previewToken string) checkoutE2ESettlementResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/leases/"+leaseID+"/checkout-settlement/finalize", checkoutE2ERequest(previewToken))
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var result checkoutE2ESettlementResponse
	decodeJSON(t, body, &result)
	return result
}

func checkoutE2ERequest(previewToken string) map[string]any {
	request := map[string]any{
		"checkout_date":       "2026-08-31",
		"reason":              "E2E normal checkout",
		"cleaning_fee":        3000,
		"key_card_loss_fee":   0,
		"other_fee":           0,
		"final_meter_reading": 120,
		"notes":               "E2E checkout settlement",
	}
	if previewToken != "" {
		request["preview_token"] = previewToken
	}
	return request
}
