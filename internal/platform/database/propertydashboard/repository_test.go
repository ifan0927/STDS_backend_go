package propertydashboard

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	appproperty "stds_backend/internal/application/property"
)

func TestGetDashboardReturnsPropertyDashboard(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	propertyID := "10000000-0000-0000-0000-000000000001"
	createdAt := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1\nFROM properties")).
		WithArgs(propertyID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, name, status\nFROM rooms")).
		WithArgs(propertyID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status"}).
			AddRow("20000000-0000-0000-0000-000000000001", "101 Room", "occupied"))
	mock.ExpectQuery("(?s)type = 'rent' AND status NOT IN \\('voided', 'written_off'\\).*AS expected_rent.*status = 'overdue' AND period_start >= \\$2 AND period_start < \\$3.*AS overdue_bill_count").
		WithArgs(
			propertyID,
			time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		).
		WillReturnRows(sqlmock.NewRows([]string{"expected_rent", "collected_rent", "overdue_bill_count"}).
			AddRow(50000, 40000, 2))
	mock.ExpectQuery("(?s)FROM journal_logs.*UNION ALL.*FROM repair_requests").
		WithArgs(propertyID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "type", "content", "created_at"}).
			AddRow("70000000-0000-0000-0000-000000000001", "repair_request", "Fix leak", createdAt.Add(time.Minute)).
			AddRow("60000000-0000-0000-0000-000000000001", "journal_log", "Changed lobby light", createdAt))

	dashboard, err := repo.GetDashboard(context.Background(), propertyID, 2026, 4)
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if dashboard.PropertyID != propertyID {
		t.Fatalf("PropertyID = %s", dashboard.PropertyID)
	}
	if len(dashboard.Rooms) != 1 || dashboard.Rooms[0].Status != "occupied" {
		t.Fatalf("unexpected rooms: %+v", dashboard.Rooms)
	}
	if dashboard.MonthlySummary.ExpectedRent != 50000 || dashboard.MonthlySummary.CollectedRent != 40000 || dashboard.MonthlySummary.OverdueBillCount != 2 {
		t.Fatalf("unexpected monthly summary: %+v", dashboard.MonthlySummary)
	}
	if len(dashboard.RecentJournals) != 2 {
		t.Fatalf("unexpected recent journals: %+v", dashboard.RecentJournals)
	}
	if dashboard.RecentJournals[0].Type != "repair_request" || dashboard.RecentJournals[0].Content != "Fix leak" {
		t.Fatalf("expected repair request first, got %+v", dashboard.RecentJournals[0])
	}
	if dashboard.RecentJournals[1].Type != "journal_log" || dashboard.RecentJournals[1].Content != "Changed lobby light" {
		t.Fatalf("expected journal log second, got %+v", dashboard.RecentJournals[1])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestGetDashboardMapsMissingProperty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	propertyID := "10000000-0000-0000-0000-000000000099"

	mock.ExpectQuery(regexp.QuoteMeta("SELECT 1\nFROM properties")).
		WithArgs(propertyID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}))

	_, err = repo.GetDashboard(context.Background(), propertyID, 2026, 4)
	if !errors.Is(err, appproperty.ErrPropertyNotFound) {
		t.Fatalf("expected ErrPropertyNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
