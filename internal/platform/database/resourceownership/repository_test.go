package resourceownership

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindPropertyIDsByTenantIDReturnsDistinctActiveLeasePropertyIDs(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	expectTenantExists(mock, "tenant-1")
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT DISTINCT property_id
FROM leases
WHERE tenant_id = $1
  AND deleted_at IS NULL
`)).
		WithArgs("tenant-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).
			AddRow("property-1").
			AddRow("property-2"))

	propertyIDs, err := repo.FindPropertyIDsByTenantID(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("FindPropertyIDsByTenantID: %v", err)
	}
	assertStringSet(t, propertyIDs, []string{"property-1", "property-2"})
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByTenantIDReturnsEmptySliceWhenTenantHasNoActiveLeaseAssociation(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	expectTenantExists(mock, "tenant-1")
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT DISTINCT property_id
FROM leases
WHERE tenant_id = $1
  AND deleted_at IS NULL
`)).
		WithArgs("tenant-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}))

	propertyIDs, err := repo.FindPropertyIDsByTenantID(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("FindPropertyIDsByTenantID: %v", err)
	}
	if propertyIDs == nil {
		t.Fatal("propertyIDs = nil, want empty slice")
	}
	if len(propertyIDs) != 0 {
		t.Fatalf("propertyIDs = %v, want empty slice", propertyIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByTenantIDMapsMissingTenantToNotFound(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("tenant-404").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindPropertyIDsByTenantID(context.Background(), "tenant-404")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByTenantIDWrapsUnexpectedTenantLookupError(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("tenant-1").
		WillReturnError(sqlmock.ErrCancelled)

	_, err := repo.FindPropertyIDsByTenantID(context.Background(), "tenant-1")
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("expected wrapped sqlmock.ErrCancelled, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("expected unexpected error, got ErrNotFound: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByTenantIDWrapsUnexpectedAssociationLookupError(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	expectTenantExists(mock, "tenant-1")
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT DISTINCT property_id
FROM leases
WHERE tenant_id = $1
  AND deleted_at IS NULL
`)).
		WithArgs("tenant-1").
		WillReturnError(sqlmock.ErrCancelled)

	_, err := repo.FindPropertyIDsByTenantID(context.Background(), "tenant-1")
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("expected wrapped sqlmock.ErrCancelled, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("expected unexpected error, got ErrNotFound: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDBySinglePropertyResourceResolversUseActiveRows(t *testing.T) {
	tests := []struct {
		name       string
		resourceID string
		wantSQL    string
		call       func(context.Context, *SQLRepository, string) (string, error)
	}{
		{
			name:       "property",
			resourceID: "property-1",
			wantSQL: `
SELECT id
FROM properties
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByPropertyID(ctx, id)
			},
		},
		{
			name:       "room",
			resourceID: "room-1",
			wantSQL: `
SELECT property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByRoomID(ctx, id)
			},
		},
		{
			name:       "lease",
			resourceID: "lease-1",
			wantSQL: `
SELECT property_id
FROM leases
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByLeaseID(ctx, id)
			},
		},
		{
			name:       "bill",
			resourceID: "bill-1",
			wantSQL: `
SELECT property_id
FROM bills
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByBillID(ctx, id)
			},
		},
		{
			name:       "journal log",
			resourceID: "journal-log-1",
			wantSQL: `
SELECT property_id
FROM journal_logs
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByJournalLogID(ctx, id)
			},
		},
		{
			name:       "repair request",
			resourceID: "repair-request-1",
			wantSQL: `
SELECT property_id
FROM repair_requests
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByRepairRequestID(ctx, id)
			},
		},
		{
			name:       "force termination",
			resourceID: "force-termination-1",
			wantSQL: `
SELECT leases.property_id
FROM force_terminations
JOIN leases ON leases.id = force_terminations.lease_id
WHERE force_terminations.id = $1
  AND leases.deleted_at IS NULL
LIMIT 1
`,
			call: func(ctx context.Context, repo *SQLRepository, id string) (string, error) {
				return repo.FindPropertyIDByForceTerminationID(ctx, id)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, repo := newResourceOwnershipRepoTest(t)
			defer closeResourceOwnershipDB(t, db)

			mock.ExpectQuery(regexp.QuoteMeta(tt.wantSQL)).
				WithArgs(tt.resourceID).
				WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-1"))

			propertyID, err := tt.call(context.Background(), repo, tt.resourceID)
			if err != nil {
				t.Fatalf("resolver returned error: %v", err)
			}
			if propertyID != "property-1" {
				t.Fatalf("propertyID = %q, want property-1", propertyID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func TestFindPropertyIDBySinglePropertyResourceResolverMapsNoRowsToNotFound(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("room-404").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindPropertyIDByRoomID(context.Background(), "room-404")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDBySinglePropertyResourceResolverWrapsUnexpectedError(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id
FROM rooms
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs("room-1").
		WillReturnError(sqlmock.ErrCancelled)

	_, err := repo.FindPropertyIDByRoomID(context.Background(), "room-1")
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("expected wrapped sqlmock.ErrCancelled, got %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("expected unexpected error, got ErrNotFound: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDByAttachmentIDRequiresActiveAttachmentAndHostResource(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(`(?s)` +
		`FROM property_attachments\s+JOIN properties ON properties.id = property_attachments.property_id\s+WHERE property_attachments.id = \$1\s+AND property_attachments.deleted_at IS NULL\s+AND properties.deleted_at IS NULL.*` +
		`FROM room_attachments\s+JOIN rooms ON rooms.id = room_attachments.room_id\s+WHERE room_attachments.id = \$1\s+AND room_attachments.deleted_at IS NULL\s+AND rooms.deleted_at IS NULL.*` +
		`FROM tenant_attachments\s+JOIN tenants ON tenants.id = tenant_attachments.tenant_id\s+JOIN leases ON leases.tenant_id = tenants.id\s+WHERE tenant_attachments.id = \$1\s+AND tenant_attachments.deleted_at IS NULL\s+AND tenants.deleted_at IS NULL\s+AND leases.deleted_at IS NULL.*` +
		`FROM lease_attachments\s+JOIN leases ON leases.id = lease_attachments.lease_id\s+WHERE lease_attachments.id = \$1\s+AND lease_attachments.deleted_at IS NULL\s+AND leases.deleted_at IS NULL.*` +
		`FROM journal_log_attachments\s+JOIN journal_logs ON journal_logs.id = journal_log_attachments.journal_log_id\s+WHERE journal_log_attachments.id = \$1\s+AND journal_log_attachments.deleted_at IS NULL\s+AND journal_logs.deleted_at IS NULL.*` +
		`FROM repair_request_attachments\s+JOIN repair_requests ON repair_requests.id = repair_request_attachments.repair_request_id\s+WHERE repair_request_attachments.id = \$1\s+AND repair_request_attachments.deleted_at IS NULL\s+AND repair_requests.deleted_at IS NULL.*` +
		`FROM bill_attachments\s+JOIN bills ON bills.id = bill_attachments.bill_id\s+WHERE bill_attachments.id = \$1\s+AND bill_attachments.deleted_at IS NULL\s+AND bills.deleted_at IS NULL`).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).AddRow("property-1"))

	propertyID, err := repo.FindPropertyIDByAttachmentID(context.Background(), "attachment-1")
	if err != nil {
		t.Fatalf("FindPropertyIDByAttachmentID: %v", err)
	}
	if propertyID != "property-1" {
		t.Fatalf("propertyID = %q, want property-1", propertyID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByAttachmentIDReturnsAllActiveAttachmentProperties(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(attachmentPropertiesQueryPattern()).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}).
			AddRow("property-1").
			AddRow("property-2"))

	propertyIDs, err := repo.FindPropertyIDsByAttachmentID(context.Background(), "attachment-1")
	if err != nil {
		t.Fatalf("FindPropertyIDsByAttachmentID: %v", err)
	}
	if len(propertyIDs) != 2 || propertyIDs[0] != "property-1" || propertyIDs[1] != "property-2" {
		t.Fatalf("propertyIDs = %+v, want [property-1 property-2]", propertyIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByAttachmentIDMapsEmptyRowsToNotFound(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(attachmentPropertiesQueryPattern()).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}))
	mock.ExpectQuery(activeAttachmentHostExistsQueryPattern()).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	_, err := repo.FindPropertyIDsByAttachmentID(context.Background(), "attachment-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestFindPropertyIDsByAttachmentIDReturnsEmptyListForActiveHostWithoutProperties(t *testing.T) {
	db, mock, repo := newResourceOwnershipRepoTest(t)
	defer closeResourceOwnershipDB(t, db)

	mock.ExpectQuery(attachmentPropertiesQueryPattern()).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"property_id"}))
	mock.ExpectQuery(activeAttachmentHostExistsQueryPattern()).
		WithArgs("attachment-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	propertyIDs, err := repo.FindPropertyIDsByAttachmentID(context.Background(), "attachment-1")
	if err != nil {
		t.Fatalf("FindPropertyIDsByAttachmentID: %v", err)
	}
	if len(propertyIDs) != 0 {
		t.Fatalf("propertyIDs = %+v, want empty list", propertyIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestEnsureTenantExists(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		mockRows  *sqlmock.Rows
		mockError error
		wantError error
	}{
		{
			name:     "success",
			tenantID: "tenant-1",
			mockRows: sqlmock.NewRows([]string{"id"}).AddRow("tenant-1"),
		},
		{
			name:      "not found",
			tenantID:  "tenant-404",
			mockError: sql.ErrNoRows,
			wantError: ErrNotFound,
		},
		{
			name:      "unexpected error",
			tenantID:  "tenant-1",
			mockError: sqlmock.ErrCancelled,
			wantError: sqlmock.ErrCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, repo := newResourceOwnershipRepoTest(t)
			defer closeResourceOwnershipDB(t, db)

			expectation := mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
				WithArgs(tt.tenantID)
			if tt.mockError != nil {
				expectation.WillReturnError(tt.mockError)
			} else {
				expectation.WillReturnRows(tt.mockRows)
			}

			err := repo.EnsureTenantExists(context.Background(), tt.tenantID)
			if tt.wantError == nil && err != nil {
				t.Fatalf("EnsureTenantExists: %v", err)
			}
			if tt.wantError != nil && !errors.Is(err, tt.wantError) {
				t.Fatalf("expected %v, got %v", tt.wantError, err)
			}
			if tt.wantError == sqlmock.ErrCancelled && errors.Is(err, ErrNotFound) {
				t.Fatalf("expected unexpected error, got ErrNotFound: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("ExpectationsWereMet: %v", err)
			}
		})
	}
}

func expectTenantExists(mock sqlmock.Sqlmock, tenantID string) {
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT id
FROM tenants
WHERE id = $1
  AND deleted_at IS NULL
LIMIT 1
`)).
		WithArgs(tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tenantID))
}

