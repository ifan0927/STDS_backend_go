package lease

import (
	"context"
	"database/sql"

	domainevents "stds_backend/internal/domain/events"
	"stds_backend/internal/platform/database/txrunner"
)

// OccupyRoomOnLeaseCreatedHandler updates room occupancy after lease creation.
type OccupyRoomOnLeaseCreatedHandler struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewOccupyRoomOnLeaseCreatedHandler returns an occupancy handler.
func NewOccupyRoomOnLeaseCreatedHandler(repo Repository, txRunner *txrunner.Runner) *OccupyRoomOnLeaseCreatedHandler {
	return &OccupyRoomOnLeaseCreatedHandler{repo: repo, txRunner: txRunner}
}

// HandleLeaseCreated marks the leased room as occupied.
func (h *OccupyRoomOnLeaseCreatedHandler) HandleLeaseCreated(ctx context.Context, event domainevents.LeaseCreated) error {
	return h.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		return h.repo.MarkRoomOccupied(ctx, tx, event.RoomID)
	})
}

// ActivateTenantOnLeaseCreatedHandler reactivates inactive tenants after lease creation.
type ActivateTenantOnLeaseCreatedHandler struct {
	repo     Repository
	txRunner *txrunner.Runner
}

// NewActivateTenantOnLeaseCreatedHandler returns a tenant activation handler.
func NewActivateTenantOnLeaseCreatedHandler(repo Repository, txRunner *txrunner.Runner) *ActivateTenantOnLeaseCreatedHandler {
	return &ActivateTenantOnLeaseCreatedHandler{repo: repo, txRunner: txRunner}
}

// HandleLeaseCreated marks the tenant active when needed.
func (h *ActivateTenantOnLeaseCreatedHandler) HandleLeaseCreated(ctx context.Context, event domainevents.LeaseCreated) error {
	return h.txRunner.WithinTransaction(ctx, func(ctx context.Context, tx *sql.Tx, _ *txrunner.EventRecorder) error {
		return h.repo.ActivateTenant(ctx, tx, event.TenantID)
	})
}
