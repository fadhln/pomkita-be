// Package relay contains the outbox relay boundary and development sink.
package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
)

// Event is the immutable event data sent to a relay sink.
type Event struct {
	EventID   uuid.UUID       `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

// Sink receives one audit event. A sink must be idempotent by EventID.
type Sink interface {
	Send(context.Context, Event) error
}

// LogSink writes one JSON line for each new event.
type LogSink struct {
	mu        sync.Mutex
	output    io.Writer
	delivered map[uuid.UUID]struct{}
}

// NewLogSink creates a development sink that writes to output.
func NewLogSink(output io.Writer) *LogSink {
	return &LogSink{output: output, delivered: make(map[uuid.UUID]struct{})}
}

// Send writes event once and ignores a repeated event ID.
func (s *LogSink) Send(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.delivered[event.EventID]; ok {
		return nil
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal relay event: %w", err)
	}
	if _, err := fmt.Fprintf(s.output, "%s\n", encoded); err != nil {
		return fmt.Errorf("write relay event: %w", err)
	}
	s.delivered[event.EventID] = struct{}{}
	return nil
}
