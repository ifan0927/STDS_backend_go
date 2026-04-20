package iam

import (
	"context"

	"stds_backend/internal/shared/apperr"
)

// SyncAuthService resolves the backend user for an authenticated Firebase UID
// and synchronizes its authorization data to Firebase custom claims.
type SyncAuthService struct {
	userRepo     UserAccountLookupRepository
	claimsSyncer *CustomClaimsService
}

// NewSyncAuthService returns a SyncAuthService with the required dependencies.
func NewSyncAuthService(userRepo UserAccountLookupRepository, claimsSyncer *CustomClaimsService) *SyncAuthService {
	return &SyncAuthService{
		userRepo:     userRepo,
		claimsSyncer: claimsSyncer,
	}
}

// Execute looks up the backend user by Firebase UID and writes the latest
// authorization fields to Firebase custom claims.
func (s *SyncAuthService) Execute(ctx context.Context, firebaseUID string) (*UserAccount, error) {
	if s.claimsSyncer == nil {
		return nil, apperr.ErrInternalServerError
	}

	user, err := s.userRepo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		switch err {
		case ErrUserAccountNotFound:
			return nil, apperr.ErrUserNotFound
		default:
			return nil, apperr.ErrInternalServerError.WithCause(err)
		}
	}

	if err := s.claimsSyncer.Sync(ctx, user.FirebaseUID, user.Role, user.AssignedPropertyIDs); err != nil {
		return nil, apperr.ErrInternalServerError.WithCause(err)
	}

	return user, nil
}
