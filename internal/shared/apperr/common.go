package apperr

import "net/http"

const (
	// CodeInternalServerError identifies unexpected server failures.
	CodeInternalServerError = "INTERNAL_SERVER_ERROR"
	// CodeBadRequest identifies malformed client requests.
	CodeBadRequest = "BAD_REQUEST"
	// CodeUnauthorized identifies requests without valid authentication.
	CodeUnauthorized = "UNAUTHORIZED"
	// CodeInvalidFirebaseToken identifies requests with an invalid Firebase ID token.
	CodeInvalidFirebaseToken = "INVALID_FIREBASE_TOKEN"
	// CodeForbidden identifies authenticated requests that are not allowed.
	CodeForbidden = "FORBIDDEN"
	// CodeConcurrentUpdateConflict identifies writes that lose an optimistic concurrency check.
	CodeConcurrentUpdateConflict = "CONCURRENT_UPDATE_CONFLICT"
	// CodeUserNotFound identifies requests that reference a missing user.
	CodeUserNotFound = "USER_NOT_FOUND"
	// CodeEmailAlreadyExists identifies user creation conflicts on email.
	CodeEmailAlreadyExists = "EMAIL_ALREADY_EXISTS"
	// CodeFirebaseUIDAlreadyExists identifies user creation conflicts on Firebase UID.
	CodeFirebaseUIDAlreadyExists = "FIREBASE_UID_ALREADY_EXISTS"
	// CodeValidationEmailRequired identifies missing required email input.
	CodeValidationEmailRequired = "VALIDATION_EMAIL_REQUIRED"
	// CodeValidationEmailInvalid identifies invalid email input.
	CodeValidationEmailInvalid = "VALIDATION_EMAIL_INVALID"
	// CodeValidationNameRequired identifies missing required name input.
	CodeValidationNameRequired = "VALIDATION_NAME_REQUIRED"
	// CodeValidationNameTooLong identifies name input that exceeds the allowed length.
	CodeValidationNameTooLong = "VALIDATION_NAME_TOO_LONG"
	// CodeValidationAddressRequired identifies missing required address input.
	CodeValidationAddressRequired = "VALIDATION_ADDRESS_REQUIRED"
	// CodeValidationOwnerIDRequired identifies missing required owner id input.
	CodeValidationOwnerIDRequired = "VALIDATION_OWNER_ID_REQUIRED"
	// CodeValidationRoleRequired identifies missing required role input.
	CodeValidationRoleRequired = "VALIDATION_ROLE_REQUIRED"
	// CodeValidationRoleInvalid identifies unsupported role input.
	CodeValidationRoleInvalid = "VALIDATION_ROLE_INVALID"
	// CodePropertyNotFound identifies requests that reference a missing property.
	CodePropertyNotFound = "PROPERTY_NOT_FOUND"
	// CodeValidationElectricityPriceInvalid identifies invalid electricity price input.
	CodeValidationElectricityPriceInvalid = "VALIDATION_ELECTRICITY_PRICE_INVALID"
	// CodeValidationWindowKeyRequired identifies missing scheduler window keys.
	CodeValidationWindowKeyRequired = "VALIDATION_WINDOW_KEY_REQUIRED"
)

var (
	// ErrInternalServerError is the default application error for unexpected failures.
	ErrInternalServerError = New(
		CodeInternalServerError,
		http.StatusInternalServerError,
		"Internal server error.",
	)
	// ErrBadRequest is returned when the request payload is malformed.
	ErrBadRequest = New(
		CodeBadRequest,
		http.StatusBadRequest,
		"Bad request.",
	)
	// ErrUnauthorized is the default application error for authentication failures.
	ErrUnauthorized = New(
		CodeUnauthorized,
		http.StatusUnauthorized,
		"Unauthorized.",
	)
	// ErrInvalidFirebaseToken is returned when Firebase token verification fails.
	ErrInvalidFirebaseToken = New(
		CodeInvalidFirebaseToken,
		http.StatusUnauthorized,
		"Invalid Firebase ID token.",
	)
	// ErrForbidden is the default application error for authorization failures.
	ErrForbidden = New(
		CodeForbidden,
		http.StatusForbidden,
		"Forbidden.",
	)
	// ErrConcurrentUpdateConflict is returned when a write conflicts with a newer persisted state.
	ErrConcurrentUpdateConflict = New(
		CodeConcurrentUpdateConflict,
		http.StatusConflict,
		"Concurrent update conflict.",
	)
	// ErrUserNotFound is returned when a requested user record does not exist.
	ErrUserNotFound = New(
		CodeUserNotFound,
		http.StatusNotFound,
		"User not found.",
	)
	// ErrEmailAlreadyExists is returned when the email is already used by another active user.
	ErrEmailAlreadyExists = New(
		CodeEmailAlreadyExists,
		http.StatusConflict,
		"Email already exists.",
	)
	// ErrFirebaseUIDAlreadyExists is returned when the Firebase UID is already used by another active user.
	ErrFirebaseUIDAlreadyExists = New(
		CodeFirebaseUIDAlreadyExists,
		http.StatusConflict,
		"Firebase UID already exists.",
	)
	// ErrValidationEmailRequired is returned when email is missing from input.
	ErrValidationEmailRequired = New(
		CodeValidationEmailRequired,
		http.StatusBadRequest,
		"Email is required.",
	)
	// ErrValidationEmailInvalid is returned when email is not a valid format.
	ErrValidationEmailInvalid = New(
		CodeValidationEmailInvalid,
		http.StatusBadRequest,
		"Email format is invalid.",
	)
	// ErrValidationNameRequired is returned when name is missing from input.
	ErrValidationNameRequired = New(
		CodeValidationNameRequired,
		http.StatusBadRequest,
		"Name is required.",
	)
	// ErrValidationNameTooLong is returned when name exceeds the allowed length.
	ErrValidationNameTooLong = New(
		CodeValidationNameTooLong,
		http.StatusBadRequest,
		"Name must be 100 characters or fewer.",
	)
	// ErrValidationAddressRequired is returned when address is missing from input.
	ErrValidationAddressRequired = New(
		CodeValidationAddressRequired,
		http.StatusBadRequest,
		"Address is required.",
	)
	// ErrValidationOwnerIDRequired is returned when owner id is missing from input.
	ErrValidationOwnerIDRequired = New(
		CodeValidationOwnerIDRequired,
		http.StatusBadRequest,
		"Owner ID is required.",
	)
	// ErrValidationRoleRequired is returned when role is missing from input.
	ErrValidationRoleRequired = New(
		CodeValidationRoleRequired,
		http.StatusBadRequest,
		"Role is required.",
	)
	// ErrValidationRoleInvalid is returned when role is not one of the supported values.
	ErrValidationRoleInvalid = New(
		CodeValidationRoleInvalid,
		http.StatusBadRequest,
		"Role is invalid.",
	)
	// ErrPropertyNotFound is returned when a requested property record does not exist.
	ErrPropertyNotFound = New(
		CodePropertyNotFound,
		http.StatusNotFound,
		"Property not found.",
	)
	// ErrValidationElectricityPriceInvalid is returned when electricity price is not positive.
	ErrValidationElectricityPriceInvalid = New(
		CodeValidationElectricityPriceInvalid,
		http.StatusBadRequest,
		"Electricity unit price is invalid.",
	)
	// ErrValidationWindowKeyRequired is returned when a scheduler request omits window_key.
	ErrValidationWindowKeyRequired = New(
		CodeValidationWindowKeyRequired,
		http.StatusBadRequest,
		"window_key is required.",
	)
)
