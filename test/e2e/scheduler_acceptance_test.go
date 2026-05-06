//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestE2ESchedulerAcceptance(t *testing.T) {
	cfg, err := loadE2EConfig()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	schedulerClient := newSchedulerE2EClient(cfg.BaseURL, cfg.SchedulerKey)

	t.Run("scheduler key protects job trigger endpoints", func(t *testing.T) {
		windowKey := schedulerE2ETodayWindowKey()
		path := "/api/v1/internal/jobs/overdue-bills/scan?window_key=" + url.QueryEscape(windowKey)

		resp, body, err := schedulerClient.post(ctx, path, "")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusUnauthorized)
		requireAPIError(t, body, "UNAUTHORIZED", "Unauthorized.")

		resp, body, err = schedulerClient.post(ctx, path, "wrong-scheduler-key")
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusUnauthorized)
		requireAPIError(t, body, "UNAUTHORIZED", "Unauthorized.")
	})

	t.Run("overdue bill scan marks pending payment bills overdue", func(t *testing.T) {
		fixture := schedulerE2ECreatePastLeaseFixture(t, ctx, adminClient, cfg.TestEmail, "overdue-scan")
		bills := billingFlowE2EListBills(t, ctx, adminClient, fixture.Lease.ID)
		rentBill := schedulerE2ERequireOverdueCandidate(t, bills, schedulerE2EToday())

		result := schedulerClient.trigger(t, ctx, "/api/v1/internal/jobs/overdue-bills/scan", schedulerE2ETodayWindowKey())
		schedulerE2ERequireTriggerResult(t, result, schedulerE2ETriggerExpectation{
			JobKey:                 "overdue_bills_scan",
			Status:                 "accepted",
			MinProcessedCount:      1,
			ExpectedSkippedCount:   0,
			ExpectedFailedCount:    0,
			ExpectPositiveDuration: true,
		})

		readBack := billingFlowE2EGetBill(t, ctx, adminClient, rentBill.ID)
		if readBack.Status != "overdue" {
			t.Fatalf("expected bill %q status overdue after scheduler scan, got %q", rentBill.ID, readBack.Status)
		}
	})

	t.Run("monthly snapshot finalizes historical financial report", func(t *testing.T) {
		property := createProperty(t, ctx, adminClient, "E2E Scheduler Snapshot Property")
		room := createRoom(t, ctx, adminClient, property.ID, "E2E Scheduler Snapshot Room")
		tenant := terminationE2ECreateTenant(t, ctx, adminClient, cfg.TestEmail, "scheduler-snapshot")

		reportYear, reportMonth, paidAt := schedulerE2EPreviousReportPaymentTime()
		lease := billingFlowE2ECreateLease(t, ctx, adminClient, tenant.ID, room.ID, reportYear, reportMonth)
		if lease.PropertyID != property.ID {
			t.Fatalf("expected lease property_id %q, got %q", property.ID, lease.PropertyID)
		}

		bills := billingFlowE2EListBills(t, ctx, adminClient, lease.ID)
		rentBill := billingFlowE2EFindBill(t, bills, "rent")
		if rentBill.Amount == nil {
			t.Fatalf("expected rent bill amount: %+v", rentBill)
		}
		rentBill = billingFlowE2ERecordPayment(t, ctx, adminClient, rentBill.ID, *rentBill.Amount, paidAt)

		windowKey := fmt.Sprintf("%04d-%02d", reportYear, reportMonth)
		result := schedulerClient.trigger(t, ctx, "/api/v1/internal/jobs/monthly-snapshots/run", windowKey)
		schedulerE2ERequireTriggerResult(t, result, schedulerE2ETriggerExpectation{
			JobKey:                 "monthly_snapshot",
			Status:                 "accepted",
			MinProcessedCount:      1,
			ExpectedSkippedCount:   0,
			ExpectedFailedCount:    0,
			ExpectPositiveDuration: true,
		})

		report := billingFlowE2EGetFinancialReport(t, ctx, adminClient, property.ID, reportYear, reportMonth)
		if !report.IsFinalized {
			t.Fatalf("expected finalized report after monthly snapshot job")
		}
		if report.PropertyID != property.ID {
			t.Fatalf("expected report property_id %q, got %q", property.ID, report.PropertyID)
		}
		if report.Year != reportYear || report.Month != reportMonth {
			t.Fatalf("expected report period %04d-%02d, got %04d-%02d", reportYear, reportMonth, report.Year, report.Month)
		}
		if report.TotalIncome < *rentBill.Amount {
			t.Fatalf("expected total_income at least %d, got %d", *rentBill.Amount, report.TotalIncome)
		}
		billingFlowE2ERequireReportEntry(t, report, "rent_payment", *rentBill.Amount)
	})
}

type schedulerE2EClient struct {
	baseURL      string
	schedulerKey string
	client       *http.Client
}

type schedulerE2ETriggerResponse struct {
	JobKey      string                        `json:"job_key"`
	Status      string                        `json:"status"`
	WindowKey   string                        `json:"window_key"`
	RetryCount  *int                          `json:"retry_count"`
	DurationMs  *int                          `json:"duration_ms"`
	Summary     *schedulerE2EExecutionSummary `json:"summary"`
	RequestedAt string                        `json:"requested_at"`
	RequestID   *string                       `json:"request_id"`
	Message     *string                       `json:"message"`
}

