package server

import (
	"context"
	"database/sql"

	apptenant "stds_backend/internal/application/tenant"
	dbtenants "stds_backend/internal/platform/database/tenants"
)

type tenantRepositoryAdapter struct {
	repo dbtenants.CommandRepository
}

func (a tenantRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params apptenant.CreateTenantParams) (*apptenant.Tenant, error) {
	tenant, err := a.repo.Create(ctx, tx, dbtenants.CreateTenantParams{
		Name:       params.Name,
		Email:      params.Email,
		Phone:      params.Phone,
		Contacts:   params.Contacts,
		BirthDate:  params.BirthDate,
		NationalID: params.NationalID,
		Address:    params.Address,
		Occupation: params.Occupation,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationTenant(tenant), nil
}

func (a tenantRepositoryAdapter) FindByID(ctx context.Context, tx *sql.Tx, id string) (*apptenant.Tenant, error) {
	tenant, err := a.repo.FindByID(ctx, tx, id)
	if err != nil {
		if err == dbtenants.ErrNotFound {
			return nil, apptenant.ErrTenantNotFound
		}
		return nil, err
	}

	return toApplicationTenant(tenant), nil
}

func (a tenantRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params apptenant.UpdateTenantParams) (*apptenant.Tenant, error) {
	tenant, err := a.repo.Update(ctx, tx, dbtenants.UpdateTenantParams{
		ID:         params.ID,
		Name:       params.Name,
		Email:      params.Email,
		Phone:      params.Phone,
		Contacts:   params.Contacts,
		BirthDate:  params.BirthDate,
		NationalID: params.NationalID,
		Address:    params.Address,
		Occupation: params.Occupation,
		Version:    params.Version,
	})
	if err != nil {
		if err == dbtenants.ErrNotFound {
			return nil, apptenant.ErrTenantNotFound
		}
		return nil, err
	}

	return toApplicationTenant(tenant), nil
}

func toApplicationTenant(tenant *dbtenants.Tenant) *apptenant.Tenant {
	return &apptenant.Tenant{
		ID:         tenant.ID,
		Name:       tenant.Name,
		Email:      tenant.Email,
		Phone:      tenant.Phone,
		Contacts:   tenant.Contacts,
		BirthDate:  tenant.BirthDate,
		NationalID: tenant.NationalID,
		Address:    tenant.Address,
		Occupation: tenant.Occupation,
		Status:     tenant.Status,
		CreatedAt:  tenant.CreatedAt,
		UpdatedAt:  tenant.UpdatedAt,
		Version:    tenant.Version,
	}
}
