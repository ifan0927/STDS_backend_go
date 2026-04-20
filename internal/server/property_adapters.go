package server

import (
	"context"
	"database/sql"

	appproperty "stds_backend/internal/application/property"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
)

type propertyExistenceCheckerAdapter struct {
	repo dbpropertyquery.Repository
}

func (a propertyExistenceCheckerAdapter) Exists(ctx context.Context, propertyID string) (bool, error) {
	_, err := a.repo.FindByID(ctx, propertyID)
	if err != nil {
		if err == dbpropertyquery.ErrNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

type propertyRepositoryAdapter struct {
	repo dbproperties.CommandRepository
}

func (a propertyRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params appproperty.CreatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Create(ctx, tx, dbproperties.CreatePropertyParams{
		Name:                             params.Name,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationProperty(property), nil
}

func toApplicationProperty(property *dbproperties.Property) *appproperty.Property {
	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}
}
