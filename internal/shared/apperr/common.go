package apperr

import "net/http"

const (
	CodeInternalServerError      = "INTERNAL_SERVER_ERROR"
	CodeUnauthorized             = "UNAUTHORIZED"
	CodeInvalidFirebaseToken     = "INVALID_FIREBASE_TOKEN"
	CodeForbidden                = "FORBIDDEN"
	CodeConcurrentUpdateConflict = "CONCURRENT_UPDATE_CONFLICT"
)

var (
	ErrInternalServerError = New(
		CodeInternalServerError,
		http.StatusInternalServerError,
		"Internal server error.",
	)
	ErrUnauthorized = New(
		CodeUnauthorized,
		http.StatusUnauthorized,
		"Unauthorized.",
	)
	ErrInvalidFirebaseToken = New(
		CodeInvalidFirebaseToken,
		http.StatusUnauthorized,
		"Invalid Firebase ID token.",
	)
	ErrForbidden = New(
		CodeForbidden,
		http.StatusForbidden,
		"Forbidden.",
	)
	ErrConcurrentUpdateConflict = New(
		CodeConcurrentUpdateConflict,
		http.StatusConflict,
		"Concurrent update conflict.",
	)
)
