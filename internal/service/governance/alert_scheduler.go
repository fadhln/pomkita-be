package governance

import (
	"context"
	"time"
)

// AlertSchedulerRepository evaluates starvation rules and records alert events.
type AlertSchedulerRepository interface {
	EvaluateStarvation(context.Context, time.Time, time.Duration) (int, error)
}

// AlertScheduler runs the time-based alert evaluation.
type AlertScheduler struct {
	repository AlertSchedulerRepository
	clock      Clock
}

// NewAlertScheduler creates an alert scheduler.
func NewAlertScheduler(repository AlertSchedulerRepository, clock Clock) *AlertScheduler {
	return &AlertScheduler{repository: repository, clock: clock}
}

// Run evaluates starvation rules for shifts older than the required window.
func (s *AlertScheduler) Run(ctx context.Context, window time.Duration) (int, error) {
	if s == nil || s.repository == nil || s.clock == nil {
		return 0, ErrDependencyUnavailable
	}
	if window != 24*time.Hour {
		return 0, ErrAlertInvalidRequest
	}
	return s.repository.EvaluateStarvation(ctx, s.clock.Now().UTC(), window)
}
