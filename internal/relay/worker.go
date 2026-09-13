package relay

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Store owns relay claim and finish calls in PostgreSQL.
type Store interface {
	Claim(context.Context, uuid.UUID, uuid.UUID) (Event, uuid.UUID, bool, error)
	Finish(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string) error
}

// Worker sends claimed events to an idempotent sink.
type Worker struct {
	store Store
	sink  Sink
}

// NewWorker creates a relay worker.
func NewWorker(store Store, sink Sink) *Worker {
	return &Worker{store: store, sink: sink}
}

// Deliver claims one event, sends it, and records the result with its lease.
func (w *Worker) Deliver(ctx context.Context, orgID, eventID uuid.UUID) (bool, error) {
	event, lease, claimed, err := w.store.Claim(ctx, orgID, eventID)
	if err != nil || !claimed {
		return false, err
	}
	if err := w.sink.Send(ctx, event); err != nil {
		if finishErr := w.store.Finish(ctx, orgID, eventID, lease, false, err.Error()); finishErr != nil {
			return true, fmt.Errorf("finish failed relay event: %w", finishErr)
		}
		return true, err
	}
	if err := w.store.Finish(ctx, orgID, eventID, lease, true, ""); err != nil {
		return true, fmt.Errorf("finish delivered relay event: %w", err)
	}
	return true, nil
}
