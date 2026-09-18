package endpoint

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalid reports that an endpoint value violates the supported domain.
	ErrInvalid = errors.New("invalid endpoint data")
	// ErrConflict reports that one connection identity maps to incompatible data.
	ErrConflict = errors.New("endpoint identity conflict")
)

// ValidationError identifies an invalid field without retaining or formatting its
// potentially sensitive input value.
type ValidationError struct {
	field   string
	problem string
}

func invalid(field, problem string) error {
	return &ValidationError{field: field, problem: problem}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid endpoint field %q: %s", e.field, e.problem)
}

func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Field returns the stable field name that failed validation.
func (e *ValidationError) Field() string { return e.field }
