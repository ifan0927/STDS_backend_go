package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"stds_backend/internal/shared/apperr"
)

func TestTenantLeaseRosterServiceForwardsScopeAndPagination(t *testing.T) {
	repo := &tenantLeaseRosterRepositoryStub{
		result: TenantLeaseRosterResult{
			Items: []TenantLeaseRosterRow{{PropertyID: testPropertyID, RoomID: testRoomID, RoomLabel: "101"}},
			Total: 1,
		},
	}
	service := NewTenantLeaseRosterService(repo, fixedClock{now: time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)})

	result, err := service.Execute(context.Background(), TenantLeaseRosterInput{
		ActorRole:           " Staff ",
		ActorUserID:         " user-1 ",
		AssignedPropertyIDs: []string{testPropertyID},
		PropertyID:          testPropertyID,
		IncludeVacant:       true,
		Limit:               10,
		Offset:              20,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Items) != 1 || result.Total != 1 {
		t.Fatalf("result = %+v", result)
	}
	if repo.query == nil {
		t.Fatal("expected repository query")
	}
	if repo.query.ActorRole != "staff" || repo.query.ActorUserID != "user-1" || repo.query.PropertyID != testPropertyID {
		t.Fatalf("query scope = %+v", repo.query)
	}
	if !repo.query.IncludeVacant || repo.query.Limit != 10 || repo.query.Offset != 20 {
		t.Fatalf("query pagination/include_vacant = %+v", repo.query)
	}
	if len(repo.query.AssignedPropertyIDs) != 1 || repo.query.AssignedPropertyIDs[0] != testPropertyID {
		t.Fatalf("assigned properties = %+v", repo.query.AssignedPropertyIDs)
	}
	if repo.query.AsOf.Format("2006-01-02") != "2026-05-09" {
		t.Fatalf("AsOf = %s", repo.query.AsOf.Format("2006-01-02"))
	}
}

func TestTenantLeaseRosterServiceValidation(t *testing.T) {
	service := NewTenantLeaseRosterService(&tenantLeaseRosterRepositoryStub{}, fixedClock{})

	tests := []struct {
		name string
		in   TenantLeaseRosterInput
		code string
	}{
		{
			name: "owner allowed",
			in: TenantLeaseRosterInput{
				ActorRole:  "owner",
				PropertyID: testPropertyID,
				Limit:      20,
			},
		},
		{
			name: "invalid role",
			in: TenantLeaseRosterInput{
				ActorRole:  "tenant",
				PropertyID: testPropertyID,
				Limit:      20,
			},
			code: apperr.CodeForbidden,
		},
		{
			name: "invalid property",
			in: TenantLeaseRosterInput{
				ActorRole:  "admin",
				PropertyID: "not-a-uuid",
				Limit:      20,
			},
			code: apperr.CodeBadRequest,
		},
		{
			name: "zero limit",
			in: TenantLeaseRosterInput{
				ActorRole:  "admin",
				PropertyID: testPropertyID,
			},
			code: apperr.CodeBadRequest,
		},
		{
			name: "negative offset",
			in: TenantLeaseRosterInput{
				ActorRole:  "admin",
				PropertyID: testPropertyID,
				Limit:      20,
				Offset:     -1,
			},
			code: apperr.CodeBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Execute(context.Background(), tt.in)
			if tt.code == "" {
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				return
			}
			assertAppErrorCode(t, err, tt.code)
		})
	}
}

func TestTenantLeaseRosterServiceMapsRepositoryError(t *testing.T) {
	service := NewTenantLeaseRosterService(&tenantLeaseRosterRepositoryStub{err: errors.New("db down")}, fixedClock{})

	_, err := service.Execute(context.Background(), TenantLeaseRosterInput{
		ActorRole:  "admin",
		PropertyID: testPropertyID,
		Limit:      20,
	})
	assertAppErrorCode(t, err, apperr.CodeInternalServerError)
}

type tenantLeaseRosterRepositoryStub struct {
	query  *TenantLeaseRosterQuery
	result TenantLeaseRosterResult
	err    error
}

func (s *tenantLeaseRosterRepositoryStub) ListTenantLeaseRoster(_ context.Context, query TenantLeaseRosterQuery) (TenantLeaseRosterResult, error) {
	s.query = &query
	if s.err != nil {
		return TenantLeaseRosterResult{}, s.err
	}
	return s.result, nil
}
