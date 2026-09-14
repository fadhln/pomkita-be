package relay

import apprelay "github.com/pomkita/pomkita-be/internal/service/relay"

// Store owns relay claim and finish calls in PostgreSQL.
type Store = apprelay.Store

// Worker sends claimed events to an idempotent sink.
type Worker = apprelay.Worker

// NewWorker creates a relay worker.
func NewWorker(store Store, sink Sink) *Worker {
	return apprelay.NewWorker(store, sink)
}

// Deliver claims one event, sends it, and records the result with its lease.
