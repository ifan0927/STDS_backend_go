package apperr

import "net/http"

const (
	// CodeInternalServerError identifies unexpected server failures.
	CodeInternalServerError = "INTERNAL_SERVER_ERROR"
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
	// CodeValidationFirebaseUIDRequired identifies missing required Firebase UID input.
	CodeValidationFirebaseUIDRequired = "VALIDATION_FIREBASE_UID_REQUIRED"
	// CodeValidationNameRequired identifies missing required name input.
	CodeValidationNameRequired = "VALIDATION_NAME_REQUIRED"
	// CodeValidationRoleRequired identifies missing required role input.
	CodeValidationRoleRequired = "VALIDATION_ROLE_REQUIRED"
	// CodeValidationRoleInvalid identifies unsupported role input.
	CodeValidationRoleInvalid = "VALIDATION_ROLE_INVALID"
)

var (
	// ErrInternalServerError is the default application error for unexpected failures.
	ErrInternalServerError = New(
		CodeInternalServerError,
		http.StatusInternalServerError,
		"Internal server error.",
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
	// ErrValidationFirebaseUIDRequired is returned when Firebase UID is missing from input.
	ErrValidationFirebaseUIDRequired = New(
		CodeValidationFirebaseUIDRequired,
		http.StatusBadRequest,
		"Firebase UID is required.",
	)
	// ErrValidationNameRequired is returned when name is missing from input.
	ErrValidationNameRequired = New(
		CodeValidationNameRequired,
		http.StatusBadRequest,
		"Name is required.",
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
)
