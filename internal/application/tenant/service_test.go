package tenant

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	domainevents "stds_backend/internal/domain/events"
	dbtxrunner "stds_backend/internal/platform/database/txrunner"
	"stds_backend/internal/shared/apperr"
)

type recordingPublisher struct {
	events []any
}

func (p *recordingPublisher) Publish(_ context.Context, event any) error {
	p.events = append(p.events, event)
	return nil
}

type tenantRepoStub struct {
	created   *Tenant
	current   *Tenant
	updated   *Tenant
	createErr error
	findErr   error
	updateErr error
}

func (s tenantRepoStub) Create(context.Context, *sql.Tx, CreateTenantParams) (*Tenant, error) {
	return s.created, s.createErr
}

func (s tenantRepoStub) FindByID(context.Context, *sql.Tx, string) (*Tenant, error) {
	if s.findErr != nil {
		return nil, s.findErr
	}
	return s.current, nil
}

func (s tenantRepoStub) Update(context.Context, *sql.Tx, UpdateTenantParams) (*Tenant, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return s.updated, nil
}

func TestCreateTenantServiceCreatesTenant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	email := "tenant@example.com"
	phone := "0912-345-678"
	service := NewCreateTenantService(tenantRepoStub{
		created: &Tenant{
			ID:        "tenant-1",
			Name:      "Tenant A",
			Email:     &email,
			Phone:     &phone,
			Contacts:  []map[string]interface{}{},
			Status:    "active",
			CreatedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
			Version:   1,
		},
	}, dbtxrunner.New(db, nil))

	created, err := service.Execute(context.Background(), CreateTenantInput{
		Name:  "Tenant A",
		Email: email,
		Phone: &phone,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if created == nil || created.ID != "tenant-1" {
		t.Fatalf("expected created tenant, got %#v", created)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateTenantServicePublishesTenantInfoUpdatedAfterCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectCommit()

	email := "tenant@example.com"
	updatedEmail := "tenant-updated@example.com"
	publisher := &recordingPublisher{}
	service := NewUpdateTenantService(tenantRepoStub{
		current: &Tenant{
			ID:       "tenant-1",
			Name:     "Tenant A",
			Email:    &email,
			Contacts: []map[string]interface{}{},
			Status:   "active",
			Version:  1,
		},
		updated: &Tenant{
			ID:        "tenant-1",
			Name:      "Tenant A",
			Email:     &updatedEmail,
			Contacts:  []map[string]interface{}{},
			Status:    "active",
			CreatedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC),
			Version:   2,
		},
	}, dbtxrunner.New(db, publisher))

	if _, err := service.Execute(context.Background(), UpdateTenantInput{
		ID:    "tenant-1",
		Email: &updatedEmail,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(publisher.events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(publisher.events))
	}
	if _, ok := publisher.events[0].(domainevents.TenantInfoUpdated); !ok {
		t.Fatalf("expected TenantInfoUpdated event, got %T", publisher.events[0])
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestUpdateTenantServiceMapsNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectRollback()

	service := NewUpdateTenantService(tenantRepoStub{
		findErr: ErrTenantNotFound,
	}, dbtxrunner.New(db, nil))

	name := "Tenant A"
	_, err = service.Execute(context.Background(), UpdateTenantInput{
		ID:   "missing",
		Name: &name,
	})
	if !errors.Is(err, apperr.ErrTenantNotFound) {
		t.Fatalf("expected ErrTenantNotFound, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
