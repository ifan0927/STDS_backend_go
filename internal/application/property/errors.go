package property

import (
	"errors"
	"net/http"

	domainproperty "stds_backend/internal/domain/property"
	"stds_backend/internal/shared/apperr"
)

const (
	codeForbiddenElectricityPriceUpdate = "FORBIDDEN_ELECTRICITY_PRICE_UPDATE"
	codeElectricityPriceMustBePositive  = "ELECTRICITY_PRICE_MUST_BE_POSITIVE"
	codePropertyHasOccupiedRooms        = "PROPERTY_HAS_OCCUPIED_ROOMS"
	codeValidationRoomNameRequired      = "VALIDATION_ROOM_NAME_REQUIRED"
	codeRoomIsOccupied                  = "ROOM_IS_OCCUPIED"
	codeRoomIsInMaintenance             = "ROOM_IS_IN_MAINTENANCE"
)

var (
	errForbiddenElectricityPriceUpdate = apperr.New(
		codeForbiddenElectricityPriceUpdate,
		http.StatusForbidden,
		"Updating electricity price requires admin or organizer role.",
	)
	errElectricityPriceMustBePositive = apperr.New(
		codeElectricityPriceMustBePositive,
		http.StatusUnprocessableEntity,
		"Electricity price must be positive.",
	)
	errPropertyHasOccupiedRooms = apperr.New(
		codePropertyHasOccupiedRooms,
		http.StatusUnprocessableEntity,
		"Property has occupied rooms.",
	)
	errValidationRoomNameRequired = apperr.New(
		codeValidationRoomNameRequired,
		http.StatusBadRequest,
		"Room name is required.",
	)
	errRoomIsOccupied = apperr.New(
		codeRoomIsOccupied,
		http.StatusUnprocessableEntity,
		"Room is occupied.",
	)
	errRoomIsInMaintenance = apperr.New(
		codeRoomIsInMaintenance,
		http.StatusUnprocessableEntity,
		"Room is in maintenance.",
	)
)

func mapDomainError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, domainproperty.ErrNameRequired):
		return apperr.ErrValidationNameRequired
	case errors.Is(err, domainproperty.ErrAddressRequired):
		return apperr.ErrValidationAddressRequired
	case errors.Is(err, domainproperty.ErrOwnerIDRequired):
		return apperr.ErrValidationOwnerIDRequired
	case errors.Is(err, domainproperty.ErrInvalidBillingCadence):
		return apperr.ErrBadRequest.WithDetails(map[string]interface{}{
			"field": "default_electricity_billing_cadence",
		})
	case errors.Is(err, domainproperty.ErrForbiddenElectricityPriceUpdate):
		return errForbiddenElectricityPriceUpdate
	case errors.Is(err, domainproperty.ErrElectricityPriceMustBePositive):
		return errElectricityPriceMustBePositive
	case errors.Is(err, domainproperty.ErrRoomNameRequired):
		return errValidationRoomNameRequired
	case errors.Is(err, domainproperty.ErrRoomIsOccupied):
		return errRoomIsOccupied
	case errors.Is(err, domainproperty.ErrRoomIsInMaintenance):
		return errRoomIsInMaintenance
	case errors.Is(err, domainproperty.ErrBadRoomStatus):
		return apperr.ErrInternalServerError.WithCause(err)
	default:
		var occupiedErr *domainproperty.OccupiedRoomsError
		if errors.As(err, &occupiedErr) {
			return errPropertyHasOccupiedRooms.WithDetails(map[string]interface{}{
				"occupied_room_ids": occupiedErr.OccupiedRoomIDs,
			})
		}
		return err
	}
}
