// Package recovery contains stale draft recovery use cases.
package recovery

import (
	"context"
	"time"

	"github.com/fadhln/pomkita-be/internal/domain"
)

var (
	// ErrInvalidRequest identifies an invalid recovery window.
	ErrInvalidRequest = domain.NewError(domain.CategoryValidation, "invalid_recovery_request")
	// ErrDependencyUnavailable identifies a missing recovery dependency.
	ErrDependencyUnavailable = domain.NewError(domain.CategoryDependency, "dependency_unavailable")
)

// Repository recovers stale submitting drafts.
type Repository interface {
	RecoverStale(context.Context, time.Time, time.Duration) (int, error)
}

// Service owns stale draft recovery.
type Service struct {
	repository Repository
}

// NewService creates a recovery service.
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// RecoverStale recovers drafts that have exceeded the submission lease window.
func (s *Service) RecoverStale(ctx context.Context, now time.Time, maxAge time.Duration) (int, error) {
	if s == nil || s.repository == nil {
		return 0, ErrDependencyUnavailable
	}
	if now.IsZero() || maxAge <= 0 {
		return 0, ErrInvalidRequest
	}
	return s.repository.RecoverStale(ctx, now.UTC(), maxAge)
}
