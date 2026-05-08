package property

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/shared/apperr"
)

func TestDashboardServiceUsesCurrentMonth(t *testing.T) {
	repo := &dashboardRepositoryStub{
		dashboard: &Dashboard{PropertyID: "10000000-0000-0000-0000-000000000001"},
	}
	service := NewDashboardService(repo)
	service.now = func() time.Time {
		return time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	}

	dashboard, err := service.Execute(context.Background(), DashboardInput{
		PropertyID: "10000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if dashboard.PropertyID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("unexpected dashboard: %+v", dashboard)
	}
	if repo.propertyID != "10000000-0000-0000-0000-000000000001" || repo.year != 2026 || repo.month != 4 {
		t.Fatalf("unexpected repository query: propertyID=%s year=%d month=%d", repo.propertyID, repo.year, repo.month)
	}
}

func TestDashboardServiceCurrentMonthUsesTaiwanTimezone(t *testing.T) {
	repo := &dashboardRepositoryStub{
		dashboard: &Dashboard{PropertyID: "10000000-0000-0000-0000-000000000001"},
	}
	service := NewDashboardService(repo)
	service.now = func() time.Time {
		return time.Date(2026, 3, 31, 16, 30, 0, 0, time.UTC)
	}

	_, err := service.Execute(context.Background(), DashboardInput{
		PropertyID: "10000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.year != 2026 || repo.month != 4 {
		t.Fatalf("unexpected repository period: year=%d month=%d", repo.year, repo.month)
	}
}

func TestDashboardServiceMapsPropertyNotFound(t *testing.T) {
	service := NewDashboardService(&dashboardRepositoryStub{err: ErrPropertyNotFound})
	service.now = func() time.Time {
		return time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	}

	_, err := service.Execute(context.Background(), DashboardInput{
		PropertyID: "10000000-0000-0000-0000-000000000099",
	})
	if !errors.Is(err, apperr.ErrPropertyNotFound) {
		t.Fatalf("expected property not found, got %v", err)
	}
}

func TestDashboardServiceRejectsInvalidPropertyID(t *testing.T) {
	service := NewDashboardService(&dashboardRepositoryStub{})

	_, err := service.Execute(context.Background(), DashboardInput{PropertyID: "not-a-uuid"})
	if !errors.Is(err, apperr.ErrBadRequest) {
		t.Fatalf("expected bad request, got %v", err)
	}
}

func TestDashboardServiceExecuteHomePreservesScopeAndTaiwanMonth(t *testing.T) {
	repo := &dashboardRepositoryStub{
		home: &HomeDashboard{
			PortfolioSummary: OccupancySummary{TotalRooms: 2},
		},
	}
	service := NewDashboardService(repo)
	service.now = func() time.Time {
		return time.Date(2026, 3, 31, 16, 30, 0, 0, time.UTC)
	}

	dashboard, err := service.ExecuteHome(context.Background(), HomeDashboardInput{
		ActorRole:           "staff",
		ActorUserID:         "90000000-0000-0000-0000-000000000001",
		AssignedPropertyIDs: []string{"10000000-0000-0000-0000-000000000001"},
	})
	if err != nil {
		t.Fatalf("ExecuteHome: %v", err)
	}
	if dashboard.PortfolioSummary.TotalRooms != 2 {
		t.Fatalf("unexpected dashboard: %+v", dashboard)
	}
	if repo.scope.Role != "staff" || repo.scope.UserID != "90000000-0000-0000-0000-000000000001" || len(repo.scope.AssignedPropertyIDs) != 1 {
		t.Fatalf("unexpected scope: %+v", repo.scope)
	}
	if repo.year != 2026 || repo.month != 4 {
		t.Fatalf("unexpected repository period: year=%d month=%d", repo.year, repo.month)
	}
}

func TestDashboardServiceExecuteHomeRequiresRole(t *testing.T) {
	service := NewDashboardService(&dashboardRepositoryStub{})

	_, err := service.ExecuteHome(context.Background(), HomeDashboardInput{})
	if !errors.Is(err, apperr.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

type dashboardRepositoryStub struct {
	propertyID string
	year       int
	month      int
	scope      DashboardScope
	dashboard  *Dashboard
	home       *HomeDashboard
	err        error
}

func (r *dashboardRepositoryStub) GetDashboard(_ context.Context, propertyID string, year int, month int) (*Dashboard, error) {
	r.propertyID = propertyID
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}

	return r.dashboard, nil
}

func (r *dashboardRepositoryStub) GetHomeDashboard(_ context.Context, scope DashboardScope, year int, month int) (*HomeDashboard, error) {
	r.scope = scope
	r.year = year
	r.month = month
	if r.err != nil {
		return nil, r.err
	}

	return r.home, nil
}
