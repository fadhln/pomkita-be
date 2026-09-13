package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLogSink_DeduplicatesEventID(t *testing.T) {
	var output strings.Builder
	sink := NewLogSink(&output)
	eventID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	event := Event{EventID: eventID, EventType: "test", Payload: []byte(`{"ok":true}`)}
	if err := sink.Send(context.Background(), event); err != nil {
		t.Fatalf("send event: %v", err)
	}
	if err := sink.Send(context.Background(), event); err != nil {
		t.Fatalf("send duplicate event: %v", err)
	}
	if got := strings.Count(output.String(), eventID.String()); got != 1 {
		t.Fatalf("sink entries: got %d, want 1", got)
	}
}
