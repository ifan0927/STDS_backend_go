package tenantquery

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestListAccessibleReturnsAssignedPropertyTenants(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT t.id)::int
FROM tenants t
JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL
WHERE t.deleted_at IS NULL
  AND l.property_id IN ($1)
  AND t.status = $2`)).
		WithArgs("10000000-0000-0000-0000-000000000001", "active").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT
	t.id,
	t.name,
	t.email,
	t.phone,
	t.contacts::text,
	t.birth_date,
	t.national_id,
	t.address,
	t.occupation,
	t.status,
	t.created_at,
	t.updated_at,
	t.version
FROM tenants t
JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL
WHERE t.deleted_at IS NULL
  AND l.property_id IN ($1)
  AND t.status = $2
ORDER BY t.created_at DESC LIMIT $3 OFFSET $4`)).
		WithArgs("10000000-0000-0000-0000-000000000001", "active", 10, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "email", "phone", "contacts", "birth_date", "national_id", "address", "occupation", "status", "created_at", "updated_at", "version",
		}).AddRow(
			"30000000-0000-0000-0000-000000000001",
			"Tenant A",
			"tenant@example.com",
			"0912-345-678",
			`[{"name":"Emergency Contact"}]`,
			nil,
			nil,
			nil,
			nil,
			"active",
			now,
			now,
			1,
		))
	mock.ExpectClose()

	result, err := repo.ListAccessible(context.Background(), "organizer", []string{"10000000-0000-0000-0000-000000000001"}, nil, "active", 10, 20)
	if err != nil {
		t.Fatalf("ListAccessible: %v", err)
	}
	if result.Total != 7 {
		t.Fatalf("expected total 7, got %d", result.Total)
	}
	tenants := result.Items
	if len(tenants) != 1 {
		t.Fatalf("expected 1 tenant, got %d", len(tenants))
	}
	if tenants[0].Name != "Tenant A" {
		t.Fatalf("expected Tenant A, got %s", tenants[0].Name)
	}
	if len(tenants[0].Contacts) != 1 {
		t.Fatalf("expected contacts to decode, got %#v", tenants[0].Contacts)
	}

}

func TestFindByIDAccessibleMapsNoRowsToErrNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	repo := NewRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT
	t.id,
	t.name,
	t.email,
	t.phone,
	t.contacts::text,
	t.birth_date,
	t.national_id,
	t.address,
	t.occupation,
	t.status,
	t.created_at,
	t.updated_at,
	t.version
FROM tenants t
JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL
WHERE t.id = $1
  AND t.deleted_at IS NULL
  AND l.property_id IN ($2)
LIMIT 1`)).
		WithArgs("30000000-0000-0000-0000-000000000099", "10000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "email", "phone", "contacts", "birth_date", "national_id", "address", "occupation", "status", "created_at", "updated_at", "version",
		}))
	mock.ExpectClose()

	_, err = repo.FindByIDAccessible(context.Background(), "30000000-0000-0000-0000-000000000099", "organizer", []string{"10000000-0000-0000-0000-000000000001"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

}

func TestListLeasesByTenantAccessibleReturnsLeaseHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("ExpectationsWereMet: %v", err)
		}
	})

	repo := NewRepository(db)
	now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)
	startDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT
	t.id,
	t.name,
	t.email,
	t.phone,
	t.contacts::text,
	t.birth_date,
	t.national_id,
	t.address,
	t.occupation,
	t.status,
	t.created_at,
	t.updated_at,
	t.version
FROM tenants t
JOIN leases l ON l.tenant_id = t.id AND l.deleted_at IS NULL
WHERE t.id = $1
  AND t.deleted_at IS NULL
  AND l.property_id IN ($2)
LIMIT 1`)).
		WithArgs("30000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "email", "phone", "contacts", "birth_date", "national_id", "address", "occupation", "status", "created_at", "updated_at", "version",
		}).AddRow(
			"30000000-0000-0000-0000-000000000001", "Tenant A", "tenant@example.com", nil, `[]`, nil, nil, nil, nil, "active", now, now, 1,
		))

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT
	l.id,
	l.tenant_id,
	l.property_id,
	l.room_id,
	l.rent_amount,
	l.start_date,
	l.end_date,
	l.rent_billing_cadence,
	l.electricity_billing_cadence,
	l.status,
	l.deposit_amount,
	l.deposit_refund_amount,
	l.deposit_deduction_amount,
	l.deposit_status,
	l.deposit_deduction_reason,
	l.notes,
	l.termination_reason,
	l.settlement_detail::text,
	l.created_at,
	l.updated_at,
	l.version
FROM leases l
WHERE l.tenant_id = $1
  AND l.deleted_at IS NULL
  AND l.property_id IN ($2)
  AND l.status = $3
ORDER BY l.created_at DESC`)).
		WithArgs("30000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000001", "active").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "property_id", "room_id", "rent_amount", "start_date", "end_date", "rent_billing_cadence", "electricity_billing_cadence", "status", "deposit_amount", "deposit_refund_amount", "deposit_deduction_amount", "deposit_status", "deposit_deduction_reason", "notes", "termination_reason", "settlement_detail", "created_at", "updated_at", "version",
		}).AddRow(
			"40000000-0000-0000-0000-000000000001",
			"30000000-0000-0000-0000-000000000001",
			"10000000-0000-0000-0000-000000000001",
			"20000000-0000-0000-0000-000000000001",
			12000,
			startDate,
			endDate,
			"quarterly",
			"monthly",
			"active",
			24000,
			nil,
			nil,
			"held",
			nil,
			nil,
			nil,
			nil,
			now,
			now,
			1,
		))
	mock.ExpectClose()

	leases, err := repo.ListLeasesByTenantAccessible(context.Background(), "30000000-0000-0000-0000-000000000001", "organizer", []string{"10000000-0000-0000-0000-000000000001"}, "active")
	if err != nil {
		t.Fatalf("ListLeasesByTenantAccessible: %v", err)
	}
	if len(leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(leases))
	}
	if leases[0].ElectricityBillingCadence != "monthly" {
		t.Fatalf("expected monthly cadence, got %s", leases[0].ElectricityBillingCadence)
	}
	if leases[0].RentBillingCadence != "quarterly" {
		t.Fatalf("expected quarterly rent cadence, got %s", leases[0].RentBillingCadence)
	}

}
