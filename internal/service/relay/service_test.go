package relay

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestWorker_DeliverMarksSuccessfulSinkDelivery(t *testing.T) {
	eventID := uuid.New()
	store := &storeSpy{event: Event{EventID: eventID, EventType: "report.submitted", Payload: []byte(`{"report_id":"x"`)}}
	sink := &sinkSpy{}
	worker := NewWorker(store, sink)

	claimed, err := worker.Deliver(context.Background(), uuid.New(), eventID)
	if err != nil || !claimed {
		t.Fatalf("deliver: claimed=%v err=%v", claimed, err)
	}
	if !sink.called || !store.finished || !store.success {
		t.Fatalf("delivery state: sink=%v finished=%v success=%v", sink.called, store.finished, store.success)
	}
}

type storeSpy struct {
	event    Event
	finished bool
	success  bool
}

func (s *storeSpy) Claim(context.Context, uuid.UUID, uuid.UUID) (Event, uuid.UUID, bool, error) {
	return s.event, uuid.New(), true, nil
}

func (s *storeSpy) Finish(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, success bool, _ string) error {
	s.finished = true
	s.success = success
	return nil
}

type sinkSpy struct{ called bool }

func (s *sinkSpy) Send(context.Context, Event) error {
	s.called = true
	return nil
}

type failingSink struct{}

func (failingSink) Send(context.Context, Event) error { return errors.New("sink unavailable") }
