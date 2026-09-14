package recovery

import (
	"context"
	"testing"
	"time"
)

func TestService_RecoverStale_ReopensDraftsWithoutReports(t *testing.T) {
	repository := &repositoryStub{recovered: 1}
	service := NewService(repository)
	count, err := service.RecoverStale(context.Background(), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), 10*time.Minute)
	if err != nil {
		t.Fatalf("recover stale: %v", err)
	}
	if count != 1 || repository.maxAge != 10*time.Minute {
		t.Fatalf("recovery: count=%d max_age=%s", count, repository.maxAge)
	}
}

type repositoryStub struct {
	recovered int
	maxAge    time.Duration
}

func (r *repositoryStub) RecoverStale(_ context.Context, _ time.Time, maxAge time.Duration) (int, error) {
	r.maxAge = maxAge
	return r.recovered, nil
}
