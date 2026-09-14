package governance

import (
	"context"
	"testing"
	"time"
)

func TestAlertScheduler_Run_RequiresTheTwentyFourHourWindow(t *testing.T) {
	repository := &alertSchedulerRepositorySpy{}
	scheduler := NewAlertScheduler(repository, governanceClock{value: time.Date(2026, 1, 3, 14, 30, 0, 0, time.UTC)})

	if _, err := scheduler.Run(context.Background(), 23*time.Hour); err != ErrAlertInvalidRequest {
		t.Fatalf("scheduler error: got %v, want %v", err, ErrAlertInvalidRequest)
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls: got %d, want 0", repository.calls)
	}
}

type alertSchedulerRepositorySpy struct {
	calls int
}

func (s *alertSchedulerRepositorySpy) EvaluateStarvation(context.Context, time.Time, time.Duration) (int, error) {
	s.calls++
	return 1, nil
}
