package property

import "errors"

var (
	ErrNameRequired                    = errors.New("property name is required")
	ErrPropertyPublicNameRequired      = errors.New("property public name is required")
	ErrAddressRequired                 = errors.New("property address is required")
	ErrOwnerIDRequired                 = errors.New("property owner id is required")
	ErrElectricityPriceMustBePositive  = errors.New("property electricity price must be positive")
	ErrInvalidBillingCadence           = errors.New("property billing cadence is invalid")
	ErrForbiddenElectricityPriceUpdate = errors.New("property electricity price update is forbidden")
	ErrRoomNameRequired                = errors.New("room name is required")
	ErrRoomIsOccupied                  = errors.New("room is occupied")
	ErrRoomIsInMaintenance             = errors.New("room is in maintenance")
	ErrBadRoomStatus                   = errors.New("room status is invalid")
)

// OccupiedRoomsError indicates that a property cannot be deleted because one or
// more rooms are still occupied.
type OccupiedRoomsError struct {
	OccupiedRoomIDs []string
}

// Error implements the error interface.
func (e *OccupiedRoomsError) Error() string {
	return "property has occupied rooms"
}
