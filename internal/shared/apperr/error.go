package apperr

// Error is the shared application error contract used across transport,
// application, and domain boundaries.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Details    any
	Cause      error
}

// Error returns the public error message, falling back to the error code when
// no message has been set.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	if e.Message != "" {
		return e.Message
	}

	return e.Code
}

// Unwrap exposes the wrapped cause so Error integrates with errors.Is and
// errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// New creates a shared error without details or wrapped cause.
func New(code string, httpStatus int, message string) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// WithDetails returns a shallow copy with details attached.
func (e *Error) WithDetails(details any) *Error {
	if e == nil {
		return nil
	}

	cloned := *e
	cloned.Details = details

	return &cloned
}

// WithCause returns a shallow copy with cause attached.
func (e *Error) WithCause(cause error) *Error {
	if e == nil {
		return nil
	}

	cloned := *e
	cloned.Cause = cause

	return &cloned
}
