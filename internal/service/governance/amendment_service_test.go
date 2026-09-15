package governance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAmendmentService_Request_RejectsMeterFields(t *testing.T) {
	repository := &amendmentRepositorySpy{}
	service := NewAmendmentService(repository, governanceClock{value: time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)})
	request := validAmendmentRequest()
	request.Items[0].Field = "meter_end"

	if _, err := service.Request(context.Background(), request); err != ErrAmendmentFieldForbidden {
		t.Fatalf("request error: got %v, want %v", err, ErrAmendmentFieldForbidden)
	}
	if repository.requestCalls != 0 {
		t.Fatalf("repository calls: got %d, want 0", repository.requestCalls)
	}
}

type amendmentRepositorySpy struct {
	requestCalls int
}

func (s *amendmentRepositorySpy) RequestAmendment(context.Context, AmendmentRequest, time.Time) (Amendment, error) {
	s.requestCalls++
	return Amendment{}, nil
}

func (s *amendmentRepositorySpy) ApproveAmendment(context.Context, ApproveAmendmentRequest, time.Time) (Amendment, error) {
	return Amendment{}, nil
}

func (s *amendmentRepositorySpy) RejectAmendment(context.Context, RejectAmendmentRequest, time.Time) error {
	return nil
}

func validAmendmentRequest() AmendmentRequest {
	return AmendmentRequest{
		OrgID: uuid.New(), StationID: uuid.New(), ShiftID: uuid.New(), BaseReportID: uuid.New(), RequesterID: uuid.New(), Role: "Supervisor", Reason: "correct cash amount", StaleCheckHash: make([]byte, 32),
		Items: []AmendmentItem{{TargetKind: "sales_declared", TargetLogicalID: uuid.New(), Field: "cash_amount", OldValue: []byte(`"100"`), NewValue: []byte(`"110"`)}},
	}
}
