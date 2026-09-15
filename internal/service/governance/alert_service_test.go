package governance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAlertService_Record_RejectsMissingReportVersion(t *testing.T) {
	repository := &alertRepositorySpy{}
	service := NewAlertService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := validAlertRequest()
	request.SourceKind = "report"

	if _, err := service.Record(context.Background(), request); err != ErrAlertSourceVersionRequired {
		t.Fatalf("record error: got %v, want %v", err, ErrAlertSourceVersionRequired)
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls: got %d, want 0", repository.calls)
	}
}

type alertRepositorySpy struct {
	calls int
}

func (s *alertRepositorySpy) RecordAlertOccurrence(context.Context, AlertOccurrenceRequest, time.Time) (AlertEvent, error) {
	s.calls++
	return AlertEvent{}, nil
}

func validAlertRequest() AlertOccurrenceRequest {
	return AlertOccurrenceRequest{OrgID: uuid.New(), StationID: uuid.New(), RuleID: uuid.New(), SubjectKind: "shift", SubjectID: uuid.New(), EventType: "fired", PeriodStart: time.Date(2026, 1, 2, 14, 0, 0, 0, time.UTC), SourceKind: "scheduler", SourceID: uuid.New(), SourceAt: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)}
}