type schedulerE2EExecutionSummary struct {
	ProcessedCount *int    `json:"processed_count"`
	SkippedCount   *int    `json:"skipped_count"`
	FailedCount    *int    `json:"failed_count"`
	Note           *string `json:"note"`
}

type schedulerE2ETriggerExpectation struct {
	JobKey                 string
	Status                 string
	MinProcessedCount      int
	ExpectedSkippedCount   int
	ExpectedFailedCount    int
	ExpectPositiveDuration bool
}

type schedulerE2ELeaseFixture struct {
	Property e2ePropertyResponse
	Room     e2eRoomResponse
	Tenant   e2eTenantResponse
	Lease    billingFlowE2ELeaseResponse
}

func newSchedulerE2EClient(baseURL string, schedulerKey string) schedulerE2EClient {
	return schedulerE2EClient{
		baseURL:      baseURL,
		schedulerKey: schedulerKey,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (c schedulerE2EClient) trigger(t *testing.T, ctx context.Context, path string, windowKey string) schedulerE2ETriggerResponse {
	t.Helper()

	resp, body, err := c.post(ctx, path+"?window_key="+url.QueryEscape(windowKey), c.schedulerKey)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusAccepted)

	var result schedulerE2ETriggerResponse
	decodeJSON(t, body, &result)
	if result.WindowKey != windowKey {
		t.Fatalf("expected scheduler window_key %q, got %q", windowKey, result.WindowKey)
	}
	return result
}

func (c schedulerE2EClient) post(ctx context.Context, path string, schedulerKey string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build scheduler request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if schedulerKey != "" {
		req.Header.Set("X-Scheduler-Key", schedulerKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("call scheduler API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read scheduler response body: %w", err)
	}
	return resp, body, nil
}

func schedulerE2ECreatePastLeaseFixture(t *testing.T, ctx context.Context, client apiClient, baseEmail string, suffix string) schedulerE2ELeaseFixture {
	t.Helper()

	property := createProperty(t, ctx, client, "E2E Scheduler Property "+suffix)
	room := createRoom(t, ctx, client, property.ID, "E2E Scheduler Room "+suffix)
	tenant := terminationE2ECreateTenant(t, ctx, client, baseEmail, suffix)

	year, month := schedulerE2EOverdueLeaseStartPeriod()
	lease := billingFlowE2ECreateLease(t, ctx, client, tenant.ID, room.ID, year, month)

	return schedulerE2ELeaseFixture{
		Property: property,
		Room:     room,
		Tenant:   tenant,
		Lease:    lease,
	}
}

func schedulerE2ERequireOverdueCandidate(t *testing.T, bills []billingFlowE2EBillResponse, today time.Time) billingFlowE2EBillResponse {
	t.Helper()

	for _, bill := range bills {
		if bill.Type != "rent" || bill.Status != "pending_payment" {
			continue
		}
		dueDate, err := time.Parse("2006-01-02", bill.DueDate)
		if err != nil {
			t.Fatalf("parse bill due_date %q: %v", bill.DueDate, err)
		}
		if dueDate.Before(today) {
			return bill
		}
	}
	t.Fatalf("expected pending rent bill due before %s in %+v", today.Format("2006-01-02"), bills)
	return billingFlowE2EBillResponse{}
}

func schedulerE2ERequireTriggerResult(t *testing.T, result schedulerE2ETriggerResponse, expected schedulerE2ETriggerExpectation) {
	t.Helper()

	if result.JobKey != expected.JobKey {
		t.Fatalf("expected job_key %q, got %q", expected.JobKey, result.JobKey)
	}
	if result.Status != expected.Status {
		t.Fatalf("expected scheduler status %q, got %q", expected.Status, result.Status)
	}
	if result.Summary == nil {
		t.Fatalf("expected scheduler summary in response: %+v", result)
	}
	processedCount := intValue(result.Summary.ProcessedCount)
	if processedCount < expected.MinProcessedCount {
		t.Fatalf("expected processed_count at least %d, got %d", expected.MinProcessedCount, processedCount)
	}
	if skippedCount := intValue(result.Summary.SkippedCount); skippedCount != expected.ExpectedSkippedCount {
		t.Fatalf("expected skipped_count %d, got %d", expected.ExpectedSkippedCount, skippedCount)
	}
	if failedCount := intValue(result.Summary.FailedCount); failedCount != expected.ExpectedFailedCount {
		t.Fatalf("expected failed_count %d, got %d", expected.ExpectedFailedCount, failedCount)
	}
	if expected.ExpectPositiveDuration && result.DurationMs == nil {
		t.Fatalf("expected duration_ms in scheduler response: %+v", result)
	}
}

func schedulerE2EToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func schedulerE2ETodayWindowKey() string {
	return schedulerE2EToday().Format("2006-01-02")
}

func schedulerE2EOverdueLeaseStartPeriod() (int, int) {
	start := schedulerE2EToday().AddDate(0, -2, 0)
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start.Year(), int(start.Month())
}

func schedulerE2EPreviousReportPaymentTime() (int, int, time.Time) {
	location := time.FixedZone("Asia/Taipei", 8*60*60)
	now := time.Now().In(location)
	periodStart := time.Date(now.Year(), now.Month(), 1, 12, 0, 0, 0, location).AddDate(0, -1, 0)
	paidAt := time.Date(periodStart.Year(), periodStart.Month(), 15, 12, 0, 0, 0, location)
	return paidAt.Year(), int(paidAt.Month()), paidAt
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
