package audit

import "github.com/fadhln/pomkita-be/internal/domain"

var (
	// ErrDependencyUnavailable identifies a missing audit dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
	// ErrInvalidRequest identifies an invalid audit event.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_audit_event")
	// ErrInvalidDeniedRequest identifies incomplete denied request metadata.
	ErrInvalidDeniedRequest = domain.NewError(domain.CategoryValidation, "invalid_denied_audit_request")
	// ErrChainTampered identifies a broken audit hash chain.
	ErrChainTampered = domain.NewError(domain.CategoryConflict, "audit_chain_tampered")
)
