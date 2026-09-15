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

func TestDeniedService_RecordsSafeRequestMetadata(t *testing.T) {
	repository := &deniedRepositorySpy{}
	now := time.Date(2026, 1, 2, 14, 30, 0, 0, time.FixedZone("test", 7*60*60))
	service := NewDeniedService(repository, auditClock{value: now})
	subjectID, jti, orgID, stationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	request := DeniedRequest{RequestID: uuid.New(), SubjectID: &subjectID, JTI: &jti, OrgID: &orgID, StationID: &stationID, Action: "read_report", Target: "report", Reason: "station_scope_forbidden", Outcome: "denied"}
	if err := service.Record(context.Background(), request); err != nil {
		t.Fatalf("record denied request: %v", err)
	}
	if repository.request.RequestID != request.RequestID || !repository.now.Equal(now.UTC()) || repository.request.Reason != request.Reason {
		t.Fatalf("denied repository call: request=%+v now=%v", repository.request, repository.now)
	}
}

func TestDeniedService_RejectsIncompleteRequest(t *testing.T) {
	repository := &deniedRepositorySpy{}
	service := NewDeniedService(repository, auditClock{value: time.Now()})
	if err := service.Record(context.Background(), DeniedRequest{Action: "read_report"}); err != ErrInvalidDeniedRequest {
		t.Fatalf("record denied error: got %v, want %v", err, ErrInvalidDeniedRequest)
	}
	if repository.request.RequestID != uuid.Nil {
		t.Fatal("repository was called for an invalid denied request")
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

type deniedRepositorySpy struct {
	request DeniedRequest
	now     time.Time
}

func (s *deniedRepositorySpy) RecordDenied(_ context.Context, request DeniedRequest, now time.Time) error {
	s.request = request
	s.now = now
	return nil
}

type auditClock struct{ value time.Time }

func (c auditClock) Now() time.Time { return c.value }