func attachmentPropertiesQueryPattern() string {
	return `(?s)` +
		`SELECT DISTINCT property_id.*` +
		`FROM property_attachments\s+JOIN properties ON properties.id = property_attachments.property_id\s+WHERE property_attachments.id = \$1\s+AND property_attachments.deleted_at IS NULL\s+AND properties.deleted_at IS NULL.*` +
		`FROM room_attachments\s+JOIN rooms ON rooms.id = room_attachments.room_id\s+WHERE room_attachments.id = \$1\s+AND room_attachments.deleted_at IS NULL\s+AND rooms.deleted_at IS NULL.*` +
		`FROM tenant_attachments\s+JOIN tenants ON tenants.id = tenant_attachments.tenant_id\s+JOIN leases ON leases.tenant_id = tenants.id\s+WHERE tenant_attachments.id = \$1\s+AND tenant_attachments.deleted_at IS NULL\s+AND tenants.deleted_at IS NULL\s+AND leases.deleted_at IS NULL.*` +
		`FROM lease_attachments\s+JOIN leases ON leases.id = lease_attachments.lease_id\s+WHERE lease_attachments.id = \$1\s+AND lease_attachments.deleted_at IS NULL\s+AND leases.deleted_at IS NULL.*` +
		`FROM journal_log_attachments\s+JOIN journal_logs ON journal_logs.id = journal_log_attachments.journal_log_id\s+WHERE journal_log_attachments.id = \$1\s+AND journal_log_attachments.deleted_at IS NULL\s+AND journal_logs.deleted_at IS NULL.*` +
		`FROM repair_request_attachments\s+JOIN repair_requests ON repair_requests.id = repair_request_attachments.repair_request_id\s+WHERE repair_request_attachments.id = \$1\s+AND repair_request_attachments.deleted_at IS NULL\s+AND repair_requests.deleted_at IS NULL.*` +
		`FROM bill_attachments\s+JOIN bills ON bills.id = bill_attachments.bill_id\s+WHERE bill_attachments.id = \$1\s+AND bill_attachments.deleted_at IS NULL\s+AND bills.deleted_at IS NULL.*` +
		`ORDER BY property_id`
}

