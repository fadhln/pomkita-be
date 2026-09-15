// Package domain contains shared business types and errors.
package domain

// ErrorCategory identifies the class of a domain error.
type ErrorCategory string

const (
	// CategoryAuthentication identifies authentication failures.
	CategoryAuthentication ErrorCategory = "authentication"
	// CategoryDependency identifies a missing or unavailable application dependency.
	CategoryDependency ErrorCategory = "dependency"
	// CategoryValidation identifies an invalid application request.
	CategoryValidation ErrorCategory = "validation"
	// CategoryConflict identifies a stale or conflicting state change.
	CategoryConflict ErrorCategory = "conflict"
	// CategoryNotFound identifies a resource outside the permitted scope.
	CategoryNotFound ErrorCategory = "not_found"
	// CategoryAuthorization identifies an authenticated actor without authority.
	CategoryAuthorization ErrorCategory = "authorization"
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
