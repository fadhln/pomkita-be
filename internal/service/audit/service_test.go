package audit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestService_Append_DelegatesWithClock(t *testing.T) {
	repository := &auditRepositorySpy{event: Event{EventID: uuid.New(), OrgSequence: 1}}
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.FixedZone("test", 7*60*60))
	service := NewService(repository, auditClock{value: now})
	result, err := service.Append(context.Background(), AppendRequest{OrgID: uuid.New(), EventID: uuid.New(), EventType: "test", Payload: []byte(`{"ok":true}`), Outcome: "success"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if result.EventID != repository.event.EventID || result.OrgSequence != repository.event.OrgSequence || !repository.now.Equal(now.UTC()) {
		t.Fatalf("repository call: result=%+v now=%v", result, repository.now)
	}
}

func TestService_Append_RejectsIncompleteEvent(t *testing.T) {
	repository := &auditRepositorySpy{}
	service := NewService(repository, auditClock{value: time.Now()})

	if _, err := service.Append(context.Background(), AppendRequest{OrgID: uuid.New()}); err != ErrInvalidRequest {
		t.Fatalf("append error: got %v, want %v", err, ErrInvalidRequest)
	}
	if repository.event.EventID != uuid.Nil {
		t.Fatal("repository was called for an invalid audit event")
	}
}

func TestService_Verify_RejectsMissingOrganization(t *testing.T) {
	repository := &auditRepositorySpy{}
	service := NewService(repository, auditClock{value: time.Now()})

	if err := service.Verify(context.Background(), uuid.Nil); err != ErrInvalidRequest {
		t.Fatalf("verify error: got %v, want %v", err, ErrInvalidRequest)
	}
}

type auditRepositorySpy struct {
	event Event
	now   time.Time
}

func (s *auditRepositorySpy) Append(_ context.Context, _ AppendRequest, now time.Time) (Event, error) {
	s.now = now
	return s.event, nil
}

func (*auditRepositorySpy) Verify(context.Context, uuid.UUID) error { return nil }

type auditClock struct{ value time.Time }

func (c auditClock) Now() time.Time { return c.value }
