package server

import (
	"context"
	"database/sql"

	appproperty "stds_backend/internal/application/property"
	dbbilling "stds_backend/internal/platform/database/billing"
	dbproperties "stds_backend/internal/platform/database/properties"
	dbpropertyquery "stds_backend/internal/platform/database/propertyquery"
	"stds_backend/internal/shared/apperr"
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

type propertyAccountRepositoryAdapter struct {
	repo *dbbilling.SQLRepository
}

func (a propertyAccountRepositoryAdapter) CreatePropertyAccount(ctx context.Context, tx *sql.Tx, params appproperty.CreatePropertyAccountParams) error {
	return a.repo.CreatePropertyAccount(ctx, tx, params.PropertyID)
}

func (a propertyRepositoryAdapter) Create(ctx context.Context, tx *sql.Tx, params appproperty.CreatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Create(ctx, tx, dbproperties.CreatePropertyParams{
		Name:                             params.Name,
		PropertyPublicName:               params.PropertyPublicName,
		Subtitle:                         params.Subtitle,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		ContactPhone:                     params.ContactPhone,
		ContactEmail:                     params.ContactEmail,
		Notes:                            params.Notes,
		Facilities:                       params.Facilities,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationProperty(property), nil
}

func (a propertyRepositoryAdapter) FindByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Property, error) {
	property, err := a.repo.FindByID(ctx, tx, id)
	if err != nil {
		if err == dbproperties.ErrNotFound {
			return nil, appproperty.ErrPropertyNotFound
		}
		return nil, err
	}

	return toApplicationProperty(property), nil
}

func (a propertyRepositoryAdapter) Update(ctx context.Context, tx *sql.Tx, params appproperty.UpdatePropertyParams) (*appproperty.Property, error) {
	property, err := a.repo.Update(ctx, tx, dbproperties.UpdatePropertyParams{
		ID:                               params.ID,
		Name:                             params.Name,
		PropertyPublicName:               params.PropertyPublicName,
		Subtitle:                         params.Subtitle,
		Address:                          params.Address,
		ElectricityUnitPrice:             params.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: params.DefaultElectricityBillingCadence,
		OwnerID:                          params.OwnerID,
		ContactPhone:                     params.ContactPhone,
		ContactEmail:                     params.ContactEmail,
		Notes:                            params.Notes,
		Facilities:                       params.Facilities,
		Version:                          params.Version,
	})
	if err != nil {
		if err == dbproperties.ErrNotFound {
			return nil, appproperty.ErrPropertyNotFound
		}
		return nil, err
	}

	return toApplicationProperty(property), nil
}

func (a propertyRepositoryAdapter) ListOccupiedRoomIDs(ctx context.Context, tx *sql.Tx, propertyID string) ([]string, error) {
	return a.repo.ListOccupiedRoomIDs(ctx, tx, propertyID)
}

func (a propertyRepositoryAdapter) SoftDelete(ctx context.Context, tx *sql.Tx, id string, version int) error {
	err := a.repo.SoftDelete(ctx, tx, id, version)
	if err == dbproperties.ErrNotFound {
		return appproperty.ErrPropertyNotFound
	}
	return err
}

func (a propertyRepositoryAdapter) CreateRoom(ctx context.Context, tx *sql.Tx, params appproperty.CreateRoomParams) (*appproperty.Room, error) {
	room, err := a.repo.CreateRoom(ctx, tx, dbproperties.CreateRoomParams{
		PropertyID:        params.PropertyID,
		Name:              params.Name,
		Size:              params.Size,
		Floor:             params.Floor,
		RoomType:          params.RoomType,
		Facilities:        params.Facilities,
		DefaultRentAmount: params.DefaultRentAmount,
		Notes:             params.Notes,
		Zone:              params.Zone,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationRoom(room), nil
}

func (a propertyRepositoryAdapter) FindRoomByID(ctx context.Context, tx *sql.Tx, id string) (*appproperty.Room, error) {
	room, err := a.repo.FindRoomByID(ctx, tx, id)
	if err != nil {
		if err == dbproperties.ErrNotFound {
			return nil, apperr.ErrRoomNotFound
		}
		return nil, err
	}

	return toApplicationRoom(room), nil
}

func (a propertyRepositoryAdapter) UpdateRoom(ctx context.Context, tx *sql.Tx, params appproperty.UpdateRoomParams) (*appproperty.Room, error) {
	room, err := a.repo.UpdateRoom(ctx, tx, dbproperties.UpdateRoomParams{
		ID:                params.ID,
		Name:              params.Name,
		Status:            params.Status,
		Size:              params.Size,
		Floor:             params.Floor,
		RoomType:          params.RoomType,
		Facilities:        params.Facilities,
		DefaultRentAmount: params.DefaultRentAmount,
		Notes:             params.Notes,
		Zone:              params.Zone,
	})
	if err != nil {
		if err == dbproperties.ErrNotFound {
			return nil, apperr.ErrRoomNotFound
		}
		return nil, err
	}

	return toApplicationRoom(room), nil
}

func (a propertyRepositoryAdapter) SoftDeleteRoom(ctx context.Context, tx *sql.Tx, id string) error {
	err := a.repo.SoftDeleteRoom(ctx, tx, id)
	if err == dbproperties.ErrNotFound {
		return apperr.ErrRoomNotFound
	}
	return err
}

func (a propertyRepositoryAdapter) CreateRepairRequest(ctx context.Context, tx *sql.Tx, params appproperty.CreateRepairRequestParams) (*appproperty.RepairRequest, error) {
	repairRequest, err := a.repo.CreateRepairRequest(ctx, tx, dbproperties.CreateRepairRequestParams{
		PropertyID:  params.PropertyID,
		RoomID:      params.RoomID,
		SubmittedBy: params.SubmittedBy,
		Title:       params.Title,
		Description: params.Description,
	})
	if err != nil {
		return nil, err
	}

	return toApplicationRepairRequest(repairRequest), nil
}

func toApplicationProperty(property *dbproperties.Property) *appproperty.Property {
	return &appproperty.Property{
		ID:                               property.ID,
		Name:                             property.Name,
		PropertyPublicName:               property.PropertyPublicName,
		Subtitle:                         property.Subtitle,
		Address:                          property.Address,
		ElectricityUnitPrice:             property.ElectricityUnitPrice,
		DefaultElectricityBillingCadence: property.DefaultElectricityBillingCadence,
		OwnerID:                          property.OwnerID,
		ContactPhone:                     property.ContactPhone,
		ContactEmail:                     property.ContactEmail,
		Notes:                            property.Notes,
		Facilities:                       property.Facilities,
		CreatedAt:                        property.CreatedAt,
		UpdatedAt:                        property.UpdatedAt,
		Version:                          property.Version,
	}
}

func toApplicationRoom(room *dbproperties.Room) *appproperty.Room {
	return &appproperty.Room{
		ID:                room.ID,
		PropertyID:        room.PropertyID,
		Name:              room.Name,
		Status:            room.Status,
		Size:              room.Size,
		Floor:             room.Floor,
		RoomType:          room.RoomType,
		Facilities:        room.Facilities,
		DefaultRentAmount: room.DefaultRentAmount,
		Notes:             room.Notes,
		Zone:              room.Zone,
		CreatedAt:         room.CreatedAt,
		UpdatedAt:         room.UpdatedAt,
	}
}

func toApplicationRepairRequest(repairRequest *dbproperties.RepairRequest) *appproperty.RepairRequest {
	return &appproperty.RepairRequest{
		ID:          repairRequest.ID,
		PropertyID:  repairRequest.PropertyID,
		RoomID:      repairRequest.RoomID,
		SubmittedBy: repairRequest.SubmittedBy,
		AssignedTo:  repairRequest.AssignedTo,
		Title:       repairRequest.Title,
		Description: repairRequest.Description,
		Status:      repairRequest.Status,
		SubmittedAt: repairRequest.SubmittedAt,
		AssignedAt:  repairRequest.AssignedAt,
		CompletedAt: repairRequest.CompletedAt,
		CreatedAt:   repairRequest.CreatedAt,
		UpdatedAt:   repairRequest.UpdatedAt,
	}
}
