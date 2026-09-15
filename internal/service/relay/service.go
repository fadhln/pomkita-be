// Package relay contains the outbox delivery use case.
package relay

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Event is immutable event data sent to an external sink.
type Event struct {
	EventID   uuid.UUID `json:"event_id"`
	EventType string    `json:"event_type"`
	Payload   []byte    `json:"payload"`
}

// Sink receives one event and must deduplicate by EventID.
type Sink interface {
	Send(context.Context, Event) error
}

// Store owns the durable claim and completion lease.
type Store interface {
	Claim(context.Context, uuid.UUID, uuid.UUID) (Event, uuid.UUID, bool, error)
	Finish(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string) error
}

// Worker delivers one claimed event to a sink.
type Worker struct {
	store Store
	sink  Sink
}

// NewWorker creates a relay worker.
func NewWorker(store Store, sink Sink) *Worker {
	return &Worker{store: store, sink: sink}
}

// Deliver claims, sends, and completes one event lease.
func (w *Worker) Deliver(ctx context.Context, orgID, eventID uuid.UUID) (bool, error) {
	if w == nil || w.store == nil || w.sink == nil {
		return false, fmt.Errorf("relay dependencies are unavailable")
	}
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
