// Package domain contains shared business types and errors.
package domain

// ErrorCategory identifies the class of a domain error.
type ErrorCategory string

const (
	// CategoryAuthentication identifies authentication failures.
	CategoryAuthentication ErrorCategory = "authentication"
	// CategoryDependency identifies a missing or unavailable application dependency.
	CategoryDependency ErrorCategory = "dependency"
)

// Error is a stable domain error with a safe machine code.
type Error struct {
	Category ErrorCategory
	Code     string
}

// Error returns the stable machine code.
func (e *Error) Error() string { return e.Code }

// NewError creates a stable domain error.
func NewError(category ErrorCategory, code string) *Error {
	return &Error{Category: category, Code: code}
}
