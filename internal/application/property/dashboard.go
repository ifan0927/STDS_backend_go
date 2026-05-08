package property

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"stds_backend/internal/shared/apperr"
)

// DashboardInput is the query payload for retrieving a property dashboard.
type DashboardInput struct {
	PropertyID string
}

// HomeDashboardInput is the query payload for retrieving the home dashboard.
type HomeDashboardInput struct {
	ActorRole           string
	ActorUserID         string
	AssignedPropertyIDs []string
}

// DashboardService retrieves the property dashboard read model.
type DashboardService struct {
	repo DashboardRepository
	now  func() time.Time
}

var taiwanDashboardLocation = loadTaiwanDashboardLocation()

func loadTaiwanDashboardLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		return time.FixedZone("Asia/Taipei", 8*60*60)
	}

	return location
}

// NewDashboardService returns a DashboardService.
func NewDashboardService(repo DashboardRepository) *DashboardService {
	return &DashboardService{
		repo: repo,
		now:  time.Now,
	}
}

// Execute validates input and returns the current property dashboard.
func (s *DashboardService) Execute(ctx context.Context, input DashboardInput) (*Dashboard, error) {
	propertyID := strings.TrimSpace(input.PropertyID)
	if propertyID == "" {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "property_id"})
	}
	if _, err := uuid.Parse(propertyID); err != nil {
		return nil, apperr.ErrBadRequest.WithDetails(map[string]interface{}{"field": "property_id"}).WithCause(err)
	}

	current := s.now().In(taiwanDashboardLocation)
	dashboard, err := s.repo.GetDashboard(ctx, propertyID, current.Year(), int(current.Month()))
	if err != nil {
		switch {
		case errors.Is(err, ErrPropertyNotFound):
			return nil, apperr.ErrPropertyNotFound.WithCause(err)
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	return dashboard, nil
}

// ExecuteHome validates scope and returns the role-scoped home dashboard.
func (s *DashboardService) ExecuteHome(ctx context.Context, input HomeDashboardInput) (*HomeDashboard, error) {
	role := strings.TrimSpace(input.ActorRole)
	userID := strings.TrimSpace(input.ActorUserID)
	if role == "" {
		return nil, apperr.ErrUnauthorized
	}
	if role == "owner" && userID == "" {
		return nil, apperr.ErrUnauthorized
	}

	current := s.now().In(taiwanDashboardLocation)
	dashboard, err := s.repo.GetHomeDashboard(ctx, DashboardScope{
		Role:                role,
		UserID:              userID,
		AssignedPropertyIDs: input.AssignedPropertyIDs,
	}, current.Year(), int(current.Month()))
	if err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	return dashboard, nil
}