func activeAttachmentHostExistsQueryPattern() string {
	return `(?s)` +
		`SELECT EXISTS.*` +
		`FROM property_attachments\s+JOIN properties ON properties.id = property_attachments.property_id\s+WHERE property_attachments.id = \$1\s+AND property_attachments.deleted_at IS NULL\s+AND properties.deleted_at IS NULL.*` +
		`FROM room_attachments\s+JOIN rooms ON rooms.id = room_attachments.room_id\s+WHERE room_attachments.id = \$1\s+AND room_attachments.deleted_at IS NULL\s+AND rooms.deleted_at IS NULL.*` +
		`FROM tenant_attachments\s+JOIN tenants ON tenants.id = tenant_attachments.tenant_id\s+WHERE tenant_attachments.id = \$1\s+AND tenant_attachments.deleted_at IS NULL\s+AND tenants.deleted_at IS NULL.*` +
		`FROM lease_attachments\s+JOIN leases ON leases.id = lease_attachments.lease_id\s+WHERE lease_attachments.id = \$1\s+AND lease_attachments.deleted_at IS NULL\s+AND leases.deleted_at IS NULL.*` +
		`FROM journal_log_attachments\s+JOIN journal_logs ON journal_logs.id = journal_log_attachments.journal_log_id\s+WHERE journal_log_attachments.id = \$1\s+AND journal_log_attachments.deleted_at IS NULL\s+AND journal_logs.deleted_at IS NULL.*` +
		`FROM repair_request_attachments\s+JOIN repair_requests ON repair_requests.id = repair_request_attachments.repair_request_id\s+WHERE repair_request_attachments.id = \$1\s+AND repair_request_attachments.deleted_at IS NULL\s+AND repair_requests.deleted_at IS NULL.*` +
		`FROM bill_attachments\s+JOIN bills ON bills.id = bill_attachments.bill_id\s+WHERE bill_attachments.id = \$1\s+AND bill_attachments.deleted_at IS NULL\s+AND bills.deleted_at IS NULL`
}

func newResourceOwnershipRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	return db, mock, NewRepository(db)
}

func closeResourceOwnershipDB(t *testing.T, db *sql.DB) {
	t.Helper()

	if err := db.Close(); err != nil {
		t.Logf("db.Close: %v", err)
	}
}

func assertStringSet(t *testing.T, got []string, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	seen := make(map[string]int, len(got))
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] != 1 {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
