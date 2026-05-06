//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestE2EAttachmentAcceptance(t *testing.T) {
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
	assignedProperty := createProperty(t, ctx, adminClient, "E2E Attachment Assigned Property")
	unassignedProperty := createProperty(t, ctx, adminClient, "E2E Attachment Unassigned Property")
	assignedRoom := createRoom(t, ctx, adminClient, assignedProperty.ID, "E2E Attachment Assigned Room")
	unassignedRoom := createRoom(t, ctx, adminClient, unassignedProperty.ID, "E2E Attachment Unassigned Room")
	assignedTenant := terminationE2ECreateTenant(t, ctx, adminClient, cfg.TestEmail, "attachment-assigned")
	unassignedTenant := terminationE2ECreateTenant(t, ctx, adminClient, cfg.TestEmail, "attachment-unassigned")
	assignedLease := terminationE2ECreateLease(t, ctx, adminClient, assignedTenant.ID, assignedRoom.ID)
	unassignedLease := terminationE2ECreateLease(t, ctx, adminClient, unassignedTenant.ID, unassignedRoom.ID)
	assignedJournal := journalE2ECreateLog(t, ctx, adminClient, journalE2ECreateRequest{
		PropertyID: assignedProperty.ID,
		RoomID:     &assignedRoom.ID,
		Content:    "E2E attachment journal",
	})
	unassignedJournal := journalE2ECreateLog(t, ctx, adminClient, journalE2ECreateRequest{
		PropertyID: unassignedProperty.ID,
		RoomID:     &unassignedRoom.ID,
		Content:    "E2E unassigned attachment journal",
	})
	assignedRepair := repairE2ECreateRequest(t, ctx, adminClient, repairE2ECreateParams{
		PropertyID:  assignedProperty.ID,
		RoomID:      assignedRoom.ID,
		Title:       "E2E attachment repair",
		Description: "Repair with metadata-only attachment",
	})
	unassignedRepair := repairE2ECreateRequest(t, ctx, adminClient, repairE2ECreateParams{
		PropertyID:  unassignedProperty.ID,
		RoomID:      unassignedRoom.ID,
		Title:       "E2E unassigned attachment repair",
		Description: "Repair outside scoped attachment access",
	})

	scopedEmail := e2eEmail(cfg.TestEmail, "attachment-scoped")
	scopedToken, err := issueFirebaseEmulatorTokenForCredentials(ctx, cfg, scopedEmail, cfg.TestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedBackendUser(ctx, db, seedBackendUserParams{
		ID:                  seededScopedUserID,
		FirebaseUID:         scopedToken.UID,
		Email:               scopedEmail,
		Name:                "E2E Attachment Organizer",
		Role:                "organizer",
		AssignedPropertyIDs: []string{assignedProperty.ID},
	}); err != nil {
		t.Fatal(err)
	}
	scopedClient := newAPIClient(cfg.BaseURL, scopedToken.IDToken)

	cases := []attachmentE2EResourceCase{
		{
			Name:                 "property",
			ResourceType:         "property",
			ResourceID:           assignedProperty.ID,
			UnassignedResourceID: unassignedProperty.ID,
			AttachmentPath:       "/api/v1/properties/" + assignedProperty.ID + "/attachments",
			UnassignedPath:       "/api/v1/properties/" + unassignedProperty.ID + "/attachments",
			FileName:             "property-attachment.pdf",
		},
		{
			Name:                 "lease",
			ResourceType:         "lease",
			ResourceID:           assignedLease.ID,
			UnassignedResourceID: unassignedLease.ID,
			AttachmentPath:       "/api/v1/leases/" + assignedLease.ID + "/attachments",
			UnassignedPath:       "/api/v1/leases/" + unassignedLease.ID + "/attachments",
			FileName:             "lease-attachment.pdf",
		},
		{
			Name:                 "journal",
			ResourceType:         "journal_log",
			ResourceID:           assignedJournal.ID,
			UnassignedResourceID: unassignedJournal.ID,
			AttachmentPath:       "/api/v1/journal-logs/" + assignedJournal.ID + "/attachments",
			UnassignedPath:       "/api/v1/journal-logs/" + unassignedJournal.ID + "/attachments",
			FileName:             "journal-attachment.pdf",
		},
		{
			Name:                 "repair",
			ResourceType:         "repair_request",
			ResourceID:           assignedRepair.ID,
			UnassignedResourceID: unassignedRepair.ID,
			AttachmentPath:       "/api/v1/repair-requests/" + assignedRepair.ID + "/attachments",
			UnassignedPath:       "/api/v1/repair-requests/" + unassignedRepair.ID + "/attachments",
			FileName:             "repair-attachment.pdf",
			SortOrder:            ptrToInt(1),
			PhotoStage:           ptrToString("before"),
		},
	}

	t.Run("metadata-only registration and read works for launch resources", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.Name, func(t *testing.T) {
				attachment := attachmentE2ECreate(t, ctx, scopedClient, tc)
				if attachment.FileName != tc.FileName {
					t.Fatalf("expected file_name %q, got %q", tc.FileName, attachment.FileName)
				}
				if !strings.Contains(attachment.ObjectPath, "/"+tc.ResourceID+"/") {
					t.Fatalf("expected object_path to include resource id %q, got %q", tc.ResourceID, attachment.ObjectPath)
				}
				if attachment.UploadedBy == nil || *attachment.UploadedBy != seededScopedUserID {
					t.Fatalf("expected uploaded_by %q, got %+v", seededScopedUserID, attachment.UploadedBy)
				}
				if tc.SortOrder != nil && (attachment.SortOrder == nil || *attachment.SortOrder != *tc.SortOrder) {
					t.Fatalf("expected sort_order %d, got %+v", *tc.SortOrder, attachment.SortOrder)
				}
				if tc.PhotoStage != nil && (attachment.PhotoStage == nil || *attachment.PhotoStage != *tc.PhotoStage) {
					t.Fatalf("expected photo_stage %q, got %+v", *tc.PhotoStage, attachment.PhotoStage)
				}

				list := attachmentE2EList(t, ctx, scopedClient, tc.AttachmentPath)
				attachmentE2ERequireListContains(t, list, attachment.ID)
			})
		}
	})

	t.Run("unassigned resource access is rejected", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.Name, func(t *testing.T) {
				resp, body, err := scopedClient.getJSON(ctx, tc.UnassignedPath)
				if err != nil {
					t.Fatal(err)
				}
				requireStatus(t, resp, body, http.StatusForbidden)
				requireAPIError(t, body, "FORBIDDEN", "Forbidden.")

				resp, body, err = scopedClient.postJSON(ctx, "/api/v1/attachments/upload-url", map[string]any{
					"resource_type": tc.ResourceType,
					"resource_id":   tc.UnassignedResourceID,
					"file_name":     tc.FileName,
					"content_type":  "application/pdf",
					"file_size":     1024,
				})
				if err != nil {
					t.Fatal(err)
				}
				requireStatus(t, resp, body, http.StatusForbidden)
				requireAPIError(t, body, "FORBIDDEN", "Forbidden.")
			})
		}
	})
}

