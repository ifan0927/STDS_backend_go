package publicbrand

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestGetProfileReadsApprovedView(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	phone := "02-1234-5678"
	email := "service@example.com"
	address := "台北市信義區"
	updatedAt := time.Now().UTC()
	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT brand_name, contact_phone, contact_email, contact_address, updated_at
FROM approved_brand_profile_v1
LIMIT 1
`)).
		WillReturnRows(sqlmock.NewRows(profileColumns()).
			AddRow("STDS", phone, email, address, updatedAt))

	profile, err := repo.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if profile == nil || profile.BrandName != "STDS" || profile.ContactPhone == nil || *profile.ContactPhone != phone {
		t.Fatalf("unexpected profile: %+v", profile)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestGetProfileReturnsNilWhenApprovedViewIsEmpty(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT brand_name, contact_phone, contact_email, contact_address, updated_at
FROM approved_brand_profile_v1
LIMIT 1
`)).
		WillReturnError(sql.ErrNoRows)

	profile, err := repo.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if profile != nil {
		t.Fatalf("expected nil profile, got %+v", profile)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestGetProfileWrapsScanError(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT brand_name, contact_phone, contact_email, contact_address, updated_at
FROM approved_brand_profile_v1
LIMIT 1
`)).
		WillReturnRows(sqlmock.NewRows(profileColumns()).
			AddRow("STDS", nil, nil, nil, "not-a-time"))

	profile, err := repo.GetProfile(context.Background())
	if profile != nil {
		t.Fatalf("expected nil profile, got %+v", profile)
	}
	if err == nil {
		t.Fatal("expected scan error")
	}
	if !regexp.MustCompile(`get approved brand profile`).MatchString(err.Error()) {
		t.Fatalf("expected operation context, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestGetProfileWrapsDatabaseError(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT brand_name, contact_phone, contact_email, contact_address, updated_at
FROM approved_brand_profile_v1
LIMIT 1
`)).
		WillReturnError(sqlmock.ErrCancelled)

	profile, err := repo.GetProfile(context.Background())
	if profile != nil {
		t.Fatalf("expected nil profile, got %+v", profile)
	}
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("expected wrapped database error, got %v", err)
	}
	if !regexp.MustCompile(`get approved brand profile`).MatchString(err.Error()) {
		t.Fatalf("expected operation context, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListFAQItemsReadsApprovedViewInSortOrder(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`)).
		WillReturnRows(sqlmock.NewRows(faqColumns()).
			AddRow("Q1", "A1", 10).
			AddRow("Q2", "A2", 20))

	items, err := repo.ListFAQItems(context.Background())
	if err != nil {
		t.Fatalf("list FAQ items: %v", err)
	}
	if len(items) != 2 || items[0].SortOrder != 10 || items[1].Question != "Q2" {
		t.Fatalf("unexpected FAQ items: %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListFAQItemsReturnsNonNilEmptySlice(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`)).
		WillReturnRows(sqlmock.NewRows(faqColumns()))

	items, err := repo.ListFAQItems(context.Background())
	if err != nil {
		t.Fatalf("list FAQ items: %v", err)
	}
	if items == nil {
		t.Fatal("expected non-nil empty FAQ slice")
	}
	if len(items) != 0 {
		t.Fatalf("expected empty FAQ slice, got %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListFAQItemsWrapsScanRowsAndDatabaseErrors(t *testing.T) {
	t.Run("scan error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`)).
			WillReturnRows(sqlmock.NewRows(faqColumns()).
				AddRow("Q1", "A1", "invalid-sort-order"))

		items, err := repo.ListFAQItems(context.Background())
		if items != nil {
			t.Fatalf("expected nil FAQ items, got %+v", items)
		}
		if err == nil || !regexp.MustCompile(`scan approved brand FAQ item`).MatchString(err.Error()) {
			t.Fatalf("expected scan operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("rows error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`)).
			WillReturnRows(sqlmock.NewRows(faqColumns()).RowError(0, sqlmock.ErrCancelled).
				AddRow("Q1", "A1", 10))

		items, err := repo.ListFAQItems(context.Background())
		if items != nil {
			t.Fatalf("expected nil FAQ items, got %+v", items)
		}
		if !errors.Is(err, sqlmock.ErrCancelled) {
			t.Fatalf("expected wrapped rows error, got %v", err)
		}
		if err == nil || !regexp.MustCompile(`iterate approved brand FAQ items`).MatchString(err.Error()) {
			t.Fatalf("expected iterate operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT question, answer, sort_order
FROM approved_brand_faq_items_v1
ORDER BY sort_order
`)).
			WillReturnError(sqlmock.ErrCancelled)

		items, err := repo.ListFAQItems(context.Background())
		if items != nil {
			t.Fatalf("expected nil FAQ items, got %+v", items)
		}
		if !errors.Is(err, sqlmock.ErrCancelled) {
			t.Fatalf("expected wrapped database error, got %v", err)
		}
		if err == nil || !regexp.MustCompile(`list approved brand FAQ items`).MatchString(err.Error()) {
			t.Fatalf("expected list operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})
}

func TestListPropertyAvailabilityReadsApprovedView(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`)).
		WillReturnRows(sqlmock.NewRows(availabilityColumns()).
			AddRow("10000000-0000-0000-0000-000000000001", "信義館", "台北市信義區", true).
			AddRow("10000000-0000-0000-0000-000000000002", "松山館", "台北市松山區", false))

	items, err := repo.ListPropertyAvailability(context.Background())
	if err != nil {
		t.Fatalf("list property availability: %v", err)
	}
	if len(items) != 2 || !items[0].HasVacantRoom || items[1].PropertyPublicName != "松山館" {
		t.Fatalf("unexpected availability items: %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListPropertyAvailabilityReturnsNonNilEmptySlice(t *testing.T) {
	db, mock, repo := newRepoTest(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`)).
		WillReturnRows(sqlmock.NewRows(availabilityColumns()))

	items, err := repo.ListPropertyAvailability(context.Background())
	if err != nil {
		t.Fatalf("list property availability: %v", err)
	}
	if items == nil {
		t.Fatal("expected non-nil empty availability slice")
	}
	if len(items) != 0 {
		t.Fatalf("expected empty availability slice, got %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestListPropertyAvailabilityWrapsScanRowsAndDatabaseErrors(t *testing.T) {
	t.Run("scan error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`)).
			WillReturnRows(sqlmock.NewRows(availabilityColumns()).
				AddRow("10000000-0000-0000-0000-000000000001", "信義館", "台北市信義區", "invalid-bool"))

		items, err := repo.ListPropertyAvailability(context.Background())
		if items != nil {
			t.Fatalf("expected nil availability items, got %+v", items)
		}
		if err == nil || !regexp.MustCompile(`scan approved brand property availability`).MatchString(err.Error()) {
			t.Fatalf("expected scan operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("rows error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`)).
			WillReturnRows(sqlmock.NewRows(availabilityColumns()).RowError(0, sqlmock.ErrCancelled).
				AddRow("10000000-0000-0000-0000-000000000001", "信義館", "台北市信義區", true))

		items, err := repo.ListPropertyAvailability(context.Background())
		if items != nil {
			t.Fatalf("expected nil availability items, got %+v", items)
		}
		if !errors.Is(err, sqlmock.ErrCancelled) {
			t.Fatalf("expected wrapped rows error, got %v", err)
		}
		if err == nil || !regexp.MustCompile(`iterate approved brand property availability`).MatchString(err.Error()) {
			t.Fatalf("expected iterate operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("database error", func(t *testing.T) {
		db, mock, repo := newRepoTest(t)
		defer db.Close()

		mock.ExpectQuery(regexp.QuoteMeta(`
SELECT property_id, property_public_name, address, has_vacant_room
FROM approved_brand_property_availability_v1
`)).
			WillReturnError(sqlmock.ErrCancelled)

		items, err := repo.ListPropertyAvailability(context.Background())
		if items != nil {
			t.Fatalf("expected nil availability items, got %+v", items)
		}
		if !errors.Is(err, sqlmock.ErrCancelled) {
			t.Fatalf("expected wrapped database error, got %v", err)
		}
		if err == nil || !regexp.MustCompile(`list approved brand property availability`).MatchString(err.Error()) {
			t.Fatalf("expected list operation context, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})
}

func newRepoTest(t *testing.T) (*sql.DB, sqlmock.Sqlmock, *SQLRepository) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}

	return db, mock, NewRepository(db)
}

func profileColumns() []string {
	return []string{"brand_name", "contact_phone", "contact_email", "contact_address", "updated_at"}
}

func faqColumns() []string {
	return []string{"question", "answer", "sort_order"}
}

func availabilityColumns() []string {
	return []string{"property_id", "property_public_name", "address", "has_vacant_room"}
}
