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

const seededRepairStaffUserID = "00000000-0000-0000-0000-000000000094"

func TestE2ERepairAcceptance(t *testing.T) {
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
	assignedProperty := createProperty(t, ctx, adminClient, "E2E Repair Assigned Property")
	unassignedProperty := createProperty(t, ctx, adminClient, "E2E Repair Unassigned Property")
	assignedRoom := createRoom(t, ctx, adminClient, assignedProperty.ID, "E2E Repair Assigned Room")
	unassignedRoom := createRoom(t, ctx, adminClient, unassignedProperty.ID, "E2E Repair Unassigned Room")

	scopedEmail := e2eEmail(cfg.TestEmail, "repair-scoped")
	scopedToken, err := issueFirebaseEmulatorTokenForCredentials(ctx, cfg, scopedEmail, cfg.TestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededScopedUserID,
		FirebaseUID:         scopedToken.UID,
		Email:               scopedEmail,
		Name:                "E2E Repair Organizer",
		Role:                "organizer",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededRepairStaffUserID,
		FirebaseUID:         "e2e-repair-staff",
		Email:               e2eEmail(cfg.TestEmail, "repair-staff"),
		Name:                "E2E Repair Staff",
		Role:                "staff",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}
	scopedClient := newAPIClient(cfg.BaseURL, scopedToken.IDToken)

	t.Run("create list detail assign progress and complete", func(t *testing.T) {
		repairRequest := repairE2ECreateRequest(t, ctx, scopedClient, repairE2ECreateParams{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
		})
		repairE2ERequireRequest(t, repairRequest, repairE2EExpectation{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			SubmittedBy: seededScopedUserID,
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
			Status:      "submitted",
		})

		list := repairE2EListRequests(t, ctx, scopedClient, assignedProperty.ID)
		repairE2ERequireListContains(t, list, repairRequest.ID)

		detail := repairE2EGetRequest(t, ctx, scopedClient, repairRequest.ID)
		repairE2ERequireRequest(t, detail, repairE2EExpectation{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			SubmittedBy: seededScopedUserID,
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
			Status:      "submitted",
		})

		assigned := repairE2EAssignRequest(t, ctx, scopedClient, repairRequest.ID, seededRepairStaffUserID)
		repairE2ERequireRequest(t, assigned, repairE2EExpectation{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			SubmittedBy: seededScopedUserID,
			AssignedTo:  ptrToString(seededRepairStaffUserID),
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
			Status:      "assigned",
		})
		if assigned.AssignedAt == nil || *assigned.AssignedAt == "" {
			t.Fatalf("expected assigned_at in response: %+v", assigned)
		}

		inProgress := repairE2EProgressRequest(t, ctx, scopedClient, repairRequest.ID)
		repairE2ERequireRequest(t, inProgress, repairE2EExpectation{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			SubmittedBy: seededScopedUserID,
			AssignedTo:  ptrToString(seededRepairStaffUserID),
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
			Status:      "in_progress",
		})

		completed := repairE2ECompleteRequest(t, ctx, scopedClient, repairRequest.ID)
		repairE2ERequireRequest(t, completed, repairE2EExpectation{
			PropertyID:  assignedProperty.ID,
			RoomID:      assignedRoom.ID,
			SubmittedBy: seededScopedUserID,
			AssignedTo:  ptrToString(seededRepairStaffUserID),
			Title:       "E2E faucet leak",
			Description: "Bathroom faucet leaks during the night",
			Status:      "completed",
		})
		if completed.CompletedAt == nil || *completed.CompletedAt == "" {
			t.Fatalf("expected completed_at in response: %+v", completed)
		}
	})

	t.Run("unassigned property access is rejected", func(t *testing.T) {
		unassignedRepair := repairE2ECreateRequest(t, ctx, adminClient, repairE2ECreateParams{
			PropertyID:  unassignedProperty.ID,
			RoomID:      unassignedRoom.ID,
			Title:       "E2E unassigned repair",
			Description: "Repair belongs to a property outside scoped access",
		})

		resp, body, err := scopedClient.getJSON(ctx, "/api/v1/repair-requests/"+unassignedRepair.ID)
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")

		resp, body, err = scopedClient.getJSON(ctx, fmt.Sprintf("/api/v1/repair-requests?property_id=%s", url.QueryEscape(unassignedProperty.ID)))
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")

		resp, body, err = scopedClient.postJSON(ctx, "/api/v1/repair-requests", map[string]any{
			"property_id": unassignedProperty.ID,
			"room_id":     unassignedRoom.ID,
			"title":       "E2E forbidden repair",
			"description": "Forbidden repair create attempt",
		})
		if err != nil {
			t.Fatal(err)
		}
		requireStatus(t, resp, body, http.StatusForbidden)
		requireAPIError(t, body, "FORBIDDEN", "Forbidden.")
	})
}