type attachmentE2EResourceCase struct {
	Name                 string
	ResourceType         string
	ResourceID           string
	UnassignedResourceID string
	AttachmentPath       string
	UnassignedPath       string
	FileName             string
	SortOrder            *int
	PhotoStage           *string
}

type attachmentE2EUploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

type attachmentE2EResponse struct {
	ID         string  `json:"id"`
	ObjectPath string  `json:"object_path"`
	FileName   string  `json:"file_name"`
	UploadedBy *string `json:"uploaded_by"`
	SortOrder  *int    `json:"sort_order"`
	PhotoStage *string `json:"photo_stage"`
}

type attachmentE2EListResponse struct {
	Data []attachmentE2EResponse `json:"data"`
}

func attachmentE2ECreate(t *testing.T, ctx context.Context, client apiClient, resource attachmentE2EResourceCase) attachmentE2EResponse {
	t.Helper()

	resp, body, err := client.postJSON(ctx, "/api/v1/attachments/upload-url", map[string]any{
		"resource_type": resource.ResourceType,
		"resource_id":   resource.ResourceID,
		"file_name":     resource.FileName,
		"content_type":  "application/pdf",
		"file_size":     1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var upload attachmentE2EUploadURLResponse
	decodeJSON(t, body, &upload)
	if upload.UploadURL == "" || upload.Nonce == "" || upload.ExpiresAt == "" {
		t.Fatalf("expected upload_url, nonce, and expires_at in response: %s", string(body))
	}

	request := map[string]any{
		"nonce":     upload.Nonce,
		"file_name": resource.FileName,
	}
	if resource.SortOrder != nil {
		request["sort_order"] = *resource.SortOrder
	}
	if resource.PhotoStage != nil {
		request["photo_stage"] = *resource.PhotoStage
	}

	resp, body, err = client.postJSON(ctx, resource.AttachmentPath, request)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusCreated)

	var attachment attachmentE2EResponse
	decodeJSON(t, body, &attachment)
	if attachment.ID == "" {
		t.Fatalf("expected attachment id in response: %s", string(body))
	}
	if attachment.ObjectPath == "" {
		t.Fatalf("expected object_path in response: %s", string(body))
	}

	return attachment
}

func attachmentE2EList(t *testing.T, ctx context.Context, client apiClient, path string) []attachmentE2EResponse {
	t.Helper()

	resp, body, err := client.getJSON(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, resp, body, http.StatusOK)

	var list attachmentE2EListResponse
	decodeJSON(t, body, &list)
	return list.Data
}

func attachmentE2ERequireListContains(t *testing.T, attachments []attachmentE2EResponse, attachmentID string) {
	t.Helper()

	for _, attachment := range attachments {
		if attachment.ID == attachmentID {
			return
		}
	}
	t.Fatalf("expected attachment %q in list: %+v", attachmentID, attachments)
}

func ptrToInt(value int) *int {
	return &value
}
