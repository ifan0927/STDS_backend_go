//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestE2EJournalAcceptance(t *testing.T) {
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
	assignedProperty := createProperty(t, ctx, adminClient, "E2E Journal Assigned Property")
	unassignedProperty := createProperty(t, ctx, adminClient, "E2E Journal Unassigned Property")
	room := createRoom(t, ctx, adminClient, assignedProperty.ID, "E2E Journal Room")

	scopedEmail := e2eEmail(cfg.TestEmail, "journal-scoped")
	scopedToken, err := issueFirebaseEmulatorTokenForCredentials(ctx, cfg, scopedEmail, cfg.TestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededScopedUserID,
		FirebaseUID:         scopedToken.UID,
		Email:               scopedEmail,
		Name:                "E2E Journal Organizer",
		Role:                "organizer",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}
	scopedClient := newAPIClient(cfg.BaseURL, scopedToken.IDToken)

	t.Run("create list detail update and report expense", func(t *testing.T) {
		plain := journalE2ECreateLog(t, ctx, scopedClient, journalE2ECreateRequest{
			PropertyID: assignedProperty.ID,
			Content:    "E2E property inspection",
		})
		if plain.ExpenseAmount != nil {
			t.Fatalf("expected plain journal expense_amount nil, got %+v", plain.ExpenseAmount)
		}

		expense := 3500
		description := "Pipe repair"
		withExpense := journalE2ECreateLog(t, ctx, scopedClient, journalE2ECreateRequest{
			PropertyID:         assignedProperty.ID,
			RoomID:             &room.ID,
			Content:            "E2E bathroom repair",
			ExpenseAmount:      &expense,
			ExpenseDescription: &description,
		})
		journalE2ERequireLog(t, withExpense, journalE2EExpectation{
			PropertyID:         assignedProperty.ID,
			RoomID:             &room.ID,
			Content:            "E2E bathroom repair",
			ExpenseAmount:      &expense,
			ExpenseDescription: &description,
		})

		list := journalE2EListLogs(t, ctx, scopedClient, assignedProperty.ID)
		journalE2ERequireListContains(t, list, plain.ID)
		journalE2ERequireListContains(t, list, withExpense.ID)

		detail := journalE2EGetLog(t, ctx, scopedClient, withExpense.ID)
		journalE2ERequireLog(t, detail, journalE2EExpectation{
			PropertyID:         assignedProperty.ID,
			RoomID:             &room.ID,
			Content:            "E2E bathroom repair",
			ExpenseAmount:      &expense,
			ExpenseDescription: &description,
		})

		updatedExpense := 4200
		updatedDescription := "Pipe repair and cleanup"
		updated := journalE2EUpdateLog(t, ctx, scopedClient, withExpense.ID, map[string]any{
			"content":             "E2E bathroom repair updated",
			"expense_amount":      updatedExpense,
			"expense_description": updatedDescription,
		})
		journalE2ERequireLog(t, updated, journalE2EExpectation{
			PropertyID:         assignedProperty.ID,
			RoomID:             &room.ID,
			Content:            "E2E bathroom repair updated",
			ExpenseAmount:      &updatedExpense,
			ExpenseDescription: &updatedDescription,
		})

		year, month := journalE2ECurrentReportPeriod()
		report := billingFlowE2EGetFinancialReport(t, ctx, scopedClient, assignedProperty.ID, year, month)
		if report.TotalExpense != updatedExpense {
			t.Fatalf("expected total_expense %d, got %d", updatedExpense, report.TotalExpense)
		}
		if report.Net != -updatedExpense {
			t.Fatalf("expected net %d, got %d", -updatedExpense, report.Net)
		}
		journalE2ERequireSingleReportEntry(t, report, "journal_expense", updatedExpense)
	})

	t.Run("unassigned property access is rejected", func(t *testing.T) {
		unassignedLog := journalE2ECreateLog(t, ctx, adminClient, journalE2ECreateRequest{
			PropertyID: unassignedProperty.ID,
			Content:    "E2E unassigned journal",
		})

		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/journal-logs/"+unassignedLog.ID)
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")

		resp, body, err = scopedClient.postJSON(ctx, "/api/v1/journal-logs", map[string]any{
			"property_id": unassignedProperty.ID,
			"content":     "E2E forbidden journal create",
		})
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")
	})
}

type journalE2ECreateRequest struct {
	PropertyID         string
	RoomID             *string
	Content            string
	ExpenseAmount      *int
	ExpenseDescription *string
}

