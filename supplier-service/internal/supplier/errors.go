package supplier

import "errors"

var (
	// ErrNotFound is returned when a supplier does not exist or has been
	// soft-deleted.
	ErrNotFound = errors.New("supplier not found")

	// ErrValidation is returned when required fields are missing or
	// malformed (FR F2.2.1).
	ErrValidation = errors.New("invalid supplier data")

	// ErrDuplicate is returned when a create/update would duplicate the
	// name and campus location of an existing supplier (FR F2.2.2).
	ErrDuplicate = errors.New("supplier with this name and location already exists")
)
