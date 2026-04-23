package tenant

import "errors"

var (
	// ErrNameRequired indicates that tenant names cannot be blank.
	ErrNameRequired = errors.New("tenant name is required")
	// ErrNameTooLong indicates that tenant names cannot exceed 100 characters.
	ErrNameTooLong = errors.New("tenant name is too long")
	// ErrEmailRequired indicates that tenant email is required on create.
	ErrEmailRequired = errors.New("tenant email is required")
	// ErrEmailInvalid indicates that tenant email must be a valid address.
	ErrEmailInvalid = errors.New("tenant email is invalid")
)