type repairE2ECreateParams struct {
	PropertyID  string
	RoomID      string
	Title       string
	Description string
}

type repairE2EExpectation struct {
	PropertyID  string
	RoomID      string
	SubmittedBy string
	AssignedTo  *string
	Title       string
	Description string
	Status      string
}

type repairE2ERequestResponse struct {
	ID          string  `json:"id"`
	PropertyID  string  `json:"property_id"`
	RoomID      string  `json:"room_id"`
	SubmittedBy string  `json:"submitted_by"`
	AssignedTo  *string `json:"assigned_to"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	SubmittedAt string  `json:"submitted_at"`
	AssignedAt  *string `json:"assigned_at"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type repairE2EListResponse struct {
	Data []repairE2ERequestResponse `json:"data"`
}

func repairE2ECreateRequest(t *testing.T, ctx context.Context, client apiClient, request repairE2ECreateParams) repairE2ERequestResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/repair-requests", map[string]any{
		"property_id": request.PropertyID,
		"room_id":     request.RoomID,
		"title":       request.Title,
		"description": request.Description,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var repairRequest repairE2ERequestResponse
	decodeJSON(t, body, &repairRequest)
	if repairRequest.ID == "" {
		t.Fatalf("expected repair request id in response: %s", string(body))
	}
	return repairRequest
}

func repairE2EListRequests(t *testing.T, ctx context.Context, client apiClient, propertyID string) []repairE2ERequestResponse {
	t.Helper()

	path := fmt.Sprintf("/api/v1/repair-requests?property_id=%s&limit=100", url.QueryEscape(propertyID))
	resp, body, err := client.getJSON(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list repairE2EListResponse
	decodeJSON(t, body, &list)
	return list.Data
}

func repairE2EGetRequest(t *testing.T, ctx context.Context, client apiClient, repairRequestID string) repairE2ERequestResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, "/api/v1/repair-requests/"+repairRequestID)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var repairRequest repairE2ERequestResponse
	decodeJSON(t, body, &repairRequest)
	return repairRequest
}

func repairE2EAssignRequest(t *testing.T, ctx context.Context, client apiClient, repairRequestID string, assignedTo string) repairE2ERequestResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/repair-requests/"+repairRequestID+"/assign", map[string]any{
		"assigned_to": assignedTo,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var repairRequest repairE2ERequestResponse
	decodeJSON(t, body, &repairRequest)
	return repairRequest
}

func repairE2EProgressRequest(t *testing.T, ctx context.Context, client apiClient, repairRequestID string) repairE2ERequestResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/repair-requests/"+repairRequestID+"/progress", nil)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var repairRequest repairE2ERequestResponse
	decodeJSON(t, body, &repairRequest)
	return repairRequest
}

func repairE2ECompleteRequest(t *testing.T, ctx context.Context, client apiClient, repairRequestID string) repairE2ERequestResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/repair-requests/"+repairRequestID+"/complete", nil)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var repairRequest repairE2ERequestResponse
	decodeJSON(t, body, &repairRequest)
	return repairRequest
}

func repairE2ERequireRequest(t *testing.T, repairRequest repairE2ERequestResponse, expected repairE2EExpectation) {
	t.Helper()

	if repairRequest.PropertyID != expected.PropertyID {
		t.Fatalf("expected property_id %q, got %q", expected.PropertyID, repairRequest.PropertyID)
	}
	if repairRequest.RoomID != expected.RoomID {
		t.Fatalf("expected room_id %q, got %q", expected.RoomID, repairRequest.RoomID)
	}
	if repairRequest.SubmittedBy != expected.SubmittedBy {
		t.Fatalf("expected submitted_by %q, got %q", expected.SubmittedBy, repairRequest.SubmittedBy)
	}
	if !stringPtrEqual(repairRequest.AssignedTo, expected.AssignedTo) {
		t.Fatalf("expected assigned_to %+v, got %+v", expected.AssignedTo, repairRequest.AssignedTo)
	}
	if repairRequest.Title != expected.Title {
		t.Fatalf("expected title %q, got %q", expected.Title, repairRequest.Title)
	}
	if repairRequest.Description != expected.Description {
		t.Fatalf("expected description %q, got %q", expected.Description, repairRequest.Description)
	}
	if repairRequest.Status != expected.Status {
		t.Fatalf("expected status %q, got %q", expected.Status, repairRequest.Status)
	}
	if repairRequest.SubmittedAt == "" {
		t.Fatalf("expected submitted_at in response: %+v", repairRequest)
	}
}

func repairE2ERequireListContains(t *testing.T, repairRequests []repairE2ERequestResponse, repairRequestID string) {
	t.Helper()

	for _, repairRequest := range repairRequests {
		if repairRequest.ID == repairRequestID {
			return
		}
	}
	t.Fatalf("expected repair request %q in list: %+v", repairRequestID, repairRequests)
}

func ptrToString(value string) *string {
	return &value
}