type journalE2EExpectation struct {
	PropertyID         string
	RoomID             *string
	Content            string
	ExpenseAmount      *int
	ExpenseDescription *string
}

type journalE2ELogResponse struct {
	ID                 string  `json:"id"`
	PropertyID         string  `json:"property_id"`
	RoomID             *string `json:"room_id"`
	AuthorID           string  `json:"author_id"`
	Content            string  `json:"content"`
	ExpenseAmount      *int    `json:"expense_amount"`
	ExpenseDescription *string `json:"expense_description"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

type journalE2EListResponse struct {
	Data []journalE2ELogResponse `json:"data"`
}

func journalE2ECreateLog(t *testing.T, ctx context.Context, client apiClient, request journalE2ECreateRequest) journalE2ELogResponse {
	t.Helper()

	body := map[string]any{
		"property_id": request.PropertyID,
		"content":     request.Content,
	}
	if request.RoomID != nil {
		body["room_id"] = *request.RoomID
	}
	if request.ExpenseAmount != nil {
		body["expense_amount"] = *request.ExpenseAmount
	}
	if request.ExpenseDescription != nil {
		body["expense_description"] = *request.ExpenseDescription
	}

	resp, respBody, err := client.postJSON(ctx, "/api/v1/journal-logs", body)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, respBody, http.StatusCreated)

	var log journalE2ELogResponse
	decodeJSON(t, respBody, &log)
	if log.ID == "" {
		t.Fatalf("expected journal id in response: %s", string(respBody))
	}
	return log
}

func journalE2EUpdateLog(t *testing.T, ctx context.Context, client apiClient, journalLogID string, request map[string]any) journalE2ELogResponse {
	t.Helper()

	resp, body, err := client.patchJSON(ctx, "/api/v1/journal-logs/"+journalLogID, request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var log journalE2ELogResponse
	decodeJSON(t, body, &log)
	return log
}

func journalE2EGetLog(t *testing.T, ctx context.Context, client apiClient, journalLogID string) journalE2ELogResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/journal-logs/"+journalLogID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var log journalE2ELogResponse
	decodeJSON(t, body, &log)
	return log
}

func journalE2EListLogs(t *testing.T, ctx context.Context, client apiClient, propertyID string) []journalE2ELogResponse {
	t.Helper()

	path := fmt.Sprintf("/api/v1/journal-logs?property_id=%s&limit=100", url.QueryEscape(propertyID))
	resp, body, err := client.getJSON(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list journalE2EListResponse
	decodeJSON(t, body, &list)
	return list.Data
}

func journalE2ERequireLog(t *testing.T, log journalE2ELogResponse, expected journalE2EExpectation) {
	t.Helper()

	if log.PropertyID != expected.PropertyID {
		t.Fatalf("expected property_id %q, got %q", expected.PropertyID, log.PropertyID)
	}
	if !stringPtrEqual(log.RoomID, expected.RoomID) {
		t.Fatalf("expected room_id %+v, got %+v", expected.RoomID, log.RoomID)
	}
	if log.Content != expected.Content {
		t.Fatalf("expected content %q, got %q", expected.Content, log.Content)
	}
	if !intPtrEqual(log.ExpenseAmount, expected.ExpenseAmount) {
		t.Fatalf("expected expense_amount %+v, got %+v", expected.ExpenseAmount, log.ExpenseAmount)
	}
	if !stringPtrEqual(log.ExpenseDescription, expected.ExpenseDescription) {
		t.Fatalf("expected expense_description %+v, got %+v", expected.ExpenseDescription, log.ExpenseDescription)
	}
}

func journalE2ERequireListContains(t *testing.T, logs []journalE2ELogResponse, journalLogID string) {
	t.Helper()

	for _, log := range logs {
		if log.ID == journalLogID {
			return
		}
	}
	t.Fatalf("expected journal log %q in list: %+v", journalLogID, logs)
}

func journalE2ERequireSingleReportEntry(t *testing.T, report billingFlowE2EFinancialReportResponse, category string, amount int) {
	t.Helper()

	count := 0
	for _, entry := range report.Entries {
		if entry.Category == category && entry.Amount == amount {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one report entry category %q amount %d, got %d in %+v", category, amount, count, report.Entries)
	}
}

func journalE2ECurrentReportPeriod() (int, int) {
	location := time.FixedZone("Asia/Taipei", 8*60*60)
	now := time.Now().In(location)
	return now.Year(), int(now.Month())
}

func stringPtrEqual(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func intPtrEqual(left *int, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
